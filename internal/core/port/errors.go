package port

import "errors"

var (
	ErrUnsupported  = errors.New("no admitido por este canal")
	ErrNotConnected = errors.New("la cuenta no está conectada")
	ErrNotFound     = errors.New("no encontrado")
	ErrInvalid      = errors.New("datos no válidos")
	ErrRateLimited  = errors.New("límite de envío alcanzado")
)
