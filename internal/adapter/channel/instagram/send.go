package instagram

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/mautrix-meta/pkg/instameow/slidetypes"
	"go.mau.fi/mautrix-meta/pkg/messagix/httpclient"
	"go.mau.fi/mautrix-meta/pkg/messagix/methods"
	"go.mau.fi/util/jsontime"

	"dconnector/internal/adapter/channel/meta"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

func strPtr(s string) *string { return &s }

// stripVariation quita el selector de variación U+FE0F: Instagram guarda las reacciones sin él.
func stripVariation(s string) string { return strings.ReplaceAll(s, "️", "") }

// Send envía un texto o un archivo (09 §3).
func (a *Adapter) Send(ctx context.Context, chatNativeID string, out model.Outgoing) (model.Message, error) {
	caps := baseCapabilities()
	if err := validateOutgoing(out, caps.MaxTextLen); err != nil {
		return model.Message{}, err
	}
	cli, err := a.liveClient()
	if err != nil {
		return model.Message{}, err
	}
	if _, err := meta.ParseID(chatNativeID); err != nil {
		return model.Message{}, fmt.Errorf("instagram: %w", err)
	}
	otid := strconv.FormatInt(methods.GenerateEpochID(), 10)
	var replyTo *string
	if out.ReplyTo != nil && out.ReplyTo.NativeID != "" {
		replyTo = strPtr(strings.SplitN(out.ReplyTo.NativeID, "#", 2)[0])
	}
	msg := model.Message{
		AccountID: a.Acc.ID, Channel: model.ChannelInstagram, ChatNativeID: chatNativeID,
		ChatKey: model.NewChatKey(a.Acc.ID, chatNativeID), SenderNativeID: a.SelfID(), SenderName: "Tú",
		FromMe: true, Kind: out.Kind, Text: out.Text, Delivery: model.DeliverySent,
	}
	if replyTo != nil {
		msg.QuotedNativeID = *replyTo
		msg.QuotedPreview, msg.QuotedSender = a.local.Quote(ctx, chatNativeID, *replyTo)
	}
	var sent slidetypes.SentMessage
	if out.Kind == model.KindText {
		resp, err := cli.SendMessage(ctx, &slidetypes.SendTextRequest{
			IGThreadIGID: strPtr(chatNativeID), OfflineThreadingID: otid, ReplyToMessageID: replyTo,
			Text: slidetypes.SensitiveString{Value: out.Text}, SendAttribution: strPtr("igd_web_chat_tab:in_thread"),
		})
		if err != nil {
			return model.Message{}, a.mapErr("enviar el mensaje", err)
		}
		sent = resp.Message
	} else {
		file, err := meta.ReadOutgoing(out, caps)
		if err != nil {
			return model.Message{}, fmt.Errorf("instagram: %w", err)
		}
		threadNum, _ := strconv.ParseInt(chatNativeID, 10, 64)
		uctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		up, err := cli.GetHTTP().SendMercuryUploadRequest(uctx, threadNum, &httpclient.MercuryUploadMedia{
			Filename: file.Name, MimeType: file.Mime, MediaData: file.Data, IsVoiceClip: out.Kind == model.KindVoice,
		})
		cancel()
		if err != nil {
			return model.Message{}, a.mapErr("subir el archivo", err)
		}
		var fbid int64
		if up != nil && up.Payload.RealMetadata != nil {
			fbid = up.Payload.RealMetadata.GetFbId()
		}
		if fbid == 0 {
			return model.Message{}, fmt.Errorf("instagram: Instagram no aceptó el archivo: %w", port.ErrInvalid)
		}
		resp, err := cli.SendMedia(ctx, &slidetypes.SendMediaRequest{
			AttachmentFBID: strconv.FormatInt(fbid, 10), ThreadID: chatNativeID, OfflineThreadingID: otid, ReplyToMessageID: replyTo,
		})
		if err != nil {
			return model.Message{}, a.mapErr("enviar el archivo", err)
		}
		sent = resp.Message
		msg.Media = &model.Media{FileName: file.Name, Mime: file.Mime, Size: file.Size, LocalPath: out.FilePath}
	}
	id := firstNonEmpty(sent.ID, sent.MessageID)
	if id == "" {
		return model.Message{}, fmt.Errorf("instagram: Instagram no confirmó el mensaje: %w", port.ErrInvalid)
	}
	msg.NativeID = id
	msg.Key = model.NewMessageKey(a.Acc.ID, chatNativeID, id)
	msg.Timestamp = sent.TimestampMS.UnixMilli()
	if msg.Timestamp <= 0 {
		msg.Timestamp = time.Now().UnixMilli()
	}
	if msg.Media != nil {
		msg.Media.ID = meta.MediaID(chatNativeID, id, 0)
	}
	a.local.Remember(chatNativeID, id)
	return msg, nil
}

