# 06 · Servicios del núcleo

Paquete `internal/core/service` salvo `bus`. Cada servicio es un struct con constructor `NewX(...)`
y métodos con `ctx context.Context` primero. Las firmas de aquí son las que llaman los servicios
Wails (11-INTERFAZ) y los comandos del protocolo (10-PROTOCOLO).

## 1. Bus (`core/bus`)

```go
type Bus struct{ /* mu, subs map[int]*sub, next int */ }
func New() *Bus
func (b *Bus) Publish(ev model.Event)                                // no bloquea
func (b *Bus) Subscribe(name string, buf int) (<-chan model.Event, func()) // func() = cancelar
func (b *Bus) Dropped() map[string]int64
```
Cada suscriptor: canal con búfer `buf`; si está lleno, se descarta para ese suscriptor y se suma
a `Dropped()[name]`. Pruebas: orden por suscriptor, descarte sin bloquear, cancelar cierra el canal.

## 2. Router (`router.go`)

```go
type OutboxWriter interface{ Enqueue(ctx context.Context, ev model.Event) }
type Router struct{ /* stores map[AccountID]port.AccountStore, app port.AppStore, bus, outbox, contacts */ }
func NewRouter(app port.AppStore, b *bus.Bus) *Router
func (r *Router) SetOutbox(o OutboxWriter)                 // lo pone main.go con el manager del protocolo
func (r *Router) AddObserver(o IncomingObserver)           // lo pone main.go con Campaigns (oleada 3)
func (r *Router) Attach(acc model.Account, st port.AccountStore) port.EventSink // un sink por cuenta
func (r *Router) SinkFor(id model.AccountID) (port.EventSink, bool)
func (r *Router) Detach(id model.AccountID)

type IncomingObserver interface {
	OnIncoming(acc model.AccountID, chatNativeID string, ts int64)
	OnOptOut(acc model.AccountID, contactID string)
}
```
Mientras no haya observadores ni outbox, el Router simplemente no los llama (comprobar `nil`).
El sink de cada cuenta tiene su canal (búfer 4.096) y una goroutine. Por cada evento, en orden:
1. `ev.ID = model.NewID()`, `ev.AccountID`, `ev.Channel`, `ev.At` (si 0, ahora).
2. Rellenar claves: `Chat.Key`, `Message.Key`, `Message.ChatKey`, `AccountID`, `Channel`.
3. Persistir según tipo:
   - `chat.updated` → `UpsertChat`; `message.received`/`message.sent` → `UpsertMessage` (si no es
     nuevo **y** ya se había publicado, se convierte en `message.updated` con los campos nuevos);
   - `message.updated` → `ApplyUpdate` (el evento publicado lleva el mensaje completo resultante
     en `ev.Message` además de `ev.Update`);
   - `reaction` → `ApplyReaction`; `receipt` → `ApplyReceipt`; `person.updated` → `UpsertPerson`;
   - `status.posted` → `UpsertStatus`; `status.viewed` → `UpsertStatusView`;
   - `history.batch` → `UpsertPerson` de cada persona, `UpsertChat` de cada chat y
     `UpsertMessages` (sin sumar no leídos);
   - `account.state` → no se persiste en `account.db`; solo se publica.
   - **Datos propios de la cuenta**: al conectar, el adaptador emite `person.updated` con su propia
     persona y además pone `ev.Person.NativeID == SelfID`. Para esa persona el Router actualiza
     `SelfID`, `SelfName` y `SelfHandle` de la cuenta en `app.db` (`SaveAccount`).
4. En chats directos: `chat.ContactID` = contacto unificado por identidad (`FindContactByIdentity`).
5. `bus.Publish(ev)` y, salvo `history.batch` y `typing`, `outbox.Enqueue(ctx, ev)`.

Un mensaje entrante (`!FromMe`) con texto que cumple `campaign.IsOptOut(text, keywords)` marca el
contacto `OptedOut` (si tiene contacto) y llama a `OnOptOut` de cada observador. Toda respuesta
entrante (`!FromMe`, chat directo, no histórica) llama a `OnIncoming(accountID, chatNativeID, ts)`
de cada observador. Las palabras de baja se leen del ajuste `optout_keywords` (caché de 1 min).

## 3. Accounts (`accounts.go`)

