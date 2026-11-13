# 11 · Interfaz

Ventana Wails v3 con React 19 + TypeScript + Vite + Tailwind v4. Aspecto de **WhatsApp Web**
(la base, `../dconnector/src/renderer/src/pages/ChatsView.tsx` y `components/chat/*`, sirve de
referencia visual y de comportamiento), con la cuenta y el canal visibles en cada chat y mensaje.

## 1. Servicios Wails (`internal/adapter/ui/wails`)

Cada servicio es un struct con métodos exportados; Wails v3 genera los enlaces TypeScript en
`frontend/bindings/` con `wails3 generate bindings`. **Son fachadas finas**: convierten tipos y
llaman al servicio del núcleo, sin lógica. Todos reciben `ctx context.Context` primero (Wails lo
admite y no lo expone a TS).

```go
type AccountsService struct{ a *service.Accounts }
func (s *AccountsService) List(ctx) ([]service.AccountView, error)
func (s *AccountsService) Create(ctx, channel string, label, color string, config map[string]string) (model.Account, error)
func (s *AccountsService) Update(ctx, id string, label, color string, enabled bool) error
func (s *AccountsService) SetConfig(ctx, id string, config map[string]string) error
func (s *AccountsService) Connect(ctx, id string) error
func (s *AccountsService) Disconnect(ctx, id string) error
func (s *AccountsService) BeginPairing(ctx, id string, req model.PairRequest) error
func (s *AccountsService) SubmitPairing(ctx, id string, in model.PairInput) error
func (s *AccountsService) Logout(ctx, id string) error
func (s *AccountsService) Delete(ctx, id string) error
func (s *AccountsService) Reorder(ctx, ids []string) error
func (s *AccountsService) Capabilities(ctx, chatKey string) (capability.Set, error)

type InboxService struct{ i *service.Inbox }
func (s *InboxService) ListChats(ctx, f model.ChatFilter) ([]model.Chat, error)
func (s *InboxService) GetChat(ctx, chatKey string) (model.Chat, error)
func (s *InboxService) OpenChat(ctx, accountID, peerNativeID string) (model.Chat, error)
func (s *InboxService) Messages(ctx, chatKey string, beforeTs int64, limit int) (model.MessagePage, error)
func (s *InboxService) Search(ctx, query string, accountIDs []string, limit int) ([]model.Message, error)
func (s *InboxService) UnreadTotals(ctx) (map[string]int, error)
func (s *InboxService) LoadOlder(ctx, chatKey string) error
func (s *InboxService) Archive(ctx, chatKey string, v bool) error
func (s *InboxService) Pin(ctx, chatKey string, v bool) error
func (s *InboxService) Mute(ctx, chatKey string, until int64) error

type MessagingService struct{ m *service.Messaging }
func (s *MessagingService) Send(ctx, chatKey string, out model.Outgoing) (model.Message, error)
func (s *MessagingService) React(ctx, msgKey, emoji string) error
func (s *MessagingService) Edit(ctx, msgKey, text string) error
func (s *MessagingService) Delete(ctx, msgKey string, forEveryone bool) error
func (s *MessagingService) Forward(ctx, msgKey, toChatKey string) (model.Message, error)
func (s *MessagingService) VotePoll(ctx, msgKey string, options []string) error
func (s *MessagingService) MarkRead(ctx, chatKey string) error
func (s *MessagingService) SetTyping(ctx, chatKey string, typing bool) error
func (s *MessagingService) SaveMediaAs(ctx, accountID, mediaID string) (string, error) // diálogo «Guardar como»

type StatusesService      // Authors, ByAuthor, Mine, Post, Delete, MarkViewed, React, Viewers, Stats (mismas firmas que 06 §6, ids como string)
type ContactsService      // List, Get, Save, Delete, FromChat, Merge, SplitIdentity, ImportFromAccount, SuggestMerges, Lists, SaveList, DeleteList, SetListMembers, ImportCSV
type CampaignsService     // List, Get, Save, Review, Launch, Pause, Resume, Cancel, Delete, Queue, GetPacing, SetPacing
type ScheduledService     // List, Save, Cancel, Delete, SendNow
type StatsService         // Conversations, Chat, Groups
type DestinationsService  // List, Save, Delete, States() []model.DestinationState, Test(ctx, id) error (conecta, espera hello enviado, cierra)
type SettingsService      // Get() AppSettings, Save(AppSettings), DataDir() string, OpenDataDir(), OpenLogsDir(), Version() string

type FilesService struct{}
func (s *FilesService) PickFiles(ctx, kind string) ([]PickedFile, error)  // diálogo de Wails; kind: "media"|"document"|"sticker"|"csv"|"any"
func (s *FilesService) SaveTemp(ctx, name string, b64 string) (PickedFile, error) // notas de voz y pegar imágenes; escribe en <data>/tmp
func (s *FilesService) Info(ctx, path string) (PickedFile, error)
type PickedFile struct{ Path, Name, Mime string; Size int64 }
```

