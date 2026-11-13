package facade

import (
	"context"
	"sync"
	"time"

	"dconnector/internal/core/model"
	"dconnector/internal/core/service"
)

type connFocus struct {
	chat    model.ChatKey
	focused bool
	at      time.Time
}

// Notifications: foco de la interfaz para decidir los avisos. Es lo único del servicio de notificaciones
// que existe en el servidor (el permiso y las pruebas del sistema los resuelve el cliente, docs/14 §4.3).
type Notifications struct {
	mu    sync.Mutex
	n     *service.Notifications
	conns map[string]connFocus
}

func (s *Notifications) setService(n *service.Notifications) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n = n
	s.applyLocked()
}

// SetFocus: la conexión (cabecera X-DConnector-Conn) dice qué chat tiene abierto ("" = ninguno) y si su
// ventana tiene el foco. Se avisa de un mensaje si ninguna conexión tiene ese chat abierto y enfocado.
// Nota: la firma lleva el contexto (que trae la conexión); por RPC el cliente solo manda [chatKey, focused].
func (s *Notifications) SetFocus(ctx context.Context, chatKey string, focused bool) {
	id := ConnID(ctx)
	if id == "" {
		// Sin conexión conocida no se puede olvidar el foco al cerrarse: un foco huérfano taparía avisos.
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conns[id] = connFocus{chat: model.ChatKey(chatKey), focused: focused, at: time.Now()}
	s.applyLocked()
}

func (s *Notifications) dropConn(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.conns[id]; !ok {
		return
	}
	delete(s.conns, id)
	s.applyLocked()
}

// applyLocked traslada al servicio el foco efectivo. El servicio del núcleo guarda un solo chat visto: si
// varias conexiones tienen chats distintos enfocados vale el de la que lo dijo más recientemente.
func (s *Notifications) applyLocked() {
	if s.n == nil {
		return
	}
	var best connFocus
	found := false
	for _, c := range s.conns {
		if c.focused && c.chat != "" && (!found || c.at.After(best.at)) {
			best, found = c, true
		}
	}
	if found {
		s.n.SetFocus(best.chat, true)
	} else {
		s.n.SetFocus("", false)
	}
}

func (s *Notifications) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
}
