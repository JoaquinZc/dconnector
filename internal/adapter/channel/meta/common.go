// Package meta: piezas comunes de los adaptadores de Messenger e Instagram (09-ADAPTADOR-META).
// Vía NO oficial: las librerías imitan el cliente web/móvil de Meta (go.mau.fi/mautrix-meta, AGPL-3.0).
package meta

import (
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
)

// OpTimeout: tope de las llamadas de red cortas que hace el adaptador por su cuenta.
const OpTimeout = 45 * time.Second

// StopWait: cuánto espera Stop a que termine la conexión.
const StopWait = 6 * time.Second

// Core reúne lo que comparten los dos adaptadores: el sink de eventos, el estado de la cuenta, el KV
// y el registro. Cada adaptador lo incrusta.
type Core struct {
	Chan model.Channel
	Acc  model.Account
	Log  *slog.Logger
	KV   *KV

	mu         sync.Mutex
	sink       port.EventSink
	lastStatus model.AccountStatus
	selfID     string
}

// NewCore prepara el núcleo común. El registro lleva channel=<canal> y accountId.
func NewCore(ch model.Channel, deps port.AdapterDeps) (*Core, error) {
	if deps.DB == nil {
		return nil, fmt.Errorf("%s: falta la base de datos de la cuenta", ch)
	}
	log := deps.Log
	if log == nil {
		log = slog.Default()
	}
	log = log.With("channel", string(ch), "accountId", string(deps.Account.ID))
	return &Core{
		Chan:   ch,
		Acc:    deps.Account,
		Log:    log,
		KV:     NewKV(deps.DB),
		selfID: deps.Account.SelfID,
	}, nil
}

// SetSink fija por dónde salen los eventos (lo llama Start).
func (c *Core) SetSink(s port.EventSink) {
	c.mu.Lock()
	c.sink = s
	c.mu.Unlock()
}

// Sink devuelve el sink actual (nil si aún no hay).
func (c *Core) Sink() port.EventSink {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sink
}

// SelfID devuelve el id propio (numérico, como texto); "" si aún no se conoce.
func (c *Core) SelfID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.selfID
}

// SetSelfID fija el id propio.
func (c *Core) SetSelfID(id string) {
	c.mu.Lock()
	c.selfID = id
	c.mu.Unlock()
}

// Emit completa AccountID, Channel y At y entrega el evento al sink (no bloquea).
func (c *Core) Emit(ev model.Event) {
	sink := c.Sink()
	if sink == nil {
		return
	}
	ev.AccountID, ev.Channel = c.Acc.ID, c.Chan
	if ev.At == 0 {
		ev.At = time.Now().UnixMilli()
	}
	sink.Emit(ev)
}

// EmitAll entrega varios eventos en orden.
func (c *Core) EmitAll(evs []model.Event) {
	for _, ev := range evs {
		c.Emit(ev)
	}
}

// SetStatus emite account.state; descarta repeticiones idénticas.
func (c *Core) SetStatus(st model.AccountStatus) {
	st.AccountID = c.Acc.ID
	c.mu.Lock()
	prev := c.lastStatus
	same := prev.State == st.State && prev.Need == st.Need && prev.QRCode == st.QRCode &&
		prev.PairCode == st.PairCode && prev.Detail == st.Detail
	if !same {
		st.Since = time.Now().UnixMilli()
		c.lastStatus = st
	}
	c.mu.Unlock()
	if same {
		return
	}
	c.Emit(model.Event{Type: model.EvAccountStatus, At: st.Since, AccountStatus: &st})
}

// LastStatus devuelve el último estado emitido.
func (c *Core) LastStatus() model.AccountStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastStatus
}

// RecoverLoop registra un pánico de una goroutine de larga vida sin tumbar el proceso (02 §6).
func (c *Core) RecoverLoop(where string) {
	if r := recover(); r != nil {
		c.Log.Error("pánico recuperado", "where", where, "panic", Redact(fmt.Sprint(r)))
	}
}

// ---------------------------------------------------------------------------------------------
// Registro sin datos personales (02 §7)

var longDigits = regexp.MustCompile(`\d{7,}`)

// Redact oculta las rachas largas de dígitos (ids, teléfonos) dejando los últimos 4.
func Redact(s string) string {
	return longDigits.ReplaceAllStringFunc(s, func(m string) string { return "…" + m[len(m)-4:] })
}

// RedactErr deja un texto de error seguro para el registro.
func RedactErr(err error) string {
	if err == nil {
		return ""
	}
	return Redact(err.Error())
}

// ShortID abrevia un id a sus últimos 4 caracteres.
func ShortID(id string) string {
	if len(id) <= 4 {
		return id
	}
	return "…" + id[len(id)-4:]
}

// ---------------------------------------------------------------------------------------------
// Ids

// NativeUser convierte un id numérico de Meta en id nativo (texto); "" si es 0.
func NativeUser(id int64) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}

// ParseID convierte un id nativo en número; error de dominio si no lo es.
func ParseID(native string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(native), 10, 64)
	if err != nil || n == 0 {
		return 0, fmt.Errorf("identificador no válido: %w", port.ErrInvalid)
	}
	return n, nil
}