```go
type Accounts struct{ /* app, router, bus, registry, open, instances map[AccountID]*instance */ }
type OpenAccountStore func(id model.AccountID) (port.AccountStore, *sql.DB, error)

func NewAccounts(app port.AppStore, r *Router, b *bus.Bus, reg map[model.Channel]port.AdapterFactory, open OpenAccountStore) *Accounts
func (a *Accounts) StartAll(ctx context.Context) error
func (a *Accounts) StopAll(ctx context.Context) error
func (a *Accounts) List(ctx context.Context) ([]AccountView, error)
func (a *Accounts) Create(ctx context.Context, ch model.Channel, label, color string, config map[string]string) (model.Account, error)
func (a *Accounts) Update(ctx context.Context, id model.AccountID, label, color string, enabled bool) error
func (a *Accounts) SetConfig(ctx context.Context, id model.AccountID, config map[string]string) error
func (a *Accounts) Connect(ctx context.Context, id model.AccountID) error
func (a *Accounts) Disconnect(ctx context.Context, id model.AccountID) error
func (a *Accounts) BeginPairing(ctx context.Context, id model.AccountID, req model.PairRequest) error
func (a *Accounts) SubmitPairing(ctx context.Context, id model.AccountID, in model.PairInput) error
func (a *Accounts) Logout(ctx context.Context, id model.AccountID) error
func (a *Accounts) Delete(ctx context.Context, id model.AccountID) error
func (a *Accounts) Reorder(ctx context.Context, ids []model.AccountID) error
func (a *Accounts) Adapter(id model.AccountID) (port.ChannelAdapter, error) // ErrNotFound
func (a *Accounts) Store(id model.AccountID) (port.AccountStore, error)
func (a *Accounts) Status(id model.AccountID) model.AccountStatus
func (a *Accounts) Capabilities(ctx context.Context, id model.AccountID, chatNativeID string) (capability.Set, error)

type AccountView struct {
	Account      model.Account       `json:"account"`
	Status       model.AccountStatus `json:"status"`
	Capabilities capability.Set      `json:"capabilities"`
	Unread       int                 `json:"unread"`
}
```
- `instance` = `{acc, adapter, store, db, cancel, status}`. `Create` crea carpeta y `account.db`,
  guarda la cuenta, guarda `pacing:<id>` = `DefaultPacing`, construye el adaptador y llama a
  `Start` (que emitirá `pairing`).
- `Start` de cada cuenta en su goroutine con `recover`; si `Start` devuelve error o el adaptador
  emite `error`, reintenta con espera 2 s → 4 → 8 … máx. 5 min; si emite `logged_out`, **no**
  reintenta y espera a `BeginPairing`.
- El estado de cada instancia se mantiene escuchando su propio `account.state` (el Router se lo
  pasa: `Accounts` se suscribe al bus con nombre `accounts`).
- `Delete`: `Logout` (ignora error), `Stop`, `router.Detach`, cerrar base, `os.RemoveAll(carpeta)`,
  `app.DeleteAccount`, borrar ajustes `pacing:<id>` y `warmup_started_at:<id>`.
- Colores por defecto al crear: el siguiente de la paleta (11-INTERFAZ §5) que no use otra cuenta.

## 4. Inbox (`inbox.go`)

```go
func NewInbox(acc *Accounts) *Inbox
func (i *Inbox) ListChats(ctx context.Context, f model.ChatFilter) ([]model.Chat, error) // solo chats con actividad salvo f.IncludeEmpty
func (i *Inbox) OpenChat(ctx context.Context, accountID model.AccountID, peerNativeID string) (model.Chat, error) // «Nuevo chat»: devuelve o crea el chat vacío, sin enviar nada
func (i *Inbox) GetChat(ctx context.Context, key model.ChatKey) (model.Chat, error)
func (i *Inbox) Messages(ctx context.Context, key model.ChatKey, beforeTs int64, limit int) (model.MessagePage, error)
func (i *Inbox) Search(ctx context.Context, query string, accountIDs []model.AccountID, limit int) ([]model.Message, error)
func (i *Inbox) UnreadTotals(ctx context.Context) (map[model.AccountID]int, error)
func (i *Inbox) LoadOlder(ctx context.Context, key model.ChatKey) error // RequestHistory al adaptador
func (i *Inbox) Archive(ctx context.Context, key model.ChatKey, v bool) error
func (i *Inbox) Pin(ctx context.Context, key model.ChatKey, v bool) error
func (i *Inbox) Mute(ctx context.Context, key model.ChatKey, until int64) error
```
- `ListChats` multi-cuenta: pide `f.Limit` a cada cuenta filtrada y mezcla por
  `pinned DESC, lastMessageAt DESC`, cortando a `f.Limit`. La paginación usa `f.Before`.
  Solo lista **conversaciones con actividad**: salvo `f.IncludeEmpty`, el almacén excluye en todas las
  pestañas los chats `direct` y `self` con `last_message_at = 0` y sin fijar (contactos de la libreta que
  el canal lista como chats vacíos); grupos y canales salen siempre. Los totales de no leídos no cambian.
