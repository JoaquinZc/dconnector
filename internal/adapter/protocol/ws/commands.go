package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// onCommand recibe un comando del servidor (10 §5). Sin «Permitir comandos» todo recibe `forbidden`.
// Los trozos de subida se procesan aquí mismo (en orden); el resto, en su propia goroutine.
func (s *session) onCommand(f frameIn) {
	if !s.c.dst.AllowCommands {
		s.logCommand(f, "", false, codeForbidden)
		s.replyError(f, errForbidden, "este destino no tiene activado «Permitir comandos»")
		return
	}
	if f.Name == nameMediaChunk {
		s.handleChunk(f)
		return
	}
	select {
	case s.sem <- struct{}{}:
	default:
		s.replyError(f, port.ErrRateLimited, "hay demasiados comandos en curso")
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() { <-s.sem }()
		s.runCommand(f)
	}()
}

// runCommand ejecuta un comando con su tope de tiempo, responde con `result` y lo deja en el
// registro (destino, nombre, cuenta, ok/error; nunca el contenido).
func (s *session) runCommand(f frameIn) {
	ctx, cancel := context.WithTimeout(s.ctx, commandTimeout)
	defer cancel()
	var (
		result any
		err    error
	)
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				s.c.log.Error("pánico en un comando", "name", f.Name, "panic", rec)
				err = fmt.Errorf("pánico: %v", rec)
			}
		}()
		result, err = s.exec(ctx, f)
	}()
	accID := ""
	if f.Account != nil {
		accID = f.Account.ID
	}
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || (ctx.Err() != nil && s.ctx.Err() == nil) {
			err = errors.New("Tiempo agotado")
		}
		s.logCommand(f, accID, false, errorCode(err))
		s.replyError(f, err, "")
		return
	}
	s.logCommand(f, accID, true, "")
	if err := s.replyOK(f, result); err != nil {
		return
	}
	if a, ok := result.(afterResult); ok {
		if err := a.after(ctx, s); err != nil {
			s.c.log.Warn("no se pudo terminar el envío del archivo", "name", f.Name)
			s.fail(fmt.Errorf("no se pudo enviar el archivo: %s", rootCause(err)))
		}
	}
}

// afterResult: resultados que mandan más frames después del `result` (media.get).
type afterResult interface {
	after(ctx context.Context, s *session) error
}

func (s *session) logCommand(f frameIn, acc string, ok bool, code string) {
	attrs := []any{"name", f.Name, "accountId", acc, "ok", ok}
	if code != "" {
		attrs = append(attrs, "code", code)
	}
	s.c.log.Info("comando del protocolo", attrs...)
}

func (s *session) replyOK(f frameIn, result any) error {
	if result == nil {
		result = empty()
	}
	out := frameOut{
		V: protocolVersion, ID: f.ID, Kind: kindResult, Name: f.Name, TS: nowMs(),
		Data: resultData{OK: true, Result: result},
	}
	err := s.writeFrame(out, writeTimeout)
	if errors.Is(err, errFrameTooBig) {
		// La respuesta no cabe: se avisa en vez de callar.
		return s.replyError(f, invalidf("la respuesta supera 1 MiB; pide menos elementos"), "")
	}
	return err
}

// replyError responde con el error traducido a su código. Si msg no está vacío, sustituye al texto.
func (s *session) replyError(f frameIn, err error, msg string) error {
	we := toWireError(err)
	if msg != "" {
		we.Message = msg
	}
	out := frameOut{
		V: protocolVersion, ID: f.ID, Kind: kindResult, Name: f.Name, TS: nowMs(),
		Data: resultData{OK: false, Error: we},
	}
	return s.writeFrame(out, writeTimeout)
}

// ---------------------------------------------------------------------------------------------
// Despacho

// exec resuelve el comando con los servicios del núcleo. No tiene lógica propia: decodifica,
// comprueba cuenta y filtro, llama al servicio y devuelve su resultado.
func (s *session) exec(ctx context.Context, f frameIn) (any, error) {
	switch f.Name {
	case "accounts.list":
		return s.cmdAccountsList(ctx)
	case "chats.list":
		return s.cmdChatsList(ctx, f)
	case "messages.list":
		return s.cmdMessagesList(ctx, f)
	case "message.send":
		return s.cmdSend(ctx, f, false)
	case "message.reply":
		return s.cmdSend(ctx, f, true)
	case "message.react":
		return s.cmdReact(ctx, f)
	case "message.edit":
		return s.cmdEdit(ctx, f)
	case "message.delete":
		return s.cmdDelete(ctx, f)
	case "message.forward":
		return s.cmdForward(ctx, f)
	case "poll.vote":
		return s.cmdPollVote(ctx, f)
	case "chat.mark_read":
		return s.cmdMarkRead(ctx, f)
	case "chat.typing":
		return s.cmdTyping(ctx, f)
	case "status.post":
		return s.cmdStatusPost(ctx, f)
	case "media.get":
		return s.cmdMediaGet(ctx, f)
	case "media.upload":
		return s.cmdMediaUpload(ctx, f)
	case "contacts.find":
		return s.cmdContactsFind(ctx, f)
	}
	return nil, invalidf("comando desconocido")
}

