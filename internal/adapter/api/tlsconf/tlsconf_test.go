package tlsconf

import (
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIsLoopback(t *testing.T) {
	yes := []string{"127.0.0.1:8080", "127.0.0.1:0", "[::1]:9", "localhost:80", "LOCALHOST:80", "127.5.4.3:1", "127.0.0.1", "::1", "[::1]"}
	no := []string{":8080", "0.0.0.0:80", "[::]:80", "192.168.1.5:80", "10.0.0.1:1", "example.com:443", "", "[2001:db8::1]:1", "8.8.8.8"}
	for _, a := range yes {
		require.True(t, IsLoopback(a), a)
	}
	for _, a := range no {
		require.False(t, IsLoopback(a), a)
	}
}

func TestCheckListen(t *testing.T) {
	cases := []struct {
		name string
		addr string
		o    Options
		err  string // fragmento esperado ("" = sin error)
	}{
		{"loopback con TLS", "127.0.0.1:8443", Options{}, ""},
		{"loopback sin TLS", "127.0.0.1:0", Options{NoTLS: true}, ""},
		{"ipv6 loopback sin TLS", "[::1]:80", Options{NoTLS: true}, ""},
		{"fuera de loopback con TLS propio", "0.0.0.0:8443", Options{}, ""},
		{"todas las interfaces con TLS externo", ":8443", Options{Cert: "a", Key: "b"}, ""},
		{"fuera de loopback sin TLS", "0.0.0.0:80", Options{NoTLS: true}, "no es loopback"},
		{"vacío sin TLS", ":80", Options{NoTLS: true}, "--insecure"},
		{"LAN sin TLS e inseguro", "192.168.1.9:80", Options{NoTLS: true, Insecure: true}, ""},
		{"insecure con TLS", "192.168.1.9:80", Options{Insecure: true}, ""},
		{"cert sin key", "127.0.0.1:1", Options{Cert: "a"}, "--tls-key"},
		{"key sin cert", "127.0.0.1:1", Options{Key: "a"}, "--tls-cert"},
		{"no-tls con cert", "127.0.0.1:1", Options{NoTLS: true, Cert: "a", Key: "b"}, "--no-tls"},
	}
	for _, c := range cases {
		err := CheckListen(c.addr, c.o)
		if c.err == "" {
			require.NoError(t, err, c.name)
			continue
		}
		require.Error(t, err, c.name)
		require.Contains(t, err.Error(), c.err, c.name)
	}
}

func readLeaf(t *testing.T, path string) *x509.Certificate {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	blk, _ := pem.Decode(b)
	require.NotNil(t, blk)
	c, err := x509.ParseCertificate(blk.Bytes)
	require.NoError(t, err)
	return c
}

func TestLoadNoTLS(t *testing.T) {
	cfg, fp, err := Load(Options{NoTLS: true, DataDir: t.TempDir()})
	require.NoError(t, err)
	require.Nil(t, cfg)
	require.Empty(t, fp)
}

func TestCertificadoPropio(t *testing.T) {
	dir := t.TempDir()
	cfg, fp, err := Load(Options{DataDir: dir, ExtraHosts: []string{"dconnector.example.test", "10.9.8.7"}})
	require.NoError(t, err)
	require.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
	require.NotNil(t, cfg.GetCertificate)

	crt := filepath.Join(dir, "tls", "server.crt")
	leaf := readLeaf(t, crt)
	require.Equal(t, Fingerprint(leaf), fp)
	require.Regexp(t, `^([0-9A-F]{2}:){31}[0-9A-F]{2}$`, fp)
	require.Equal(t, fp, CurrentFingerprint())

	// ECDSA P-256, un año, SAN completos.
	require.Equal(t, x509.ECDSA, leaf.PublicKeyAlgorithm)
	require.Equal(t, "P-256", leaf.PublicKey.(*ecdsa.PublicKey).Curve.Params().Name)
	d := leaf.NotAfter.Sub(leaf.NotBefore)
	require.InDelta(t, (365*24+1)*time.Hour, d, float64(time.Minute))
	require.Contains(t, leaf.DNSNames, "localhost")
	require.Contains(t, leaf.DNSNames, "dconnector.example.test")
	var ips []string
	for _, ip := range leaf.IPAddresses {
		ips = append(ips, ip.String())
	}
	require.Contains(t, ips, "127.0.0.1")
	require.Contains(t, ips, "::1")
	require.Contains(t, ips, "10.9.8.7")
	if h, err := os.Hostname(); err == nil && h != "" {
		require.Contains(t, leaf.DNSNames, h)
	}
	require.NoError(t, leaf.VerifyHostname("localhost"))
	require.NoError(t, leaf.VerifyHostname("127.0.0.1"))
	require.NoError(t, leaf.VerifyHostname("::1"))

	// Permisos de la clave (en Windows no hay bits de permisos).
	if os.PathSeparator == '/' {
		st, err := os.Stat(filepath.Join(dir, "tls", "server.key"))
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), st.Mode().Perm())
		sd, _ := os.Stat(filepath.Join(dir, "tls"))
		require.Equal(t, os.FileMode(0o700), sd.Mode().Perm())
	}

	// Reutilizar: segundo arranque, misma huella (el cliente la fijó).
	_, fp2, err := Load(Options{DataDir: dir})
	require.NoError(t, err)
	require.Equal(t, fp, fp2)
	// Reload con certificado propio no hace nada.
	require.NoError(t, Reload())
	require.Equal(t, fp, CurrentFingerprint())
}

