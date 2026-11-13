# 04 · Puertos (código literal)

`internal/core/port/*.go`. Se copia tal cual. **Ningún agente cambia estas firmas.**

## errors.go

```go
package port

import "errors"

var (
	ErrUnsupported  = errors.New("no admitido por este canal")
	ErrNotConnected = errors.New("la cuenta no está conectada")
	ErrNotFound     = errors.New("no encontrado")
	ErrInvalid      = errors.New("datos no válidos")
	ErrRateLimited  = errors.New("límite de envío alcanzado")
)
```

## channel.go

```go
package port

import (
	"context"
	"database/sql"
	"io"
	"log/slog"

	"dconnector/internal/core/capability"
	"dconnector/internal/core/model"
)

// EventSink: por donde el adaptador entrega todo lo que pasa. Emit no bloquea (ver 02 §5).
// El adaptador rellena Type, AccountID, Channel, At y el puntero que toque; el Router pone ID y Keys.
type EventSink interface {
	Emit(ev model.Event)
}

type AdapterDeps struct {
	Account   model.Account
	DB        *sql.DB // account.db de esta cuenta (compartido con AccountStore)
	DBPath    string
	MediaDir  string // accounts/<id>/media
	AvatarDir string // accounts/<id>/avatars
	Log       *slog.Logger
}

type AdapterFactory func(deps AdapterDeps) (ChannelAdapter, error)

// ChannelAdapter: una instancia por cuenta. Lo que el canal no admite devuelve ErrUnsupported.
// Todos los métodos son seguros para llamadas concurrentes.
type ChannelAdapter interface {
	Channel() model.Channel

	// Capabilities de la cuenta (chat == nil) o de un chat concreto (p. ej. canal de solo lectura).
	Capabilities(chat *model.Chat) capability.Set

	// Start conecta con la sesión guardada y vuelve enseguida; la conexión vive en goroutines propias
	// hasta Stop o hasta que ctx se cancele. Si no hay sesión, emite AccountStatus{State: pairing}.
	Start(ctx context.Context, sink EventSink) error
	Stop(ctx context.Context) error
	// Logout cierra la sesión en el servidor del canal (dispositivo vinculado) y borra la sesión local.
	Logout(ctx context.Context) error

	// Vinculación: BeginPairing arranca el método; el adaptador informa por AccountStatus (QR, código,
	// qué falta). SubmitPairing entrega lo que pidió (código, contraseña, cookies).
	BeginPairing(ctx context.Context, req model.PairRequest) error
	SubmitPairing(ctx context.Context, in model.PairInput) error

	// Mensajes
	Send(ctx context.Context, chatNativeID string, out model.Outgoing) (model.Message, error)
	React(ctx context.Context, msg model.MessageRef, emoji string) error // "" = quitar
	Edit(ctx context.Context, msg model.MessageRef, newText string) error
	Delete(ctx context.Context, msg model.MessageRef, forEveryone bool) error
	Forward(ctx context.Context, msg model.MessageRef, toChatNativeID string) (model.Message, error)
	VotePoll(ctx context.Context, msg model.MessageRef, options []string) error
	MarkRead(ctx context.Context, chatNativeID string, upTo []model.MessageRef) error
	SetTyping(ctx context.Context, chatNativeID string, typing bool) error

	// Medios: devuelve el contenido descifrado. El servicio lo guarda en MediaDir.
	DownloadMedia(ctx context.Context, m model.Media) (io.ReadCloser, error)
	// Avatar: guarda en AvatarDir y devuelve el nombre de archivo ("" si no tiene).
	FetchAvatar(ctx context.Context, peerOrChatNativeID string) (string, error)

	// Chats e historial
	SyncChats(ctx context.Context) error // emite chat.updated de todos los chats que conozca
	RequestHistory(ctx context.Context, chatNativeID string, before *model.MessageRef, count int) error // emite history.batch
	ArchiveChat(ctx context.Context, chatNativeID string, archived bool) error
	PinChat(ctx context.Context, chatNativeID string, pinned bool) error
	MuteChat(ctx context.Context, chatNativeID string, until int64) error

	// Personas
	ResolvePerson(ctx context.Context, peerNativeID string) (model.Person, error)
	// ListPersons: agenda/contactos que el canal conozca (para importar a Contactos).
	ListPersons(ctx context.Context) ([]model.Person, error)

	// Estados / historias
	PostStatus(ctx context.Context, s model.OutgoingStatus) (model.Status, error)
	DeleteStatus(ctx context.Context, statusNativeID string) error
	MarkStatusViewed(ctx context.Context, st model.Status) error
	ReactStatus(ctx context.Context, st model.Status, emoji string) error
	StatusViewers(ctx context.Context, statusNativeID string) ([]model.StatusView, error)
}

// Base: los adaptadores la incrustan para devolver ErrUnsupported en lo que no implementan.
type Base struct{}
```