- `OpenChat` («Nuevo chat»): devuelve el chat existente (aunque esté vacío) o crea uno vacío (directo,
  `LastMessageAt` 0, título del contacto → persona → nombre del canal → id) sin enviar nada. Un chat
  nuevo exige cuenta habilitada, conectada y `Capabilities.StartChat` (WhatsApp y Telegram sí; Messenger
  e Instagram solo continúan conversaciones). Si el adaptador implementa la interfaz opcional
  `ResolvePeer(ctx, input) (nativeID, name, err)` (WhatsApp: comprueba «¿está en WhatsApp?» con
  `IsOnWhatsApp`) el identificador puede ser un teléfono que no está en la libreta. Con el primer mensaje
  el chat sube a la lista por el `chat.updated` normal.
- `Archive/Pin/Mute`: llama al adaptador; si devuelve `ErrUnsupported`, aplica **solo en local**
  (`PatchChat`) y no da error. Siempre publica `chat.updated`.
- `Messages`: si la página local viene con menos de `limit` y el canal tiene `HistoryOnDemand`,
  devuelve lo que hay y además llama en segundo plano a `LoadOlder`.

## 5. Messaging (`messaging.go`)

```go
func NewMessaging(acc *Accounts, r *Router, b *bus.Bus, app port.AppStore) *Messaging // rutas con el paquete paths
func (m *Messaging) Send(ctx context.Context, key model.ChatKey, out model.Outgoing) (model.Message, error)
func (m *Messaging) React(ctx context.Context, key model.MessageKey, emoji string) error
func (m *Messaging) Edit(ctx context.Context, key model.MessageKey, text string) error
func (m *Messaging) Delete(ctx context.Context, key model.MessageKey, forEveryone bool) error
func (m *Messaging) Forward(ctx context.Context, key model.MessageKey, to model.ChatKey) (model.Message, error)
func (m *Messaging) VotePoll(ctx context.Context, key model.MessageKey, options []string) error
func (m *Messaging) MarkRead(ctx context.Context, key model.ChatKey) error
func (m *Messaging) SetTyping(ctx context.Context, key model.ChatKey, typing bool) error
func (m *Messaging) MediaPath(ctx context.Context, acc model.AccountID, mediaID string) (string, error) // descarga si falta
```
- **Validación con capacidades antes de llamar al adaptador** (`ErrUnsupported` si la capacidad del
  chat está en false; `ErrInvalid` si el archivo supera el máximo, el texto supera `MaxTextLen`, o
  editar/borrar está fuera de `EditWindow`/`DeleteWindow` contando desde `Timestamp`).
- `Send`: el adaptador devuelve el mensaje; Messaging lo emite **por el sink de la cuenta** como
  `message.sent` (así se persiste y se publica igual que todo lo demás). Expone
  `Router.SinkFor(id)` para eso.
- `MarkRead`: toma los mensajes no leídos entrantes (máx. 50 más recientes) y los pasa al
  adaptador; pone `unreadCount = 0` con `PatchChat` y publica `chat.updated`.
- `MediaPath`: si `LocalPath` existe y el archivo está, lo devuelve. Si no, `DownloadMedia` →
  escribe en `MediaDir/<mediaId><ext por mime>` → `SetMediaPath` → emite `message.updated` con
  `Media`. Una sola descarga simultánea por `mediaId` (`singleflight` hecho a mano con mapa + mutex).
- Descarga automática al recibir: Messaging se suscribe al bus (`automedia`) y, según el ajuste
  `auto_download_media`, descarga en segundo plano (máx. 3 a la vez).

## 6. Statuses (`statuses.go`)

