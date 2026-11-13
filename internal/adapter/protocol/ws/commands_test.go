package ws

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

func TestCommandsRequireAllowCommands(t *testing.T) {
	ts := newTestServer(t, "t")
	e := newEnv(t)
	acc, ad := e.newAccount("Ventas")
	e.addDest(ts, false, allFilter) // «Permitir comandos» apagado
	e.start()
	ts.waitHello()

	for i, name := range []string{"accounts.list", "message.send", "media.get", "media.upload"} {
		rd := ts.command(fmt.Sprintf("c%d", i), name, string(acc.ID), map[string]any{
			"chatId": "x", "message": map[string]any{"kind": "text", "text": "no debe salir"},
		})
		if rd.OK || rd.Error == nil || rd.Error.Code != "forbidden" {
			t.Fatalf("%s sin permiso debe dar forbidden: %+v", name, rd)
		}
	}
	// Tampoco un trozo de subida.
	rd := ts.command("chunk", "media.chunk", "", map[string]any{"uploadId": "x", "seq": 0, "last": true, "b64": ""})
	if rd.OK || rd.Error.Code != "forbidden" {
		t.Fatalf("media.chunk sin permiso: %+v", rd)
	}
	if len(ad.Sent()) != 0 {
		t.Fatal("un destino sin permiso no puede enviar nada")
	}
}

