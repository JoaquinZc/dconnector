# 03 · Dominio (código literal)

Se copia tal cual en `internal/core/model/*.go` y `internal/core/capability/capability.go`. Las
etiquetas JSON son el contrato con la interfaz y con el protocolo: **no se renombran**.

## model/ids.go

```go
package model

import (
	"strings"

	"github.com/oklog/ulid/v2"
)

type Channel string

const (
	ChannelWhatsApp  Channel = "whatsapp"
	ChannelTelegram  Channel = "telegram"
	ChannelMessenger Channel = "messenger"
	ChannelInstagram Channel = "instagram"
)

var AllChannels = []Channel{ChannelWhatsApp, ChannelTelegram, ChannelMessenger, ChannelInstagram}

// AccountID: prefijo del canal + ULID en minúsculas. Ej.: "wa-01jc…", "tg-…", "fb-…", "ig-…".
type AccountID string

func NewAccountID(c Channel) AccountID {
	p := map[Channel]string{ChannelWhatsApp: "wa", ChannelTelegram: "tg", ChannelMessenger: "fb", ChannelInstagram: "ig"}[c]
	return AccountID(p + "-" + strings.ToLower(ulid.Make().String()))
}

// NewID para el resto de entidades de app.db (contactos, listas, campañas…).
func NewID() string { return strings.ToLower(ulid.Make().String()) }

const keySep = "|"

// ChatKey = "<accountId>|<idNativoDelChat>"
type ChatKey string

func NewChatKey(acc AccountID, nativeChatID string) ChatKey {
	return ChatKey(string(acc) + keySep + nativeChatID)
}

func (k ChatKey) Split() (AccountID, string) {
	p := strings.SplitN(string(k), keySep, 2)
	if len(p) != 2 {
		return "", ""
	}
	return AccountID(p[0]), p[1]
}

// MessageKey = "<accountId>|<idNativoDelChat>|<idNativoDelMensaje>"
type MessageKey string

func NewMessageKey(acc AccountID, nativeChatID, nativeMsgID string) MessageKey {
	return MessageKey(string(acc) + keySep + nativeChatID + keySep + nativeMsgID)
}

func (k MessageKey) Split() (AccountID, string, string) {
	p := strings.SplitN(string(k), keySep, 3)
	if len(p) != 3 {
		return "", "", ""
	}
	return AccountID(p[0]), p[1], p[2]
}

func (k MessageKey) Chat() ChatKey { a, c, _ := k.Split(); return NewChatKey(a, c) }

// MessageRef: lo mínimo que un adaptador necesita para actuar sobre un mensaje.
type MessageRef struct {
	ChatNativeID   string `json:"chatNativeId"`
	NativeID       string `json:"nativeId"`
	SenderNativeID string `json:"senderNativeId"`
	FromMe         bool   `json:"fromMe"`
	Timestamp      int64  `json:"timestamp"` // ms
}
```

## model/account.go

```go
package model

type Account struct {
	ID         AccountID         `json:"id"`
	Channel    Channel           `json:"channel"`
	Label      string            `json:"label"`      // nombre que pone el usuario: «Ventas», «Personal»
	Color      string            `json:"color"`      // "#RRGGBB" de la paleta de 11-INTERFAZ §5
	Enabled    bool              `json:"enabled"`
	SelfID     string            `json:"selfId"`     // id nativo propio (JID, user:<id>…)
	SelfName   string            `json:"selfName"`
	SelfHandle string            `json:"selfHandle"` // +593…, @usuario
	CreatedAt  int64             `json:"createdAt"`
	Config     map[string]string `json:"config"`     // telegram: "api_id", "api_hash"
}

type ConnState string

const (
	StateDisconnected ConnState = "disconnected"
	StateConnecting   ConnState = "connecting"
	StatePairing      ConnState = "pairing"   // esperando QR, código o credenciales
	StateConnected    ConnState = "connected"
	StateLoggedOut    ConnState = "logged_out" // la sesión ya no vale: hay que volver a vincular
	StateError        ConnState = "error"
)

// PairNeed: qué le falta al adaptador durante la vinculación.
type PairNeed string

const (
	NeedNone      PairNeed = ""
	NeedScanQR    PairNeed = "scan_qr"    // mostrar AccountStatus.QRCode
	NeedEnterCode PairNeed = "enter_code" // mostrar AccountStatus.PairCode para teclearlo en el teléfono (WhatsApp)
	NeedPhone     PairNeed = "phone"      // pedir número (Telegram, WhatsApp por código)
	NeedLoginCode PairNeed = "login_code" // pedir código recibido (Telegram)
	NeedPassword  PairNeed = "password"   // 2FA (Telegram)
	NeedCookies   PairNeed = "cookies"    // Meta
)

type AccountStatus struct {
	AccountID AccountID `json:"accountId"`
	State     ConnState `json:"state"`
	Need      PairNeed  `json:"need"`
	QRCode    string    `json:"qrCode,omitempty"`   // texto a pintar como QR
	PairCode  string    `json:"pairCode,omitempty"` // "ABCD-EFGH"
	Detail    string    `json:"detail,omitempty"`   // texto para el usuario, en español
	Since     int64     `json:"since"`
}

// Pairing: lo que la interfaz manda durante la vinculación.
type PairRequest struct {
	Method string `json:"method"` // "qr" | "phone" (WhatsApp) | "login" (Telegram) | "cookies" | "password_login" (Meta)
	Phone  string `json:"phone,omitempty"`
}

type PairInput struct {
	Code     string `json:"code,omitempty"`
	Password string `json:"password,omitempty"`
	Username string `json:"username,omitempty"`
	Cookies  string `json:"cookies,omitempty"`
}
```

