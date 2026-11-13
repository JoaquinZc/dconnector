package connection

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strings"
)

// CertError: el certificado del servidor no es de confianza todavía (sin huella fijada) o cambió respecto
// de la fijada. El texto lleva la huella con el formato «Huella SHA-256: AB:CD:…».
type CertError struct {
	HostPort    string
	Fingerprint string // hex en minúsculas
	Changed     bool
}

func (e *CertError) Error() string {
	if e.Changed {
		return "La huella del certificado de " + e.HostPort + " cambió: puede que el servidor renovara su certificado o que alguien se interponga. " +
			"Por seguridad no se conectó. Si confías en el certificado nuevo, confirma su huella. Huella SHA-256: " + FormatFingerprint(e.Fingerprint)
	}
	return "El servidor " + e.HostPort + " usa un certificado que no es de confianza todavía. " +
		"Compara la huella con la que imprime el servidor al arrancar y, si coincide, confía en ella. Huella SHA-256: " + FormatFingerprint(e.Fingerprint)
}

// FormatFingerprint: «AB:CD:…» en mayúsculas a partir del hex.
func FormatFingerprint(hexFP string) string {
	hexFP = strings.ToUpper(hexFP)
	var b strings.Builder
	for i := 0; i < len(hexFP); i += 2 {
		if i > 0 {
			b.WriteByte(':')
		}
		b.WriteString(hexFP[i:min(i+2, len(hexFP))])
	}
	return b.String()
}

// NormalizeFingerprint acepta «AB:CD…», «ab cd…» o hex corrido y devuelve 64 caracteres hex en minúsculas.
func NormalizeFingerprint(s string) (string, error) {
	r := strings.NewReplacer(":", "", " ", "", "-", "", "\t", "", "\n", "")
	s = strings.ToLower(r.Replace(strings.TrimSpace(s)))
	s = strings.TrimPrefix(s, "sha256")
	if len(s) != 64 {
		return "", userError("La huella debe ser un SHA-256: 64 caracteres hexadecimales (32 pares separados por «:»).")
	}
	if _, err := hex.DecodeString(s); err != nil {
		return "", userError("La huella solo puede llevar caracteres hexadecimales (0-9, A-F).")
	}
	return s, nil
}

func fingerprintOf(c *x509.Certificate) string {
	sum := sha256.Sum256(c.Raw)
	return hex.EncodeToString(sum[:])
}

// pinnedTransport crea un transporte HTTP que, para el servidor hostport, acepta el certificado si su
// huella es la fijada (pin devuelve "" si no hay) o, sin huella fijada, si el sistema lo da por bueno. En
// cualquier otro caso informa por report y falla con *CertError.
func pinnedTransport(hostport string, pin func() string, report func(*CertError)) *http.Transport {
	host, _, _ := net.SplitHostPort(hostport)
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, // la verificación la hace VerifyConnection (huella o sistema)
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("el servidor no presentó certificado")
			}
			leaf := cs.PeerCertificates[0]
			fp := fingerprintOf(leaf)
			if want := pin(); want != "" {
				if fp == want {
					return nil
				}
				ce := &CertError{HostPort: hostport, Fingerprint: fp, Changed: true}
				report(ce)
				return ce
			}
			inter := x509.NewCertPool()
			for _, c := range cs.PeerCertificates[1:] {
				inter.AddCert(c)
			}
			if _, err := leaf.Verify(x509.VerifyOptions{Intermediates: inter}); err == nil && leaf.VerifyHostname(host) == nil {
				return nil
			}
			ce := &CertError{HostPort: hostport, Fingerprint: fp}
			report(ce)
			return ce
		},
	}
	return tr
}
