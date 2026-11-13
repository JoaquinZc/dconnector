package model

type Account struct {
	ID         AccountID         `json:"id"`
	Channel    Channel           `json:"channel"`
	Label      string            `json:"label"` // nombre que pone el usuario: «Ventas», «Personal»
	Color      string            `json:"color"` // "#RRGGBB" de la paleta de 11-INTERFAZ §5
	Enabled    bool              `json:"enabled"`
	SelfID     string            `json:"selfId"` // id nativo propio (JID, user:<id>…)
	SelfName   string            `json:"selfName"`
	SelfHandle string            `json:"selfHandle"` // +593…, @usuario
	CreatedAt  int64             `json:"createdAt"`
	Config     map[string]string `json:"config"` // telegram: "api_id", "api_hash"
}

type ConnState string

const (
	StateDisconnected ConnState = "disconnected"
	StateConnecting   ConnState = "connecting"
	StatePairing      ConnState = "pairing" // esperando QR, código o credenciales
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
