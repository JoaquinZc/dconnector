package telegram

// Personas (08 §10).

import (
	"context"
	"fmt"

	"github.com/gotd/td/tg"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// ResolvePerson devuelve los datos de una persona (o el nombre de un grupo o canal).
func (a *Adapter) ResolvePerson(ctx context.Context, peerNativeID string) (model.Person, error) {
	kind, id, ok := parseNative(peerNativeID)
	if !ok {
		return model.Person{}, fmt.Errorf("telegram: identificador de persona no válido: %w", port.ErrInvalid)
	}
	if kind != kindUser {
		name := nameOfNative(a.peers, peerNativeID)
		if name == "" {
			return model.Person{}, fmt.Errorf("telegram: no se conoce ese chat: %w", port.ErrNotFound)
		}
		return model.Person{AccountID: a.acc.ID, NativeID: peerNativeID, Name: name}, nil
	}
	if u, ok := a.peers.User(id); ok && !u.Min {
		return personOf(a.acc.ID, u), nil
	}
	api, err := a.liveAPI()
	if err != nil {
		return model.Person{}, err
	}
	in, err := a.peers.inputUser(ctx, peerNativeID)
	if err != nil {
		if u, ok := a.peers.User(id); ok {
			return personOf(a.acc.ID, u), nil
		}
		return model.Person{}, err
	}
	users, err := api.UsersGetUsers(ctx, []tg.InputUserClass{in})
	if err != nil {
		return model.Person{}, a.mapErr("pedir los datos de la persona", err)
	}
	a.peers.feedLists(ctx, users, nil)
	for _, uc := range users {
		if u, ok := uc.(*tg.User); ok && u.ID == id {
			return personOf(a.acc.ID, u), nil
		}
	}
	return model.Person{}, fmt.Errorf("telegram: no se encontró a esa persona: %w", port.ErrNotFound)
}

// ListPersons devuelve los contactos de la cuenta (nombre, teléfono y @usuario).
func (a *Adapter) ListPersons(ctx context.Context) ([]model.Person, error) {
	api, err := a.liveAPI()
	if err != nil {
		return nil, err
	}
	res, err := api.ContactsGetContacts(ctx, 0)
	if err != nil {
		return nil, a.mapErr("pedir los contactos", err)
	}
	cs, ok := res.(*tg.ContactsContacts)
	if !ok {
		return nil, nil
	}
	a.peers.feedLists(ctx, cs.Users, nil)
	out := make([]model.Person, 0, len(cs.Users))
	for _, uc := range cs.Users {
		u, ok := uc.(*tg.User)
		if !ok || u.Deleted || u.Self {
			continue
		}
		p := personOf(a.acc.ID, u)
		if p.Name == "" { // los usuarios de la lista de contactos son de agenda
			p.Name, p.PushName = p.PushName, ""
		}
		out = append(out, p)
	}
	return out, nil
}
