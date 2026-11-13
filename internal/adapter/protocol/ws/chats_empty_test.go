package ws

import (
	"testing"

	"dconnector/internal/core/model"
)

// chats.list: por defecto solo chats con actividad; includeEmpty añade los directos vacíos (contactos
// de la libreta que el canal lista como conversaciones).
func TestChatsListIncludeEmpty(t *testing.T) {
	x, ts := newScopeFixture(t)
	e := x.e
	e.addDest(ts, true, scope("all", nil, nil))
	e.start()
	ts.waitHello()
	account := string(x.acc.ID)

	st, err := e.accounts.Store(x.acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertChat(x.ctx, model.Chat{NativeID: "c-vacio", Kind: model.ChatDirect, Title: "Vacío", PeerNativeID: "c-vacio"}); err != nil {
		t.Fatal(err)
	}

	rd := ts.command("e1", "chats.list", account, map[string]any{"limit": 100})
	if !rd.OK {
		t.Fatalf("chats.list: %+v", rd.Error)
	}
	if got := listChatIDs(t, rd); got["c-vacio"] || !got["c-ana"] {
		t.Fatalf("por defecto solo con actividad: %v", got)
	}

	rd = ts.command("e2", "chats.list", account, map[string]any{"limit": 100, "includeEmpty": true})
	if !rd.OK {
		t.Fatalf("chats.list includeEmpty: %+v", rd.Error)
	}
	if got := listChatIDs(t, rd); !got["c-vacio"] || !got["c-ana"] {
		t.Fatalf("includeEmpty debe traer también el vacío: %v", got)
	}
}
