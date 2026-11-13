// Package tlsconf: TLS del servidor (certificado propio autogenerado o externo) y comprobación de la
// dirección de escucha (docs/14 §8).
package tlsconf

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
)

// Options: ver docs/14 §8.
type Options struct {
	DataDir  string // <data>; el certificado propio vive en <data>/tls/
	Cert     string // certificado externo (PEM), opcional
	Key      string
	NoTLS    bool
	Insecure bool

	// ExtraHosts son nombres o IP adicionales para el SAN del certificado propio (opcional).
	ExtraHosts []string
}

// IsLoopback dice si una dirección «host:puerto» escucha solo en loopback. «localhost» cuenta; una
// dirección vacía (":8080"), 0.0.0.0, :: o cualquier otro nombre, no.
func IsLoopback(addr string) bool {
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if i := strings.IndexByte(host, '%'); i >= 0 {
		host = host[:i]
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// CheckListen aplica las reglas: fuera de loopback exige TLS salvo Insecure; NoTLS solo en loopback o con
// Insecure. Devuelve un error en español, accionable.
func CheckListen(addr string, o Options) error {
	if (o.Cert == "") != (o.Key == "") {
		if o.Cert != "" {
			return errors.New("Indicó --tls-cert pero falta --tls-key: pase el certificado y su clave privada juntos.")
		}
		return errors.New("Indicó --tls-key pero falta --tls-cert: pase el certificado y su clave privada juntos.")
	}
	if o.NoTLS && o.Cert != "" {
		return errors.New("--no-tls no se puede combinar con --tls-cert/--tls-key: quite una de las dos opciones.")
	}
	if IsLoopback(addr) {
		return nil
	}
	if o.NoTLS && !o.Insecure {
		return fmt.Errorf("El servidor escucha en %q, que no es loopback, y --no-tls dejaría las claves y los mensajes sin cifrar. "+
			"Quite --no-tls (el servidor generará un certificado propio), use --tls-cert/--tls-key, "+
			"escuche solo en 127.0.0.1, o añada --insecure si hay un proxy inverso que ya pone TLS.", addr)
	}
	return nil
}

// Fingerprint devuelve la huella SHA-256 (hex en mayúsculas separado por «:») de un certificado.
func Fingerprint(c *x509.Certificate) string {
	sum := sha256.Sum256(c.Raw)
	parts := make([]string, len(sum))
	for i, b := range sum {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, ":")
}

var (
	curMu sync.Mutex
	cur   *source
)

// Load devuelve la configuración TLS y la huella SHA-256 (hex con «:») del certificado hoja.
// Con NoTLS devuelve (nil, "", nil). Con Cert/Key recarga el certificado externo al llamar a Reload.
func Load(o Options) (cfg *tls.Config, fingerprint string, err error) {
	if o.NoTLS {
		return nil, "", nil
	}
	if (o.Cert == "") != (o.Key == "") {
		return nil, "", CheckListen("127.0.0.1:0", o)
	}
	src, err := newSource(o)
	if err != nil {
		return nil, "", err
	}
	curMu.Lock()
	cur = src
	curMu.Unlock()
	cfg = &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: src.getCertificate,
	}
	return cfg, src.fingerprint(), nil
}

// Reload vuelve a leer el certificado externo (SIGHUP). No hace nada con certificado propio.
// Si falla, se conserva el certificado anterior.
func Reload() error {
	curMu.Lock()
	src := cur
	curMu.Unlock()
	if src == nil || !src.external {
		return nil
	}
	return src.reload()
}

// CurrentFingerprint devuelve la huella del certificado en uso ("" si no hay TLS).
func CurrentFingerprint() string {
	curMu.Lock()
	src := cur
	curMu.Unlock()
	if src == nil {
		return ""
	}
	return src.fingerprint()
}
