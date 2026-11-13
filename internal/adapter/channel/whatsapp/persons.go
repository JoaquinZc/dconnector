package whatsapp

// Personas (07 §11): agenda, resolución de una persona y LID → teléfono.

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"go.mau.fi/whatsmeow/types"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// personFor arma la persona completa de un JID con lo que hay en el almacén de contactos y el
// avatar guardado, si lo hay.
func (a *Adapter) personFor(jid types.JID) model.Person {
	jid = jid.ToNonAD()
	ci, _ := a.contactInfo(jid)
	avatar := ""
	if f := a.latestAvatar(jid); f != "" {
		avatar = "/avatar/" + string(a.acc.ID) + "/" + f
	}
	return personFrom(a.acc.ID, jid, ci, avatar)
}

// ResolvePerson: persona de un id nativo (contacto, o grupo con su nombre como Name).
func (a *Adapter) ResolvePerson(ctx context.Context, peerNativeID string) (model.Person, error) {
	jid, err := nativeToJID(peerNativeID)
	if err != nil {
		return model.Person{}, err
	}
	c := a.conv()
	jid = c.lidToPN(jid)
	switch jid.Server {
	case types.GroupServer:
		p := a.personFor(jid)
		p.Name = a.groupTitle(ctx, jid, true)
		return p, nil
	case types.NewsletterServer, types.BroadcastServer:
		p := a.personFor(jid)
		a.mu.Lock()
		p.Name = a.titles[jidToNative(jid)]
		a.mu.Unlock()
		return p, nil
	}
	return a.personFor(jid), nil
}

// ListPersons: la agenda del teléfono (GetAllContacts). Solo contactos de teléfono; los @lid se
// pasan a su teléfono si se conoce y, si no, se omiten.
func (a *Adapter) ListPersons(ctx context.Context) ([]model.Person, error) {
	cli := a.getClient()
	if cli == nil || cli.Store == nil || cli.Store.Contacts == nil {
		return nil, port.ErrNotConnected
	}
	all, err := cli.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		return nil, fmt.Errorf("whatsapp: no se pudo leer la agenda: %w", err)
	}
	c := a.conv()
	byID := map[string]model.Person{}
	for jid, ci := range all {
		pn := c.lidToPN(jid)
		if pn.Server != types.DefaultUserServer || pn.User == "" {
			continue
		}
		p := personFrom(a.acc.ID, pn, ci, "")
		if p.Name == "" && p.PushName == "" {
			continue
		}
		if prev, ok := byID[p.NativeID]; ok {
			if p.Name == "" {
				p.Name = prev.Name
			}
			if p.PushName == "" {
				p.PushName = prev.PushName
			}
		}
		byID[p.NativeID] = p
	}
	out := make([]model.Person, 0, len(byID))
	for _, p := range byID {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		di, dj := strings.ToLower(out[i].Display()), strings.ToLower(out[j].Display())
		if di != dj {
			return di < dj
		}
		return out[i].NativeID < out[j].NativeID
	})
	return out, nil
}
