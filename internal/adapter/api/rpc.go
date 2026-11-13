package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"time"

	"dconnector/internal/adapter/api/facade"
)

var (
	ctxType = reflect.TypeOf((*context.Context)(nil)).Elem()
	errType = reflect.TypeOf((*error)(nil)).Elem()
)

const maxRPCBody = 1 << 20 // 1 MiB

// rpcMethod: un método de una fachada, listo para llamarlo por reflexión.
type rpcMethod struct {
	service, name string
	fn            reflect.Value
	hasCtx        bool
	in            []reflect.Type // sin el contexto
	nOut          int            // valores de retorno sin el error
	hasErr        bool
}

// desktopOnly: métodos que existen en las fachadas de Wails pero no en el servidor (docs/14 §4.3).
var desktopOnly = map[string]bool{
	"Files/PickFiles": true, "Files/SaveTemp": true, "Files/Info": true,
	"Messaging/SaveMediaAs":            true,
	"Settings/OpenDataDir":             true,
	"Settings/OpenLogsDir":             true,
	"Notifications/Status":             true,
	"Notifications/RequestPermission":  true,
	"Notifications/SendTest":           true,
	"Notifications/OpenSystemSettings": true,
	"Notifications/MarkAsked":          true,
}

func isDesktopOnly(svc, method string) bool {
	return desktopOnly[svc+"/"+method] || svc == "Files"
}

// fileArgs: argumentos que son una ruta de archivo por posición, además de los campos FilePath de las
// estructuras (que se detectan por nombre en todos los argumentos).
var fileArgs = map[string][]int{
	"Contacts/ImportCSV": {0},
}

// buildRPC recorre por reflexión todos los métodos exportados de las fachadas.
func buildRPC(services map[string]any) map[string]map[string]*rpcMethod {
	out := map[string]map[string]*rpcMethod{}
	for name, svc := range services {
		v := reflect.ValueOf(svc)
		t := v.Type()
		ms := map[string]*rpcMethod{}
		for i := 0; i < t.NumMethod(); i++ {
			mt := t.Method(i)
			fn := v.Method(i)
			ft := fn.Type()
			m := &rpcMethod{service: name, name: mt.Name, fn: fn}
			start := 0
			if ft.NumIn() > 0 && ft.In(0) == ctxType {
				m.hasCtx, start = true, 1
			}
			for j := start; j < ft.NumIn(); j++ {
				m.in = append(m.in, ft.In(j))
			}
			n := ft.NumOut()
			if n > 0 && ft.Out(n-1).Implements(errType) && ft.Out(n-1).Kind() == reflect.Interface {
				m.hasErr = true
				n--
			}
			m.nOut = n
			ms[mt.Name] = m
		}
		out[name] = ms
	}
	return out
}

// call ejecuta el método con los argumentos ya decodificados. Un pánico se convierte en error.
func (m *rpcMethod) call(ctx context.Context, args []reflect.Value) (res any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("pánico en %s.%s: %v", m.service, m.name, r)
		}
	}()
	in := make([]reflect.Value, 0, len(args)+1)
	if m.hasCtx {
		in = append(in, reflect.ValueOf(ctx))
	}
	in = append(in, args...)
	outs := m.fn.Call(in)
	if m.hasErr {
		if e, _ := outs[len(outs)-1].Interface().(error); e != nil {
			return nil, e
		}
	}
	switch m.nOut {
	case 0:
		return nil, nil
	case 1:
		return outs[0].Interface(), nil
	default:
		arr := make([]any, m.nOut)
		for i := range arr {
			arr[i] = outs[i].Interface()
		}
		return arr, nil
	}
}

