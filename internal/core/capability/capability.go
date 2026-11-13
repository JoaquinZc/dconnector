package capability

import (
	"encoding/json"
	"time"
)

// Set: qué admite una cuenta o un chat concreto. La interfaz y el protocolo solo ofrecen lo que esté en true.
type Set struct {
	SendText     bool `json:"sendText"`
	SendImage    bool `json:"sendImage"`
	SendVideo    bool `json:"sendVideo"`
	SendAudio    bool `json:"sendAudio"`
	SendVoice    bool `json:"sendVoice"`
	SendDocument bool `json:"sendDocument"`
	SendSticker  bool `json:"sendSticker"`
	SendLocation bool `json:"sendLocation"`
	SendContact  bool `json:"sendContact"`
	SendPoll     bool `json:"sendPoll"`
	VotePoll     bool `json:"votePoll"`

	Reply               bool `json:"reply"`
	Forward             bool `json:"forward"`
	React               bool `json:"react"`
	MaxReactionsPerUser int  `json:"maxReactionsPerUser"` // 1 en WhatsApp

	Edit              bool          `json:"edit"`
	EditWindow        time.Duration `json:"editWindowMs"` // 0 = sin límite (se serializa en ms por MarshalJSON)
	DeleteForEveryone bool          `json:"deleteForEveryone"`
	DeleteWindow      time.Duration `json:"deleteWindowMs"`

	ReadReceipts bool `json:"readReceipts"`
	Typing       bool `json:"typing"`
	Mentions     bool `json:"mentions"`

	Groups bool `json:"groups"`

	// StartChat: se puede iniciar una conversación nueva con alguien que aún no ha escrito
	// (WhatsApp, Telegram). Messenger e Instagram solo continúan conversaciones que ya existen.
	StartChat bool `json:"startChat"`

	Statuses       bool `json:"statuses"` // ver estados de otros
	PostStatus     bool `json:"postStatus"`
	StatusViewers  bool `json:"statusViewers"`  // ver quién vio los míos
	StatusAudience bool `json:"statusAudience"` // se puede elegir audiencia por estado (si no: la privacidad del teléfono)
	StatusReact    bool `json:"statusReact"`

	E2EEChats bool `json:"e2eeChats"` // Messenger: chats cifrados de extremo a extremo disponibles

	HistoryOnDemand bool `json:"historyOnDemand"`

	MaxFileSize  int64 `json:"maxFileSize"` // bytes, documentos
	MaxImageSize int64 `json:"maxImageSize"`
	MaxVideoSize int64 `json:"maxVideoSize"`
	MaxTextLen   int   `json:"maxTextLen"`
}

// ReadOnly devuelve el conjunto con todos los envíos apagados (chats de solo lectura).
func (s Set) ReadOnly() Set {
	r := s
	r.SendText, r.SendImage, r.SendVideo, r.SendAudio, r.SendVoice = false, false, false, false, false
	r.SendDocument, r.SendSticker, r.SendLocation, r.SendContact, r.SendPoll = false, false, false, false, false
	r.Reply, r.React, r.Edit, r.DeleteForEveryone, r.VotePoll = false, false, false, false, false
	return r
}

// EditWindowMs y DeleteWindowMs: las ventanas en milisegundos (0 = sin límite).
func (s Set) EditWindowMs() int64   { return int64(s.EditWindow / time.Millisecond) }
func (s Set) DeleteWindowMs() int64 { return int64(s.DeleteWindow / time.Millisecond) }

// alias evita la recursión de MarshalJSON/UnmarshalJSON.
type alias Set

type wireSet struct {
	alias
	EditWindowMs   int64 `json:"editWindowMs"`
	DeleteWindowMs int64 `json:"deleteWindowMs"`
}

// MarshalJSON escribe las ventanas de tiempo en milisegundos (contrato con la interfaz y el protocolo).
func (s Set) MarshalJSON() ([]byte, error) {
	return json.Marshal(wireSet{
		alias:          alias(s),
		EditWindowMs:   s.EditWindowMs(),
		DeleteWindowMs: s.DeleteWindowMs(),
	})
}

// UnmarshalJSON es la inversa de MarshalJSON.
func (s *Set) UnmarshalJSON(b []byte) error {
	var w wireSet
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*s = Set(w.alias)
	s.EditWindow = time.Duration(w.EditWindowMs) * time.Millisecond
	s.DeleteWindow = time.Duration(w.DeleteWindowMs) * time.Millisecond
	return nil
}
