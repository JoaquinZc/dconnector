package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// Destinations: CRUD de destinos del protocolo (06 §11). El estado en vivo lo da ws.Manager.
type Destinations struct {
	app port.AppStore

	mu       sync.Mutex
	onChange func()
}

func NewDestinations(app port.AppStore) *Destinations { return &Destinations{app: app} }

func (d *Destinations) List(ctx context.Context) ([]model.Destination, error) {
	return d.app.ListDestinations(ctx)
}

// ValidateDestinationURL aplica la regla P1 (y 10 §1): wss:// obligatorio, salvo ws://localhost,
// ws://127.0.0.1 y ws://[::1].
func ValidateDestinationURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("%w: falta la dirección del destino", port.ErrInvalid)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%w: la dirección del destino no es válida", port.ErrInvalid)
	}
	switch strings.ToLower(u.Scheme) {
	case "wss":
		return nil
	case "ws":
		host := strings.ToLower(u.Hostname())
		if host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return nil
		}
		return fmt.Errorf("%w: la dirección debe empezar por wss:// (ws:// solo se admite con localhost, 127.0.0.1 o [::1])", port.ErrInvalid)
	}
	return fmt.Errorf("%w: la dirección debe empezar por wss://", port.ErrInvalid)
}

func newDestinationToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Save valida la URL, genera ID y Token si faltan (y conserva el token y la fecha de alta al
// editar un destino que ya existe) y avisa a OnChange.
func (d *Destinations) Save(ctx context.Context, dst model.Destination) (model.Destination, error) {
	dst.URL = strings.TrimSpace(dst.URL)
	if err := ValidateDestinationURL(dst.URL); err != nil {
		return model.Destination{}, err
	}
	scope, err := d.cleanContactScope(ctx, dst.Filter.ContactScope)
	if err != nil {
		return model.Destination{}, err
	}
	dst.Filter.ContactScope = scope
	dst.Name = strings.TrimSpace(dst.Name)
	if dst.Name == "" {
		if u, err := url.Parse(dst.URL); err == nil {
			dst.Name = u.Host
		}
	}
	if dst.ID == "" {
		dst.ID = model.NewID()
	} else if dst.Token == "" || dst.CreatedAt == 0 {
		// edición: no rotar el token si la interfaz no lo manda
		if all, err := d.app.ListDestinations(ctx); err == nil {
			for _, old := range all {
				if old.ID != dst.ID {
					continue
				}
				if dst.Token == "" {
					dst.Token = old.Token
				}
				if dst.CreatedAt == 0 {
					dst.CreatedAt = old.CreatedAt
				}
				break
			}
		}
	}
	if dst.Token == "" {
		tok, err := newDestinationToken()
		if err != nil {
			return model.Destination{}, fmt.Errorf("destinos: generar token: %w", err)
		}
		dst.Token = tok
	}
	if dst.CreatedAt == 0 {
		dst.CreatedAt = time.Now().UnixMilli()
	}
	if err := d.app.SaveDestination(ctx, dst); err != nil {
		return model.Destination{}, err
	}
	d.notify()
	return dst, nil
}

// maxScopeLists: tope de listas en cada lado (incluidas y excluidas) de un alcance de contactos.
const maxScopeLists = 100

// cleanContactScope valida y normaliza el alcance de contactos de un destino:
//   - el modo es «all» o «lists» (vacío = «all»);
//   - en modo «all» la lista de incluidas no cuenta y se vacía;
//   - los ids se recortan, se quitan los repetidos y los de listas que ya no existen (se descartan
//     sin error: nadie está en una lista borrada);
//   - «lists» sin ninguna lista incluida (que exista) es un error: no dejaría pasar a nadie;
//   - una lista no puede estar a la vez incluida y excluida;
//   - hay un máximo de listas por lado.
func (d *Destinations) cleanContactScope(ctx context.Context, in model.ContactScope) (model.ContactScope, error) {
	out := model.ContactScope{Mode: strings.TrimSpace(in.Mode), IncludeListIDs: []string{}, ExcludeListIDs: []string{}}
	switch out.Mode {
	case "", model.ContactScopeAll:
		out.Mode = model.ContactScopeAll
	case model.ContactScopeLists:
	default:
		return model.ContactScope{}, fmt.Errorf("%w: el alcance de contactos debe ser «todos» o «solo estas listas»", port.ErrInvalid)
	}
	include, exclude := dedupeIDs(in.IncludeListIDs), dedupeIDs(in.ExcludeListIDs)
	if out.Mode == model.ContactScopeAll {
		include = nil
	}
	if len(include) > maxScopeLists || len(exclude) > maxScopeLists {
		return model.ContactScope{}, fmt.Errorf("%w: como máximo %d listas por lado en el alcance de contactos", port.ErrInvalid, maxScopeLists)
	}
	if len(include) > 0 || len(exclude) > 0 {
		lists, err := d.app.ListLists(ctx)
		if err != nil {
			return model.ContactScope{}, err
		}
		exist := make(map[string]struct{}, len(lists))
		for _, l := range lists {
			exist[l.ID] = struct{}{}
		}
		include, exclude = keepExisting(include, exist), keepExisting(exclude, exist)
	}
	if out.Mode == model.ContactScopeLists && len(include) == 0 {
		return model.ContactScope{}, fmt.Errorf("%w: elige al menos una lista para recibir solo de ciertas listas", port.ErrInvalid)
	}
	for _, id := range include {
		for _, ex := range exclude {
			if id == ex {
				return model.ContactScope{}, fmt.Errorf("%w: una lista no puede estar a la vez en «incluir» y en «excluir»", port.ErrInvalid)
			}
		}
	}
	if len(include) > 0 {
		out.IncludeListIDs = include
	}
	if len(exclude) > 0 {
		out.ExcludeListIDs = exclude
	}
	return out, nil
}

func dedupeIDs(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, id := range in {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func keepExisting(ids []string, exist map[string]struct{}) []string {
	out := ids[:0:0]
	for _, id := range ids {
		if _, ok := exist[id]; ok {
			out = append(out, id)
		}
	}
	return out
}

func (d *Destinations) Delete(ctx context.Context, id string) error {
	if err := d.app.DeleteDestination(ctx, id); err != nil {
		return err
	}
	d.notify()
	return nil
}

// OnChange registra la función que avisa al manager del protocolo.
func (d *Destinations) OnChange(fn func()) {
	d.mu.Lock()
	d.onChange = fn
	d.mu.Unlock()
}

func (d *Destinations) notify() {
	d.mu.Lock()
	fn := d.onChange
	d.mu.Unlock()
	if fn != nil {
		fn()
	}
}