```go
func NewStatuses(acc *Accounts, r *Router, app port.AppStore) *Statuses // rutas con el paquete paths
func (s *Statuses) Authors(ctx context.Context, accountIDs []model.AccountID) ([]model.StatusAuthor, error)
func (s *Statuses) ByAuthor(ctx context.Context, acc model.AccountID, authorID string) ([]model.Status, error)
func (s *Statuses) Mine(ctx context.Context, acc model.AccountID, from, to int64) ([]model.Status, error)
func (s *Statuses) Post(ctx context.Context, acc model.AccountID, st model.OutgoingStatus) (model.Status, error)
func (s *Statuses) Delete(ctx context.Context, acc model.AccountID, statusNativeID string) error
func (s *Statuses) MarkViewed(ctx context.Context, acc model.AccountID, statusKey string) error
func (s *Statuses) React(ctx context.Context, acc model.AccountID, statusKey, emoji string) error
func (s *Statuses) Viewers(ctx context.Context, acc model.AccountID, statusNativeID string) ([]model.StatusView, error)
func (s *Statuses) Stats(ctx context.Context, acc model.AccountID, rng stats.Range) (stats.StatusStats, error)
```
- `Post`: si `Audience.ListIDs` no está vacío, se convierte en `PeerIDs` con las identidades de esa
  cuenta de los miembros de las listas y `Mode = "allow"`. Copia el archivo a `status-media/`.
- Las vistas se refrescan: cada 10 min, para mis estados de las últimas 24 h, `StatusViewers` del
  adaptador → `UpsertStatusView` (si el adaptador las empuja como eventos, también sirve).

## 7. Contacts (`contacts.go`)

```go
func NewContacts(app port.AppStore, acc *Accounts) *Contacts
func (c *Contacts) List(ctx context.Context, f model.ContactFilter) ([]model.Contact, int, error)
func (c *Contacts) Get(ctx context.Context, id string) (model.Contact, error)
func (c *Contacts) Save(ctx context.Context, ct model.Contact) (model.Contact, error)
func (c *Contacts) Delete(ctx context.Context, id string) error
func (c *Contacts) FromChat(ctx context.Context, key model.ChatKey) (model.Contact, error) // crea o devuelve el del chat directo
func (c *Contacts) Merge(ctx context.Context, keepID, mergeID string) (model.Contact, error)
func (c *Contacts) SplitIdentity(ctx context.Context, contactID string, acc model.AccountID, peer string) (model.Contact, error)
func (c *Contacts) ImportFromAccount(ctx context.Context, acc model.AccountID) (ImportResult, error)
func (c *Contacts) SuggestMerges(ctx context.Context) ([][2]string, error) // mismos teléfonos E.164
func (c *Contacts) Lists(ctx context.Context) ([]model.ContactList, error)
func (c *Contacts) SaveList(ctx context.Context, l model.ContactList) (model.ContactList, error)
func (c *Contacts) DeleteList(ctx context.Context, id string) error
func (c *Contacts) SetListMembers(ctx context.Context, listID string, add, remove []string) error
func (c *Contacts) ImportCSV(ctx context.Context, path string, mapping CSVMapping) (ImportResult, error)

type ImportResult struct{ Created, Updated, Skipped int }
type CSVMapping struct{ Name, Phone, Tags, Notes int /* índice de columna, -1 = no */; ListID string; HasHeader bool }
```
- `ImportFromAccount`: `adapter.ListPersons` → por cada persona con `Phone`, busca contacto con ese
  teléfono (`FindContactsByPhone`); si hay uno, le añade la identidad; si no, crea uno.
  Sin teléfono (Telegram por usuario, Meta): crea contacto con la identidad sola.
- Teléfonos normalizados a E.164 con un normalizador simple (solo dígitos, `+` delante; si no
  empieza por `+` y tiene 10 dígitos que empiezan por `0`, se asume Ecuador: `+593` + sin el 0).
- `ImportCSV`: lee con `encoding/csv` (detecta `;` o `,` en la primera línea).

## 8. Campaigns (`campaigns.go` + paquete puro `core/campaign`)

### 8.1 Lógica pura a portar (con sus pruebas)

