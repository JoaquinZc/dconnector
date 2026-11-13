package service

import (
	"context"
	"testing"

	"dconnector/internal/core/model"
)

// Un contacto automático tiene identidad: entra en una campaña como cualquier otro (no es un
// «contacto sin identidad» que se omita) y recibe el mensaje.
func TestCampaignSendsToAutoContact(t *testing.T) {
	e := newCampEnv(t)
	ctx := context.Background()
	acc := e.newAccount(model.ChannelWhatsApp, "")
	e.seedChat(acc.ID, peerOf("Eva"), "Eva", 1000)
	e.seedMessage(acc.ID, peerOf("Eva"), "in1", 1000, false, "hola")

	contacts := NewContacts(e.testEnv.app, e.accounts)
	n, err := contacts.SyncAccount(ctx, acc, e.store(acc.ID),
		[]model.Person{{NativeID: peerOf("Eva"), Name: "Eva", Phone: "+593991112233"}}, nil)
	if err != nil || n != 1 {
		t.Fatalf("creados %d, err %v", n, err)
	}
	ct, err := e.testEnv.app.FindContactByIdentity(ctx, acc.ID, peerOf("Eva"))
	if err != nil || ct.Source != model.ContactSourceAuto {
		t.Fatalf("contacto: %+v %v", ct, err)
	}

	cp := e.save(acc.ID, nil, ct)
	review, err := e.c.Review(ctx, cp)
	if err != nil {
		t.Fatal(err)
	}
	if review.Recipients != 1 || review.Skipped["noIdentity"] != 0 {
		t.Fatalf("revisión: %+v", review)
	}
	e.launch(cp.ID)
	e.tick(acc.ID)
	if got := e.sentTexts(acc.ID); len(got) != 1 || got[0] != "Hola Eva, tenemos algo para ti" {
		t.Fatalf("enviados: %q", got)
	}
	// Con la baja marcada, un contacto automático tampoco recibe campañas.
	ct.OptedOut = true
	if err := e.testEnv.app.SaveContact(ctx, ct); err != nil {
		t.Fatal(err)
	}
	if _, err := contacts.SyncAccount(ctx, acc, e.store(acc.ID),
		[]model.Person{{NativeID: peerOf("Eva"), Name: "Eva", Phone: "+593991112233"}}, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.testEnv.app.GetContact(ctx, ct.ID); !got.OptedOut {
		t.Fatal("la sincronización quitó la baja")
	}
}
