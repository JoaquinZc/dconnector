//go:build !windows

package secret

type unavailable struct{}

// Default: fuera de Windows no hay almacén seguro.
func Default() Protector { return unavailable{} }

func (unavailable) Protect([]byte) ([]byte, error)   { return nil, ErrUnavailable }
func (unavailable) Unprotect([]byte) ([]byte, error) { return nil, ErrUnavailable }