| Go (`core/campaign`) | Base (leer y portar 1:1) | Pruebas a portar |
| --- | --- | --- |
| `template.go`: `RenderTemplate`, `BuildVariables`, `ExtractTags`, `FirstName` | `../dconnector/src/shared/template.ts` | `tests/template.test.ts`, `tests/templateFields.test.ts` (solo lo que no sea de ventas) |
| `variants.go`: `Spin`, `SpinCount`, `SeededRandom`, `Similarity`, `MaxSimilarity`, `AssignVariants`, `ReviewMessage`, `WorstLevel`, `RequiredVariants` | `src/shared/variants.ts` | `tests/variants.test.ts` |
| `pacing.go`: `WarmupLimit`, `BatchRestMs`, `BrakeReason`, `CampaignHealth` | `src/shared/pacing.ts` | `tests/pacing.test.ts` |
| `outreach.go`: `Risk` (`warm`/`lukewarm`/`cold`), `RiskOrder`, `Warmest`, `CountRisks`, `EstimateOutreach`, `OutreachWarnings` | `src/shared/outreach.ts` (sin `waMeLink` ni sesión) | `tests/outreach.test.ts` |
| `abtest.go`: `ABConfig`, `ABDefaults`, `ABSample`, `VariantScore`, `PickWinners` | `src/shared/abtest.ts` | `tests/abtest.test.ts` |
| `optout.go`: `IsOptOut`, `SuggestOptOut` | `src/shared/optout.ts` | (escribir 6 casos con los ejemplos de los comentarios) |

`SeededRandom` debe dar **la misma secuencia** que el TS (mismo algoritmo); la prueba compara 5
valores calculados con Node a partir de la misma semilla y los fija en el test.

### 8.2 Servicio

```go
func NewCampaigns(app port.AppStore, acc *Accounts, msg *Messaging, b *bus.Bus) *Campaigns
func (c *Campaigns) Start(ctx context.Context) // bucle del motor
func (c *Campaigns) Stop()
func (c *Campaigns) List(ctx context.Context) ([]model.Campaign, error)
func (c *Campaigns) Get(ctx context.Context, id string) (model.Campaign, error)
func (c *Campaigns) Save(ctx context.Context, cp model.Campaign) (model.Campaign, error) // solo draft/ready/paused
func (c *Campaigns) Review(ctx context.Context, cp model.Campaign) (CampaignReview, error)
func (c *Campaigns) Launch(ctx context.Context, id string) error   // genera la cola y pasa a ready/running
func (c *Campaigns) Pause(ctx context.Context, id string) error
func (c *Campaigns) Resume(ctx context.Context, id string) error   // también quita el freno
func (c *Campaigns) Cancel(ctx context.Context, id string) error
func (c *Campaigns) Delete(ctx context.Context, id string) error
func (c *Campaigns) Queue(ctx context.Context, id string, states []string, limit int) ([]model.QueueItem, error)
func (c *Campaigns) GetPacing(ctx context.Context, acc model.AccountID) (model.PacingSettings, error)
func (c *Campaigns) SetPacing(ctx context.Context, acc model.AccountID, p model.PacingSettings) error
func (c *Campaigns) OnIncoming(acc model.AccountID, chatNativeID string, ts int64)
func (c *Campaigns) OnOptOut(acc model.AccountID, contactID string)

type CampaignReview struct {
	Recipients   int                    `json:"recipients"`
	Risks        map[string]int         `json:"risks"`
	Required     int                    `json:"requiredVariants"`
	Items        []campaign.ReviewItem  `json:"items"`
	Level        string                 `json:"level"` // green | yellow | red (red impide lanzar)
	Estimate     campaign.Estimate      `json:"estimate"`
	Warnings     []string               `json:"warnings"`
	Skipped      map[string]int         `json:"skipped"` // optedOut, noIdentity, frequency
}
```

**`Launch` (generar la cola):**
1. Miembros únicos de `ListIDs` + `ContactIDs`. Se saltan: `OptedOut`, sin identidad en la cuenta
   de la campaña, y quienes ya recibieron `FreqMax` campañas en `FreqDays`.
2. Riesgo por destinatario (en la `AccountStore` de la cuenta): `warm` si hay algún mensaje
   entrante suyo en el chat directo; `lukewarm` si solo hay mensajes míos; `cold` si no hay chat.
3. Variantes con `AssignVariants` (semilla = id de campaña); muestra A/B con `ABSample` si
   `ABEnabled`; texto final = `Spin(RenderTemplate(variante, vars), SeededRandom(campaña+contacto))`
   + pie de baja si `OptOutFooter`.
4. Orden: `warm` → `lukewarm` → `cold`, y dentro de cada uno, aleatorio sembrado.
5. `ReplaceQueue` con `state = pending` (o `held` para los no-muestra cuando hay A/B).
6. `State = ready` si `StartAt > ahora`, si no `running`.

