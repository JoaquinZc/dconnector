package ws

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"dconnector/internal/core/model"
)

func randomBytes(t *testing.T, n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMediaGetInChunks(t *testing.T) {
	ts := newTestServer(t, "t")
	e := newEnv(t)
	acc, ad := e.newAccount("Ventas")
	e.addDest(ts, true, allFilter)
	e.start()
	ts.waitHello()

	// Un documento de 1,2 MiB: tres trozos (512 KiB, 512 KiB y el resto).
	data := randomBytes(t, 1<<20+200<<10)
	ad.SetMedia("doc-1", data)
	const chat = "c1"
	ad.Inject(model.Event{Type: model.EvChatUpsert, Chat: directChat(chat, "Ana")})
	msg := incoming(chat, "doc-1", "")
	msg.Kind = model.KindDocument
	msg.Media = &model.Media{ID: "doc-1", Mime: "application/pdf", FileName: "informe.pdf", Size: int64(len(data))}
	ad.Inject(model.Event{Type: model.EvMessageNew, Message: msg})
	ts.waitEvent("message.received", nil)

	rd := ts.command("m1", "media.get", string(acc.ID), map[string]any{"mediaId": "doc-1"})
	if !rd.OK {
		t.Fatalf("media.get: %+v", rd.Error)
	}
	res := jsonOf(t, resultBytes(rd))
	if res["chunks"].(float64) != 3 || res["size"].(float64) != float64(len(data)) ||
		res["fileName"] != "informe.pdf" || res["mime"] != "application/pdf" || res["transferId"] == "" {
		t.Fatalf("result de media.get: %v", res)
	}

	waitFor(t, "3 trozos", func() bool { return len(ts.eventsNamed("media.chunk")) == 3 })
	var got []byte
	for i, r := range ts.eventsNamed("media.chunk") {
		d := jsonOf(t, r.f.Data)
		if d["transferId"] != res["transferId"] || int(d["seq"].(float64)) != i || d["last"] != (i == 2) {
			t.Fatalf("trozo %d: %v", i, d)
		}
		raw, err := base64.StdEncoding.DecodeString(d["b64"].(string))
		if err != nil {
			t.Fatal(err)
		}
		if i < 2 && len(raw) != 512<<10 {
			t.Fatalf("trozo %d de %d bytes", i, len(raw))
		}
		got = append(got, raw...)
		if r.f.Kind != kindEvent || r.f.Name != "media.chunk" {
			t.Fatalf("los trozos van como evento media.chunk: %+v", r.f)
		}
	}
	if !bytes.Equal(got, data) {
		t.Fatal("el archivo reensamblado no coincide")
	}
	// Los trozos no pasan por el outbox ni piden ack.
	time.Sleep(100 * time.Millisecond)
	for _, d := range e.mgr.States() {
		if d.Pending != 0 {
			t.Fatalf("pendientes: %+v", d)
		}
	}

	// Un medio inexistente.
	rd = ts.command("m2", "media.get", string(acc.ID), map[string]any{"mediaId": "nada"})
	if rd.OK || rd.Error.Code != "not_found" {
		t.Fatalf("medio inexistente: %+v", rd)
	}
}

func TestMediaUploadThenSend(t *testing.T) {
	ts := newTestServer(t, "t")
	e := newEnv(t)
	acc, ad := e.newAccount("Ventas")
	e.addDest(ts, true, allFilter)
	e.start()
	ts.waitHello()

	data := randomBytes(t, 100_000)
	rd := ts.command("u1", "media.upload", "", map[string]any{"fileName": "foto.jpg", "mime": "image/jpeg", "size": len(data)})
	if !rd.OK {
		t.Fatalf("media.upload: %+v", rd.Error)
	}
	uploadID := jsonOf(t, resultBytes(rd))["uploadId"].(string)
	if uploadID == "" {
		t.Fatal("falta uploadId")
	}

	chunk := func(id string, seq int, last bool, part []byte) map[string]any {
		return map[string]any{
			"v": 1, "id": id, "kind": "command", "name": "media.chunk",
			"data": map[string]any{"uploadId": uploadID, "seq": seq, "last": last, "b64": base64.StdEncoding.EncodeToString(part)},
		}
	}
	ts.send(chunk("k0", 0, false, data[:40_000]))
	ts.send(chunk("k1", 1, false, data[40_000:70_000]))
	ts.send(chunk("k2", 2, true, data[70_000:]))
	// Solo el último trozo recibe result.
	last := ts.waitResult("k2")
	if !last.OK {
		t.Fatalf("último trozo: %+v", last.Error)
	}
	for _, r := range ts.snapshot() {
		if r.f.Kind == kindResult && (r.f.ID == "k0" || r.f.ID == "k1") {
			t.Fatalf("los trozos intermedios no reciben result: %+v", r.f)
		}
	}

	// Enviar el archivo subido.
	rd = ts.command("s1", "message.send", string(acc.ID), map[string]any{
		"chatId": "c@s.whatsapp.net", "message": map[string]any{"kind": "image", "text": "pie", "uploadId": uploadID},
	})
	if !rd.OK {
		t.Fatalf("message.send con uploadId: %+v", rd.Error)
	}
	sent := ad.Sent()
	if len(sent) != 1 || sent[0].Kind != model.KindImage || sent[0].FileName != "foto.jpg" || sent[0].Mime != "image/jpeg" {
		t.Fatalf("envío: %+v", sent)
	}
	onDisk, err := os.ReadFile(sent[0].FilePath)
	if err != nil || !bytes.Equal(onDisk, data) {
		t.Fatalf("el archivo local no coincide (%v)", err)
	}
}

func TestMediaUploadErrors(t *testing.T) {
	ts := newTestServer(t, "t")
	e := newEnv(t)
	e.newAccount("Ventas")
	e.addDest(ts, true, allFilter)
	e.start()
	ts.waitHello()

	// Tamaño excesivo.
	rd := ts.command("a", "media.upload", "", map[string]any{"fileName": "x", "size": 101 << 20})
	if rd.OK || rd.Error.Code != "invalid" {
		t.Fatalf("subida de más de 100 MB: %+v", rd)
	}
	// Trozo fuera de orden: la subida se descarta.
	rd = ts.command("b", "media.upload", "", map[string]any{"fileName": "x", "size": 10})
	id := jsonOf(t, resultBytes(rd))["uploadId"].(string)
	ts.send(map[string]any{"v": 1, "id": "bad", "kind": "command", "name": "media.chunk",
		"data": map[string]any{"uploadId": id, "seq": 3, "last": false, "b64": "AAAA"}})
	if r := ts.waitResult("bad"); r.OK || r.Error.Code != "invalid" {
		t.Fatalf("trozo fuera de orden: %+v", r)
	}
	// uploadId desconocido.
	ts.send(map[string]any{"v": 1, "id": "ghost", "kind": "command", "name": "media.chunk",
		"data": map[string]any{"uploadId": "nada", "seq": 0, "last": true, "b64": ""}})
	if r := ts.waitResult("ghost"); r.OK || r.Error.Code != "invalid" {
		t.Fatalf("uploadId desconocido: %+v", r)
	}
	// Tamaño distinto del anunciado.
	rd = ts.command("c", "media.upload", "", map[string]any{"fileName": "x", "size": 10})
	id = jsonOf(t, resultBytes(rd))["uploadId"].(string)
	ts.send(map[string]any{"v": 1, "id": "short", "kind": "command", "name": "media.chunk",
		"data": map[string]any{"uploadId": id, "seq": 0, "last": true, "b64": base64.StdEncoding.EncodeToString([]byte("abc"))}})
	if r := ts.waitResult("short"); r.OK || r.Error.Code != "invalid" {
		t.Fatalf("subida incompleta: %+v", r)
	}
}

func TestMediaUploadExpires(t *testing.T) {
	ts := newTestServer(t, "t")
	e := newEnv(t)
	e.newAccount("Ventas")
	e.addDest(ts, true, allFilter)
	e.start()
	ts.waitHello()
	rd := ts.command("a", "media.upload", "", map[string]any{"fileName": "x", "size": 3})
	id := jsonOf(t, resultBytes(rd))["uploadId"].(string)
	// Se envejece la subida 11 minutos.
	e.mgr.umu.Lock()
	e.mgr.uploads[id].created = time.Now().Add(-11 * time.Minute)
	path := e.mgr.uploads[id].path
	e.mgr.umu.Unlock()
	ts.send(map[string]any{"v": 1, "id": "late", "kind": "command", "name": "media.chunk",
		"data": map[string]any{"uploadId": id, "seq": 0, "last": true, "b64": "YWJj"}})
	if r := ts.waitResult("late"); r.OK || r.Error.Code != "invalid" {
		t.Fatalf("un uploadId caducado no vale: %+v", r)
	}
	e.mgr.sweepUploads()
	if _, err := os.Stat(path); err == nil {
		// el trozo tardío ya borró la subida; sweep no debe fallar si no existe
		t.Fatal("el archivo de la subida debía borrarse")
	}
}

func TestSendDownloadsFileURL(t *testing.T) {
	content := randomBytes(t, 30_000)
	files := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write(content)
	}))
	defer files.Close()

	ts := newTestServer(t, "t")
	e := newEnv(t)
	acc, ad := e.newAccount("Ventas")
	e.mgr.httpClient = files.Client()
	e.addDest(ts, true, allFilter)
	e.start()
	ts.waitHello()

	rd := ts.command("d1", "message.send", string(acc.ID), map[string]any{
		"chatId": "c@s.whatsapp.net", "message": map[string]any{"kind": "document", "fileUrl": files.URL + "/docs/contrato.pdf"},
	})
	if !rd.OK {
		t.Fatalf("message.send con fileUrl: %+v", rd.Error)
	}
	sent := ad.Sent()
	if len(sent) != 1 || sent[0].FilePath == "" || sent[0].Mime != "application/pdf" || sent[0].FileName != "contrato.pdf" {
		t.Fatalf("envío: %+v", sent)
	}
	// Lo descargado vive en tmp/ solo mientras dura el envío.
	if _, err := os.Stat(sent[0].FilePath); err == nil {
		t.Fatalf("el archivo descargado debía borrarse tras el envío: %s", sent[0].FilePath)
	}

	// Por encima del máximo de la capacidad: invalid.
	caps := ad.Capabilities(nil)
	caps.MaxFileSize = 1000
	ad.SetCapabilities(caps)
	rd = ts.command("d2", "message.send", string(acc.ID), map[string]any{
		"chatId": "c@s.whatsapp.net", "message": map[string]any{"kind": "document", "fileUrl": files.URL + "/docs/contrato.pdf"},
	})
	if rd.OK || rd.Error.Code != "invalid" {
		t.Fatalf("archivo demasiado grande: %+v", rd)
	}
}