func TestCommandsWithPermission(t *testing.T) {
	ts := newTestServer(t, "t")
	e := newEnv(t)
	acc, ad := e.newAccount("Ventas")
	other, _ := e.newAccount("Otra")
	e.addDest(ts, true, model.DestinationFilter{AccountIDs: []model.AccountID{acc.ID}, IncludeFromMe: true, IncludeGroups: true})
	e.start()
	ts.waitHello()
	const chat = "5939@s.whatsapp.net"
	account := string(acc.ID)

	// accounts.list: solo la del filtro, sin credenciales ni QR.
	rd := ts.command("l1", "accounts.list", "", nil)
	if !rd.OK {
		t.Fatalf("accounts.list: %+v", rd.Error)
	}
	var list []map[string]any
	mustUnmarshal(t, resultBytes(rd), &list)
	if len(list) != 1 || list[0]["account"].(map[string]any)["id"] != account {
		t.Fatalf("accounts.list: %v", list)
	}
	if list[0]["account"].(map[string]any)["config"] != nil {
		t.Fatalf("la configuración de la cuenta (credenciales) no puede salir: %v", list[0]["account"])
	}
	if list[0]["capabilities"].(map[string]any)["sendText"] != true || list[0]["status"].(map[string]any)["state"] != "connected" {
		t.Fatalf("accounts.list sin capabilities/status: %v", list[0])
	}

	// Una cuenta fuera del filtro: forbidden.
	rd = ts.command("o1", "message.send", string(other.ID), map[string]any{
		"chatId": chat, "message": map[string]any{"kind": "text", "text": "hola"},
	})
	if rd.OK || rd.Error.Code != "forbidden" {
		t.Fatalf("cuenta fuera del filtro: %+v", rd)
	}
	// Falta la cuenta.
	rd = ts.command("o2", "message.send", "", map[string]any{
		"chatId": chat, "message": map[string]any{"kind": "text", "text": "hola"},
	})
	if rd.OK || rd.Error.Code != "invalid" {
		t.Fatalf("sin account.id: %+v", rd)
	}

	// message.send y messages.list / chats.list.
	rd = ts.command("s1", "message.send", account, map[string]any{
		"chatId": chat, "message": map[string]any{"kind": "text", "text": "uno"},
	})
	if !rd.OK {
		t.Fatalf("message.send: %+v", rd.Error)
	}
	sent := jsonOf(t, resultBytes(rd))
	nativeID := sent["nativeId"].(string)
	waitFor(t, "mensaje guardado", func() bool {
		r := ts.command(fmt.Sprintf("ml-%d", len(ts.snapshot())), "messages.list", account, map[string]any{"chatId": chat, "limit": 10})
		if !r.OK {
			t.Fatalf("messages.list: %+v", r.Error)
		}
		return len(jsonOf(t, resultBytes(r))["messages"].([]any)) == 1
	})
	waitFor(t, "chat guardado", func() bool {
		r := ts.command(fmt.Sprintf("cl-%d", len(ts.snapshot())), "chats.list", "", map[string]any{})
		if !r.OK {
			t.Fatalf("chats.list: %+v", r.Error)
		}
		var chats []map[string]any
		mustUnmarshal(t, resultBytes(r), &chats)
		return len(chats) == 1 && chats[0]["nativeId"] == chat
	})

	// message.reply: busca el mensaje y lo pone como ReplyTo.
	rd = ts.command("r1", "message.reply", account, map[string]any{
		"chatId": chat, "replyTo": nativeID, "message": map[string]any{"kind": "text", "text": "respuesta"},
	})
	if !rd.OK {
		t.Fatalf("message.reply: %+v", rd.Error)
	}
	last := ad.Sent()[len(ad.Sent())-1]
	if last.ReplyTo == nil || last.ReplyTo.NativeID != nativeID {
		t.Fatalf("ReplyTo: %+v", last.ReplyTo)
	}
	rd = ts.command("r2", "message.reply", account, map[string]any{
		"chatId": chat, "replyTo": "no-existe", "message": map[string]any{"kind": "text", "text": "x"},
	})
	if rd.OK || rd.Error.Code != "not_found" {
		t.Fatalf("reply a un mensaje inexistente: %+v", rd)
	}

	// Reacciones, edición, borrado, reenvío, leído y escribiendo.
	for i, c := range []struct {
		name string
		data map[string]any
		call string
	}{
		{"message.react", map[string]any{"chatId": chat, "messageId": nativeID, "emoji": "👍"}, "React:" + nativeID + ":👍"},
		{"message.edit", map[string]any{"chatId": chat, "messageId": nativeID, "text": "uno editado"}, "Edit:" + nativeID + ":uno editado"},
		{"message.forward", map[string]any{"chatId": chat, "messageId": nativeID, "toChatId": "otro@s.whatsapp.net"}, "Forward:" + nativeID + ":otro@s.whatsapp.net"},
		{"chat.typing", map[string]any{"chatId": chat, "typing": true}, "SetTyping:" + chat + ":true"},
		{"message.delete", map[string]any{"chatId": chat, "messageId": nativeID, "forEveryone": true}, "Delete:" + nativeID + ":true"},
	} {
		rd := ts.command(fmt.Sprintf("x%d", i), c.name, account, c.data)
		if !rd.OK {
			t.Fatalf("%s: %+v", c.name, rd.Error)
		}
		found := false
		for _, call := range ad.Calls() {
			if call == c.call {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: el adaptador no recibió %q (llamadas: %v)", c.name, c.call, ad.Calls())
		}
	}
	// El chat se acepta también como clave «cuenta|chat».
	rd = ts.command("k1", "chat.typing", "", map[string]any{"chatId": account + "|" + chat, "typing": false})
	if !rd.OK {
		t.Fatalf("chat.typing con clave: %+v", rd.Error)
	}

	// Comando desconocido y datos rotos.
	rd = ts.command("u1", "no.existe", account, nil)
	if rd.OK || rd.Error.Code != "invalid" {
		t.Fatalf("comando desconocido: %+v", rd)
	}
	rd = ts.command("u2", "message.send", account, "esto no es un objeto")
	if rd.OK || rd.Error.Code != "invalid" {
		t.Fatalf("datos rotos: %+v", rd)
	}
	rd = ts.command("u3", "message.send", account, map[string]any{"message": map[string]any{"kind": "text", "text": "x"}})
	if rd.OK || rd.Error.Code != "invalid" {
		t.Fatalf("sin chatId: %+v", rd)
	}
}

func TestCommandErrorsAreTranslated(t *testing.T) {
	ts := newTestServer(t, "t")
	e := newEnv(t)
	acc, ad := e.newAccount("Ventas")
	e.addDest(ts, true, allFilter)
	e.start()
	ts.waitHello()
	send := func(id string) *wireErrorOrNil {
		rd := ts.command(id, "message.send", string(acc.ID), map[string]any{
			"chatId": "c@s.whatsapp.net", "message": map[string]any{"kind": "text", "text": "hola"},
		})
		if rd.OK {
			return &wireErrorOrNil{ok: true}
		}
		return &wireErrorOrNil{code: rd.Error.Code, msg: rd.Error.Message}
	}

	for i, c := range []struct {
		err  error
		code string
	}{
		{fmt.Errorf("whatsapp: x: %w", port.ErrNotConnected), "not_connected"},
		{fmt.Errorf("whatsapp: x: %w", port.ErrRateLimited), "rate_limited"},
		{fmt.Errorf("whatsapp: x: %w", port.ErrNotFound), "not_found"},
		{fmt.Errorf("whatsapp: x: %w", port.ErrInvalid), "invalid"},
		{fmt.Errorf("whatsapp: x: %w", port.ErrUnsupported), "unsupported"},
		{errors.New("algo raro"), "internal"},
	} {
		ad.SendErr = c.err
		got := send(fmt.Sprintf("e%d", i))
		if got.ok || got.code != c.code || got.msg == "" {
			t.Fatalf("%v debía dar %q y dio %+v", c.err, c.code, got)
		}
	}
	ad.SendErr = nil

	// Una capacidad que la cuenta no tiene: el servicio responde ErrUnsupported → unsupported.
	caps := ad.Capabilities(nil)
	caps.SendPoll = false
	ad.SetCapabilities(caps)
	rd := ts.command("poll", "message.send", string(acc.ID), map[string]any{
		"chatId":  "c@s.whatsapp.net",
		"message": map[string]any{"kind": "poll", "poll": map[string]any{"question": "¿Sí?", "options": []map[string]any{{"name": "a"}, {"name": "b"}}}},
	})
	if rd.OK || rd.Error.Code != "unsupported" {
		t.Fatalf("encuesta sin capacidad: %+v", rd)
	}
}

type wireErrorOrNil struct {
	ok        bool
	code, msg string
}

func TestErrorCodeTable(t *testing.T) {
	for err, want := range map[error]string{
		port.ErrUnsupported:  "unsupported",
		port.ErrNotConnected: "not_connected",
		port.ErrNotFound:     "not_found",
		port.ErrInvalid:      "invalid",
		port.ErrRateLimited:  "rate_limited",
		errForbidden:         "forbidden",
		errors.New("otro"):   "internal",
	} {
		if got := errorCode(fmt.Errorf("envuelto: %w", err)); got != want {
			t.Errorf("%v → %s, quería %s", err, got, want)
		}
	}
}

func TestSendWithFileURLRequiresHTTPS(t *testing.T) {
	ts := newTestServer(t, "t")
	e := newEnv(t)
	acc, ad := e.newAccount("Ventas")
	e.addDest(ts, true, allFilter)
	e.start()
	ts.waitHello()
	// http no vale, y filePath nunca se acepta por el protocolo.
	rd := ts.command("f1", "message.send", string(acc.ID), map[string]any{
		"chatId": "c@s.whatsapp.net", "message": map[string]any{"kind": "document", "fileUrl": "http://ejemplo.com/a.pdf"},
	})
	if rd.OK || rd.Error.Code != "invalid" {
		t.Fatalf("fileUrl http: %+v", rd)
	}
	rd = ts.command("f2", "message.send", string(acc.ID), map[string]any{
		"chatId": "c@s.whatsapp.net", "message": map[string]any{"kind": "document", "filePath": `C:\Windows\win.ini`},
	})
	if rd.OK {
		t.Fatalf("filePath no puede aceptarse por el protocolo: %+v", rd)
	}
	if len(ad.Sent()) != 0 {
		t.Fatal("no debe haberse enviado nada")
	}
}

func mustUnmarshal(t *testing.T, raw []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("JSON no válido: %v: %s", err, raw)
	}
}