`services.go`: `func Register(app *application.App, deps Deps)` añade todos con
`application.NewService(...)`.

## 2. Eventos en vivo (`events.go`)

Una goroutine suscrita al bus (`ui`, búfer 4.096) reemite a la ventana con
`app.Event.Emit(nombre, datos)` (comprobar el nombre exacto del método en la versión de Wails v3):

| Evento Wails | Datos | Origen |
| --- | --- | --- |
| `account:status` | `AccountStatus` | `account.state` |
| `account:caps` | `{accountId, capabilities}` | `account.capabilities` |
| `chat:upsert` | `Chat` | `chat.updated` |
| `chat:removed` | `{chatKey}` | `chat.removed` |
| `message:upsert` | `Message` | `message.received`, `message.sent` |
| `message:update` | `Message` (el completo) | `message.updated`, `reaction` |
| `receipt` | `ReceiptEvent` | `receipt` |
| `typing` | `TypingEvent` | `typing` |
| `person:upsert` | `Person` | `person.updated` |
| `status:upsert` | `Status` | `status.posted` |
| `status:view` | `StatusView` | `status.viewed` |
| `history:progress` | `{accountId, progress}` | `history.batch` (sin los datos: la interfaz recarga la lista) |
| `campaign:progress` | `CampaignProgress` | `Campaigns.Progress()` |
| `destination:state` | `DestinationState` | `ws.Manager` |
| `unread:total` | `{total, byAccount}` | recalculado como mucho 1 vez/s tras `chat:upsert` |

Para no saturar: `message:upsert` y `chat:upsert` se agrupan cada 100 ms en arrays
(`message:upsert` lleva `Message[]`, `chat:upsert` lleva `Chat[]`).

## 3. Archivos para la vista web (`media_handler.go`)

En las opciones de assets de Wails v3, un middleware que atiende:
- `GET /media/<accountId>/<mediaId>` → `Messaging.MediaPath` (descarga si falta) → `http.ServeFile`
  con `Content-Type` del mime. Soporta `Range` (vídeo y audio).
- `GET /avatar/<accountId>/<archivo>` → `AvatarDir/<archivo>`; si no existe, 404 (la interfaz pinta
  iniciales).
- `GET /local?path=<ruta>` → **solo** rutas dentro de `<data>/tmp` (vista previa de lo que se va a
  enviar). Cualquier otra ruta: 403.

## 4. Bandeja, ventana y arranque (`tray.go`, `autostart.go`)

- Bandeja: icono de la app; tooltip `dConnector · N sin leer`; menú: «Abrir dConnector», separador,
  una línea deshabilitada por cuenta `● Ventas (WhatsApp) — Conectada`, separador, «Salir».
  Clic izquierdo = abrir.
- Cerrar la ventana: si `closeToTray`, se oculta (la primera vez, notificación «dConnector sigue
  abierto en la bandeja»); si no, sale.
- `startWithWindows`: escribe/borra `HKCU\Software\Microsoft\Windows\CurrentVersion\Run\dConnector`
  = `"<ruta exe>" --minimized` (`golang.org/x/sys/windows/registry`). `--minimized` arranca oculto.
- Notificaciones (servicio de notificaciones de Wails v3): mensaje nuevo entrante de chat no
  silenciado, con la ventana oculta o sin foco: título `<chat> · <etiqueta de cuenta>`, cuerpo
  la vista previa. Clic → abrir ese chat (evento `ui:open-chat` con la clave).

## 5. Diseño

- Fuente del sistema (`Segoe UI`), 14 px base.
- Tokens en `styles.css` con `@theme` de Tailwind v4 y modo oscuro por clase `dark` en `<html>`
  (ajuste `theme`):

| Token | Claro | Oscuro |
| --- | --- | --- |
| `--bg` | `#f0f2f5` | `#0b141a` |
| `--panel` | `#ffffff` | `#111b21` |
| `--panel-2` | `#f7f8fa` | `#202c33` |
| `--chat-bg` | `#efeae2` | `#0b141a` |
| `--bubble-in` | `#ffffff` | `#202c33` |
| `--bubble-out` | `#d9fdd3` | `#005c4b` |
| `--text` | `#111b21` | `#e9edef` |
| `--muted` | `#667781` | `#8696a0` |
| `--border` | `#e9edef` | `#222d34` |
| `--accent` | `#00a884` | `#00a884` |
| `--danger` | `#ea0038` | `#f15c6d` |