Además, en `channel.go`, `Base` implementa **todos** los métodos opcionales con
`return ErrUnsupported` (o valores cero + `ErrUnsupported`): `Forward`, `VotePoll`, `PostStatus`,
`DeleteStatus`, `MarkStatusViewed`, `ReactStatus`, `StatusViewers`, `RequestHistory`,
`ArchiveChat`, `PinChat`, `MuteChat`, `ListPersons`, `Edit`. Cada adaptador incrusta `port.Base` y
sobrescribe lo que su canal sí admite.

## store.go

```go
package port

import (
	"context"

	"dconnector/internal/core/model"
)

// AccountStore: account.db de una cuenta. Toda escritura es idempotente (upsert por clave).
type AccountStore interface {
	AccountID() model.AccountID
	Close() error

	UpsertChat(ctx context.Context, c model.Chat) error
	// PatchChat aplica solo los campos de la lista `fields` (nombres JSON: "title", "unreadCount"…).
	PatchChat(ctx context.Context, c model.Chat, fields []string) error
	GetChat(ctx context.Context, nativeID string) (model.Chat, error) // ErrNotFound
	ListChats(ctx context.Context, f model.ChatFilter) ([]model.Chat, error)
	DeleteChat(ctx context.Context, nativeID string) error

	UpsertPerson(ctx context.Context, p model.Person) error
	GetPerson(ctx context.Context, nativeID string) (model.Person, error)
	ListPersons(ctx context.Context, query string, limit int) ([]model.Person, error)

	// UpsertMessage: si existe, conserva Reactions, Media.LocalPath y Delivery mayor.
	UpsertMessage(ctx context.Context, m model.Message) (created bool, err error)
	UpsertMessages(ctx context.Context, ms []model.Message) (createdCount int, err error) // una transacción
	ApplyUpdate(ctx context.Context, u model.MessageUpdate) (model.Message, error)
	ApplyReaction(ctx context.Context, r model.ReactionEvent) (model.Message, error)
	ApplyReceipt(ctx context.Context, r model.ReceiptEvent) error
	GetMessage(ctx context.Context, chatNativeID, nativeID string) (model.Message, error)
	ListMessages(ctx context.Context, chatNativeID string, beforeTs int64, limit int) (model.MessagePage, error)
	SearchMessages(ctx context.Context, query string, chatNativeID string, limit int) ([]model.Message, error) // FTS5
	SetMediaPath(ctx context.Context, mediaID, localPath string) error
	GetMedia(ctx context.Context, mediaID string) (model.Media, string /*msg chat*/, error)
	// Para estadísticas: todos los mensajes (sin texto) desde `from` hasta `to`.
	MessageStats(ctx context.Context, from, to int64, chatNativeIDs []string) ([]StatRow, error)

	UpsertStatus(ctx context.Context, s model.Status) error
	ListStatusAuthors(ctx context.Context, now int64) ([]model.StatusAuthor, error)
	ListStatuses(ctx context.Context, authorID string, now int64) ([]model.Status, error)
	ListMyStatuses(ctx context.Context, from, to int64) ([]model.Status, error)
	UpsertStatusView(ctx context.Context, v model.StatusView) error
	ListStatusViews(ctx context.Context, statusKey string) ([]model.StatusView, error)

	// KV para el estado propio del adaptador (sesión de Telegram, cursores, cookies de Meta…).
	KVGet(ctx context.Context, key string) ([]byte, error) // nil, nil si no existe
	KVSet(ctx context.Context, key string, value []byte) error
}

type StatRow struct {
	ChatNativeID string
	ChatKind     model.ChatKind
	SenderID     string
	FromMe       bool
	Timestamp    int64
	Kind         model.ContentKind
	TextLen      int
}

// AppStore: app.db.
type AppStore interface {
	Close() error

	// Cuentas
	ListAccounts(ctx context.Context) ([]model.Account, error)
	GetAccount(ctx context.Context, id model.AccountID) (model.Account, error)
	SaveAccount(ctx context.Context, a model.Account) error
	DeleteAccount(ctx context.Context, id model.AccountID) error // borra también identidades e items de cola

	// Ajustes (valor JSON)
	GetSetting(ctx context.Context, key string) (string, error) // "" si no existe
	SetSetting(ctx context.Context, key, value string) error

	// Contactos y listas
	ListContacts(ctx context.Context, f model.ContactFilter) ([]model.Contact, int /*total*/, error)
	GetContact(ctx context.Context, id string) (model.Contact, error)
	FindContactByIdentity(ctx context.Context, acc model.AccountID, peerNativeID string) (model.Contact, error)
	FindContactsByPhone(ctx context.Context, phone string) ([]model.Contact, error)
	SaveContact(ctx context.Context, c model.Contact) error // guarda identidades, etiquetas y listas
	DeleteContact(ctx context.Context, id string) error
	ListLists(ctx context.Context) ([]model.ContactList, error)
	SaveList(ctx context.Context, l model.ContactList) error
	DeleteList(ctx context.Context, id string) error
	SetListMembers(ctx context.Context, listID string, add, remove []string) error
	ListMembers(ctx context.Context, listIDs []string) ([]model.Contact, error) // únicos

	// Campañas
	ListCampaigns(ctx context.Context) ([]model.Campaign, error)
	GetCampaign(ctx context.Context, id string) (model.Campaign, error)
	SaveCampaign(ctx context.Context, c model.Campaign) error
	DeleteCampaign(ctx context.Context, id string) error
	ReplaceQueue(ctx context.Context, campaignID string, items []model.QueueItem) error
	ListQueue(ctx context.Context, campaignID string, states []string, limit int) ([]model.QueueItem, error)
	UpdateQueueItem(ctx context.Context, it model.QueueItem) error
	CountSentSince(ctx context.Context, acc model.AccountID, since int64) (int, error) // límites hora/día
	QueueItemByMessage(ctx context.Context, key model.MessageKey) (model.QueueItem, error)

	// Programados
	ListScheduled(ctx context.Context, from, to int64, states []string) ([]model.Scheduled, error)
	DueScheduled(ctx context.Context, now int64) ([]model.Scheduled, error)
	SaveScheduled(ctx context.Context, s model.Scheduled) error
	DeleteScheduled(ctx context.Context, id string) error

	// Destinos y outbox
	ListDestinations(ctx context.Context) ([]model.Destination, error)
	SaveDestination(ctx context.Context, d model.Destination) error
	DeleteDestination(ctx context.Context, id string) error // borra su outbox
	OutboxAdd(ctx context.Context, destinationID string, eventID string, payload []byte) error
	OutboxPending(ctx context.Context, destinationID string, limit int) ([]OutboxRow, error)
	OutboxAck(ctx context.Context, destinationID string, eventIDs []string) error
	OutboxCount(ctx context.Context, destinationID string) (int, error)
	OutboxTrim(ctx context.Context, olderThan int64, maxRows int) (int, error)
}

type OutboxRow struct {
	EventID   string
	Payload   []byte
	CreatedAt int64
	Attempts  int
}
```