// handleRPC atiende POST /api/v1/rpc/{servicio}/{método}.
func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, apiBase+"/rpc/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		writeError(w, http.StatusNotFound, codeNotFound, "No existe ese servicio o método.")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, codeMethod, "Usa POST para llamar a un método.")
		return
	}
	svc, name := parts[0], parts[1]
	if isDesktopOnly(svc, name) {
		writeError(w, http.StatusNotFound, codeNotAvailable, "Esta función solo está disponible en la aplicación de escritorio.")
		return
	}
	m := s.rpc[svc][name]
	if m == nil {
		writeError(w, http.StatusNotFound, codeNotFound, "No existe ese servicio o método.")
		return
	}
	if !s.gateReady(w) {
		return
	}

	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Now().Add(30 * time.Second))
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRPCBody))
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeError(w, http.StatusRequestEntityTooLarge, codeTooLarge, "La petición es demasiado grande. Los archivos se suben por /files.")
			return
		}
		writeError(w, http.StatusBadRequest, codeInvalid, "No se pudo leer la petición.")
		return
	}

	args, err := decodeArgs(m, body)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeInvalid, err.Error())
		return
	}
	if err := s.resolveFileArgs(r, m, args); err != nil {
		writeServiceError(w, s.log, svc+"."+name, err)
		return
	}

	ctx := facade.WithConnID(r.Context(), r.Header.Get(connHeader))
	res, err := m.call(ctx, args)
	if err != nil {
		writeServiceError(w, s.log, svc+"."+name, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// decodeArgs convierte el array JSON del cuerpo en los argumentos de la firma.
func decodeArgs(m *rpcMethod, body []byte) ([]reflect.Value, error) {
	var raw []json.RawMessage
	if len(strings.TrimSpace(string(body))) == 0 {
		raw = nil
	} else if err := json.Unmarshal(body, &raw); err != nil {
		return nil, errors.New("El cuerpo debe ser un array JSON con los argumentos.")
	}
	if len(raw) != len(m.in) {
		return nil, fmt.Errorf("%s.%s espera %d argumento(s) y llegaron %d.", m.service, m.name, len(m.in), len(raw))
	}
	args := make([]reflect.Value, len(raw))
	for i, rm := range raw {
		p := reflect.New(m.in[i])
		if err := json.Unmarshal(rm, p.Interface()); err != nil {
			return nil, fmt.Errorf("El argumento %d de %s.%s no es válido.", i+1, m.service, m.name)
		}
		args[i] = p.Elem()
	}
	return args, nil
}

// resolveFileArgs sustituye las rutas de archivo de los argumentos (docs/14 §4.4): `upload://<id>` pasa a
// la ruta bajo <data>/uploads. Con el token local se aceptan además rutas absolutas; un cliente remoto solo
// puede mandar `upload://` (o una ruta que ya esté en las carpetas de medios del propio servidor, que es lo que
// devuelve el servidor al leer una campaña o un programado y el cliente reenvía al editarlo).
func (s *Server) resolveFileArgs(r *http.Request, m *rpcMethod, args []reflect.Value) error {
	local := s.cfg.Auth != nil && s.cfg.Auth.IsLocal(r)
	for _, i := range fileArgs[m.service+"/"+m.name] {
		if i < len(args) && args[i].Kind() == reflect.String {
			p, _, err := s.files.resolvePath(args[i].String(), local)
			if err != nil {
				return err
			}
			args[i].SetString(p)
		}
	}
	for _, a := range args {
		if err := s.walkFilePaths(a, local); err != nil {
			return err
		}
	}
	return nil
}

// walkFilePaths busca en el valor los campos FilePath (Outgoing, OutgoingStatus, Campaign, Scheduled…).
func (s *Server) walkFilePaths(v reflect.Value, local bool) error {
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return s.walkFilePaths(v.Elem(), local)
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if err := s.walkFilePaths(v.Index(i), local); err != nil {
				return err
			}
		}
	case reflect.Map:
		// Los valores de un mapa no son direccionables: se copian, se recorren y se vuelven a poner.
		for _, k := range v.MapKeys() {
			cp := reflect.New(v.Type().Elem()).Elem()
			cp.Set(v.MapIndex(k))
			if err := s.walkFilePaths(cp, local); err != nil {
				return err
			}
			v.SetMapIndex(k, cp)
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			sf := t.Field(i)
			if !sf.IsExported() {
				continue
			}
			if sf.Name == "FilePath" && f.Kind() == reflect.String {
				if f.String() == "" {
					continue
				}
				p, meta, err := s.files.resolvePath(f.String(), local)
				if err != nil {
					return err
				}
				f.SetString(p)
				if meta != nil { // un archivo subido: conserva su nombre y su tipo si el cliente no los dio
					fillString(v, "FileName", meta.Name)
					fillString(v, "Mime", meta.Mime)
				}
				continue
			}
			if err := s.walkFilePaths(f, local); err != nil {
				return err
			}
		}
	}
	return nil
}

func fillString(st reflect.Value, field, val string) {
	f := st.FieldByName(field)
	if f.IsValid() && f.Kind() == reflect.String && f.CanSet() && f.String() == "" {
		f.SetString(val)
	}
}
