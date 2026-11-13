package api

import (
	"context"
	"strings"
	"sync"
	"time"

	"dconnector/internal/core/model"
	"dconnector/internal/core/port"
	"dconnector/internal/core/service"
)

// notifier cumple port.Notifier para el servidor: no hay escritorio donde mostrar nada, así que convierte
// Show y PlaySound en eventos `notify` (docs/14 §5.1) y es el cliente quien muestra el aviso y suena.
// El permiso siempre consta como concedido: el del sistema lo resuelve el cliente.
type notifier struct {
	emit func(name string, data any)
	// decisionEvery: cada cuánto se mira si el servicio decidió algo nuevo.
	decisionEvery time.Duration

	mu        sync.Mutex
	svc       *service.Notifications
	delivered bool          // se avisó (o sonó) desde la última decisión
	lastChat  model.ChatKey // chat del último aviso
	lastAt    int64
}

func newNotifier(emit func(string, any)) *notifier {
	return &notifier{emit: emit, decisionEvery: decisionPoll}
}

func (n *notifier) Permission(context.Context) port.NotifyPermission {
	return port.NotifyPermission{State: port.NotifyGranted, Quiet: port.QuietInactive}
}

func (n *notifier) RequestPermission(ctx context.Context) (port.NotifyPermission, error) {
	return n.Permission(ctx), nil
}

func (n *notifier) OpenSystemSettings() error { return nil }

var channelByPrefix = map[string]model.Channel{
	"wa": model.ChannelWhatsApp, "tg": model.ChannelTelegram, "fb": model.ChannelMessenger, "ig": model.ChannelInstagram,
}

// decisionPoll: intervalo del vigía de decisiones (variable para las pruebas).
var decisionPoll = time.Second

// notifyPayload es la carga del evento `notify`.
type notifyPayload struct {
	ChatKey   string `json:"chatKey"`
	AccountID string `json:"accountId"`
	Channel   string `json:"channel"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Preview   bool   `json:"preview"` // el cuerpo es la vista previa del mensaje (no el texto neutro)
	Silent    bool   `json:"silent"`
	Sound     bool   `json:"sound"`
	Reason    string `json:"reason"`
}

// Show avisa de un mensaje: ventana con título y cuerpo, sin sonido propio (lo manda aparte PlaySound).
func (n *notifier) Show(_ context.Context, nt port.Notification) error {
	acc, _ := nt.ChatKey.Split()
	prefix, _, _ := strings.Cut(string(acc), "-")
	n.mu.Lock()
	n.delivered, n.lastChat = true, nt.ChatKey
	n.mu.Unlock()
	n.emit(EvtNotify, notifyPayload{
		ChatKey: string(nt.ChatKey), AccountID: string(acc), Channel: string(channelByPrefix[prefix]),
		Title: nt.Title, Body: nt.Body, Preview: nt.Body != "" && nt.Body != "Mensaje nuevo",
		Silent: nt.Silent, Sound: false, Reason: "message",
	})
	return nil
}

// PlaySound: solo sonido, sin ventana (silent=true y sound=true).
func (n *notifier) PlaySound(context.Context) error {
	n.mu.Lock()
	n.delivered = true
	n.mu.Unlock()
	n.emit(EvtNotify, notifyPayload{Silent: true, Sound: true, Reason: "sound"})
	return nil
}

// setService conecta el servicio del núcleo y arranca (una vez) el vigía de sus decisiones. El núcleo no
// avisa de por qué un mensaje no avisó, solo lo deja en Status().LastDecision: se consulta cada segundo y
// cada decisión nueva sale como `notify:decision`.
func (n *notifier) setService(svc *service.Notifications) {
	n.mu.Lock()
	n.svc = svc
	n.mu.Unlock()
}

// watch bloquea hasta que ctx se cancela.
func (n *notifier) watch(ctx context.Context) {
	t := time.NewTicker(n.decisionEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n.mu.Lock()
			svc := n.svc
			n.mu.Unlock()
			if svc == nil {
				continue
			}
			d := svc.Status(ctx).LastDecision
			n.mu.Lock()
			if d.At == 0 || d.At == n.lastAt {
				n.mu.Unlock()
				continue
			}
			n.lastAt = d.At
			shown, chat := n.delivered, n.lastChat
			n.delivered, n.lastChat = false, ""
			n.mu.Unlock()
			n.emit(EvtNotifyDecision, map[string]any{"chatKey": string(chat), "shown": shown, "reason": d.Text})
		}
	}
}
