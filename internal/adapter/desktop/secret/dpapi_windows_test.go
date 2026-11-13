//go:build windows

package secret

import (
	"bytes"
	"testing"
)

func TestDPAPIRoundTrip(t *testing.T) {
	p := Default()
	plain := []byte("clave-de-prueba-ñandú-123")
	enc, err := p.Protect(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(enc, plain) || bytes.Contains(enc, []byte("prueba")) {
		t.Fatal("el cifrado dejó la clave en claro")
	}
	dec, err := p.Unprotect(enc)
	if err != nil || !bytes.Equal(dec, plain) {
		t.Fatalf("ida y vuelta: %q %v", dec, err)
	}
	enc[len(enc)/2] ^= 0xFF
	if _, err := p.Unprotect(enc); err == nil {
		t.Fatal("un bloque alterado debe fallar")
	}
	if _, err := p.Protect(nil); err == nil {
		t.Fatal("vacío debe fallar")
	}
}
