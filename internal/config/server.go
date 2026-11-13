// Package config lee la configuración del servidor: indicadores de línea de comandos y variables
// DCONNECTOR_* (el indicador gana a la variable, la variable gana al valor por defecto).
package config

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// Server es la configuración de cmd/dconnector-server.
type Server struct {
	Listen      string // dirección de escucha, p. ej. 127.0.0.1:8443
	Data        string // carpeta de datos (vacía = la de la plataforma)
	TLSCert     string // certificado externo (PEM); vacío = autogenerado
	TLSKey      string // clave privada del certificado externo
	NoTLS       bool   // solo permitido en loopback o con Insecure
	Insecure    bool   // permite escuchar en una interfaz pública sin TLS (ruidoso)
	Verbose     bool   // registro detallado
	NoAccounts  bool   // no inicia cuentas ni servicios de fondo (history, scheduler, campaigns, statuses, protocol): sirve para revisar datos copiados sin conectar a nada
	Version     bool
	Selftest    bool
	Out         string   // archivo para la salida de --version y --selftest
	Service     string   // install|uninstall|start|stop (Windows)
	LocalToken  string   // token propio del modo local (lo pasa el escritorio por entorno, nunca por argumento)
	AdminUser   string   // nombre del administrador del primer arranque
	AllowOrigin string   // orígenes extra permitidos para el WebSocket y CORS, separados por comas (opcional)
	Args        []string // argumentos que no son indicadores (subcomandos: user, healthcheck)
}

// DefaultListen: loopback y puerto fijo; el escritorio usa 127.0.0.1:0 (puerto efímero).
const DefaultListen = "127.0.0.1:8443"

// ParseServer interpreta args (sin el nombre del programa) y el entorno dado.
func ParseServer(args []string, getenv func(string) string, errOut io.Writer) (Server, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	c := Server{
		Listen:      envStr(getenv, "DCONNECTOR_LISTEN", DefaultListen),
		Data:        envStr(getenv, "DCONNECTOR_DATA", ""),
		TLSCert:     envStr(getenv, "DCONNECTOR_TLS_CERT", ""),
		TLSKey:      envStr(getenv, "DCONNECTOR_TLS_KEY", ""),
		NoTLS:       envBool(getenv, "DCONNECTOR_NO_TLS"),
		Insecure:    envBool(getenv, "DCONNECTOR_INSECURE"),
		Verbose:     envBool(getenv, "DCONNECTOR_VERBOSE"),
		NoAccounts:  envBool(getenv, "DCONNECTOR_NO_ACCOUNTS"),
		LocalToken:  getenv("DCONNECTOR_LOCAL_TOKEN"),
		AdminUser:   envStr(getenv, "DCONNECTOR_ADMIN_USER", "admin"),
		AllowOrigin: envStr(getenv, "DCONNECTOR_ALLOW_ORIGIN", ""),
	}
	fs := flag.NewFlagSet("dconnector-server", flag.ContinueOnError)
	if errOut != nil {
		fs.SetOutput(errOut)
	}
	fs.StringVar(&c.Listen, "listen", c.Listen, "dirección y puerto de escucha")
	fs.StringVar(&c.Data, "data", c.Data, "carpeta de datos")
	fs.StringVar(&c.TLSCert, "tls-cert", c.TLSCert, "certificado TLS externo (PEM)")
	fs.StringVar(&c.TLSKey, "tls-key", c.TLSKey, "clave privada del certificado TLS")
	fs.BoolVar(&c.NoTLS, "no-tls", c.NoTLS, "sirve sin TLS (solo loopback, o detrás de un proxy inverso con --insecure)")
	fs.BoolVar(&c.Insecure, "insecure", c.Insecure, "permite escuchar en una interfaz pública sin TLS (NO recomendado)")
	fs.BoolVar(&c.Verbose, "verbose", c.Verbose, "registro detallado")
	fs.BoolVar(&c.NoAccounts, "no-accounts", c.NoAccounts, "no inicia las cuentas ni los servicios de fondo (solo sirve los datos existentes)")
	fs.StringVar(&c.AdminUser, "admin-user", c.AdminUser, "nombre del administrador creado en el primer arranque")
	fs.StringVar(&c.AllowOrigin, "allow-origin", c.AllowOrigin, "origen adicional permitido para el WebSocket")
	fs.StringVar(&c.Service, "service", "", "servicio de Windows: install, uninstall, start o stop")
	fs.BoolVar(&c.Version, "version", false, "imprime la versión y sale")
	fs.BoolVar(&c.Selftest, "selftest", false, "comprobación sin red: abre los almacenes y sale")
	fs.StringVar(&c.Out, "out", "", "archivo donde escribir la salida de --version y --selftest")
	if err := fs.Parse(args); err != nil {
		return c, err
	}
	c.Args = fs.Args()
	// `healthcheck --listen X --no-tls`: los indicadores también se admiten después del subcomando.
	if len(c.Args) > 1 && c.Args[0] == "healthcheck" {
		if err := fs.Parse(c.Args[1:]); err != nil {
			return c, err
		}
		c.Args = append([]string{"healthcheck"}, fs.Args()...)
	}
	switch c.Service {
	case "", "install", "uninstall", "start", "stop":
	default:
		return c, fmt.Errorf("--service debe ser install, uninstall, start o stop (recibido %q)", c.Service)
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		return c, fmt.Errorf("--tls-cert y --tls-key van juntos")
	}
	if c.NoTLS && c.TLSCert != "" {
		return c, fmt.Errorf("--no-tls no se puede combinar con --tls-cert y --tls-key")
	}
	return c, nil
}

// Origins devuelve los orígenes extra permitidos (AllowOrigin separado por comas, sin vacíos ni barras finales).
func (c Server) Origins() []string {
	var out []string
	for _, o := range strings.Split(c.AllowOrigin, ",") {
		if o = strings.TrimRight(strings.TrimSpace(o), "/"); o != "" {
			out = append(out, o)
		}
	}
	return out
}

// ServiceArgs son los argumentos que se guardan al instalar el servicio de Windows (van en la línea de
// comandos del servicio). Nunca incluye el token local ni claves: esos valores no se guardan.
func (c Server) ServiceArgs() []string {
	var a []string
	if c.Data != "" {
		a = append(a, "--data", c.Data)
	}
	if c.Listen != "" {
		a = append(a, "--listen", c.Listen)
	}
	if c.TLSCert != "" {
		a = append(a, "--tls-cert", c.TLSCert, "--tls-key", c.TLSKey)
	}
	if c.NoTLS {
		a = append(a, "--no-tls")
	}
	if c.Insecure {
		a = append(a, "--insecure")
	}
	if c.Verbose {
		a = append(a, "--verbose")
	}
	if c.NoAccounts {
		a = append(a, "--no-accounts")
	}
	if c.AdminUser != "" && c.AdminUser != "admin" {
		a = append(a, "--admin-user", c.AdminUser)
	}
	if c.AllowOrigin != "" {
		a = append(a, "--allow-origin", c.AllowOrigin)
	}
	return a
}

func envStr(g func(string) string, k, def string) string {
	if v := g(k); v != "" {
		return v
	}
	return def
}

func envBool(g func(string) string, k string) bool {
	b, _ := strconv.ParseBool(g(k))
	return b
}
