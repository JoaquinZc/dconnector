package service

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

func TestValidateDestinationURL(t *testing.T) {
	ok := []string{
		"wss://servidor.com/ws", "WSS://servidor.com", "wss://1.2.3.4:8443/x",
		"ws://localhost:8080/ws", "ws://127.0.0.1/ws", "ws://LOCALHOST/x", "wss://localhost/x", "ws://[::1]:8787/ws",
	}
	bad := []string{
		"", "   ", "ws://servidor.com/ws", "ws://192.168.1.5:8080", "ws://localhost.evil.com/x",
		"http://localhost/x", "https://servidor.com", "servidor.com", "wss://", "ftp://x.com", "ws://127.0.0.2/x",
	}
	for _, u := range ok {
		if err := ValidateDestinationURL(u); err != nil {
			t.Errorf("%q debería valer: %v", u, err)
		}
	}
	for _, u := range bad {
		if err := ValidateDestinationURL(u); !errors.Is(err, port.ErrInvalid) {
			t.Errorf("%q no debería valer (err=%v)", u, err)
		}
	}
}

func TestDestinationsSave(t *testing.T) {
	app := newMemApp()
	d := NewDestinations(app)
	ctx := context.Background()
	changes := 0
	d.OnChange(func() { changes++ })

	saved, err := d.Save(ctx, model.Destination{URL: " wss://crm.example.com/ws ", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID == "" || len(saved.Token) != 64 || saved.CreatedAt == 0 || saved.Name != "crm.example.com" || saved.URL != "wss://crm.example.com/ws" {
		t.Fatalf("destino: %+v", saved)
	}
	if _, err := hex.DecodeString(saved.Token); err != nil {
		t.Fatalf("el token debe ser hexadecimal: %v", err)
	}
	if changes != 1 {
		t.Fatalf("OnChange = %d", changes)
	}
	other, _ := d.Save(ctx, model.Destination{Name: "Otro", URL: "ws://localhost:9000/ws"})
	if other.Token == saved.Token || other.ID == saved.ID {
		t.Fatal("cada destino lleva su propio ID y token")
	}

	// editar sin mandar token ni fecha: se conservan
	edited, err := d.Save(ctx, model.Destination{ID: saved.ID, Name: "CRM", URL: "wss://crm.example.com/v2"})
	if err != nil {
		t.Fatal(err)
	}
	if edited.Token != saved.Token || edited.CreatedAt != saved.CreatedAt || edited.Name != "CRM" || edited.URL != "wss://crm.example.com/v2" {
		t.Fatalf("edición: %+v (antes %+v)", edited, saved)
	}
	// mandando un token nuevo, se respeta
	rot, _ := d.Save(ctx, model.Destination{ID: saved.ID, URL: edited.URL, Token: "abc"})
	if rot.Token != "abc" {
		t.Fatalf("token explícito: %+v", rot)
	}

	list, err := d.List(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("lista: %+v %v", list, err)
	}
	if changes != 4 {
		t.Fatalf("OnChange = %d, quería 4", changes)
	}
}

func TestDestinationsSaveRejectsInsecureURL(t *testing.T) {
	app := newMemApp()
	d := NewDestinations(app)
	called := false
	d.OnChange(func() { called = true })
	_, err := d.Save(context.Background(), model.Destination{URL: "ws://servidor.com/ws"})
	if !errors.Is(err, port.ErrInvalid) {
		t.Fatalf("ws:// a un servidor remoto: %v", err)
	}
	if called {
		t.Fatal("no debe avisar si no guardó")
	}
	if list, _ := d.List(context.Background()); len(list) != 0 {
		t.Fatalf("no debe guardarse: %+v", list)
	}
}

func TestDestinationsDelete(t *testing.T) {
	app := newMemApp()
	d := NewDestinations(app)
	ctx := context.Background()
	n := 0
	d.OnChange(func() { n++ })
	saved, _ := d.Save(ctx, model.Destination{URL: "wss://a.com/ws"})
	if err := d.Delete(ctx, saved.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := d.List(ctx); len(list) != 0 {
		t.Fatalf("lista: %+v", list)
	}
	if n != 2 {
		t.Fatalf("OnChange = %d, quería 2", n)
	}
	// sin OnChange registrado no hay problema
	d2 := NewDestinations(app)
	if _, err := d2.Save(ctx, model.Destination{URL: "wss://b.com/ws"}); err != nil {
		t.Fatal(err)
	}
}

// listsApp: memApp con listas de contactos fijas (memApp devuelve siempre ninguna).
type listsApp struct {
	*memApp
	lists []model.ContactList
}

func (a *listsApp) ListLists(ctx context.Context) ([]model.ContactList, error) { return a.lists, nil }

func TestDestinationsSaveContactScope(t *testing.T) {
	app := &listsApp{memApp: newMemApp(), lists: []model.ContactList{{ID: "fam", Name: "Familia"}, {ID: "ami", Name: "Amigos"}, {ID: "vip", Name: "VIP"}}}
	d := NewDestinations(app)
	ctx := context.Background()
	save := func(cs model.ContactScope) (model.Destination, error) {
		return d.Save(ctx, model.Destination{URL: "wss://a.com/ws", Filter: model.DestinationFilter{ContactScope: cs}})
	}

	t.Run("sin el campo = todos", func(t *testing.T) {
		got, err := save(model.ContactScope{})
		if err != nil {
			t.Fatal(err)
		}
		cs := got.Filter.ContactScope
		if cs.Mode != "all" || len(cs.IncludeListIDs) != 0 || len(cs.ExcludeListIDs) != 0 || cs.IncludeListIDs == nil || cs.ExcludeListIDs == nil {
			t.Fatalf("alcance: %+v", cs)
		}
	})
	t.Run("todos menos una lista", func(t *testing.T) {
		got, err := save(model.ContactScope{Mode: "all", ExcludeListIDs: []string{"fam", " fam ", "", "ami"}})
		if err != nil {
			t.Fatal(err)
		}
		if cs := got.Filter.ContactScope; len(cs.ExcludeListIDs) != 2 || cs.ExcludeListIDs[0] != "fam" || cs.ExcludeListIDs[1] != "ami" {
			t.Fatalf("alcance: %+v", cs)
		}
	})
	t.Run("en modo todos las incluidas no cuentan", func(t *testing.T) {
		got, err := save(model.ContactScope{Mode: "all", IncludeListIDs: []string{"fam"}, ExcludeListIDs: []string{"fam"}})
		if err != nil {
			t.Fatalf("incluida y excluida en modo «todos» no debe fallar: %v", err)
		}
		if cs := got.Filter.ContactScope; len(cs.IncludeListIDs) != 0 || len(cs.ExcludeListIDs) != 1 {
			t.Fatalf("alcance: %+v", cs)
		}
	})
	t.Run("solo listas", func(t *testing.T) {
		got, err := save(model.ContactScope{Mode: "lists", IncludeListIDs: []string{"vip", "fam"}, ExcludeListIDs: []string{"ami"}})
		if err != nil {
			t.Fatal(err)
		}
		if cs := got.Filter.ContactScope; cs.Mode != "lists" || len(cs.IncludeListIDs) != 2 || len(cs.ExcludeListIDs) != 1 {
			t.Fatalf("alcance: %+v", cs)
		}
	})
	t.Run("las listas que no existen se descartan", func(t *testing.T) {
		got, err := save(model.ContactScope{Mode: "lists", IncludeListIDs: []string{"vip", "borrada"}, ExcludeListIDs: []string{"tambien-borrada"}})
		if err != nil {
			t.Fatal(err)
		}
		if cs := got.Filter.ContactScope; len(cs.IncludeListIDs) != 1 || cs.IncludeListIDs[0] != "vip" || len(cs.ExcludeListIDs) != 0 {
			t.Fatalf("alcance: %+v", cs)
		}
	})
	t.Run("solo listas sin ninguna que exista es un error", func(t *testing.T) {
		for _, in := range [][]string{nil, {}, {"borrada"}, {" "}} {
			if _, err := save(model.ContactScope{Mode: "lists", IncludeListIDs: in}); !errors.Is(err, port.ErrInvalid) {
				t.Fatalf("incluidas %v: %v", in, err)
			}
		}
	})
	t.Run("una lista no puede estar en los dos lados", func(t *testing.T) {
		_, err := save(model.ContactScope{Mode: "lists", IncludeListIDs: []string{"fam", "vip"}, ExcludeListIDs: []string{"fam"}})
		if !errors.Is(err, port.ErrInvalid) {
			t.Fatalf("incluida y excluida: %v", err)
		}
	})
	t.Run("modo desconocido", func(t *testing.T) {
		if _, err := save(model.ContactScope{Mode: "algunos"}); !errors.Is(err, port.ErrInvalid) {
			t.Fatalf("modo desconocido: %v", err)
		}
	})
	t.Run("máximo de listas por lado", func(t *testing.T) {
		many := make([]string, maxScopeLists+1)
		for i := range many {
			many[i] = "l" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		}
		if _, err := save(model.ContactScope{Mode: "all", ExcludeListIDs: many}); !errors.Is(err, port.ErrInvalid) {
			t.Fatalf("demasiadas listas: %v", err)
		}
	})
	t.Run("un rechazo no guarda ni avisa", func(t *testing.T) {
		app2 := &listsApp{memApp: newMemApp()}
		d2 := NewDestinations(app2)
		called := false
		d2.OnChange(func() { called = true })
		if _, err := d2.Save(ctx, model.Destination{URL: "wss://a.com/ws", Filter: model.DestinationFilter{ContactScope: model.ContactScope{Mode: "lists"}}}); err == nil {
			t.Fatal("debía fallar")
		}
		if list, _ := d2.List(ctx); len(list) != 0 || called {
			t.Fatalf("no debe guardarse ni avisar: %+v called=%v", list, called)
		}
	})
}

func TestContactScopeAllows(t *testing.T) {
	cases := []struct {
		name  string
		scope model.ContactScope
		lists []string
		want  bool
	}{
		{"vacío (destino antiguo): todos", model.ContactScope{}, nil, true},
		{"vacío con listas", model.ContactScope{}, []string{"a"}, true},
		{"todos, sin contacto", model.ContactScope{Mode: "all"}, nil, true},
		{"todos menos Familia, de Familia", model.ContactScope{Mode: "all", ExcludeListIDs: []string{"fam"}}, []string{"fam"}, false},
		{"todos menos Familia, de otra", model.ContactScope{Mode: "all", ExcludeListIDs: []string{"fam"}}, []string{"ami"}, true},
		{"todos menos Familia, sin contacto", model.ContactScope{Mode: "all", ExcludeListIDs: []string{"fam"}}, nil, true},
		{"solo VIP, de VIP", model.ContactScope{Mode: "lists", IncludeListIDs: []string{"vip"}}, []string{"vip"}, true},
		{"solo VIP, de otra", model.ContactScope{Mode: "lists", IncludeListIDs: []string{"vip"}}, []string{"ami"}, false},
		{"solo VIP, sin contacto", model.ContactScope{Mode: "lists", IncludeListIDs: []string{"vip"}}, nil, false},
		{"solo listas sin incluidas", model.ContactScope{Mode: "lists"}, []string{"vip"}, false},
		{"incluida y excluida a la vez: gana la exclusión", model.ContactScope{Mode: "lists", IncludeListIDs: []string{"vip"}, ExcludeListIDs: []string{"fam"}}, []string{"vip", "fam"}, false},
		{"modo desconocido no abre", model.ContactScope{Mode: "otro"}, []string{"vip"}, false},
	}
	for _, c := range cases {
		if got := c.scope.Allows(c.lists); got != c.want {
			t.Errorf("%s: Allows = %v, quería %v", c.name, got, c.want)
		}
	}
	if (model.ContactScope{}).Restricts() || !(model.ContactScope{Mode: "lists"}).Restricts() || !(model.ContactScope{ExcludeListIDs: []string{"x"}}).Restricts() {
		t.Fatal("Restricts")
	}
}
