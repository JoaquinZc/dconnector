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
	MutedUntil    int64     `json:"mutedUntil"`   // 0 = no; -1 = siempre
	ReadOnly      bool      `json:"readOnly"`     // canal sin permiso de publicar
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
	// IncludeEmpty: incluye también los chats directos sin actividad (sin ningún mensaje y sin fijar),
	// que son los contactos de la libreta que el canal lista como conversaciones vacías. Por defecto
	// (false) una lista de chats solo muestra conversaciones con actividad.
	IncludeEmpty bool `json:"includeEmpty"`
}
