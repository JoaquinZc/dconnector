package whatsapp

// Estados (07 §10). Los estados entrantes, las vistas y las reacciones salen en convert.go /
// events.go. Aquí: publicar, borrar y marcar como visto.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// defaultStatusBg: fondo de un estado de texto sin color elegido.
const defaultStatusBg = "#128C7E"

// buildStatusText arma el mensaje de un estado de texto (función pura).
func buildStatusText(s model.OutgoingStatus) (*waE2E.Message, error) {
	if strings.TrimSpace(s.Text) == "" {
		return nil, fmt.Errorf("whatsapp: el estado de texto está vacío: %w", port.ErrInvalid)
	}
	bg, ok := hexToArgb(s.BgColor)
	if !ok {
		bg, _ = hexToArgb(defaultStatusBg)
	}
	font := waE2E.ExtendedTextMessage_FontType(s.Font)
	if _, known := waE2E.ExtendedTextMessage_FontType_name[int32(font)]; !known {
		font = waE2E.ExtendedTextMessage_SYSTEM
	}
	return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text: proto.String(s.Text), BackgroundArgb: proto.Uint32(bg), TextArgb: proto.Uint32(0xFFFFFFFF),
		Font: font.Enum(),
	}}, nil
}

// PostStatus publica un estado de texto, foto o vídeo. La audiencia por estado no se puede fijar
// con la versión instalada de whatsmeow (StatusAudience = false): se usa la privacidad del teléfono.
func (a *Adapter) PostStatus(ctx context.Context, s model.OutgoingStatus) (model.Status, error) {
	cli, err := a.live()
	if err != nil {
		return model.Status{}, err
	}
	var msg *waE2E.Message
	switch s.Kind {
	case model.KindText:
		msg, err = buildStatusText(s)
	case model.KindImage, model.KindVideo:
		var pf preparedFile
		pf, err = readFile(s.Kind, model.Outgoing{Kind: s.Kind, FilePath: s.FilePath, Mime: s.Mime})
		if err != nil {
			break
		}
		mt, _ := mediaTypeFor(s.Kind)
		up, uerr := cli.Upload(ctx, pf.data, mt)
		if uerr != nil {
			return model.Status{}, a.mapErr("subir el archivo del estado", uerr)
		}
		pf.up = up
		msg, err = buildMediaMessage(s.Kind, pf, s.Text, nil)
	default:
		err = fmt.Errorf("whatsapp: un estado solo puede ser texto, foto o vídeo: %w", port.ErrInvalid)
	}
	if err != nil {
		return model.Status{}, err
	}
	resp, err := cli.SendMessage(ctx, types.StatusBroadcastJID, msg)
	if err != nil {
		return model.Status{}, a.mapErr("publicar el estado", err)
	}
	c := a.conv()
	ts := resp.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	info := types.MessageInfo{
		MessageSource: types.MessageSource{Chat: types.StatusBroadcastJID, Sender: c.self, IsFromMe: true},
		ID:            resp.ID, Timestamp: ts,
	}
	st, ok := c.status(info, msg)
	if !ok {
		return model.Status{}, fmt.Errorf("whatsapp: el estado se publicó, pero no se pudo leer su resultado: %w", port.ErrInvalid)
	}
	if st.Media != nil && s.FilePath != "" {
		st.Media.LocalPath = s.FilePath
	}
	return st, nil
}

// DeleteStatus borra un estado propio para todos.
func (a *Adapter) DeleteStatus(ctx context.Context, statusNativeID string) error {
	cli, err := a.live()
	if err != nil {
		return err
	}
	if statusNativeID == "" {
		return fmt.Errorf("whatsapp: falta el id del estado: %w", port.ErrInvalid)
	}
	revoke := cli.BuildRevoke(types.StatusBroadcastJID, types.EmptyJID, statusNativeID)
	if _, err := cli.SendMessage(ctx, types.StatusBroadcastJID, revoke); err != nil {
		return a.mapErr("borrar el estado", err)
	}
	return nil
}

// MarkStatusViewed avisa a quien lo publicó que lo vimos.
func (a *Adapter) MarkStatusViewed(ctx context.Context, st model.Status) error {
	cli, err := a.live()
	if err != nil {
		return err
	}
	if st.NativeID == "" || st.AuthorNativeID == "" {
		return fmt.Errorf("whatsapp: faltan datos del estado: %w", port.ErrInvalid)
	}
	author, err := nativeToJID(st.AuthorNativeID)
	if err != nil {
		return err
	}
	if err := cli.MarkRead(ctx, []string{st.NativeID}, time.Now(), types.StatusBroadcastJID, a.wireJID(author)); err != nil {
		return a.mapErr("marcar el estado como visto", err)
	}
	return nil
}

// ReactStatus y StatusViewers no se implementan (StatusReact = false; las vistas llegan como
// eventos status.viewed y el servicio las lee de la base): ambos salen de port.Base como
// ErrUnsupported.
