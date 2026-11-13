package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

const (
	inboxDefaultChats    = 60
	inboxDefaultMessages = 50
	inboxDefaultSearch   = 50
	// historyThrottle: tiempo mínimo entre dos peticiones de historial del mismo chat.
	historyThrottle = 5 * time.Second
	// historyCount: cuántos mensajes viejos se piden de una vez.
	historyCount = 50
	// oldestScanPages: tope de páginas que se recorren para hallar el mensaje más antiguo.
	oldestScanPages = 25
)

// Inbox: bandeja unificada y búsqueda global (06 §4).
type Inbox struct {
	acc *Accounts

	hmu     sync.Mutex
	history map[model.ChatKey]time.Time // última petición de historial por chat
	now     func() time.Time
	// bgWG cuenta las peticiones de historial en segundo plano (para las pruebas).
	bgWG sync.WaitGroup
}

func NewInbox(acc *Accounts) *Inbox {
	return &Inbox{acc: acc, history: map[model.ChatKey]time.Time{}, now: time.Now}
}

func inboxInvalidKey(what string) error {
	return fmt.Errorf("%w: %s", port.ErrInvalid, what)
}

// chatTarget resuelve una clave de chat a su instancia.
func (a *Accounts) chatTarget(key model.ChatKey) (*instance, string, error) {
	accID, native := key.Split()
	if accID == "" || native == "" {
		return nil, "", inboxInvalidKey("clave de chat no válida")
	}
	inst, err := a.mustGet(accID)
	if err != nil {
		return nil, "", err
	}
	return inst, native, nil
}

// ListChats mezcla los chats de las cuentas filtradas por «fijados primero, más reciente primero».
// Pide f.Limit a cada cuenta y corta al final; la paginación usa f.Before.
func (i *Inbox) ListChats(ctx context.Context, f model.ChatFilter) ([]model.Chat, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = inboxDefaultChats
	}
	wantAcc := map[model.AccountID]bool{}
	for _, id := range f.AccountIDs {
		wantAcc[id] = true
	}
	wantCh := map[model.Channel]bool{}
	for _, c := range f.Channels {
		wantCh[c] = true
	}
	per := f
	per.Limit = limit
	per.AccountIDs, per.Channels = nil, nil

	var all []model.Chat
	for _, inst := range i.acc.snapshot() {
		if len(wantAcc) > 0 && !wantAcc[inst.acc.ID] {
			continue
		}
		if len(wantCh) > 0 && !wantCh[inst.acc.Channel] {
			continue
		}
		chats, err := inst.store.ListChats(ctx, per)
		if err != nil {
			return nil, fmt.Errorf("bandeja: listar chats de %s: %w", inst.acc.ID, err)
		}
		all = append(all, chats...)
	}
	sort.SliceStable(all, func(x, y int) bool {
		if all[x].Pinned != all[y].Pinned {
			return all[x].Pinned
		}
		if all[x].LastMessageAt != all[y].LastMessageAt {
			return all[x].LastMessageAt > all[y].LastMessageAt
		}
		return all[x].Key < all[y].Key
	})
	if len(all) > limit {
		all = all[:limit]
	}
	if all == nil {
		all = []model.Chat{}
	}
	return all, nil
}

func (i *Inbox) GetChat(ctx context.Context, key model.ChatKey) (model.Chat, error) {
	inst, native, err := i.acc.chatTarget(key)
	if err != nil {
		return model.Chat{}, err
	}
	c, err := inst.store.GetChat(ctx, native)
	if err != nil {
		return model.Chat{}, err
	}
	c.AccountID, c.Channel, c.Key = inst.acc.ID, inst.acc.Channel, key
	return c, nil
}

