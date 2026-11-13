package messenger

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/mautrix-meta/pkg/messagix"
	"go.mau.fi/mautrix-meta/pkg/messagix/httpclient"
	"go.mau.fi/mautrix-meta/pkg/messagix/methods"
	"go.mau.fi/mautrix-meta/pkg/messagix/socket"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"

	"dconnector/internal/adapter/channel/meta"
	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// stripVariation quita el selector de variación U+FE0F: Messenger guarda las reacciones sin él.
func stripVariation(s string) string { return strings.ReplaceAll(s, "️", "") }

// Send envía un texto o un archivo (09 §3). Los chats cifrados salen por su propio canal (e2ee.go).
func (a *Adapter) Send(ctx context.Context, chatNativeID string, out model.Outgoing) (model.Message, error) {
	caps := baseCapabilities(a.e2ee != nil)
	if err := validateOutgoing(out, caps.MaxTextLen); err != nil {
		return model.Message{}, err
	}
	if jid, ok := a.e2eeChat(chatNativeID); ok {
		return a.e2eeSend(ctx, jid, chatNativeID, out)
	}
	cli, err := a.liveClient()
	if err != nil {
		return model.Message{}, err
	}
	threadID, err := meta.ParseID(chatNativeID)
	if err != nil {
		return model.Message{}, fmt.Errorf("messenger: %w", err)
	}
	otid := methods.GenerateEpochID()
	task := &socket.SendMessageTask{
		ThreadId: threadID, Otid: otid,
		Source: table.MESSENGER_INBOX_IN_THREAD, InitiatingSource: table.FACEBOOK_INBOX,
		SendType: table.TEXT, SyncGroup: 1, Text: out.Text,
	}
	if out.ReplyTo != nil && out.ReplyTo.NativeID != "" {
		task.ReplyMetaData = &socket.ReplyMetaData{ReplyMessageId: out.ReplyTo.NativeID, ReplySourceType: 1, ReplyType: 0}
	}
	msg := model.Message{
		AccountID: a.Acc.ID, Channel: model.ChannelMessenger, ChatNativeID: chatNativeID,
		ChatKey: model.NewChatKey(a.Acc.ID, chatNativeID), SenderNativeID: a.SelfID(), SenderName: "Tú",
		FromMe: true, Kind: out.Kind, Text: out.Text, Delivery: model.DeliverySent,
	}
	if out.Kind != model.KindText {
		file, err := meta.ReadOutgoing(out, caps)
		if err != nil {
			return model.Message{}, fmt.Errorf("messenger: %w", err)
		}
		uctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		up, err := cli.GetHTTP().SendMercuryUploadRequest(uctx, threadID, &httpclient.MercuryUploadMedia{
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
			return model.Message{}, fmt.Errorf("messenger: Messenger no aceptó el archivo: %w", port.ErrInvalid)
		}
		task.SendType = table.MEDIA
		task.AttachmentFBIds = []int64{fbid}
		msg.Media = &model.Media{FileName: file.Name, Mime: file.Mime, Size: file.Size, LocalPath: out.FilePath}
	}
	if out.ReplyTo != nil && out.ReplyTo.NativeID != "" {
		msg.QuotedNativeID = out.ReplyTo.NativeID
		msg.QuotedPreview, msg.QuotedSender = a.local.Quote(ctx, chatNativeID, out.ReplyTo.NativeID)
	}
	msgID, err := a.execSend(ctx, cli, task, otid)
	if err != nil {
		return model.Message{}, err
	}
	msg.NativeID = msgID
	msg.Key = model.NewMessageKey(a.Acc.ID, chatNativeID, msgID)
	msg.Timestamp = sendTimestamp(msgID)
	if msg.Media != nil {
		msg.Media.ID = meta.MediaID(chatNativeID, msgID, 0)
	}
	a.local.Remember(chatNativeID, msgID)
	return msg, nil
}

func validateOutgoing(out model.Outgoing, maxText int) error {
	switch out.Kind {
	case model.KindText:
		if strings.TrimSpace(out.Text) == "" {
			return fmt.Errorf("messenger: el mensaje está vacío: %w", port.ErrInvalid)
		}
	case model.KindImage, model.KindVideo, model.KindAudio, model.KindVoice, model.KindDocument:
	default:
		return fmt.Errorf("messenger: Messenger no admite enviar «%s» desde aquí: %w", out.Kind, port.ErrUnsupported)
	}
	if n := len([]rune(out.Text)); maxText > 0 && n > maxText {
		return fmt.Errorf("messenger: el mensaje es demasiado largo (máximo %d caracteres): %w", maxText, port.ErrInvalid)
	}
	return nil
}

// execSend manda las tareas de envío y devuelve el id del mensaje que confirma el servidor.
func (a *Adapter) execSend(ctx context.Context, cli *messagix.Client, task *socket.SendMessageTask, otid int64) (string, error) {
	otidStr := strconv.FormatInt(otid, 10)
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if err := cli.WaitUntilCanSendMessages(ctx, 15*time.Second); err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			continue
		}
		// Con cada envío también se marca el chat como leído (como hace el cliente web).
		tbl, err := cli.ExecuteTasks(ctx, task, &socket.ThreadMarkReadTask{
			ThreadId: task.ThreadId, SyncGroup: 1, LastReadWatermarkTs: time.Now().UnixMilli(),
		})
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			time.Sleep(time.Duration(attempt+1) * time.Second)
			continue
		}
		if tbl != nil {
			for _, rep := range tbl.LSReplaceOptimsiticMessage {
				if rep.OfflineThreadingId == otidStr && rep.MessageId != "" {
					return rep.MessageId, nil
				}
			}
			if len(tbl.LSMarkOptimisticMessageFailed) > 0 {
				return "", fmt.Errorf("messenger: Messenger rechazó el mensaje: %w", port.ErrInvalid)
			}
			if len(tbl.LSHandleFailedTask) > 0 {
				return "", fmt.Errorf("messenger: Messenger rechazó el mensaje: %w", port.ErrInvalid)
			}
		}
		return "", fmt.Errorf("messenger: Messenger no confirmó el mensaje: %w", port.ErrInvalid)
	}
	return "", a.mapErr("enviar el mensaje", lastErr)
}