## model/chat.go

```go
package model

type ChatKind string

const (
	ChatDirect  ChatKind = "direct"
	ChatGroup   ChatKind = "group"
	ChatChannel ChatKind = "channel" // canal de Telegram / canal de WhatsApp
	ChatSelf    ChatKind = "self"    // chat contigo mismo / Mensajes guardados
)

type Chat struct {
	Key           ChatKey   `json:"key"`
	AccountID     AccountID `json:"accountId"`
	Channel       Channel   `json:"channel"`
	NativeID      string    `json:"nativeId"`
	Kind          ChatKind  `json:"kind"`
	Title         string    `json:"title"`
	AvatarURL     string    `json:"avatarUrl"` // "/avatar/<accountId>/<archivo>" o ""
	UnreadCount   int       `json:"unreadCount"`
	LastMessageAt int64     `json:"lastMessageAt"`
	LastPreview   string    `json:"lastPreview"` // texto corto: «📷 Foto», «Hola…»
	LastFromMe    bool      `json:"lastFromMe"`
	Archived      bool      `json:"archived"`
	Pinned        bool      `json:"pinned"`
	MutedUntil    int64     `json:"mutedUntil"` // 0 = no; -1 = siempre
	ReadOnly      bool      `json:"readOnly"`   // canal sin permiso de publicar
	PeerNativeID  string    `json:"peerNativeId"` // en directos: la otra persona
	ContactID     string    `json:"contactId"`    // contacto unificado de app.db, "" si no hay
}

type Person struct {
	AccountID AccountID `json:"accountId"`
	NativeID  string    `json:"nativeId"`
	Name      string    `json:"name"`     // nombre de agenda si lo hay
	PushName  string    `json:"pushName"` // nombre que la persona se puso
	Phone     string    `json:"phone"`    // E.164 "+593…" o ""
	Username  string    `json:"username"`
	AvatarURL string    `json:"avatarUrl"`
}

func (p Person) Display() string {
	switch {
	case p.Name != "":
		return p.Name
	case p.PushName != "":
		return p.PushName
	case p.Username != "":
		return "@" + p.Username
	case p.Phone != "":
		return p.Phone
	}
	return p.NativeID
}

type ChatFilter struct {
	AccountIDs []AccountID `json:"accountIds"` // vacío = todas
	Channels   []Channel   `json:"channels"`   // vacío = todos
	Tab        string      `json:"tab"`        // "all" | "unread" | "groups" | "archived"
	Query      string      `json:"query"`
	Before     int64       `json:"before"` // paginación por LastMessageAt; 0 = desde lo más reciente
	Limit      int         `json:"limit"`  // por defecto 60
}
```

## model/message.go

```go
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
	DeliverySent      DeliveryState = "sent"      // llegó al servidor
	DeliveryDelivered DeliveryState = "delivered"
	DeliveryRead      DeliveryState = "read"
	DeliveryPlayed    DeliveryState = "played"
	DeliveryFailed    DeliveryState = "failed"
)

type Media struct {
	ID        string  `json:"id"`       // único dentro de la cuenta
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
	Text      string       `json:"text"`                // texto o pie
	FilePath  string       `json:"filePath,omitempty"`  // ruta local (image/video/audio/voice/document/sticker)
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
```

## model/status.go