// Messages devuelve una página local. Si viene con menos de `limit` y el canal tiene
// HistoryOnDemand, además pide historial más antiguo en segundo plano (LoadOlder).
func (i *Inbox) Messages(ctx context.Context, key model.ChatKey, beforeTs int64, limit int) (model.MessagePage, error) {
	inst, native, err := i.acc.chatTarget(key)
	if err != nil {
		return model.MessagePage{}, err
	}
	if limit <= 0 {
		limit = inboxDefaultMessages
	}
	page, err := inst.store.ListMessages(ctx, native, beforeTs, limit)
	if err != nil {
		return model.MessagePage{}, err
	}
	if page.Messages == nil {
		page.Messages = []model.Message{}
	}
	if len(page.Messages) < limit {
		chat, cerr := inst.store.GetChat(ctx, native)
		var cp *model.Chat
		if cerr == nil {
			cp = &chat
		}
		if inst.getAdapter().Capabilities(cp).HistoryOnDemand && i.allowHistory(key) {
			i.bgWG.Add(1)
			go func() {
				defer i.bgWG.Done()
				defer func() {
					if rec := recover(); rec != nil {
						slog.Error("pánico al pedir historial", "panic", rec)
					}
				}()
				bctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if err := i.LoadOlder(bctx, key); err != nil && !errors.Is(err, port.ErrUnsupported) {
					slog.Debug("no se pudo pedir historial", "accountId", string(inst.acc.ID), "err", err)
				}
			}()
		}
	}
	return page, nil
}

// allowHistory limita a una petición cada historyThrottle por chat.
func (i *Inbox) allowHistory(key model.ChatKey) bool {
	i.hmu.Lock()
	defer i.hmu.Unlock()
	now := i.now()
	if last, ok := i.history[key]; ok && now.Sub(last) < historyThrottle {
		return false
	}
	i.history[key] = now
	return true
}

