package whatsapp

import (
	"context"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/types"

	"dconnector/internal/core/port"
)

// ResolvePeer es la interfaz OPCIONAL que usa «Nuevo chat» (service.Inbox.OpenChat, se detecta con una
// aserción de tipo): convierte lo que escribió el usuario en el id nativo de la persona y su nombre.
//   - Un id con «@» (el de un contacto de la libreta, ya conocido) solo se normaliza: sin red.
//   - Un número («+593 99 123 4567», con o sin «+», espacios o guiones) se comprueba con
//     whatsmeow IsOnWhatsApp: si no está en WhatsApp devuelve port.ErrNotFound; si está, devuelve el JID
//     de teléfono (si WhatsApp solo da un @lid y se conoce su teléfono, el teléfono) y un nombre si lo hay.
//
// Necesita la sesión conectada solo para comprobar un número.
func (a *Adapter) ResolvePeer(ctx context.Context, input string) (nativeID, name string, err error) {
	jid, err := nativeToJID(input)
	if err != nil {
		return "", "", fmt.Errorf("whatsapp: «%s» no parece un número de teléfono: %w", strings.TrimSpace(input), port.ErrInvalid)
	}
	if strings.Contains(input, "@") {
		// Id nativo: personas se admiten; grupos, canales y difusiones no son «una persona».
		if jid.Server != types.DefaultUserServer && jid.Server != types.HiddenUserServer {
			return "", "", fmt.Errorf("whatsapp: solo se puede iniciar un chat con una persona: %w", port.ErrInvalid)
		}
		jid = a.conv().lidToPN(jid)
		return jidToNative(jid), a.contactName(jid), nil
	}

	cli, err := a.live()
	if err != nil {
		return "", "", fmt.Errorf("whatsapp: conecta la cuenta para comprobar el número: %w", err)
	}
	cctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	res, err := cli.IsOnWhatsApp(cctx, []string{"+" + jid.User})
	if err != nil {
		return "", "", a.mapErr("comprobar el número", err)
	}
	for _, r := range res {
		if !r.IsIn {
			continue
		}
		found := r.JID
		if !r.PhoneNumber.IsEmpty() && r.PhoneNumber.Server == types.DefaultUserServer {
			found = r.PhoneNumber
		} else if found.Server == types.HiddenUserServer {
			found = a.conv().lidToPN(found)
		}
		if found.IsEmpty() {
			continue
		}
		nm := a.contactName(found)
		if nm == "" && r.VerifiedName != nil && r.VerifiedName.Details != nil {
			nm = r.VerifiedName.Details.GetVerifiedName()
		}
		if nm == "" {
			nm = phoneOf(found)
		}
		return jidToNative(found), nm, nil
	}
	return "", "", fmt.Errorf("whatsapp: el número +%s no está en WhatsApp: %w", jid.User, port.ErrNotFound)
}
