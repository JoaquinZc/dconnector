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