- Colores de canal (insignias): WhatsApp `#25D366`, Telegram `#229ED9`, Messenger `#0084FF`,
  Instagram `#E1306C`. Iconos de canal: SVG propios simples en `components/common/ChannelIcon.tsx`
  (círculo de color + letra W, T, M, I en blanco; sin logotipos de marca).
- Paleta de cuentas (color que elige el usuario, en orden por defecto): `#25D366 #229ED9 #8B5CF6
  #F59E0B #EF4444 #0EA5E9 #10B981 #EC4899 #6366F1 #84CC16`.
- Sin textos que explican la pantalla: títulos, etiquetas y vacíos cortos. Textos largos con `…` y
  `title` con el texto completo.
- Cada formulario largo guarda borrador en `localStorage` (`draft:<pantalla>:<id>`) y lo restaura.

## 6. Pantallas

Navegación por estado (`store/ui.ts`: `page`), sin router. Atajos: `Ctrl+1…9` páginas,
`Ctrl+K` búsqueda global, `Esc` cierra modales.

### 6.1 Marco (`components/layout/`)
- `Rail` (64 px, izquierda): Chats (insignia no leídos), Estados, Campañas, Programados, Contactos,
  Estadísticas, Destinos; abajo Cuentas (punto verde/ámbar/rojo según el peor estado) y Ajustes.
- `Toasts` (errores y avisos), `ConfirmDialog`, `Modal` (tamaños `sm|md|lg|full`).

### 6.2 Chats (`pages/ChatsPage.tsx`, `components/chat/`)
Tres columnas: lista (360 px), conversación (resto), panel de información (360 px, plegable).
- **Filtro de cuentas** (`AccountFilter`): fila de chips: «Todas», uno por canal presente, y uno por
  cuenta (color + etiqueta). Selección múltiple; se guarda en `localStorage`. Cambia `ChatFilter`.
- **Nuevo chat**: botón (`MessageSquarePlus`) junto al buscador → `NewChatDialog` (contactos por nombre o
  teléfono, con la cuenta por identidad; deshabilitadas con motivo las desconectadas y los canales sin
  `startChat`; un número suelto se ofrece por las cuentas de WhatsApp) → `InboxService.OpenChat` → se abre
  el chat. La lista de chats solo muestra conversaciones con actividad; si no hay ninguna, el estado vacío
  invita a pulsar «Nuevo chat». Contactos tiene el botón «Escribir» en la ficha.
- Pestañas: Todos · No leídos · Grupos · Archivados. Buscador (título del chat; `Ctrl+K` busca en
  mensajes de todas las cuentas: `QuickSearch`).
- `ChatList` virtualizada (`@tanstack/react-virtual`), paginación al bajar (`before`).
  `ChatRow`: avatar 48 px con `ChannelIcon` de 16 px en la esquina inferior derecha; título; a la
  derecha la hora (hoy `HH:mm`, ayer «Ayer», semana: día, si no `dd/MM/yy`); debajo la vista previa
  (con «Tú: » si es mío y ✓/✓✓ azul) y la **píldora de cuenta** (color de la cuenta, etiqueta,
  máx. 12 caracteres); a la derecha el contador de no leídos, 📌, 🔇.
- `Conversation`: cabecera (avatar, título, `ChannelIcon` + píldora de cuenta, «escribiendo…»,
  botones según capacidades: buscar en el chat, archivar, silenciar, info). Fondo `--chat-bg`.
  Mensajes virtualizados de abajo arriba, separadores de día, carga de antiguos al subir
  (`Messages(before)` y `LoadOlder`).
- `Bubble` por tipo: texto (formato WhatsApp `*negrita*`, `_cursiva_`, `~tachado~`, `` `mono` ``,
  enlaces clicables con `target=_blank` → navegador del sistema); foto (miniatura → completa al
  cargar, clic = visor); vídeo (`<video controls>` desde `/media/…`); voz (reproductor con
  velocidades 1× / 1,5× / 2×); audio; documento (icono, nombre, tamaño, «Guardar como»); sticker
  (128 px, sin burbuja); ubicación (tarjeta con coordenadas y enlace a OpenStreetMap, **sin mapa
  embebido**); contacto (tarjeta con nombre y teléfono, botón «Abrir chat» si existe); encuesta
  (opciones con barras y votantes; votar si `VotePoll`); sistema (centrado, gris); no compatible.
  En grupos, nombre del emisor arriba con color estable por id. Cita arriba (clic = ir al mensaje).
  Pie: «editado», hora, ✓ enviado, ✓✓ entregado, ✓✓ azul leído, 🕓 pendiente, ⚠ fallido.
  Reacciones debajo en píldora.
