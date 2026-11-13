// Package secret protege claves en disco con lo que ofrece el sistema operativo (DPAPI en Windows): solo el
// mismo usuario de Windows en la misma máquina puede recuperarlas.
package secret

import "errors"

// ErrUnavailable: este sistema no tiene un almacén seguro de claves.
var ErrUnavailable = errors.New("este sistema no tiene un almacén seguro de claves")

// Protector cifra y descifra datos pequeños (claves) para guardarlos en disco.
type Protector interface {
	Protect(plain []byte) ([]byte, error)
	Unprotect(blob []byte) ([]byte, error)
}

// entropy ata el cifrado a dConnector (otra aplicación del mismo usuario no lo abre por descuido).
var entropy = []byte("dConnector.desktop.remote-password.v1")