```go
package model

type Status struct {
	Key            string        `json:"key"` // "<accountId>|<idNativo>"
	AccountID      AccountID     `json:"accountId"`
	Channel        Channel       `json:"channel"`
	NativeID       string        `json:"nativeId"`
	AuthorNativeID string        `json:"authorNativeId"`
	AuthorName     string        `json:"authorName"`
	FromMe         bool          `json:"fromMe"`
	PostedAt       int64         `json:"postedAt"`
	ExpiresAt      int64         `json:"expiresAt"`
	Kind           ContentKind   `json:"kind"` // text | image | video
	Text           string        `json:"text"`
	BgColor        string        `json:"bgColor"` // "#RRGGBB" en estados de texto
	Font           int           `json:"font"`
	Media          *Media        `json:"media,omitempty"`
	Viewed         bool          `json:"viewed"` // lo vi yo (estados ajenos)
	ViewCount      int           `json:"viewCount"`
	ReactionCount  int           `json:"reactionCount"`
	ReplyCount     int           `json:"replyCount"`
	DeletedAt      int64         `json:"deletedAt"`
	Delivery       DeliveryState `json:"delivery"`
}

type StatusView struct {
	StatusKey  string `json:"statusKey"`
	ViewerID   string `json:"viewerId"`
	ViewerName string `json:"viewerName"`
	At         int64  `json:"at"`
	Reaction   string `json:"reaction"` // "" si solo vio
}

type StatusAudience struct {
	Mode    string   `json:"mode"` // "contacts" | "allow" | "deny"
	PeerIDs []string `json:"peerIds"`
	ListIDs []string `json:"listIds"` // listas de app.db; el servicio las convierte en PeerIDs de esa cuenta
}

type OutgoingStatus struct {
	Kind     ContentKind    `json:"kind"` // text | image | video
	Text     string         `json:"text"`
	BgColor  string         `json:"bgColor"`
	Font     int            `json:"font"`
	FilePath string         `json:"filePath,omitempty"`
	Mime     string         `json:"mime,omitempty"`
	Audience StatusAudience `json:"audience"`
}

// StatusAuthor: fila de la lista «Estados» (una por persona con estados vigentes).
type StatusAuthor struct {
	AccountID  AccountID `json:"accountId"`
	AuthorID   string    `json:"authorId"`
	AuthorName string    `json:"authorName"`
	AvatarURL  string    `json:"avatarUrl"`
	Count      int       `json:"count"`
	Unviewed   int       `json:"unviewed"`
	LastAt     int64     `json:"lastAt"`
}
```

## model/contact.go

```go
package model

// Contact: persona unificada (app.db). Puede tener identidades en varias cuentas.
type Contact struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Phone      string     `json:"phone"` // E.164 principal
	Notes      string     `json:"notes"`
	Tags       []string   `json:"tags"`
	Identities []Identity `json:"identities"`
	ListIDs    []string   `json:"listIds"`
	OptedOut   bool       `json:"optedOut"` // no recibe campañas
	CreatedAt  int64      `json:"createdAt"`
	UpdatedAt  int64      `json:"updatedAt"`
}

type Identity struct {
	AccountID    AccountID `json:"accountId"`
	Channel      Channel   `json:"channel"`
	PeerNativeID string    `json:"peerNativeId"`
	DisplayName  string    `json:"displayName"`
	Phone        string    `json:"phone"`
}

type ContactList struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	MemberCount int    `json:"memberCount"`
}

type ContactFilter struct {
	Query  string `json:"query"`
	ListID string `json:"listId"`
	Tag    string `json:"tag"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"` // por defecto 100
}
```

## model/campaign.go

```go
package model

type CampaignState string

const (
	CampaignDraft    CampaignState = "draft"
	CampaignReady    CampaignState = "ready"    // programada, esperando StartAt
	CampaignRunning  CampaignState = "running"
	CampaignPaused   CampaignState = "paused"   // a mano
	CampaignBraked   CampaignState = "braked"   // freno automático (pacing.brakeReason)
	CampaignDone     CampaignState = "done"
	CampaignCanceled CampaignState = "canceled"
)

type SendWindow struct {
	FromMinute int   `json:"fromMinute"` // minutos desde 00:00 locales; 540 = 09:00
	ToMinute   int   `json:"toMinute"`
	Weekdays   []int `json:"weekdays"` // 0=domingo … 6=sábado
}

