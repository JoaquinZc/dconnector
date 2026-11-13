package connection

import (
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
)

// relay es un reenvío en loopback hacia un servidor remoto con certificado fijado por huella. La interfaz
// (el WebView) no sabe de huellas: habla con 127.0.0.1 en http y el escritorio abre el TLS verificando la
// huella. Reenvía también los WebSocket. El servidor sigue exigiendo el token en cada llamada.
type relay struct {
	ln   net.Listener
	srv  *http.Server
	once sync.Once
}

func newRelay(target string, rt http.RoundTripper) (*relay, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	rp := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(u)
			r.Out.Host = u.Host
		},
		Transport:     rt,
		FlushInterval: -1,
	}
	r := &relay{ln: ln, srv: &http.Server{Handler: rp}}
	go func() { _ = r.srv.Serve(ln) }()
	return r, nil
}

// URL es la dirección local del reenvío.
func (r *relay) URL() string { return "http://" + r.ln.Addr().String() }

func (r *relay) Close() {
	r.once.Do(func() { _ = r.srv.Close() })
}