**Motor (`Start`)**: una goroutine por cuenta con campañas `running`; tick cada 5 s:
1. Dentro de `Window` (hora local)? Si no, esperar.
2. Límites: enviados en la última hora (`CountSentSince`) < `HourlyLimit`; del día <
   `WarmupLimit(DailyLimit, …, warmup_started_at:<acc>)`; fríos del día < `ColdDailyLimit`;
   tandas: tras `BatchSize` envíos seguidos, descanso `BatchRestMs`.
3. Cuenta `connected`? Si no, la campaña sigue `running` pero no envía (y la interfaz lo muestra).
4. Siguiente `pending` con `SendAfter <= ahora` → `SetTyping(true)` durante
   `min(len(text)*TypingMsPerChar, 8 s)` → `Messaging.Send` → `sent` con `MessageKey` o `failed`
   con `Error` → siguiente espera aleatoria entre `MinDelaySec..MaxDelaySec` (o las `Cold*` si el
   siguiente es frío).
5. Tras cada envío: `CampaignHealth` (enviados, fallidos, entregados/leídos por la cola y los
   recibos, respondidos por `OnIncoming`, bajas por `OnOptOut`) → `BrakeReason`; si hay motivo:
   `State = braked`, `BrakeReason`, aviso.
6. A/B: cuando pasen `WaitHours` desde el último envío de la muestra, `PickWinners` → reparte los
   `held` entre las ganadoras (vuelve a personalizar) y los pasa a `pending`.
7. Sin `pending` ni `held` → `done`.
8. Cada cambio de una campaña (estado, totales, siguiente envío) se avisa por un canal propio,
   **no por el bus**: `func (c *Campaigns) Progress() <-chan CampaignProgress` (búfer 256, descarta
   si está lleno). Lo escucha `ui/wails` y lo pasa a la interfaz como `campaign:progress`.

```go
type CampaignProgress struct {
	CampaignID string               `json:"campaignId"`
	State      model.CampaignState  `json:"state"`
	Totals     model.CampaignTotals `json:"totals"`
	NextAt     int64                `json:"nextAt"` // próximo envío previsto, 0 si no hay
	Waiting    string               `json:"waiting"` // "" | "window" | "hourly" | "daily" | "batch_rest" | "disconnected"
	BrakeReason string              `json:"brakeReason"`
}
```

## 9. Scheduler (`scheduler.go`)

```go
func NewScheduler(app port.AppStore, msg *Messaging, st *Statuses, acc *Accounts) *Scheduler
func (s *Scheduler) Start(ctx context.Context) // tick cada 15 s
func (s *Scheduler) Stop()
func (s *Scheduler) List(ctx context.Context, from, to int64) ([]model.Scheduled, error)
func (s *Scheduler) Save(ctx context.Context, it model.Scheduled) (model.Scheduled, error) // pending; corrige uno failed → pending
func (s *Scheduler) Cancel(ctx context.Context, id string) error
func (s *Scheduler) Delete(ctx context.Context, id string) error
func (s *Scheduler) SendNow(ctx context.Context, id string) error
// Recurrentes y varios por chat (oleada 5-D; detalle en docs/bitacora/5-D.md):
func (s *Scheduler) ListForChat(ctx context.Context, accountID model.AccountID, chatNativeID string, includeDone bool) ([]model.Scheduled, error)
func (s *Scheduler) Update(ctx context.Context, it model.Scheduled) (model.Scheduled, error)
func (s *Scheduler) UpdateSeries(ctx context.Context, seriesID string, it model.Scheduled) (model.Scheduled, error)
func (s *Scheduler) CancelSeries(ctx context.Context, seriesID string) error
func (s *Scheduler) DeleteSeries(ctx context.Context, seriesID string) error
func (s *Scheduler) Preview(r model.Recurrence, first int64, n int) []int64
func ValidateRecurrence(r model.Recurrence, first int64) error
func NextOccurrence(r model.Recurrence, first, after int64) (next int64, ok bool) // recurrence.go, hora local
```
`Scheduled.Recurrence` (nil = una sola vez) y `Scheduled.SeriesID`: cada ocurrencia es su propia fila; al
cerrarse una (enviada, fallida u omitida) se crea la siguiente en la misma transacción. Una ocurrencia con
más de 1 h de atraso se omite (`canceled`, «Omitido: …»). Tope de 20 envíos por minuto y cuenta; pausa de
1,5 s entre envíos al mismo chat. El almacén ofrece (interfaz opcional `scheduledSeriesStore`) `GetScheduled`,
`ListScheduledByChat`, `ListScheduledBySeries` y `SaveScheduledWithNext`.

