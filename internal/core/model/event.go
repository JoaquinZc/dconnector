package model

type EventType string

const (
	EvAccountStatus EventType = "account.state"
	EvChatUpsert    EventType = "chat.updated"
	EvChatRemoved   EventType = "chat.removed"
	EvMessageNew    EventType = "message.received" // entrante, o saliente hecho desde otro dispositivo
	EvMessageSent   EventType = "message.sent"     // saliente hecho desde dConnector
	EvMessageUpdate EventType = "message.updated"  // editado, borrado, entrega, votos
	EvReaction      EventType = "reaction"
	EvReceipt       EventType = "receipt"
	EvTyping        EventType = "typing"
	EvPerson        EventType = "person.updated"
	EvStatusUpsert  EventType = "status.posted"
	EvStatusView    EventType = "status.viewed"
	EvHistoryBatch  EventType = "history.batch" // no va al protocolo
	EvCapabilities  EventType = "account.capabilities"
)

type MessageUpdate struct {
	Key      MessageKey    `json:"key"`
	Text     *string       `json:"text,omitempty"`
	EditedAt int64         `json:"editedAt,omitempty"`
	Deleted  bool          `json:"deleted,omitempty"`
	Delivery DeliveryState `json:"delivery,omitempty"`
	Poll     *Poll         `json:"poll,omitempty"`
	Media    *Media        `json:"media,omitempty"` // al terminar una descarga
}

type ReactionEvent struct {
	MessageKey MessageKey `json:"messageKey"`
	Reaction   Reaction   `json:"reaction"`
}

type ReceiptEvent struct {
	ChatKey     ChatKey       `json:"chatKey"`
	MessageKeys []MessageKey  `json:"messageKeys"`
	Delivery    DeliveryState `json:"delivery"`
	ReaderID    string        `json:"readerId"`
	At          int64         `json:"at"`
}

type TypingEvent struct {
	ChatKey   ChatKey `json:"chatKey"`
	PeerID    string  `json:"peerId"`
	Typing    bool    `json:"typing"`
	Recording bool    `json:"recording"`
}

type HistoryBatch struct {
	Chats    []Chat    `json:"chats"`
	Messages []Message `json:"messages"`
	Persons  []Person  `json:"persons"`
	Progress float64   `json:"progress"` // 0..1, -1 si no se sabe
}

// Event: unión etiquetada. Solo uno de los punteros va lleno según Type.
type Event struct {
	ID            string         `json:"id"` // ULID que pone el Router
	Type          EventType      `json:"type"`
	AccountID     AccountID      `json:"accountId"`
	Channel       Channel        `json:"channel"`
	At            int64          `json:"at"`
	AccountStatus *AccountStatus `json:"accountStatus,omitempty"`
	Chat          *Chat          `json:"chat,omitempty"`
	ChatKey       ChatKey        `json:"chatKey,omitempty"`
	Message       *Message       `json:"message,omitempty"`
	Update        *MessageUpdate `json:"update,omitempty"`
	Reaction      *ReactionEvent `json:"reaction,omitempty"`
	Receipt       *ReceiptEvent  `json:"receipt,omitempty"`
	Typing        *TypingEvent   `json:"typing,omitempty"`
	Person        *Person        `json:"person,omitempty"`
	Status        *Status        `json:"status,omitempty"`
	StatusView    *StatusView    `json:"statusView,omitempty"`
	History       *HistoryBatch  `json:"history,omitempty"`
	Capabilities  any            `json:"capabilities,omitempty"` // capability.Set (evita ciclo de import)
}