// PacingSettings: ritmo anti-bloqueo POR CUENTA. Sale de la base: outreach.ts (SAFE_SENDING),
// pacing.ts (PACING_DEFAULTS) y los ajustes freq_* de schema.ts. Los valores por defecto están en
// DefaultPacing y son los mismos de D'Ventas.
type PacingSettings struct {
	MinDelaySec      int     `json:"minDelaySec"`      // espera entre envíos a conocidos/sin respuesta
	MaxDelaySec      int     `json:"maxDelaySec"`
	ColdMinDelaySec  int     `json:"coldMinDelaySec"`  // espera con contactos sin conversación
	ColdMaxDelaySec  int     `json:"coldMaxDelaySec"`
	DailyLimit       int     `json:"dailyLimit"`       // 0 = sin límite
	ColdDailyLimit   int     `json:"coldDailyLimit"`   // 0 = sin límite propio
	TypingMsPerChar  int     `json:"typingMsPerChar"`  // «escribiendo…» antes de enviar
	BatchSize        int     `json:"batchSize"`        // 0 = sin tandas
	BatchRestMinutes int     `json:"batchRestMinutes"` // ±30 %
	HourlyLimit      int     `json:"hourlyLimit"`      // 0 = sin límite
	WarmupEnabled    bool    `json:"warmupEnabled"`
	WarmupStart      int     `json:"warmupStart"`
	WarmupDays       int     `json:"warmupDays"`
	BrakeEnabled     bool    `json:"brakeEnabled"`
	BrakeFailRate    float64 `json:"brakeFailRate"`   // %
	BrakeMinSent     int     `json:"brakeMinSent"`
	BrakeUnreadRate  float64 `json:"brakeUnreadRate"` // %
	BrakeOptOuts     int     `json:"brakeOptOuts"`
	FreqMax          int     `json:"freqMax"`  // campañas máx. por contacto en FreqDays (0 = sin límite)
	FreqDays         int     `json:"freqDays"`
}

var DefaultPacing = PacingSettings{
	MinDelaySec: 20, MaxDelaySec: 45, ColdMinDelaySec: 60, ColdMaxDelaySec: 120,
	DailyLimit: 200, ColdDailyLimit: 20, TypingMsPerChar: 300,
	BatchSize: 25, BatchRestMinutes: 10, HourlyLimit: 60,
	WarmupEnabled: true, WarmupStart: 20, WarmupDays: 14,
	BrakeEnabled: true, BrakeFailRate: 30, BrakeMinSent: 30, BrakeUnreadRate: 95, BrakeOptOuts: 3,
	FreqMax: 3, FreqDays: 7,
}

type Campaign struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	AccountID    AccountID      `json:"accountId"`
	State        CampaignState  `json:"state"`
	Text         string         `json:"text"`     // plantilla con {nombre} y {a|b} (template.ts)
	Variants     []string       `json:"variants"` // B…E (variants.ts)
	FilePath     string         `json:"filePath"` // adjunto opcional (copiado a <data>/campaign-media)
	ListIDs      []string       `json:"listIds"`
	ContactIDs   []string       `json:"contactIds"`
	StartAt      int64          `json:"startAt"` // 0 = al pulsar «Iniciar»
	Window       SendWindow     `json:"window"`
	Pacing       PacingSettings `json:"pacing"`
	OptOutFooter bool           `json:"optOutFooter"`
	ABEnabled    bool           `json:"abEnabled"`
	BrakeReason  string         `json:"brakeReason"`
	CreatedAt    int64          `json:"createdAt"`
	UpdatedAt    int64          `json:"updatedAt"`
	Totals       CampaignTotals `json:"totals"`
}