func validateOutgoing(out model.Outgoing, maxText int) error {
	switch out.Kind {
	case model.KindText:
		if strings.TrimSpace(out.Text) == "" {
			return fmt.Errorf("instagram: el mensaje está vacío: %w", port.ErrInvalid)
		}
	case model.KindImage, model.KindVideo, model.KindAudio, model.KindVoice:
	default:
		return fmt.Errorf("instagram: Instagram no admite enviar «%s» desde aquí: %w", out.Kind, port.ErrUnsupported)
	}
	if n := len([]rune(out.Text)); maxText > 0 && n > maxText {
		return fmt.Errorf("instagram: el mensaje es demasiado largo (máximo %d caracteres): %w", maxText, port.ErrInvalid)
	}
	return nil
}

// baseMsgID: el id del mensaje sin el sufijo «#n» de los adjuntos adicionales.
func baseMsgID(id string) string { return strings.SplitN(id, "#", 2)[0] }

// React pone o quita (emoji vacío) mi reacción a un mensaje.
func (a *Adapter) React(ctx context.Context, msg model.MessageRef, emoji string) error {
	cli, err := a.liveClient()
	if err != nil {
		return err
	}
	if _, err := meta.ParseID(msg.ChatNativeID); err != nil {
		return fmt.Errorf("instagram: %w", err)
	}
	id := baseMsgID(msg.NativeID)
	emoji = stripVariation(strings.TrimSpace(emoji))
	status := slidetypes.ReactionStatusCreated
	reacted := emoji
	if emoji == "" {
		status = slidetypes.ReactionStatusDeleted
		reacted = a.local.MyReaction(ctx, msg.ChatNativeID, msg.NativeID, a.SelfID())
		if reacted == "" {
			return fmt.Errorf("instagram: no consta con qué emoji reaccionaste a ese mensaje: %w", port.ErrNotFound)
		}
	}
	_, err = cli.SendReaction(ctx, &slidetypes.CreateReactionRequest{Input: slidetypes.ReactionInput{
		Emoji: reacted, MessageID: id, ReactionStatus: status, ThreadID: msg.ChatNativeID,
	}})
	if err != nil {
		return a.mapErr("reaccionar", err)
	}
	ev := model.ReactionEvent{
		MessageKey: model.NewMessageKey(a.Acc.ID, msg.ChatNativeID, msg.NativeID),
		Reaction:   model.Reaction{SenderNativeID: a.SelfID(), SenderName: "Tú", Emoji: emoji, At: time.Now().UnixMilli()},
	}
	a.Emit(model.Event{Type: model.EvReaction, Reaction: &ev, ChatKey: ev.MessageKey.Chat()})
	return nil
}

// Edit cambia el texto de un mensaje propio (15 minutos).
func (a *Adapter) Edit(ctx context.Context, msg model.MessageRef, newText string) error {
	if strings.TrimSpace(newText) == "" {
		return fmt.Errorf("instagram: el mensaje está vacío: %w", port.ErrInvalid)
	}
	if n := len([]rune(newText)); n > baseCapabilities().MaxTextLen {
		return fmt.Errorf("instagram: el mensaje es demasiado largo: %w", port.ErrInvalid)
	}
	if !msg.FromMe {
		return fmt.Errorf("instagram: solo puedes editar tus propios mensajes: %w", port.ErrInvalid)
	}
	if msg.Timestamp > 0 && time.Since(time.UnixMilli(msg.Timestamp)) > editWindow {
		return fmt.Errorf("instagram: ya pasó el tiempo para editar ese mensaje: %w", port.ErrInvalid)
	}
	cli, err := a.liveClient()
	if err != nil {
		return err
	}
	_, err = cli.EditMessage(ctx, &slidetypes.EditMessageRequest{
		ThreadID: msg.ChatNativeID, Body: slidetypes.SensitiveString{Value: newText},
		OfflineThreadingID: strconv.FormatInt(methods.GenerateEpochID(), 10), TargetMessageID: baseMsgID(msg.NativeID),
	})
	if err != nil {
		return a.mapErr("editar el mensaje", err)
	}
	t := newText
	u := model.MessageUpdate{Key: model.NewMessageKey(a.Acc.ID, msg.ChatNativeID, msg.NativeID), Text: &t, EditedAt: time.Now().UnixMilli()}
	a.Emit(model.Event{Type: model.EvMessageUpdate, Update: &u, ChatKey: u.Key.Chat()})
	return nil
}