Cada tick: `DueScheduled(ahora)` → por cada uno, si la cuenta está conectada, enviar (mensaje con
`Messaging.Send`, estado con `Statuses.Post`) → `sent`/`failed`. Si la cuenta no está conectada se
reintenta en el siguiente tick; si pasan 6 h del `SendAt` sin poder enviar → `failed` con
«La cuenta no estuvo conectada».

## 10. Stats (`stats.go` + paquete puro `core/stats`)

| Go (`core/stats`) | Base | Pruebas |
| --- | --- | --- |
| `conversation.go`: `Summarize`, `PerDay`, `Heatmap`, `PerChat`, `Median`, `ConversationGap` | `src/shared/conversationStats.ts` (sin listas de clientes: `bestCustomers`, `latePayers`, `unhappy`) | `tests/conversationStats.test.ts` |
| `status.go`: `ReachRatios`, `LoyaltyBuckets`, `ViewsCurve`, `PctChange` | `src/shared/statusStats.ts` (sin categorías de producto) | `tests/statusInsights.test.ts` (lo aplicable) |
| `periods.go`: `Range`, `Bounds`, `Granularity`, `BucketKey`, `Bucketize` | `src/shared/periods.ts` | `tests/periods.test.ts` |

```go
func NewStats(acc *Accounts) *Stats
func (s *Stats) Conversations(ctx context.Context, accountIDs []model.AccountID, rng stats.Range) (stats.ConversationOverview, error)
func (s *Stats) Chat(ctx context.Context, key model.ChatKey, rng stats.Range) (stats.ContactConversationStats, error)
func (s *Stats) Groups(ctx context.Context, accountIDs []model.AccountID, rng stats.Range) (stats.GroupsOverview, error)
```
Las filas salen de `AccountStore.MessageStats` de cada cuenta y se juntan antes de calcular.

## 11. Destinations (`destinations.go`)

```go
func NewDestinations(app port.AppStore) *Destinations
func (d *Destinations) List(ctx context.Context) ([]model.Destination, error)
func (d *Destinations) Save(ctx context.Context, dst model.Destination) (model.Destination, error) // valida URL (P1)
func (d *Destinations) Delete(ctx context.Context, id string) error
func (d *Destinations) OnChange(fn func()) // el manager del protocolo se re-sincroniza
```
El estado en vivo de cada destino lo da `ws.Manager.States()`.

`Save` también valida y normaliza `Filter.ContactScope` (alcance por listas de contactos): modo `all`
o `lists` (vacío = `all`); en `all` las listas incluidas se vacían; se quitan ids repetidos y los de
listas que ya no existen; `lists` sin ninguna lista incluida, una lista a la vez incluida y excluida o
más de 100 listas por lado son `ErrInvalid`. El filtro se guarda en `destinations.filter_json` (sin
migración). Lo aplica `ws.Manager` (eventos y comandos); ver `docs/PROTOCOLO-v1.md`.

## 12. Settings (`settings.go`)

```go
func NewSettings(app port.AppStore) *Settings
func (s *Settings) EnsureDefaults(ctx context.Context) error
func (s *Settings) All(ctx context.Context) (AppSettings, error)
func (s *Settings) Save(ctx context.Context, v AppSettings) error
func (s *Settings) InstanceID(ctx context.Context) string

type AppSettings struct {
	CloseToTray       bool              `json:"closeToTray"`
	StartWithWindows  bool              `json:"startWithWindows"`
	StartMinimized    bool              `json:"startMinimized"`
	Notifications     bool              `json:"notifications"`
	Theme             string            `json:"theme"`
	VerboseLog        bool              `json:"verboseLog"`
	AutoDownload      map[string]any    `json:"autoDownload"`
	OptOutKeywords    string            `json:"optOutKeywords"`
	OptOutFooterText  string            `json:"optOutFooterText"`
}
```

## 13. Adaptador falso (`core/fake`)

`fake.Adapter` implementa `port.ChannelAdapter` en memoria: capacidades configurables, `Start`
emite `connected`, `Send` devuelve el mensaje con id incremental, y tiene métodos de prueba
`Inject(ev model.Event)` y `Sent() []model.Outgoing`. Lo usan las pruebas de todos los servicios.