// sendTimestamp saca la hora del id del mensaje (mid.$…); si no cuadra con ahora (±1 h), usa la hora actual.
func sendTimestamp(msgID string) int64 {
	now := time.Now()
	if ms, err := methods.ParseMessageID(msgID); err == nil {
		t := time.UnixMilli(ms)
		if t.After(now.Add(-time.Hour)) && t.Before(now.Add(time.Hour)) {
			return ms
		}
	}
	return now.UnixMilli()
}

// React pone o quita (emoji vacío) mi reacción a un mensaje.
func (a *Adapter) React(ctx context.Context, msg model.MessageRef, emoji string) error {
	if jid, ok := a.e2eeChat(msg.ChatNativeID); ok {
		return a.e2eeReact(ctx, jid, msg, emoji)
	}
	cli, err := a.liveClient()
	if err != nil {
		return err
	}
	threadID, err := meta.ParseID(msg.ChatNativeID)
	if err != nil {
		return fmt.Errorf("messenger: %w", err)
	}
	self := selfInt(a.SelfID())
	emoji = stripVariation(strings.TrimSpace(emoji))
	_, err = cli.ExecuteTasks(ctx, &socket.SendReactionTask{
		ThreadKey: threadID, MessageID: msg.NativeID, Reaction: emoji, ActorID: self,
		SyncGroup: 1, SendAttribution: table.MESSENGER_INBOX_IN_THREAD,
	})
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

// Edit cambia el texto de un mensaje propio (Messenger lo permite durante 15 minutos).
func (a *Adapter) Edit(ctx context.Context, msg model.MessageRef, newText string) error {
	if strings.TrimSpace(newText) == "" {
		return fmt.Errorf("messenger: el mensaje está vacío: %w", port.ErrInvalid)
	}
	if n := len([]rune(newText)); n > baseCapabilities(false).MaxTextLen {
		return fmt.Errorf("messenger: el mensaje es demasiado largo: %w", port.ErrInvalid)
	}
	if !msg.FromMe {
		return fmt.Errorf("messenger: solo puedes editar tus propios mensajes: %w", port.ErrInvalid)
	}
	if msg.Timestamp > 0 && time.Since(time.UnixMilli(msg.Timestamp)) > editWindow {
		return fmt.Errorf("messenger: ya pasó el tiempo para editar ese mensaje: %w", port.ErrInvalid)
	}
	if jid, ok := a.e2eeChat(msg.ChatNativeID); ok {
		return a.e2eeEdit(ctx, jid, msg, newText)
	}
	cli, err := a.liveClient()
	if err != nil {
		return err
	}
	tbl, err := cli.ExecuteTasks(ctx, &socket.EditMessageTask{MessageID: msg.NativeID, Text: newText})
	if err != nil {
		return a.mapErr("editar el mensaje", err)
	}
	// Messenger responde con el mensaje editado; si el texto vuelve igual al original, rechazó la edición.
	if tbl == nil || len(tbl.LSEditMessage) == 0 || tbl.LSEditMessage[0].Text != newText {
		return fmt.Errorf("messenger: Messenger no aceptó la edición: %w", port.ErrInvalid)
	}
	t := newText
	u := model.MessageUpdate{Key: model.NewMessageKey(a.Acc.ID, msg.ChatNativeID, msg.NativeID), Text: &t, EditedAt: time.Now().UnixMilli()}
	a.Emit(model.Event{Type: model.EvMessageUpdate, Update: &u, ChatKey: u.Key.Chat()})
	return nil
}

// Delete borra un mensaje: para todos (deshacer el envío) o solo para mí.
func (a *Adapter) Delete(ctx context.Context, msg model.MessageRef, forEveryone bool) error {
	if jid, ok := a.e2eeChat(msg.ChatNativeID); ok {
		return a.e2eeDelete(ctx, jid, msg, forEveryone)
	}
	cli, err := a.liveClient()
	if err != nil {
		return err
	}
	if forEveryone {
		if !msg.FromMe {
			return fmt.Errorf("messenger: solo puedes borrar para todos tus propios mensajes: %w", port.ErrInvalid)
		}
		if _, err := cli.ExecuteTasks(ctx, &socket.DeleteMessageTask{MessageId: msg.NativeID}); err != nil {
			return a.mapErr("borrar el mensaje", err)
		}
		u := model.MessageUpdate{Key: model.NewMessageKey(a.Acc.ID, msg.ChatNativeID, msg.NativeID), Deleted: true}
		a.Emit(model.Event{Type: model.EvMessageUpdate, Update: &u, ChatKey: u.Key.Chat()})
		return nil
	}
	threadID, err := meta.ParseID(msg.ChatNativeID)
	if err != nil {
		return fmt.Errorf("messenger: %w", err)
	}
	if _, err := cli.ExecuteTasks(ctx, &socket.DeleteMessageMeOnlyTask{ThreadKey: threadID, MessageId: msg.NativeID}); err != nil {
		return a.mapErr("borrar el mensaje", err)
	}
	return nil
}

// MarkRead marca el chat como leído hasta el mensaje más reciente de upTo (o hasta ahora si no hay).
func (a *Adapter) MarkRead(ctx context.Context, chatNativeID string, upTo []model.MessageRef) error {
	mark := int64(0)
	for _, m := range upTo {
		if m.Timestamp > mark {
			mark = m.Timestamp
		}
	}
	if mark == 0 {
		mark = time.Now().UnixMilli()
	}
	if jid, ok := a.e2eeChat(chatNativeID); ok {
		return a.e2eeMarkRead(ctx, jid, chatNativeID, upTo)
	}
	cli, err := a.liveClient()
	if err != nil {
		return err
	}
	threadID, err := meta.ParseID(chatNativeID)
	if err != nil {
		return fmt.Errorf("messenger: %w", err)
	}
	if _, err := cli.ExecuteTasks(ctx, &socket.ThreadMarkReadTask{ThreadId: threadID, LastReadWatermarkTs: mark, SyncGroup: 1}); err != nil {
		return a.mapErr("marcar como leído", err)
	}
	return nil
}

// SetTyping avisa «escribiendo…» (o que se dejó de escribir).
func (a *Adapter) SetTyping(ctx context.Context, chatNativeID string, typing bool) error {
	if jid, ok := a.e2eeChat(chatNativeID); ok {
		return a.e2eeTyping(ctx, jid, typing)
	}
	cli, err := a.liveClient()
	if err != nil {
		return err
	}
	threadID, err := meta.ParseID(chatNativeID)
	if err != nil {
		return fmt.Errorf("messenger: %w", err)
	}
	_, group, typ := a.conv.ThreadKnown(threadID)
	task := &socket.UpdatePresenceTask{ThreadKey: threadID, SyncGroup: 1, ThreadType: int64(typ)}
	if group {
		task.IsGroupThread = 1
	}
	if typing {
		task.IsTyping = 1
	}
	if err := cli.ExecuteStatelessTask(ctx, task); err != nil {
		return a.mapErr("avisar que escribes", err)
	}
	return nil
}
