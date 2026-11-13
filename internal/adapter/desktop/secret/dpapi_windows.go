//go:build windows

package secret

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// entropy ata el cifrado a dConnector (otra aplicación del mismo usuario no lo abre por descuido).
var entropy = []byte("dConnector.desktop.remote-password.v1")

type dpapi struct{}

// Default devuelve el protector del sistema: DPAPI (CryptProtectData, ámbito del usuario actual).
func Default() Protector { return dpapi{} }

const cryptProtectUIForbidden = 0x1

func blob(b []byte) *windows.DataBlob {
	if len(b) == 0 {
		return &windows.DataBlob{}
	}
	return &windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
}

func (dpapi) Protect(plain []byte) ([]byte, error) {
	if len(plain) == 0 {
		return nil, errors.New("no hay nada que proteger")
	}
	var out windows.DataBlob
	if err := windows.CryptProtectData(blob(plain), nil, blob(entropy), 0, nil, cryptProtectUIForbidden, &out); err != nil {
		return nil, fmt.Errorf("no se pudo proteger la clave con Windows: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}

func (dpapi) Unprotect(b []byte) ([]byte, error) {
	if len(b) == 0 {
		return nil, errors.New("la clave guardada está vacía")
	}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(blob(b), nil, blob(entropy), 0, nil, cryptProtectUIForbidden, &out); err != nil {
		return nil, fmt.Errorf("no se pudo recuperar la clave guardada (¿otro usuario o equipo?): %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}
