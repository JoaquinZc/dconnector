package model

// Origen de un contacto (Contact.Source).
const (
	ContactSourceManual = "manual" // creado o editado a mano (también el valor de un origen vacío)
	ContactSourceImport = "import" // importado (CSV o agenda de una cuenta)
	ContactSourceAuto   = "auto"   // creado solo a partir de las personas y chats de una cuenta
)

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
	// Source: "manual" | "import" | "auto" (vacío = "manual"). Un contacto «auto» se crea y se
	// completa solo; si el usuario le cambia el nombre pasa a «manual» y deja de refrescarse.
	Source string `json:"source"`
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
