package tlsconf

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	certValidity = 365 * 24 * time.Hour
	renewBefore  = 30 * 24 * time.Hour
	recheckEvery = time.Hour
	ownCertFile  = "server.crt"
	ownKeyFile   = "server.key"
)

// now es sustituible en pruebas.
var now = time.Now

type source struct {
	o        Options
	external bool

	mu      sync.RWMutex
	cert    *tls.Certificate
	leaf    *x509.Certificate
	checked time.Time
}

func newSource(o Options) (*source, error) {
	s := &source{o: o, external: o.Cert != ""}
	if s.external {
		return s, s.reload()
	}
	if o.DataDir == "" {
		return nil, errors.New("No se indicó la carpeta de datos para guardar el certificado propio (<datos>/tls/).")
	}
	return s, s.loadOrCreateOwn()
}

func (s *source) fingerprint() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.leaf == nil {
		return ""
	}
	return Fingerprint(s.leaf)
}

func (s *source) set(c tls.Certificate) error {
	leaf, err := x509.ParseCertificate(c.Certificate[0])
	if err != nil {
		return err
	}
	c.Leaf = leaf
	s.mu.Lock()
	s.cert, s.leaf, s.checked = &c, leaf, now()
	s.mu.Unlock()
	return nil
}

func (s *source) reload() error {
	c, err := tls.LoadX509KeyPair(s.o.Cert, s.o.Key)
	if err != nil {
		return fmt.Errorf("No se pudo leer el certificado TLS (%s / %s): %v", filepath.Base(s.o.Cert), filepath.Base(s.o.Key), err)
	}
	return s.set(c)
}

func (s *source) getCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	s.mu.RLock()
	c, leaf, checked := s.cert, s.leaf, s.checked
	s.mu.RUnlock()
	if !s.external && now().Sub(checked) > recheckEvery && needsRenewal(leaf) {
		// Renovación en caliente del certificado propio (servidores que llevan meses encendidos).
		if err := s.loadOrCreateOwn(); err == nil {
			s.mu.RLock()
			c = s.cert
			s.mu.RUnlock()
		}
	}
	return c, nil
}

func needsRenewal(leaf *x509.Certificate) bool {
	return leaf == nil || now().Add(renewBefore).After(leaf.NotAfter)
}

func (s *source) paths() (dir, crt, key string) {
	dir = filepath.Join(s.o.DataDir, "tls")
	return dir, filepath.Join(dir, ownCertFile), filepath.Join(dir, ownKeyFile)
}

// loadOrCreateOwn reutiliza el certificado propio si es válido y no está a punto de caducar; si no,
// genera uno nuevo. No se regenera por cambios de IP de la máquina (cambiaría la huella fijada).
func (s *source) loadOrCreateOwn() error {
	dir, crtPath, keyPath := s.paths()
	if c, err := tls.LoadX509KeyPair(crtPath, keyPath); err == nil {
		if leaf, err := x509.ParseCertificate(c.Certificate[0]); err == nil &&
			!needsRenewal(leaf) && !now().Before(leaf.NotBefore) {
			if !isWindows() { // una clave que quedó legible por otros (copia, restauración) se cierra
				if st, err := os.Stat(keyPath); err == nil && st.Mode().Perm()&0o077 != 0 {
					_ = os.Chmod(keyPath, 0o600)
				}
			}
			return s.set(c)
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("No se pudo crear la carpeta del certificado %s: %v", dir, err)
	}
	certPEM, keyPEM, err := generateOwn(s.o.ExtraHosts)
	if err != nil {
		return err
	}
	// La clave se escribe primero y con permisos restrictivos.
	if err := writeAtomic(keyPath, keyPEM, 0o600); err != nil {
		return fmt.Errorf("No se pudo guardar la clave del certificado: %v", err)
	}
	if err := writeAtomic(crtPath, certPEM, 0o644); err != nil {
		return fmt.Errorf("No se pudo guardar el certificado: %v", err)
	}
	c, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return err
	}
	return s.set(c)
}

func writeAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(perm); err != nil && perm == 0o600 && !isWindows() {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func isWindows() bool { return os.PathSeparator == '\\' }

// ownHosts devuelve los SAN: localhost, loopback, el nombre y las IP de la máquina y los extras.
func ownHosts(extra []string) (dns []string, ips []net.IP) {
	dns = []string{"localhost"}
	ips = []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}
	seenD, seenI := map[string]bool{"localhost": true}, map[string]bool{"127.0.0.1": true, "::1": true}
	addName := func(h string) {
		if h == "" {
			return
		}
		if ip := net.ParseIP(h); ip != nil {
			if !seenI[ip.String()] {
				seenI[ip.String()] = true
				ips = append(ips, ip)
			}
			return
		}
		if !seenD[h] {
			seenD[h] = true
			dns = append(dns, h)
		}
	}
	if h, err := os.Hostname(); err == nil {
		addName(h)
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLinkLocalUnicast() && !ipn.IP.IsMulticast() && !ipn.IP.IsUnspecified() {
				addName(ipn.IP.String())
			}
		}
	}
	for _, h := range extra {
		addName(h)
	}
	return dns, ips
}

func generateOwn(extra []string) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, nil, err
	}
	dns, ips := ownHosts(extra)
	t := now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "dConnector", Organization: []string{"dConnector"}},
		NotBefore:             t.Add(-time.Hour), // tolera relojes algo atrasados
		NotAfter:              t.Add(certValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dns,
		IPAddresses:           ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})
	return certPEM, keyPEM, nil
}
