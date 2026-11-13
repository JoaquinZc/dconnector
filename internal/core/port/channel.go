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
	SyncChats(ctx context.Context) error                                                                // emite chat.updated de todos los chats que conozca
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
// Implementa solo los métodos opcionales; los obligatorios (Start, Send…) los escribe cada adaptador.
type Base struct{}

func (Base) Edit(ctx context.Context, msg model.MessageRef, newText string) error {
	return ErrUnsupported
}

func (Base) Forward(ctx context.Context, msg model.MessageRef, toChatNativeID string) (model.Message, error) {
	return model.Message{}, ErrUnsupported
}

func (Base) VotePoll(ctx context.Context, msg model.MessageRef, options []string) error {
	return ErrUnsupported
}

func (Base) RequestHistory(ctx context.Context, chatNativeID string, before *model.MessageRef, count int) error {
	return ErrUnsupported
}

func (Base) ArchiveChat(ctx context.Context, chatNativeID string, archived bool) error {
	return ErrUnsupported
}

func (Base) PinChat(ctx context.Context, chatNativeID string, pinned bool) error {
	return ErrUnsupported
}

func (Base) MuteChat(ctx context.Context, chatNativeID string, until int64) error {
	return ErrUnsupported
}

func (Base) ListPersons(ctx context.Context) ([]model.Person, error) {
	return nil, ErrUnsupported
}

func (Base) PostStatus(ctx context.Context, s model.OutgoingStatus) (model.Status, error) {
	return model.Status{}, ErrUnsupported
}

func (Base) DeleteStatus(ctx context.Context, statusNativeID string) error {
	return ErrUnsupported
}

func (Base) MarkStatusViewed(ctx context.Context, st model.Status) error {
	return ErrUnsupported
}

func (Base) ReactStatus(ctx context.Context, st model.Status, emoji string) error {
	return ErrUnsupported
}

func (Base) StatusViewers(ctx context.Context, statusNativeID string) ([]model.StatusView, error) {
	return nil, ErrUnsupported
}