// Delete deshace el envío de un mensaje propio (para todos). Instagram no tiene «borrar solo para mí».
func (a *Adapter) Delete(ctx context.Context, msg model.MessageRef, forEveryone bool) error {
	if !forEveryone {
		return fmt.Errorf("instagram: Instagram solo permite borrar para todos: %w", port.ErrUnsupported)
	}
	if !msg.FromMe {
		return fmt.Errorf("instagram: solo puedes borrar para todos tus propios mensajes: %w", port.ErrInvalid)
	}
	cli, err := a.liveClient()
	if err != nil {
		return err
	}
	long, err := a.ensureLongID(ctx, msg.ChatNativeID)
	if err != nil {
		return err
	}
	if _, err := cli.UnsendMessage(ctx, &slidetypes.UnsendMessageRequest{
		MessageID: baseMsgID(msg.NativeID), SendData: slidetypes.SendData{ThreadID: long},
	}); err != nil {
		return a.mapErr("borrar el mensaje", err)
	}
	u := model.MessageUpdate{Key: model.NewMessageKey(a.Acc.ID, msg.ChatNativeID, msg.NativeID), Deleted: true}
	a.Emit(model.Event{Type: model.EvMessageUpdate, Update: &u, ChatKey: u.Key.Chat()})
	return nil
}

// MarkRead marca el chat como leído hasta el mensaje más reciente de upTo (o el más reciente guardado).
func (a *Adapter) MarkRead(ctx context.Context, chatNativeID string, upTo []model.MessageRef) error {
	cli, err := a.liveClient()
	if err != nil {
		return err
	}
	lastID, lastTS := "", int64(0)
	for _, m := range upTo {
		if m.NativeID != "" && m.Timestamp >= lastTS {
			lastID, lastTS = baseMsgID(m.NativeID), m.Timestamp
		}
	}
	if lastID == "" {
		lastID, lastTS = a.local.Newest(ctx, chatNativeID)
		lastID = baseMsgID(lastID)
	}
	if lastID == "" {
		return nil // no hay nada que marcar
	}
	long, err := a.ensureLongID(ctx, chatNativeID)
	if err != nil {
		return err
	}
	md := slidetypes.MarkReadMetadata{IGThreadIGID: long}
	if _, err := cli.MarkRead(ctx, &slidetypes.MarkReadRequest{Metadata: md, Data: slidetypes.MarkReadData{MessageID: lastID, ItemID: strPtr("")}}); err != nil {
		return a.mapErr("marcar como leído", err)
	}
	ts := time.UnixMilli(lastTS)
	if _, err := cli.MarkReadValidation(ctx, &slidetypes.MarkReadRequest{Metadata: md, Data: slidetypes.MarkReadData{
		MessageID: lastID, MessageTimestampMS: jsontime.UnixMilliString{Time: ts},
	}}); err != nil {
		a.Log.Debug("la validación de lectura falló", "err", meta.RedactErr(err))
	}
	return nil
}

// SetTyping avisa «escribiendo…» (o que se dejó de escribir).
func (a *Adapter) SetTyping(ctx context.Context, chatNativeID string, typing bool) error {
	cli, err := a.liveClient()
	if err != nil {
		return err
	}
	if err := cli.SetTyping(ctx, chatNativeID, typing); err != nil {
		return a.mapErr("avisar que escribes", err)
	}
	return nil
}

// ensureLongID devuelve el id largo del hilo (thread_igid); si no se conoce, pide la ficha del hilo.
func (a *Adapter) ensureLongID(ctx context.Context, chat string) (string, error) {
	if long := a.conv.LongID(chat); long != "" {
		return long, nil
	}
	if err := a.fetchThread(ctx, chat); err != nil {
		return "", a.mapErr("pedir el chat", err)
	}
	if long := a.conv.LongID(chat); long != "" {
		return long, nil
	}
	return "", fmt.Errorf("instagram: no se pudo identificar el chat en Instagram: %w", port.ErrNotFound)
}