type CampaignTotals struct {
	Queued  int `json:"queued"`
	Sent    int `json:"sent"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
	Replied int `json:"replied"`
	OptOut  int `json:"optOut"`
}

type QueueItem struct {
	ID           string    `json:"id"`
	CampaignID   string    `json:"campaignId"`
	ContactID    string    `json:"contactId"`
	ChatNativeID string    `json:"chatNativeId"`
	Risk         string    `json:"risk"` // "warm" | "lukewarm" | "cold" (outreach.ts: WARM/LUKEWARM/COLD)
	Variant      int       `json:"variant"`
	ABSample     bool      `json:"abSample"`
	Text         string    `json:"text"` // ya personalizado
	State        string    `json:"state"` // "pending" | "held" | "sent" | "failed" | "skipped"
	SendAfter    int64     `json:"sendAfter"`
	SentAt       int64     `json:"sentAt"`
	MessageKey   MessageKey `json:"messageKey"`
	Error        string    `json:"error"`
}
```

## model/scheduled.go

```go
package model

type Scheduled struct {
	ID           string          `json:"id"`
	AccountID    AccountID       `json:"accountId"`
	Kind         string          `json:"kind"` // "message" | "status"
	ChatNativeID string          `json:"chatNativeId"`
	ChatTitle    string          `json:"chatTitle"`
	Message      *Outgoing       `json:"message,omitempty"`
	Status       *OutgoingStatus `json:"status,omitempty"`
	SendAt       int64           `json:"sendAt"`
	State        string          `json:"state"` // "pending" | "sent" | "failed" | "canceled"
	SentAt       int64           `json:"sentAt"`
	Error        string          `json:"error"`
	CreatedAt    int64           `json:"createdAt"`
}
```

## model/destination.go

```go
package model

type DestinationFilter struct {
	AccountIDs    []AccountID `json:"accountIds"` // vacío = todas
	Channels      []Channel   `json:"channels"`   // vacío = todos
	Events        []string    `json:"events"`     // vacío = todos (nombres de 10-PROTOCOLO §4)
	IncludeFromMe bool        `json:"includeFromMe"`
	IncludeGroups bool        `json:"includeGroups"`
}

type Destination struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	URL           string            `json:"url"`
	Token         string            `json:"token"`
	Enabled       bool              `json:"enabled"`
	AllowCommands bool              `json:"allowCommands"`
	Filter        DestinationFilter `json:"filter"`
	CreatedAt     int64             `json:"createdAt"`
}

type DestinationState struct {
	DestinationID string `json:"destinationId"`
	State         string `json:"state"` // "off" | "connecting" | "online" | "error"
	LastError     string `json:"lastError"`
	ConnectedAt   int64  `json:"connectedAt"`
	Pending       int    `json:"pending"` // eventos en outbox sin ack
}
```

## model/event.go

```go
package model

type EventType string

const (
	EvAccountStatus  EventType = "account.state"
	EvChatUpsert     EventType = "chat.updated"
	EvChatRemoved    EventType = "chat.removed"
	EvMessageNew     EventType = "message.received" // entrante, o saliente hecho desde otro dispositivo
	EvMessageSent    EventType = "message.sent"     // saliente hecho desde dConnector
	EvMessageUpdate  EventType = "message.updated"  // editado, borrado, entrega, votos
	EvReaction       EventType = "reaction"
	EvReceipt        EventType = "receipt"
	EvTyping         EventType = "typing"
	EvPerson         EventType = "person.updated"
	EvStatusUpsert   EventType = "status.posted"
	EvStatusView     EventType = "status.viewed"
	EvHistoryBatch   EventType = "history.batch" // no va al protocolo
	EvCapabilities   EventType = "account.capabilities"
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
	ChatKey  ChatKey `json:"chatKey"`
	PeerID   string  `json:"peerId"`
	Typing   bool    `json:"typing"`
	Recording bool   `json:"recording"`
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
```

## capability/capability.go

```go
package capability

import "time"

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

	Reply             bool `json:"reply"`
	Forward           bool `json:"forward"`
	React             bool `json:"react"`
	MaxReactionsPerUser int `json:"maxReactionsPerUser"` // 1 en WhatsApp

	Edit              bool          `json:"edit"`
	EditWindow        time.Duration `json:"editWindowMs"`   // 0 = sin límite (se serializa en ns; la interfaz usa EditWindowMs())
	DeleteForEveryone bool          `json:"deleteForEveryone"`
	DeleteWindow      time.Duration `json:"deleteWindowMs"`

	ReadReceipts bool `json:"readReceipts"`
	Typing       bool `json:"typing"`
	Mentions     bool `json:"mentions"`

	Groups bool `json:"groups"`

	// StartChat: se puede iniciar una conversación nueva con alguien que aún no ha escrito
	// (WhatsApp, Telegram); Messenger e Instagram solo continúan las que ya existen.
	StartChat bool `json:"startChat"`

	Statuses      bool `json:"statuses"`      // ver estados de otros
	PostStatus    bool `json:"postStatus"`
	StatusViewers bool `json:"statusViewers"` // ver quién vio los míos
	StatusAudience bool `json:"statusAudience"` // se puede elegir audiencia por estado (si no: la privacidad del teléfono)
	StatusReact   bool `json:"statusReact"`

	E2EEChats bool `json:"e2eeChats"` // Messenger: chats cifrados de extremo a extremo disponibles

	HistoryOnDemand bool `json:"historyOnDemand"`

	MaxFileSize  int64 `json:"maxFileSize"`  // bytes, documentos
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
```

> `EditWindow`/`DeleteWindow` se serializan a JSON en **milisegundos**: `Set` implementa
> `MarshalJSON` con un alias que escribe `int64(d / time.Millisecond)` en `editWindowMs` y
> `deleteWindowMs`. Prueba unitaria obligatoria de ida y vuelta.