func TestCertificadoPropioHandshakeTLS12Min(t *testing.T) {
	dir := t.TempDir()
	cfg, fp, err := Load(Options{DataDir: dir})
	require.NoError(t, err)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	require.NoError(t, err)
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); _ = c.(*tls.Conn).Handshake() }()
		}
	}()
	// El cliente fija la huella (confianza al primer uso).
	conn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
	require.NoError(t, err)
	got := Fingerprint(conn.ConnectionState().PeerCertificates[0])
	conn.Close()
	require.Equal(t, fp, got)
	// TLS 1.1 se rechaza.
	_, err = tls.Dial("tcp", ln.Addr().String(), &tls.Config{InsecureSkipVerify: true, MaxVersion: tls.VersionTLS11})
	require.Error(t, err)
}

func TestRenovacion30DiasAntes(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { return t0 }
	t.Cleanup(func() { now = time.Now })

	_, fp1, err := Load(Options{DataDir: dir})
	require.NoError(t, err)

	// A 300 días aún no toca renovar (quedan 65).
	now = func() time.Time { return t0.Add(300 * 24 * time.Hour) }
	_, fp2, err := Load(Options{DataDir: dir})
	require.NoError(t, err)
	require.Equal(t, fp1, fp2)

	// A 340 días quedan menos de 30: se renueva y cambia la huella.
	now = func() time.Time { return t0.Add(340 * 24 * time.Hour) }
	cfg, fp3, err := Load(Options{DataDir: dir})
	require.NoError(t, err)
	require.NotEqual(t, fp1, fp3)
	leaf := readLeaf(t, filepath.Join(dir, "tls", "server.crt"))
	require.True(t, leaf.NotAfter.After(now().Add(300*24*time.Hour)))
	c, err := cfg.GetCertificate(nil)
	require.NoError(t, err)
	require.Equal(t, fp3, Fingerprint(c.Leaf))

	// Renovación en caliente con el servidor ya levantado.
	now = func() time.Time { return t0.Add(700 * 24 * time.Hour) }
	c, err = cfg.GetCertificate(nil)
	require.NoError(t, err)
	require.NotEqual(t, fp3, Fingerprint(c.Leaf))
	require.Equal(t, Fingerprint(c.Leaf), CurrentFingerprint())
}

func TestCertificadoCorruptoSeRegenera(t *testing.T) {
	dir := t.TempDir()
	_, fp1, err := Load(Options{DataDir: dir})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tls", "server.crt"), []byte("basura"), 0o644))
	_, fp2, err := Load(Options{DataDir: dir})
	require.NoError(t, err)
	require.NotEqual(t, fp1, fp2)
}

func TestCertificadoExternoYReload(t *testing.T) {
	src := t.TempDir()
	// Se genera uno con el generador propio y se usa como «externo».
	c1, k1, err := generateOwn([]string{"ext.test"})
	require.NoError(t, err)
	crt, key := filepath.Join(src, "a.crt"), filepath.Join(src, "a.key")
	require.NoError(t, os.WriteFile(crt, c1, 0o644))
	require.NoError(t, os.WriteFile(key, k1, 0o600))

	cfg, fp1, err := Load(Options{Cert: crt, Key: key, DataDir: t.TempDir()})
	require.NoError(t, err)
	got, err := cfg.GetCertificate(nil)
	require.NoError(t, err)
	require.Equal(t, fp1, Fingerprint(got.Leaf))

	// Cambia el archivo y se recarga (SIGHUP).
	c2, k2, err := generateOwn(nil)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(crt, c2, 0o644))
	require.NoError(t, os.WriteFile(key, k2, 0o600))
	require.Equal(t, fp1, CurrentFingerprint(), "no cambia hasta Reload")
	require.NoError(t, Reload())
	got, _ = cfg.GetCertificate(nil)
	require.NotEqual(t, fp1, Fingerprint(got.Leaf))

	// Un archivo roto no tumba el certificado en uso.
	fp2 := CurrentFingerprint()
	require.NoError(t, os.WriteFile(crt, []byte("roto"), 0o644))
	err = Reload()
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "certificado"))
	require.Equal(t, fp2, CurrentFingerprint())

	// Externo inexistente o a medias.
	_, _, err = Load(Options{Cert: filepath.Join(src, "no.crt"), Key: key})
	require.Error(t, err)
	_, _, err = Load(Options{Cert: crt})
	require.Error(t, err)
	_, _, err = Load(Options{})
	require.Error(t, err, "sin carpeta de datos no hay dónde guardar el certificado propio")
}