- `MessageMenu` (clic derecho o flecha al pasar): Responder, Reaccionar (barra de 6 emojis +
  «más» con `emoji-picker-react`), Reenviar (selector de chat **de cualquier cuenta del mismo
  canal**), Copiar, Editar, Eliminar para mí / para todos, Info. Solo lo que permiten las
  capacidades del chat y las ventanas de tiempo (`lib/caps.ts`).
- `Composer`: caja de texto multilínea (Enter envía, Shift+Enter salto), emoji, adjuntar (menú
  según capacidades: Fotos y vídeos, Documento, Sticker, Ubicación, Contacto, Encuesta), arrastrar
  y soltar archivos (vista previa con pie antes de enviar), pegar imagen (`SaveTemp`), micrófono
  (grabación con `MediaRecorder` en `audio/webm;codecs=opus` → **`lib/webmToOgg.ts` copiado tal
  cual de `../dconnector/src/shared/webmToOgg.ts`** → `SaveTemp` → `Send{kind: voice}`), barra de
  respuesta o edición encima. `SetTyping(true)` al teclear (como mucho cada 4 s) y `false` al enviar.
  Si el chat es `ReadOnly`, en lugar del compositor: «Solo los administradores pueden enviar mensajes».
- `ContactInfoPanel`: foto, nombre, teléfono/usuario, cuenta y canal, **contacto unificado**
  (identidades en otras cuentas con botón «Abrir chat», o «Crear contacto»), listas (añadir/quitar),
  etiquetas, notas, archivos del chat (cuadrícula de medios), estadísticas del chat (`StatsService.Chat`).
- Al abrir un chat: `MarkRead` (si la ventana tiene foco).

### 6.3 Estados (`pages/StatusesPage.tsx`, `components/status/`)
- Izquierda: selector de cuenta (solo cuentas con `Statuses`); «Mis estados» (anillo con los míos
  vigentes, botón «+»); recientes (no vistos) y vistos, con anillos segmentados.
- `StoryViewer` a pantalla completa: barras de progreso, 5 s por foto/texto, duración del vídeo;
  ←/→, pausa al mantener; responder (abre el chat con la cita) y reaccionar si `StatusReact`;
  `MarkViewed` al mostrar.
- `PostStatusModal`: Texto (color de fondo de 12 colores, 4 letras, vista previa) o Foto/Vídeo con
  pie. Audiencia: si `StatusAudience`: Mis contactos · Solo compartir con… (contactos o listas) ·
  Mis contactos excepto…; si no: texto fijo «Se usa la privacidad de estados del teléfono».
  Botones Publicar y Programar (abre fecha/hora → `ScheduledService.Save{kind:status}`).
- `MyStatusDetail`: vistas (lista con hora y reacción), contador, borrar.
- `StatusStatsPanel`: periodo + KPIs (publicados, vistas medias, alcance) y curva de vistas.

### 6.4 Campañas (`pages/CampaignsPage.tsx`, `components/campaigns/`)
- Lista: nombre, cuenta, estado (píldora), progreso `enviados/total`, próximo envío o espera
  («Fuera de horario», «Límite por hora», «Descanso», «Cuenta desconectada», «Frenada: <motivo>»).
- `CampaignWizard` (pasos): 1) Nombre y cuenta (solo cuentas conectables de WhatsApp o Telegram;
  Messenger/Instagram no hacen campañas en v1). 2) Audiencia: listas y contactos sueltos, con
  recuento por riesgo (🟢🟡🔴) y omitidos (baja, sin identidad, frecuencia). 3) Mensaje: texto con
  etiquetas `{nombre}`, `{a|b}`, variantes B…E (`VariantsEditor`), adjunto, pie de baja; panel de
  revisión (`Review`) en vivo con niveles verde/amarillo/rojo. 4) Envío: inicio (ahora / fecha),
  ventana horaria y días, A/B, y el ritmo de la cuenta (enlace a «Ritmo de la cuenta»).
  5) Resumen con estimación y botón «Lanzar» (deshabilitado en rojo).
- Detalle: totales, cola (pestañas por estado), pausar/reanudar/cancelar, respuestas y bajas.
- «Ritmo de la cuenta»: formulario con todos los campos de `PacingSettings` agrupados (Esperas,
  Límites, Tandas, Calentamiento, Freno, Frecuencia), con «Restablecer valores seguros».