// Search busca mensajes en las cuentas indicadas (todas si no se indica ninguna).
func (i *Inbox) Search(ctx context.Context, query string, accountIDs []model.AccountID, limit int) ([]model.Message, error) {
	if limit <= 0 {
		limit = inboxDefaultSearch
	}
	want := map[model.AccountID]bool{}
	for _, id := range accountIDs {
		want[id] = true
	}
	out := []model.Message{}
	for _, inst := range i.acc.snapshot() {
		if len(want) > 0 && !want[inst.acc.ID] {
			continue
		}
		ms, err := inst.store.SearchMessages(ctx, query, "", limit)
		if err != nil {
			return nil, fmt.Errorf("bandeja: buscar en %s: %w", inst.acc.ID, err)
		}
		out = append(out, ms...)
	}
	sort.SliceStable(out, func(x, y int) bool { return out[x].Timestamp > out[y].Timestamp })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// UnreadTotals: no leídos por cuenta.
func (i *Inbox) UnreadTotals(ctx context.Context) (map[model.AccountID]int, error) {
	out := map[model.AccountID]int{}
	for _, inst := range i.acc.snapshot() {
		out[inst.acc.ID] = unreadOf(ctx, inst.store)
	}
	return out, nil
}

// LoadOlder pide al adaptador historial anterior al mensaje más antiguo que hay en local.
func (i *Inbox) LoadOlder(ctx context.Context, key model.ChatKey) error {
	inst, native, err := i.acc.chatTarget(key)
	if err != nil {
		return err
	}
	var before *model.MessageRef
	if oldest, ok := oldestMessage(ctx, inst.store, native); ok {
		ref := oldest.Ref()
		before = &ref
	}
	return inst.getAdapter().RequestHistory(ctx, native, before, historyCount)
}

// oldestMessage recorre las páginas locales hacia atrás hasta dar con el mensaje más antiguo
// (con tope de oldestScanPages páginas; si no llega, devuelve el más antiguo visto).
func oldestMessage(ctx context.Context, st port.AccountStore, native string) (model.Message, bool) {
	const pageSize = 200
	var oldest model.Message
	found := false
	beforeTs := int64(0)
	for p := 0; p < oldestScanPages; p++ {
		page, err := st.ListMessages(ctx, native, beforeTs, pageSize)
		if err != nil || len(page.Messages) == 0 {
			break
		}
		oldest, found = page.Messages[0], true
		if len(page.Messages) < pageSize {
			break
		}
		beforeTs = oldest.Timestamp
	}
	return oldest, found
}

// chatAction aplica Archive/Pin/Mute: primero el adaptador; si no lo admite (ErrUnsupported)
// se aplica solo en local. Siempre se publica chat.updated.
func (i *Inbox) chatAction(ctx context.Context, key model.ChatKey, field string,
	remote func(ad port.ChannelAdapter, native string) error, apply func(c *model.Chat)) error {
	inst, native, err := i.acc.chatTarget(key)
	if err != nil {
		return err
	}
	chat, err := inst.store.GetChat(ctx, native)
	if err != nil {
		return err
	}
	if err := remote(inst.getAdapter(), native); err != nil && !errors.Is(err, port.ErrUnsupported) {
		return err
	}
	apply(&chat)
	if err := inst.store.PatchChat(ctx, chat, []string{field}); err != nil {
		return err
	}
	fresh, err := inst.store.GetChat(ctx, native)
	if err != nil {
		fresh = chat
	}
	fresh.AccountID, fresh.Channel, fresh.Key = inst.acc.ID, inst.acc.Channel, key
	i.acc.router.Publish(model.Event{
		Type: model.EvChatUpsert, AccountID: inst.acc.ID, Channel: inst.acc.Channel,
		Chat: &fresh, ChatKey: key,
	})
	return nil
}

func (i *Inbox) Archive(ctx context.Context, key model.ChatKey, v bool) error {
	return i.chatAction(ctx, key, "archived",
		func(ad port.ChannelAdapter, native string) error { return ad.ArchiveChat(ctx, native, v) },
		func(c *model.Chat) { c.Archived = v })
}

func (i *Inbox) Pin(ctx context.Context, key model.ChatKey, v bool) error {
	return i.chatAction(ctx, key, "pinned",
		func(ad port.ChannelAdapter, native string) error { return ad.PinChat(ctx, native, v) },
		func(c *model.Chat) { c.Pinned = v })
}

func (i *Inbox) Mute(ctx context.Context, key model.ChatKey, until int64) error {
	return i.chatAction(ctx, key, "mutedUntil",
		func(ad port.ChannelAdapter, native string) error { return ad.MuteChat(ctx, native, until) },
		func(c *model.Chat) { c.MutedUntil = until })
}

// ---------------------------------------------------------------------------------------------
// Nuevo chat

// peerResolver es una interfaz OPCIONAL de un adaptador (se detecta con una aserción de tipo; no
// forma parte de port.ChannelAdapter): convierte lo que escribió el usuario (un número de teléfono
// con o sin «+», o un id nativo) en el id nativo de la persona y, si lo sabe, su nombre. Si la
// persona no existe en el canal devuelve port.ErrNotFound («ese número no está en WhatsApp»).
type peerResolver interface {
	ResolvePeer(ctx context.Context, input string) (nativeID, name string, err error)
}

// OpenChat devuelve el chat directo con `peerNativeID` de la cuenta, aunque esté vacío, o lo crea
// vacío (tipo directo, LastMessageAt 0, título del contacto o de la persona) sin enviar nada. Un
// chat sin actividad no sale en la lista de chats (ChatFilter.IncludeEmpty) pero se puede abrir y
// escribir: con el primer mensaje el chat pasa a tener actividad y el almacén lo sube a la lista
// (chat.updated). `peerNativeID` puede ser el id nativo (el de la identidad del contacto) o, si el
// canal sabe resolverlo (WhatsApp), un número de teléfono que no está en la libreta.
func (i *Inbox) OpenChat(ctx context.Context, accountID model.AccountID, peerNativeID string) (model.Chat, error) {
	peer := strings.TrimSpace(peerNativeID)
	if peer == "" {
		return model.Chat{}, inboxInvalidKey("indica a quién quieres escribir")
	}
	inst, err := i.acc.mustGet(accountID)
	if err != nil {
		return model.Chat{}, fmt.Errorf("%w: la cuenta ya no existe", port.ErrNotFound)
	}
	inst.mu.Lock()
	acc := inst.acc
	inst.mu.Unlock()
	label := acc.Label
	if label == "" {
		label = string(acc.ID)
	}
	if !acc.Enabled {
		return model.Chat{}, fmt.Errorf("%w: la cuenta «%s» está deshabilitada; actívala para escribir", port.ErrInvalid, label)
	}

	// 1. El chat ya existe (aunque esté vacío): se devuelve tal cual, sin consultar al canal.
	if c, ok := i.existingChat(ctx, inst, peer); ok {
		return c, nil
	}

	// 2. Hay que crearlo: el canal debe permitir iniciar conversaciones y la cuenta estar conectada.
	ad := inst.getAdapter()
	caps := ad.Capabilities(nil)
	if !caps.StartChat {
		return model.Chat{}, fmt.Errorf("%w: %s no permite iniciar una conversación nueva; solo puedes responder a quien ya te escribió",
			port.ErrUnsupported, channelLabel(acc.Channel))
	}
	if !caps.SendText {
		return model.Chat{}, fmt.Errorf("%w: la cuenta «%s» no puede enviar mensajes", port.ErrUnsupported, label)
	}
	if st := inst.getStatus().State; st != model.StateConnected {
		return model.Chat{}, fmt.Errorf("%w: conecta la cuenta «%s» para escribir a alguien nuevo", port.ErrNotConnected, label)
	}

	// 3. Resolver el interlocutor (número → id nativo) si el canal sabe hacerlo.
	native, name := peer, ""
	if r, ok := ad.(peerResolver); ok {
		n, nm, rerr := r.ResolvePeer(ctx, peer)
		if rerr != nil {
			return model.Chat{}, rerr
		}
		if n == "" {
			return model.Chat{}, fmt.Errorf("%w: no se pudo identificar a esa persona", port.ErrNotFound)
		}
		native, name = n, nm
		if native != peer {
			// El número resuelto puede corresponder a un chat que ya existe.
			if c, ok := i.existingChat(ctx, inst, native); ok {
				return c, nil
			}
		}
	}

	// 4. Crearlo vacío, con el título del contacto o de la persona.
	chat := model.Chat{
		NativeID: native, Kind: model.ChatDirect, PeerNativeID: native, UnreadCount: 0,
	}
	if native == acc.SelfID && native != "" {
		chat.Kind = model.ChatSelf
	}
	contact, cerr := i.acc.app.FindContactByIdentity(ctx, acc.ID, native)
	if cerr == nil {
		chat.ContactID = contact.ID
	}
	chat.Title = i.peerTitle(ctx, inst, chat.Kind, native, name, contact.Name)
	if err := inst.store.UpsertChat(ctx, chat); err != nil {
		return model.Chat{}, fmt.Errorf("bandeja: no se pudo crear el chat: %w", err)
	}
	saved, err := inst.store.GetChat(ctx, native)
	if err != nil {
		saved = chat
	}
	saved.AccountID, saved.Channel, saved.Key = acc.ID, acc.Channel, model.NewChatKey(acc.ID, native)
	saved.ContactID = chat.ContactID
	return saved, nil
}

// existingChat devuelve el chat guardado con ese id nativo (con su contacto resuelto).
func (i *Inbox) existingChat(ctx context.Context, inst *instance, native string) (model.Chat, bool) {
	c, err := inst.store.GetChat(ctx, native)
	if err != nil {
		return model.Chat{}, false
	}
	c.AccountID, c.Channel, c.Key = inst.acc.ID, inst.acc.Channel, model.NewChatKey(inst.acc.ID, native)
	if c.Kind == model.ChatDirect || c.Kind == model.ChatSelf {
		peer := c.PeerNativeID
		if peer == "" {
			peer = native
		}
		if ct, err := i.acc.app.FindContactByIdentity(ctx, inst.acc.ID, peer); err == nil {
			c.ContactID = ct.ID
		}
	}
	return c, true
}

// peerTitle elige el título de un chat nuevo: nombre del contacto, nombre de la persona en la
// cuenta, nombre que dio el canal, teléfono y, por último, el id nativo.
func (i *Inbox) peerTitle(ctx context.Context, inst *instance, kind model.ChatKind, native, resolved, contactName string) string {
	if kind == model.ChatSelf {
		return "Tú"
	}
	if strings.TrimSpace(contactName) != "" {
		return contactName
	}
	if p, err := inst.store.GetPerson(ctx, native); err == nil && p.Display() != p.NativeID {
		return p.Display()
	}
	if strings.TrimSpace(resolved) != "" {
		return resolved
	}
	return native
}

func channelLabel(ch model.Channel) string {
	switch ch {
	case model.ChannelWhatsApp:
		return "WhatsApp"
	case model.ChannelTelegram:
		return "Telegram"
	case model.ChannelMessenger:
		return "Messenger"
	case model.ChannelInstagram:
		return "Instagram"
	}
	return string(ch)
}
