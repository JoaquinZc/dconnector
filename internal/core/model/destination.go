package model

type DestinationFilter struct {
	AccountIDs    []AccountID `json:"accountIds"` // vacío = todas
	Channels      []Channel   `json:"channels"`   // vacío = todos
	Events        []string    `json:"events"`     // vacío = todos (nombres de 10-PROTOCOLO §4)
	IncludeFromMe bool        `json:"includeFromMe"`
	IncludeGroups bool        `json:"includeGroups"`
	// ContactScope: de qué contactos recibe el destino (por listas). Un destino guardado sin este
	// campo recibe de todos, como antes.
	ContactScope ContactScope `json:"contactScope"`
}

// Modos de ContactScope.Mode.
const (
	ContactScopeAll   = "all"   // todos los contactos (también el valor vacío)
	ContactScopeLists = "lists" // solo los contactos de las listas incluidas
)

// ContactScope: alcance de un destino sobre los contactos. Primero la inclusión (todos, o solo las
// listas de IncludeListIDs) y después la exclusión (los de ExcludeListIDs nunca pasan).
type ContactScope struct {
	Mode           string   `json:"mode"`           // "all" | "lists"; vacío = "all"
	IncludeListIDs []string `json:"includeListIds"` // solo con Mode "lists"
	ExcludeListIDs []string `json:"excludeListIds"` // siempre se restan
}

// OnlyLists indica si el alcance se limita a las listas incluidas. Cualquier modo que no sea
// vacío ni «all» se trata como «lists» (ante la duda, no se abre).
func (s ContactScope) OnlyLists() bool {
	return s.Mode != "" && s.Mode != ContactScopeAll
}

// Restricts indica si el alcance deja fuera a alguien (si no, no hace falta consultar nada).
func (s ContactScope) Restricts() bool {
	return s.OnlyLists() || len(s.ExcludeListIDs) > 0
}

// Allows decide si pasa alguien que está en esas listas (listIDs vacío: sin contacto, o contacto
// sin listas). Pasa si (Mode «all» o está en alguna lista incluida) y no está en ninguna excluida.
func (s ContactScope) Allows(listIDs []string) bool {
	for _, id := range listIDs {
		for _, ex := range s.ExcludeListIDs {
			if id == ex {
				return false
			}
		}
	}
	if !s.OnlyLists() {
		return true
	}
	for _, id := range listIDs {
		for _, in := range s.IncludeListIDs {
			if id == in {
				return true
			}
		}
	}
	return false
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
