package model

type ContentKind string

const (
	KindText        ContentKind = "text"
	KindImage       ContentKind = "image"
	KindVideo       ContentKind = "video"
	KindAudio       ContentKind = "audio"
	KindVoice       ContentKind = "voice"
	KindDocument    ContentKind = "document"
	KindSticker     ContentKind = "sticker"
	KindLocation    ContentKind = "location"
	KindContact     ContentKind = "contact"
	KindPoll        ContentKind = "poll"
	KindSystem      ContentKind = "system"      // altas/bajas de grupo, cambios de título…
	KindUnsupported ContentKind = "unsupported" // se muestra «Mensaje no compatible»
)

type DeliveryState string

const (
	DeliveryPending   DeliveryState = "pending"
	DeliverySent      DeliveryState = "sent" // llegó al servidor
	DeliveryDelivered DeliveryState = "delivered"
	DeliveryRead      DeliveryState = "read"
	DeliveryPlayed    DeliveryState = "played"
	DeliveryFailed    DeliveryState = "failed"
)

type Media struct {
	ID        string  `json:"id"` // único dentro de la cuenta
	Mime      string  `json:"mime"`
	FileName  string  `json:"fileName"`
	Size      int64   `json:"size"`
	Width     int     `json:"width"`
	Height    int     `json:"height"`
	Duration  float64 `json:"duration"` // segundos
	Thumb     string  `json:"thumb"`    // data URL JPEG pequeño, "" si no hay
	LocalPath string  `json:"-"`        // ruta en disco cuando ya se descargó
	URL       string  `json:"url"`      // "/media/<accountId>/<mediaId>" (lo sirve ui/wails)
	Handle    []byte  `json:"-"`        // dato opaco del adaptador para descargar (p. ej. proto serializado)
}

type Location struct {
	Lat     float64 `json:"lat"`
	Lng     float64 `json:"lng"`
	Name    string  `json:"name"`
	Address string  `json:"address"`
	Live    bool    `json:"live"`
}

type ContactCard struct {
	Name  string `json:"name"`
	Phone string `json:"phone"`
	VCard string `json:"vcard"`
}

type PollOption struct {
	Name   string   `json:"name"`
	Voters []string `json:"voters"` // ids nativos
}

type Poll struct {
	Question string       `json:"question"`
	Options  []PollOption `json:"options"`
	Multi    bool         `json:"multi"`
}

type Reaction struct {
	SenderNativeID string `json:"senderNativeId"`
	SenderName     string `json:"senderName"`
	Emoji          string `json:"emoji"` // "" = reacción quitada
	At             int64  `json:"at"`
}

type Message struct {
	Key            MessageKey    `json:"key"`
	ChatKey        ChatKey       `json:"chatKey"`
	AccountID      AccountID     `json:"accountId"`
	Channel        Channel       `json:"channel"`
	NativeID       string        `json:"nativeId"`
	ChatNativeID   string        `json:"chatNativeId"`
	SenderNativeID string        `json:"senderNativeId"`
	SenderName     string        `json:"senderName"`
	FromMe         bool          `json:"fromMe"`
	Timestamp      int64         `json:"timestamp"` // ms
	Kind           ContentKind   `json:"kind"`
	Text           string        `json:"text"` // cuerpo o pie de foto
	Media          *Media        `json:"media,omitempty"`
	Location       *Location     `json:"location,omitempty"`
	Contacts       []ContactCard `json:"contacts,omitempty"`
	Poll           *Poll         `json:"poll,omitempty"`
	QuotedNativeID string        `json:"quotedNativeId,omitempty"`
	QuotedPreview  string        `json:"quotedPreview,omitempty"`
	QuotedSender   string        `json:"quotedSender,omitempty"`
	Forwarded      bool          `json:"forwarded"`
	Mentions       []string      `json:"mentions,omitempty"`
	EditedAt       int64         `json:"editedAt"`  // 0 = no
	DeletedAt      int64         `json:"deletedAt"` // 0 = no; borrado para todos
	Delivery       DeliveryState `json:"delivery"`
	Reactions      []Reaction    `json:"reactions,omitempty"`
	FromStatusID   string        `json:"fromStatusId,omitempty"` // respuesta a un estado
}

func (m Message) Ref() MessageRef {
	return MessageRef{ChatNativeID: m.ChatNativeID, NativeID: m.NativeID, SenderNativeID: m.SenderNativeID, FromMe: m.FromMe, Timestamp: m.Timestamp}
}

// Outgoing: lo que se pide enviar. Exactamente uno de los grupos según Kind.
type Outgoing struct {
	Kind      ContentKind  `json:"kind"`
	Text      string       `json:"text"`               // texto o pie
	FilePath  string       `json:"filePath,omitempty"` // ruta local (image/video/audio/voice/document/sticker)
	FileName  string       `json:"fileName,omitempty"`
	Mime      string       `json:"mime,omitempty"`
	Location  *Location    `json:"location,omitempty"`
	Contact   *ContactCard `json:"contact,omitempty"`
	Poll      *Poll        `json:"poll,omitempty"` // Voters vacío
	ReplyTo   *MessageRef  `json:"replyTo,omitempty"`
	Mentions  []string     `json:"mentions,omitempty"`
	ClientRef string       `json:"clientRef,omitempty"` // id que pone quien pide (interfaz/servidor) para casar la respuesta
}

type MessagePage struct {
	Messages []Message `json:"messages"` // de más antiguo a más reciente
	HasMore  bool      `json:"hasMore"`
}