func decodeData(f frameIn, v any) error {
	if len(f.Data) == 0 || string(f.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(f.Data, v); err != nil {
		return invalidf("los datos del comando no son válidos")
	}
	return nil
}

// accountFor resuelve la cuenta del comando (account.id o la que lleva la clave del chat) y
// comprueba que pase el filtro del destino.
func (s *session) accountFor(ctx context.Context, f frameIn, hint model.AccountID) (model.AccountID, error) {
	var id model.AccountID
	if f.Account != nil {
		id = model.AccountID(f.Account.ID)
	}
	if id == "" {
		id = hint
	} else if hint != "" && hint != id {
		return "", invalidf("el chat no es de la cuenta indicada")
	}
	if id == "" {
		return "", invalidf("falta account.id")
	}
	acc, err := s.m.account(ctx, id)
	if err != nil {
		return "", err
	}
	if !filterAllowsAccount(s.c.dst.Filter, acc.ID, acc.Channel) {
		return "", fmt.Errorf("%w: la cuenta no pasa el filtro de este destino", errForbidden)
	}
	return acc.ID, nil
}

// chatRef resuelve un chatId (id nativo, o la clave "<cuenta>|<id>") junto con su cuenta.
func (s *session) chatRef(ctx context.Context, f frameIn, chatID string) (model.AccountID, string, error) {
	if chatID == "" {
		return "", "", invalidf("falta chatId")
	}
	var hint model.AccountID
	native := chatID
	if strings.Contains(chatID, "|") {
		a, n := model.ChatKey(chatID).Split()
		hint, native = a, n
		if native == "" {
			return "", "", invalidf("chatId no válido")
		}
	}
	acc, err := s.accountFor(ctx, f, hint)
	if err != nil {
		return "", "", err
	}
	if err := s.checkChatScope(ctx, acc, native); err != nil {
		return "", "", err
	}
	return acc, native, nil
}

func requireText(name, v string) error {
	if strings.TrimSpace(v) == "" {
		return invalidf("falta %s", name)
	}
	return nil
}

// ---------------------------------------------------------------------------------------------
// Comandos

type accountItem struct {
	Account      model.Account       `json:"account"`
	Status       model.AccountStatus `json:"status"`
	Capabilities any                 `json:"capabilities"`
}

func (s *session) cmdAccountsList(ctx context.Context) (any, error) {
	accs, err := s.m.allowedAccounts(ctx, s.c.dst)
	if err != nil {
		return nil, err
	}
	out := make([]accountItem, 0, len(accs))
	for _, a := range accs {
		a.Config = nil // lleva credenciales (p. ej. api_hash de Telegram): no sale
		st := s.m.svc.Accounts.Status(a.ID)
		st.QRCode, st.PairCode = "", ""
		it := accountItem{Account: a, Status: st}
		if caps, err := s.m.svc.Accounts.Capabilities(ctx, a.ID, ""); err == nil {
			it.Capabilities = caps
		}
		out = append(out, it)
	}
	return out, nil
}

func (s *session) cmdChatsList(ctx context.Context, f frameIn) (any, error) {
	var flt model.ChatFilter
	if err := decodeData(f, &flt); err != nil {
		return nil, err
	}
	if f.Account != nil && f.Account.ID != "" {
		acc, err := s.accountFor(ctx, f, "")
		if err != nil {
			return nil, err
		}
		flt.AccountIDs = []model.AccountID{acc}
	} else {
		accs, err := s.m.allowedAccounts(ctx, s.c.dst)
		if err != nil {
			return nil, err
		}
		if len(accs) == 0 {
			return []model.Chat{}, nil
		}
		flt.AccountIDs = flt.AccountIDs[:0:0]
		for _, a := range accs {
			flt.AccountIDs = append(flt.AccountIDs, a.ID)
		}
	}
	if flt.Limit <= 0 || flt.Limit > 200 {
		flt.Limit = 60
	}
	chats, err := s.m.svc.Inbox.ListChats(ctx, flt)
	if err != nil {
		return nil, err
	}
	if !s.c.dst.Filter.IncludeGroups {
		kept := chats[:0]
		for _, c := range chats {
			if c.Kind != model.ChatGroup && c.Kind != model.ChatChannel {
				kept = append(kept, c)
			}
		}
		chats = kept
	}
	if cs := s.c.dst.Filter.ContactScope; cs.Restricts() {
		kept := chats[:0]
		for i := range chats {
			c := chats[i]
			ids, err := s.m.chatContactLists(ctx, c.AccountID, c.NativeID, &c)
			if err != nil {
				return nil, fmt.Errorf("no se pudo comprobar el filtro de contactos: %w", err)
			}
			if cs.Allows(ids) {
				kept = append(kept, c)
			}
		}
		chats = kept
	}
	return chats, nil
}

func (s *session) cmdMessagesList(ctx context.Context, f frameIn) (any, error) {
	var d struct {
		ChatID   string `json:"chatId"`
		BeforeTs int64  `json:"beforeTs"`
		Limit    int    `json:"limit"`
	}
	if err := decodeData(f, &d); err != nil {
		return nil, err
	}
	acc, native, err := s.chatRef(ctx, f, d.ChatID)
	if err != nil {
		return nil, err
	}
	if d.Limit <= 0 || d.Limit > 200 {
		d.Limit = 50
	}
	return s.m.svc.Inbox.Messages(ctx, model.NewChatKey(acc, native), d.BeforeTs, d.Limit)
}

// outgoingIn: data.message de un envío. El archivo llega por fileUrl o uploadId; filePath nunca
// se acepta por el protocolo (10 §5).
type outgoingIn struct {
	model.Outgoing
	FileURL  string `json:"fileUrl"`
	UploadID string `json:"uploadId"`
}

func (s *session) cmdSend(ctx context.Context, f frameIn, reply bool) (any, error) {
	var d struct {
		ChatID    string     `json:"chatId"`
		ReplyTo   string     `json:"replyTo"`
		Message   outgoingIn `json:"message"`
		ClientRef string     `json:"clientRef"`
	}
	if err := decodeData(f, &d); err != nil {
		return nil, err
	}
	acc, native, err := s.chatRef(ctx, f, d.ChatID)
	if err != nil {
		return nil, err
	}
	out := d.Message.Outgoing
	out.FilePath = ""
	if d.ClientRef != "" {
		out.ClientRef = d.ClientRef
	}
	if reply {
		if err := requireText("replyTo", d.ReplyTo); err != nil {
			return nil, err
		}
		st, err := s.m.svc.Accounts.Store(acc)
		if err != nil {
			return nil, err
		}
		orig, err := st.GetMessage(ctx, native, d.ReplyTo)
		if err != nil {
			return nil, err
		}
		ref := orig.Ref()
		out.ReplyTo = &ref
	}
	path, cleanup, err := s.m.resolveFile(ctx, s.c.dst.ID, acc, native, out.Kind, d.Message.FileURL, d.Message.UploadID, &out.FileName, &out.Mime)
	if err != nil {
		return nil, err
	}
	if cleanup != nil {
		defer cleanup()
	}
	out.FilePath = path

	key := model.NewChatKey(acc, native)
	var ticket *sendTicket
	if out.ClientRef != "" {
		if ticket, err = s.m.beginSend(ctx, key, out.ClientRef); err != nil {
			return nil, err
		}
	}
	msg, err := s.m.svc.Messaging.Send(ctx, key, out)
	if ticket != nil {
		if err == nil {
			s.m.setTicketKey(ticket, msg.Key)
		}
		s.m.endSend(ctx, ticket, err == nil)
	}
	if err != nil {
		return nil, err
	}
	return msg, nil
}

type msgArgs struct {
	ChatID      string   `json:"chatId"`
	MessageID   string   `json:"messageId"`
	Emoji       string   `json:"emoji"`
	Text        string   `json:"text"`
	ForEveryone bool     `json:"forEveryone"`
	ToChatID    string   `json:"toChatId"`
	Options     []string `json:"options"`
	Typing      bool     `json:"typing"`
}

// messageKey decodifica los datos de un comando sobre un mensaje y arma su clave.
func (s *session) messageKey(ctx context.Context, f frameIn) (msgArgs, model.AccountID, model.MessageKey, error) {
	var d msgArgs
	if err := decodeData(f, &d); err != nil {
		return d, "", "", err
	}
	acc, native, err := s.chatRef(ctx, f, d.ChatID)
	if err != nil {
		return d, "", "", err
	}
	if err := requireText("messageId", d.MessageID); err != nil {
		return d, "", "", err
	}
	return d, acc, model.NewMessageKey(acc, native, d.MessageID), nil
}

func (s *session) cmdReact(ctx context.Context, f frameIn) (any, error) {
	d, _, key, err := s.messageKey(ctx, f)
	if err != nil {
		return nil, err
	}
	return empty(), s.m.svc.Messaging.React(ctx, key, d.Emoji)
}

func (s *session) cmdEdit(ctx context.Context, f frameIn) (any, error) {
	d, _, key, err := s.messageKey(ctx, f)
	if err != nil {
		return nil, err
	}
	return empty(), s.m.svc.Messaging.Edit(ctx, key, d.Text)
}

func (s *session) cmdDelete(ctx context.Context, f frameIn) (any, error) {
	d, _, key, err := s.messageKey(ctx, f)
	if err != nil {
		return nil, err
	}
	return empty(), s.m.svc.Messaging.Delete(ctx, key, d.ForEveryone)
}

func (s *session) cmdForward(ctx context.Context, f frameIn) (any, error) {
	d, acc, key, err := s.messageKey(ctx, f)
	if err != nil {
		return nil, err
	}
	if err := requireText("toChatId", d.ToChatID); err != nil {
		return nil, err
	}
	to := model.NewChatKey(acc, d.ToChatID)
	if strings.Contains(d.ToChatID, "|") {
		to = model.ChatKey(d.ToChatID)
	}
	// El chat de destino también debe estar dentro del filtro del destino (cuenta y contactos).
	toAcc, toNative := to.Split()
	if toAcc != acc {
		a, err := s.m.account(ctx, toAcc)
		if err != nil {
			return nil, err
		}
		if !filterAllowsAccount(s.c.dst.Filter, a.ID, a.Channel) {
			return nil, fmt.Errorf("%w: la cuenta no pasa el filtro de este destino", errForbidden)
		}
	}
	if err := s.checkChatScope(ctx, toAcc, toNative); err != nil {
		return nil, err
	}
	return s.m.svc.Messaging.Forward(ctx, key, to)
}

func (s *session) cmdPollVote(ctx context.Context, f frameIn) (any, error) {
	d, _, key, err := s.messageKey(ctx, f)
	if err != nil {
		return nil, err
	}
	return empty(), s.m.svc.Messaging.VotePoll(ctx, key, d.Options)
}

func (s *session) cmdMarkRead(ctx context.Context, f frameIn) (any, error) {
	var d msgArgs
	if err := decodeData(f, &d); err != nil {
		return nil, err
	}
	acc, native, err := s.chatRef(ctx, f, d.ChatID)
	if err != nil {
		return nil, err
	}
	return empty(), s.m.svc.Messaging.MarkRead(ctx, model.NewChatKey(acc, native))
}

func (s *session) cmdTyping(ctx context.Context, f frameIn) (any, error) {
	var d msgArgs
	if err := decodeData(f, &d); err != nil {
		return nil, err
	}
	acc, native, err := s.chatRef(ctx, f, d.ChatID)
	if err != nil {
		return nil, err
	}
	return empty(), s.m.svc.Messaging.SetTyping(ctx, model.NewChatKey(acc, native), d.Typing)
}

// statusIn: OutgoingStatus con archivo por fileUrl o uploadId.
type statusIn struct {
	model.OutgoingStatus
	FileURL  string `json:"fileUrl"`
	UploadID string `json:"uploadId"`
}

func (s *session) cmdStatusPost(ctx context.Context, f frameIn) (any, error) {
	var d statusIn
	if err := decodeData(f, &d); err != nil {
		return nil, err
	}
	acc, err := s.accountFor(ctx, f, "")
	if err != nil {
		return nil, err
	}
	st := d.OutgoingStatus
	st.FilePath = ""
	path, cleanup, err := s.m.resolveFile(ctx, s.c.dst.ID, acc, "", st.Kind, d.FileURL, d.UploadID, nil, &st.Mime)
	if err != nil {
		return nil, err
	}
	if cleanup != nil {
		defer cleanup()
	}
	st.FilePath = path
	return s.m.svc.Statuses.Post(ctx, acc, st)
}

func (s *session) cmdContactsFind(ctx context.Context, f frameIn) (any, error) {
	var d struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := decodeData(f, &d); err != nil {
		return nil, err
	}
	if d.Limit <= 0 || d.Limit > 200 {
		d.Limit = 50
	}
	list, _, err := s.m.svc.Contacts.List(ctx, model.ContactFilter{Query: d.Query, Limit: d.Limit})
	if err != nil {
		return nil, err
	}
	// Solo se muestran las identidades de las cuentas que pasan el filtro del destino.
	out := make([]model.Contact, 0, len(list))
	for _, c := range list {
		if ok, err := s.m.contactAllowed(ctx, s.c.dst.Filter.ContactScope, c); err != nil {
			return nil, fmt.Errorf("no se pudo comprobar el filtro de contactos: %w", err)
		} else if !ok {
			continue
		}
		ids := make([]model.Identity, 0, len(c.Identities))
		for _, id := range c.Identities {
			if filterAllowsAccount(s.c.dst.Filter, id.AccountID, id.Channel) {
				ids = append(ids, id)
			}
		}
		c.Identities = ids
		out = append(out, c)
	}
	return out, nil
}
