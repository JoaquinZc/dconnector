package telegram

// Historias de Telegram (08 §9): se traducen a «estados».

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

const (
	// storyRefresh: cada cuánto se vuelven a pedir las historias vigentes.
	storyRefresh = 15 * time.Minute
	// storyPeriod: duración de una historia propia (24 h).
	storyPeriod = 86400
	// storyCaptionMax: pie de una historia (Telegram admite 200 sin Premium y 2.048 con él).
	storyCaptionMax = 2048
	// storyViewersLimit y storyViewersPages: tope de espectadores que se piden.
	storyViewersLimit = 100
	storyViewersPages = 5
)

// refreshStoryCapability consulta si se pueden publicar historias y, si cambió, vuelve a emitir
// account.capabilities.
func (a *Adapter) refreshStoryCapability(ctx context.Context) {
	api, err := a.liveAPI()
	if err != nil {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	_, err = api.StoriesCanSendStory(cctx, &tg.InputPeerSelf{})
	can := err == nil
	if prev := a.postStatus.Swap(can); prev != can {
		a.emit(model.Event{Type: model.EvCapabilities, Capabilities: a.Capabilities(nil)})
	}
}

// storyLoop trae las historias vigentes al conectar y cada 15 minutos.
func (a *Adapter) storyLoop(ctx context.Context) {
	t := time.NewTicker(storyRefresh)
	defer t.Stop()
	for {
		fctx, cancel := context.WithTimeout(ctx, 2*opTimeout)
		if err := a.fetchStories(fctx); err != nil && ctx.Err() == nil {
			a.log.Debug("no se pudieron traer las historias", "err", redactErr(err))
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.refreshStoryCapability(ctx)
		}
	}
}

// fetchStories pide todas las historias vigentes y emite status.posted de cada una.
func (a *Adapter) fetchStories(ctx context.Context) error {
	api, err := a.liveAPI()
	if err != nil {
		return err
	}
	req := &tg.StoriesGetAllStoriesRequest{}
	for page := 0; page < 10; page++ {
		res, err := api.StoriesGetAllStories(ctx, req)
		if err != nil {
			return a.mapErr("pedir las historias", err)
		}
		all, ok := res.(*tg.StoriesAllStories)
		if !ok {
			return nil // sin cambios
		}
		ents := tg.EntitiesFrom(all.Users, all.Chats)
		a.feedEntities(ctx, ents)
		c := a.conv(&ents)
		for _, ps := range all.PeerStories {
			for _, sc := range ps.Stories {
				it, ok := sc.(*tg.StoryItem)
				if !ok {
					continue
				}
				st := c.storyOf(ps.Peer, it)
				st.Viewed = it.ID <= ps.MaxReadID
				a.emit(model.Event{Type: model.EvStatusUpsert, At: st.PostedAt, Status: &st})
			}
		}
		if !all.HasMore {
			return nil
		}
		req = &tg.StoriesGetAllStoriesRequest{Next: true, State: all.State}
	}
	return nil
}

// onStory: una historia nueva o borrada llegó en vivo.
func (a *Adapter) onStory(e tg.Entities, u *tg.UpdateStory) {
	author := nativeOf(u.Peer)
	if author == "" {
		return
	}
	switch it := u.Story.(type) {
	case *tg.StoryItem:
		st := a.conv(&e).storyOf(u.Peer, it)
		a.emit(model.Event{Type: model.EvStatusUpsert, At: st.PostedAt, Status: &st})
	case *tg.StoryItemDeleted:
		nid := storyNative(author, it.ID)
		now := time.Now().UnixMilli()
		st := model.Status{
			Key: string(a.acc.ID) + "|" + nid, AccountID: a.acc.ID, Channel: model.ChannelTelegram,
			NativeID: nid, AuthorNativeID: author, DeletedAt: now,
		}
		a.emit(model.Event{Type: model.EvStatusUpsert, At: now, Status: &st})
	}
}

// ---------------------------------------------------------------------------------------------
// Publicar y gestionar historias propias

// privacyRules traduce la audiencia al formato de Telegram (08 §9).
func (a *Adapter) privacyRules(ctx context.Context, aud model.StatusAudience) ([]tg.InputPrivacyRuleClass, error) {
	users := func() ([]tg.InputUserClass, error) {
		var out []tg.InputUserClass
		for _, id := range aud.PeerIDs {
			if in, err := a.peers.inputUser(ctx, id); err == nil {
				out = append(out, in)
			}
		}
		if len(aud.PeerIDs) > 0 && len(out) == 0 {
			return nil, fmt.Errorf("telegram: Telegram aún no conoce a las personas elegidas: %w", port.ErrInvalid)
		}
		return out, nil
	}
	switch aud.Mode {
	case "", "contacts":
		return []tg.InputPrivacyRuleClass{&tg.InputPrivacyValueAllowContacts{}}, nil
	case "allow":
		us, err := users()
		if err != nil {
			return nil, err
		}
		if len(us) == 0 {
			return nil, fmt.Errorf("telegram: elige al menos una persona para la historia: %w", port.ErrInvalid)
		}
		return []tg.InputPrivacyRuleClass{&tg.InputPrivacyValueAllowUsers{Users: us}}, nil
	case "deny":
		us, err := users()
		if err != nil {
			return nil, err
		}
		rules := []tg.InputPrivacyRuleClass{&tg.InputPrivacyValueAllowContacts{}}
		if len(us) > 0 {
			rules = append(rules, &tg.InputPrivacyValueDisallowUsers{Users: us})
		}
		return rules, nil
	}
	return nil, fmt.Errorf("telegram: audiencia de historia no válida: %w", port.ErrInvalid)
}

// PostStatus publica una historia (foto o vídeo; Telegram no admite historias solo de texto).
func (a *Adapter) PostStatus(ctx context.Context, s model.OutgoingStatus) (model.Status, error) {
	if s.Kind == model.KindText || s.Kind == "" {
		return model.Status{}, fmt.Errorf("telegram: Telegram no admite historias solo de texto: %w", port.ErrUnsupported)
	}
	if s.Kind != model.KindImage && s.Kind != model.KindVideo {
		return model.Status{}, fmt.Errorf("telegram: las historias solo pueden ser foto o vídeo: %w", port.ErrInvalid)
	}
	if strings.TrimSpace(s.FilePath) == "" {
		return model.Status{}, fmt.Errorf("telegram: falta el archivo de la historia: %w", port.ErrInvalid)
	}
	if utf8.RuneCountInString(s.Text) > storyCaptionMax {
		return model.Status{}, fmt.Errorf("telegram: el texto de la historia es demasiado largo: %w", port.ErrInvalid)
	}
	fi, err := os.Stat(s.FilePath)
	if err != nil || fi.IsDir() {
		return model.Status{}, fmt.Errorf("telegram: no se pudo abrir el archivo: %w", port.ErrNotFound)
	}
	api, err := a.liveAPI()
	if err != nil {
		return model.Status{}, err
	}
	if _, err := api.StoriesCanSendStory(ctx, &tg.InputPeerSelf{}); err != nil {
		return model.Status{}, fmt.Errorf("telegram: ahora no se puede publicar una historia (%s): %w", reasonOf(err), port.ErrUnsupported)
	}
	rules, err := a.privacyRules(ctx, s.Audience)
	if err != nil {
		return model.Status{}, err
	}
	f, err := uploader.NewUploader(api).FromPath(ctx, s.FilePath)
	if err != nil {
		return model.Status{}, a.mapErr("subir la historia", err)
	}
	var media tg.InputMediaClass
	if s.Kind == model.KindImage {
		media = &tg.InputMediaUploadedPhoto{File: f}
	} else {
		mt := detectMime(s.Mime, s.FilePath)
		if !strings.HasPrefix(mt, "video/") {
			mt = "video/mp4"
		}
		va := &tg.DocumentAttributeVideo{SupportsStreaming: true, W: 720, H: 1280}
		if info := probeMP4File(s.FilePath); info.OK {
			va.Duration = info.Duration
			if info.W > 0 {
				va.W, va.H = info.W, info.H
			}
		}
		media = &tg.InputMediaUploadedDocument{
			File: f, MimeType: mt,
			Attributes: []tg.DocumentAttributeClass{va, &tg.DocumentAttributeFilename{FileName: filepath.Base(s.FilePath)}},
		}
	}
	upd, err := api.StoriesSendStory(ctx, &tg.StoriesSendStoryRequest{
		Peer: &tg.InputPeerSelf{}, Media: media, Caption: s.Text, PrivacyRules: rules,
		RandomID: randID(), Period: storyPeriod,
	})
	if err != nil {
		return model.Status{}, a.mapErr("publicar la historia", err)
	}
	if st, ok := a.storyFromUpdates(upd); ok {
		return st, nil
	}
	// Telegram no devolvió la historia: se arma una mínima con lo que se sabe.
	now := time.Now()
	self := userNative(a.selfID.Load())
	return model.Status{
		Key: string(a.acc.ID) + "|" + self + "/0", AccountID: a.acc.ID, Channel: model.ChannelTelegram,
		NativeID: self + "/0", AuthorNativeID: self, FromMe: true, PostedAt: now.UnixMilli(),
		ExpiresAt: now.Add(storyPeriod * time.Second).UnixMilli(), Kind: s.Kind, Text: s.Text, Delivery: model.DeliverySent,
	}, nil
}

// reasonOf: motivo corto y legible de un error de Telegram.
func reasonOf(err error) string {
	switch {
	case isTg(err, "PREMIUM_ACCOUNT_REQUIRED"):
		return "hace falta Telegram Premium"
	case isTg(err, "STORIES_TOO_MUCH", "STORY_SEND_FLOOD_WEEKLY", "STORY_SEND_FLOOD_MONTHLY"):
		return "llegaste al límite de historias"
	case isTg(err, "BOOSTS_REQUIRED"):
		return "hacen falta más impulsos"
	}
	return redactErr(err)
}

// storyFromUpdates busca en la respuesta de StoriesSendStory la historia creada.
func (a *Adapter) storyFromUpdates(u tg.UpdatesClass) (model.Status, bool) {
	var ups []tg.UpdateClass
	var ents tg.Entities
	switch x := u.(type) {
	case *tg.Updates:
		ups, ents = x.Updates, tg.EntitiesFrom(x.Users, x.Chats)
	case *tg.UpdatesCombined:
		ups, ents = x.Updates, tg.EntitiesFrom(x.Users, x.Chats)
	default:
		return model.Status{}, false
	}
	for _, up := range ups {
		if us, ok := up.(*tg.UpdateStory); ok {
			if it, ok := us.Story.(*tg.StoryItem); ok {
				st := a.conv(&ents).storyOf(us.Peer, it)
				st.FromMe = true
				return st, true
			}
		}
	}
	return model.Status{}, false
}

// ownStoryID extrae el id de una historia propia de su id nativo «<autor>/<id>».
func (a *Adapter) ownStoryID(statusNativeID string) (int, error) {
	author, id, ok := parseStoryNative(statusNativeID)
	if !ok {
		return 0, fmt.Errorf("telegram: identificador de historia no válido: %w", port.ErrInvalid)
	}
	if self := a.selfID.Load(); self == 0 || author != userNative(self) {
		return 0, fmt.Errorf("telegram: solo puedes hacer eso con tus propias historias: %w", port.ErrInvalid)
	}
	return id, nil
}

// DeleteStatus borra una historia propia.
func (a *Adapter) DeleteStatus(ctx context.Context, statusNativeID string) error {
	id, err := a.ownStoryID(statusNativeID)
	if err != nil {
		return err
	}
	api, err := a.liveAPI()
	if err != nil {
		return err
	}
	if _, err := api.StoriesDeleteStories(ctx, &tg.StoriesDeleteStoriesRequest{Peer: &tg.InputPeerSelf{}, ID: []int{id}}); err != nil {
		return a.mapErr("borrar la historia", err)
	}
	return nil
}

// storyTarget resuelve el autor y el id de una historia ajena o propia.
func (a *Adapter) storyTarget(ctx context.Context, st model.Status) (tg.InputPeerClass, int, error) {
	author, id, ok := parseStoryNative(st.NativeID)
	if !ok {
		return nil, 0, fmt.Errorf("telegram: identificador de historia no válido: %w", port.ErrInvalid)
	}
	peer, err := a.peers.input(ctx, author)
	if err != nil {
		return nil, 0, err
	}
	return peer, id, nil
}

// MarkStatusViewed marca como vista una historia ajena.
func (a *Adapter) MarkStatusViewed(ctx context.Context, st model.Status) error {
	api, err := a.liveAPI()
	if err != nil {
		return err
	}
	peer, id, err := a.storyTarget(ctx, st)
	if err != nil {
		return err
	}
	if _, err := api.StoriesReadStories(ctx, &tg.StoriesReadStoriesRequest{Peer: peer, MaxID: id}); err != nil {
		return a.mapErr("marcar la historia como vista", err)
	}
	return nil
}

// ReactStatus reacciona a una historia ("" quita la reacción).
func (a *Adapter) ReactStatus(ctx context.Context, st model.Status, emoji string) error {
	api, err := a.liveAPI()
	if err != nil {
		return err
	}
	peer, id, err := a.storyTarget(ctx, st)
	if err != nil {
		return err
	}
	var reaction tg.ReactionClass = &tg.ReactionEmpty{}
	if emoji != "" {
		reaction = &tg.ReactionEmoji{Emoticon: emoji}
	}
	if _, err := api.StoriesSendReaction(ctx, &tg.StoriesSendReactionRequest{Peer: peer, StoryID: id, Reaction: reaction}); err != nil {
		return a.mapErr("reaccionar a la historia", err)
	}
	return nil
}

// StatusViewers devuelve quién vio una historia propia (hasta 500) y con qué reacción.
func (a *Adapter) StatusViewers(ctx context.Context, statusNativeID string) ([]model.StatusView, error) {
	id, err := a.ownStoryID(statusNativeID)
	if err != nil {
		return nil, err
	}
	api, err := a.liveAPI()
	if err != nil {
		return nil, err
	}
	key := string(a.acc.ID) + "|" + statusNativeID
	var out []model.StatusView
	offset := ""
	for page := 0; page < storyViewersPages; page++ {
		res, err := api.StoriesGetStoryViewsList(ctx, &tg.StoriesGetStoryViewsListRequest{
			Peer: &tg.InputPeerSelf{}, ID: id, Offset: offset, Limit: storyViewersLimit,
		})
		if err != nil {
			return out, a.mapErr("pedir quién vio la historia", err)
		}
		a.feedEntities(ctx, tg.EntitiesFrom(res.Users, res.Chats))
		c := a.conv(nil)
		for _, vc := range res.Views {
			v, ok := vc.(*tg.StoryView)
			if !ok {
				continue
			}
			viewer := userNative(v.UserID)
			out = append(out, model.StatusView{
				StatusKey: key, ViewerID: viewer, ViewerName: c.nameOr(viewer, ""),
				At: int64(v.Date) * 1000, Reaction: emojiOf(v.Reaction),
			})
		}
		if res.NextOffset == "" || len(res.Views) == 0 {
			break
		}
		offset = res.NextOffset
	}
	return out, nil
}