### 6.5 Programados
Tabla (fecha, cuenta, destino, tipo, contenido, estado) con filtros por estado; «Nuevo»: cuenta →
chat (buscador) → mensaje (compositor reducido: texto + un archivo) → fecha y hora. Acciones:
editar (si pending/failed), enviar ahora, cancelar, borrar.

### 6.6 Contactos
Barra lateral de listas (crear, renombrar, color, borrar). Tabla virtualizada (nombre, teléfono,
canales como iconos, listas, etiquetas). Ficha: identidades (por cuenta, con abrir chat y separar),
unir con otro contacto, listas, etiquetas, notas, «No recibe campañas». Menú ⋮: Importar de una
cuenta, Importar CSV (asistente de columnas con vista previa de 5 filas), Sugerencias de unión.

### 6.7 Estadísticas
Selector de periodo (7 / 30 / 90 días / personalizado) y de cuentas. Pestañas: Conversaciones
(KPIs con comparación, por día, mapa de calor día×hora, tiempos de respuesta, quién inicia, chats
más activos) y Grupos (más activos, personas más activas). Gráficos con `recharts`. Portar la
presentación de `../dconnector/src/renderer/src/components/stats/*` sin listas de clientes.

### 6.8 Cuentas (`pages/AccountsPage.tsx`, `components/accounts/`)
- Tarjetas por cuenta (arrastrar para ordenar): color, etiqueta, canal, propio nombre y número/
  usuario, estado con detalle, botones Conectar/Desconectar, Editar, Volver a vincular, Cerrar
  sesión, Eliminar (confirmación escribiendo la etiqueta).
- «Añadir cuenta» (`AddAccountWizard`): 1) canal (4 tarjetas). 2) etiqueta y color (+ en Telegram
  `api_id` y `api_hash` con enlace a my.telegram.org; + en Meta el aviso de vía no oficial con
  casilla obligatoria). 3) vinculación según `AccountStatus.Need`, escuchando `account:status`:
  - WhatsApp: pestañas «Código QR» (QR grande con `qrcode.react`, se refresca solo) y «Con número»
    (teléfono → muestra el código `XXXX-XXXX` en grande con instrucciones de 3 pasos del teléfono).
  - Telegram: teléfono → código → contraseña (si la pide).
  - Messenger/Instagram: caja grande «Pega aquí las cookies» con ayuda plegable de cómo copiarlas
    desde el navegador; y «Usuario y contraseña» si la capacidad existe.
  4) «Conectada ✓» con nombre propio y botón «Ir a chats».

### 6.9 Destinos
Lista con estado en vivo (punto, «En línea», error, pendientes). Formulario: nombre, URL, token
(oculto con mostrar), cuentas y canales (selección), eventos (casillas), incluir los míos, incluir
grupos, «Permitir comandos» (con aviso «El servidor podrá enviar mensajes en tu nombre»). Botón
«Probar conexión». Enlace «Ver el protocolo» abre `docs/PROTOCOLO-v1.md` en el navegador del sistema.

### 6.10 Ajustes
Secciones: General (cerrar a la bandeja, iniciar con Windows, iniciar minimizado, notificaciones,
tema), Descargas automáticas (por tipo y tamaño máx.), Campañas (palabras de baja, texto del pie),
Datos (carpeta, abrir, abrir registros, registro detallado), Acerca de (versión, protocolo v1,
licencias de terceros: lista estática con whatsmeow MPL-2.0, gotd MIT, messagix AGPL-3.0, Wails MIT,
modernc sqlite BSD).

## 7. Estado en el cliente (`store/`)

- `accounts.ts`: `views: Record<id, AccountView>`; escucha `account:status`, `account:caps`.
- `inbox.ts`: `chats: Map<chatKey, Chat>` + orden; `filter`; carga páginas; escucha `chat:upsert`.
- `chat.ts`: chat abierto, `messages: Message[]`, `hasMore`, `replyTo`, `editing`; escucha
  `message:upsert`/`message:update`/`receipt`/`typing` y solo aplica los del chat abierto.
- `statuses.ts`, `ui.ts` (página, modales, toasts, tema).
- `api/` envuelve los bindings y convierte errores a `toast`.

## 8. Pruebas de interfaz

`vitest` para `lib/` (formato de hora, formato WhatsApp, `caps.ts`, `webmToOgg` con su prueba
portada de `../dconnector/tests/webmToOgg.test.ts`) y para los reductores de `store/`.
