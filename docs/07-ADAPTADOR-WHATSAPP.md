# 07 · Adaptador WhatsApp (whatsmeow)

Paquete `internal/adapter/channel/whatsapp`. Librería `go.mau.fi/whatsmeow` y sus subpaquetes
`store/sqlstore`, `types`, `types/events`, `proto/waE2E`, `appstate`, `util/log` (waLog).

> Los nombres de funciones de whatsmeow de este documento son los de las versiones de 2025-2026.
> **Antes de escribir cada llamada se comprueba con `go doc go.mau.fi/whatsmeow.Client.<Método>`**:
> varias recibieron `ctx context.Context` como primer argumento en 2025. Si cambian, se adapta aquí.

## 1. Archivos

| Archivo | Contenido |
| --- | --- |
| `adapter.go` | `type Adapter struct{ port.Base; deps; client *whatsmeow.Client; container *sqlstore.Container; sink; mu }`, `New(deps) (port.ChannelAdapter, error)`, `Channel()`, `Capabilities()`, `Start`, `Stop`, `Logout`, manejador de eventos (`handle(evt any)`) |
| `pairing.go` | `BeginPairing`, `SubmitPairing` (no se usa en WhatsApp: `ErrUnsupported`) |
| `convert.go` | **funciones puras**: `convertMessage(info types.MessageInfo, msg *waE2E.Message, selfJID) (model.Message, isStatus bool)`, `previewOf`, `mediaFrom…`, `jidToNative`, `nativeToJID`, `chatKindOf` |
| `send.go` | `Send`, `React`, `Edit`, `Delete`, `Forward`, `VotePoll`, `MarkRead`, `SetTyping`, `ArchiveChat`, `PinChat`, `MuteChat` |
| `media.go` | `DownloadMedia`, `FetchAvatar`, codificar/decodificar `Media.Handle` |
| `history.go` | `HistorySync` → `history.batch`, `RequestHistory`, `SyncChats` |
| `status.go` | estados: entrantes, `PostStatus`, `DeleteStatus`, `MarkStatusViewed`, `StatusViewers` |
| `persons.go` | `ResolvePerson`, `ListPersons`, LID → teléfono |
| `convert_test.go` | pruebas de traducción con protos construidos a mano (texto, cita, foto, voz, documento, sticker, ubicación, contacto, encuesta, reacción, edición, borrado, estado) |

## 2. Sesión y almacén

1. `container := sqlstore.NewWithDB(deps.DB, "sqlite3", waLogAdapter)` y `container.Upgrade(ctx)`
   (crea las tablas `whatsmeow_*` en `account.db`). El driver `modernc` se registra como `"sqlite"`;
   el dialecto que se pasa a whatsmeow es `"sqlite3"`. Si `Upgrade` falla por el dialecto, registrar
   el driver también como `"sqlite3"` (`sql.Register("sqlite3", &sqlite.Driver{})`) y abrir
   `account.db` con ese nombre; anotarlo en la bitácora.
2. El JID del dispositivo se guarda en `kv["wa.device"]`. Si existe → `container.GetDevice(ctx, jid)`;
   si no → `container.NewDevice()`.
3. `store.DeviceProps`: `Os = "dConnector"`, `PlatformType = DESKTOP` (con `store.SetOSInfo`
   si existe en la versión).
4. `client := whatsmeow.NewClient(device, waLogAdapter)`; `client.AddEventHandler(a.handle)`;
   `client.EnableAutoReconnect = true`; `client.AutomaticMessageRerequestFromPhone = true`.
5. `waLogAdapter` envuelve `deps.Log` con `waLog.Logger` (Debugf/Infof/Warnf/Errorf/Sub).

## 3. Ciclo de vida

- `Start`: si `client.Store.ID == nil` → emitir `AccountStatus{State: pairing, Need: ""}` y esperar a
  `BeginPairing`. Si no → `client.Connect()` y emitir `connecting`.
- Eventos de conexión:
  - `*events.Connected` → `connected`; `client.SendPresence(types.PresenceUnavailable)` (para que el
    teléfono siga recibiendo avisos); emitir `person.updated` propio (`NativeID = SelfID`, `PushName`
    = `client.Store.PushName`, `Phone` = `+` + usuario del JID); llamar `SyncChats` en segundo plano.
  - `*events.Disconnected` → `connecting` (whatsmeow reconecta solo).
  - `*events.LoggedOut`, `*events.StreamReplaced` (otra sesión), `*events.ClientOutdated` →
    `logged_out` con `Detail` en español; borrar `kv["wa.device"]` y `client.Store.Delete(ctx)`.
  - `*events.TemporaryBan` → `error` con `Detail = "WhatsApp bloqueó temporalmente la cuenta: <motivo>, hasta <fecha>"`.
- `Stop`: `client.Disconnect()`. `Logout`: `client.Logout(ctx)`, luego lo mismo que `LoggedOut`.

## 4. Vinculación

- `BeginPairing{Method: "qr"}`: `qr, _ := client.GetQRChannel(ctx)` **antes** de `client.Connect()`.
  Por cada elemento: `"code"` → emitir `pairing` + `Need: scan_qr` + `QRCode = item.Code`;
  `"success"` → `connecting` y guardar `kv["wa.device"] = client.Store.ID.String()`; `"timeout"` →
  `pairing` + `Detail = "El código QR caducó. Vuelve a intentarlo."`.
- `BeginPairing{Method: "phone", Phone}`: `client.Connect()`, esperar el primer `"code"` del canal QR
  (señal de que el socket está listo) y llamar
  `client.PairPhone(ctx, telefonoSoloDigitos, true, whatsmeow.PairClientChrome, "Chrome (Windows)")`.
  Emitir `pairing` + `Need: enter_code` + `PairCode` (formato `XXXX-XXXX`).
- `*events.PairSuccess` → guardar `kv["wa.device"]`.

## 5. Ids

- `nativeChatID` = `jid.ToNonAD().String()`. Grupos `@g.us`, directos `@s.whatsapp.net`, canales
  `@newsletter`, difusión de estados `status@broadcast` (**no** es un chat: va a estados).
- `@lid`: si `info.Chat.Server == types.HiddenUserServer`, buscar el teléfono con
  `client.Store.LIDs.GetPNForLID(ctx, lid)`; si lo hay, usar el JID de teléfono como id del chat y
  del emisor. Igual en reacciones, recibos y vistas de estados.
- Chat contigo mismo: `info.Chat.User == client.Store.ID.User` → `ChatSelf`.
- `NativeID` del mensaje = `info.ID`. `SenderNativeID` = `info.Sender.ToNonAD().String()` (resuelto
  de LID). `FromMe = info.IsFromMe`.
- `Media.ID` = primeros 24 hex de `sha1(chatNativeID + "/" + msgID)`.

## 6. Traducción de entrada (`convertMessage`)

Se trabaja sobre `evt.Message` (whatsmeow ya quita envoltorios efímeros / ver una vez; si aún viene
`EphemeralMessage`, `ViewOnceMessage`, `ViewOnceMessageV2`, `DocumentWithCaptionMessage` o
`EditedMessage`, se desenvuelve primero).

| Proto | Kind | Campos |
| --- | --- | --- |
| `Conversation` / `ExtendedTextMessage.Text` | text | `ContextInfo` → cita (`StanzaID`, `Participant`, texto de `QuotedMessage`), `MentionedJID`, `IsForwarded` |
| `ImageMessage` | image | `Caption`, `Mimetype`, `FileLength`, `Width`, `Height`, `JPEGThumbnail` → `Thumb` (data URL) |
| `VideoMessage` | video | igual + `Seconds` → `Duration` |
| `AudioMessage` | `voice` si `PTT`, si no `audio` | `Seconds`, `Mimetype` |
| `DocumentMessage` | document | `FileName`, `Title`, `Caption`, `Mimetype`, `FileLength` |
| `StickerMessage` | sticker | `Mimetype` (`image/webp`), `Width`, `Height` |
| `LocationMessage`, `LiveLocationMessage` | location | grados, `Name`, `Address`, `Live` |
| `ContactMessage`, `ContactsArrayMessage` | contact | `DisplayName`, `Vcard` (teléfono: primera línea `TEL`) |
| `PollCreationMessage`, `…V2`, `…V3` | poll | `Name`, `Options[].OptionName`, `SelectableOptionsCount != 1` → `Multi` |
| `ReactionMessage` | — | evento `reaction` (`Key.ID`, `Text`; vacío = quitada) |
| `ProtocolMessage` tipo `REVOKE` | — | `message.updated{Deleted}` del `Key.ID` |
| `ProtocolMessage` tipo `MESSAGE_EDIT` | — | `message.updated{Text, EditedAt}` con el texto de `EditedMessage` |
| `PollUpdateMessage` | — | `client.DecryptPollVote(ctx, evt)` → hashes de opciones → nombres (sha256 de cada opción) → `message.updated{Poll}` con votantes |
| `ProtocolMessage` de otros tipos, `SenderKeyDistributionMessage` solo | — | se ignoran |
| Cualquier otro | unsupported | `Text = ""` |

`Media.Handle` = `[]byte{tipo} + proto.Marshal(subMensaje)` (tipo: 1 imagen, 2 vídeo, 3 audio,
4 documento, 5 sticker). `DownloadMedia` lo deshace y llama a `client.Download(ctx, sub)`.

`previewOf(m)`: texto recortado a 80 runas; si no hay texto: «📷 Foto», «🎥 Vídeo», «🎤 Nota de voz»,
«🎵 Audio», «📄 <nombre>», «Sticker», «📍 Ubicación», «👤 <nombre>», «📊 <pregunta>», «Mensaje
eliminado», «Mensaje no compatible». Es común a todos los adaptadores: va en
`internal/core/model/preview.go` (`func Preview(m Message) string`), **lo escribe el orquestador en
la oleada 0**.

## 7. Otros eventos

| Evento whatsmeow | Se emite |
| --- | --- |
| `*events.Message` (no estado) | `message.received` (también si `IsFromMe`: enviado desde el teléfono) |
| `*events.Message` en `status@broadcast` | `status.posted` (autor `info.Sender`, `ExpiresAt = ts + 24 h`) |
| `*events.Receipt` | `receipt` con `delivered` (tipo vacío), `read`, `played`; en `status@broadcast` con `read`/`played` → `status.viewed` por cada id |
| `*events.ChatPresence` | `typing` (`composing` → typing, `Media == audio` → recording) |
| `*events.PushName` | `person.updated` |
| `*events.Picture` | `FetchAvatar` y `chat.updated`/`person.updated` con el avatar |
| `*events.GroupInfo`, `*events.JoinedGroup` | `chat.updated` (título) y `message.received` de tipo `system` («X se unió», «X salió», «X cambió el asunto») |
| `*events.Archive`, `*events.Pin`, `*events.Mute`, `*events.MarkChatAsRead` | `chat.updated` |
| `*events.UndecryptableMessage` | `message.received` `unsupported` con `Text = "Esperando este mensaje…"` |
| `*events.HistorySync` | `history.batch` (§8) |

## 8. Historial

- `*events.HistorySync`: por cada `Conversation` → `Chat` (id, `Name`/`DisplayName`, `UnreadCount`,
  `Archived`, `Pinned`, `MuteEndTime`, `ConversationTimestamp`); por cada `Messages[i].Message`
  (`*waWeb.WebMessageInfo`) → `client.ParseWebMessage(chatJID, webMsg)` → `convertMessage`.
  `Pushnames` → personas. `Progress` → `HistoryBatch.Progress` (0..1). Emitir por lotes de 500
  mensajes como máximo.
- `RequestHistory(chat, before, count)`: si `before == nil`, tomar el más antiguo de la base.
  `msg := client.BuildHistorySyncRequest(&types.MessageInfo{…del más antiguo…}, count)` y
  `client.SendMessage(ctx, client.Store.ID.ToNonAD(), msg, whatsmeow.SendRequestExtra{Peer: true})`.
  La respuesta llega como `HistorySync` de tipo `ON_DEMAND`. Necesita el teléfono en línea.
- `SyncChats`: `client.GetJoinedGroups(ctx)` → `chat.updated` por grupo (título); los directos
  salen del historial y de los mensajes.

## 9. Envío

`Send(chat, out)`:
- texto → `waE2E.Message{ExtendedTextMessage: {Text, ContextInfo}}` (`Conversation` si no hay cita
  ni menciones).
- medios → `os.ReadFile(out.FilePath)`, `client.Upload(ctx, data, whatsmeow.MediaImage|MediaVideo|MediaAudio|MediaDocument)`
  y el submensaje con `URL`, `DirectPath`, `MediaKey`, `FileEncSHA256`, `FileSHA256`,
  `FileLength`, `Mimetype`. Imagen: ancho/alto con `image.DecodeConfig` (registrar `image/png`,
  `image/jpeg`, `golang.org/x/image/webp`); miniatura JPEG de 72 px de lado mayor con
  `golang.org/x/image/draw.ApproxBiLinear` en `media.go`. Voz: `AudioMessage{PTT: true, Mimetype: "audio/ogg; codecs=opus", Seconds}`
  (la interfaz ya manda OGG/Opus). Sticker: solo `.webp` (`StickerMessage`); otro formato →
  `ErrInvalid` «El sticker debe ser .webp». Documento: `FileName`.
- ubicación → `LocationMessage`; contacto → `ContactMessage{DisplayName, Vcard}` (si no trae vCard,
  armar una mínima `BEGIN:VCARD…TEL;type=CELL;waid=<dígitos>:+<dígitos>…END:VCARD`).
- encuesta → `client.BuildPollCreation(question, options, 1 o len(options))`.
- cita: `ContextInfo{StanzaID: ReplyTo.NativeID, Participant: ReplyTo.SenderNativeID, QuotedMessage: &waE2E.Message{Conversation: textoCitado}}`
  (texto citado de la base; si no está, vacío).
- `resp, err := client.SendMessage(ctx, jid, msg)` → `model.Message` con `NativeID = resp.ID`,
  `Timestamp = resp.Timestamp`, `Delivery = sent`, `FromMe = true`, y para medios `Media.LocalPath =
  out.FilePath` (ya está en disco).
- `React`: `client.BuildReaction(chat, sender, id, emoji)` + `SendMessage`.
- `Edit`: `client.BuildEdit(chat, id, &waE2E.Message{Conversation: texto})` + `SendMessage`.
- `Delete(forEveryone=true)`: `client.BuildRevoke(chat, sender, id)` + `SendMessage`.
  `forEveryone=false`: solo local (el servicio lo marca borrado sin llamar; el adaptador devuelve nil).
- `Forward`: reenviar el proto original no se guarda; v1 reenvía **reconstruyendo** desde la base:
  texto o el archivo local (descargándolo antes) con `ContextInfo.IsForwarded = true`.
- `VotePoll`: `client.BuildPollVote(ctx, &types.MessageInfo{…de la encuesta…}, opciones)` + `SendMessage`.
- `MarkRead`: `client.MarkRead(ctx, ids, time.Now(), chat, sender)` (en grupos, agrupar por emisor).
- `SetTyping`: `client.SendChatPresence(ctx, chat, types.ChatPresenceComposing|Paused, "")`.
- `ArchiveChat` / `PinChat` / `MuteChat`: `client.SendAppState(ctx, appstate.BuildArchive(...) | BuildPin | BuildMute)`.

## 10. Estados

- Entrantes: §7. `MarkStatusViewed`: `client.MarkRead(ctx, []id, now, types.StatusBroadcastJID, autor)`.
- `PostStatus`: texto → `ExtendedTextMessage{Text, BackgroundArgb: 0xFF<RRGGBB>, Font}`; foto/vídeo →
  igual que medios con `Caption`; `client.SendMessage(ctx, types.StatusBroadcastJID, msg)`.
- **Audiencia**: buscar en la versión instalada una forma de fijar la lista de destinatarios de un
  estado (opción en `SendRequestExtra` o un ajuste de privacidad de estados con lista). Si existe:
  `StatusAudience = true` y se hace como D'Ventas (`../dconnector/docs/WHATSAPP-WEB-INTERNOS.md` §4:
  guardar la privacidad, cambiarla, publicar, restaurarla). Si no existe: `StatusAudience = false` y
  se publica con la privacidad del teléfono.
- `DeleteStatus`: `client.BuildRevoke(types.StatusBroadcastJID, propioJID, id)` + `SendMessage`.
- `StatusViewers`: de la base (los recibos ya llegan como `status.viewed`); el adaptador devuelve
  `ErrUnsupported` y el servicio lee de `ListStatusViews`.
- `ReactStatus`: `ErrUnsupported` en v1 (`StatusReact = false`).

## 11. Personas y avatares

- `ListPersons`: `client.Store.Contacts.GetAllContacts(ctx)` → `Name = FullName|FirstName`,
  `PushName`, `Phone = "+" + jid.User` (solo `@s.whatsapp.net`).
- `ResolvePerson`: lo mismo para uno + `client.Store.Contacts.GetContact`.
- `FetchAvatar`: `client.GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{Preview: true})`
  → `http.Get(info.URL)` (timeout 15 s) → `AvatarDir/<sha1(jid)[:16]>-<info.ID>.jpg`. Se pide como
  mucho una vez cada 24 h por JID (`kv["wa.avatar.<jid>"] = ts`).

## 12. Capacidades

```go
capability.Set{
	SendText: true, SendImage: true, SendVideo: true, SendAudio: true, SendVoice: true,
	SendDocument: true, SendSticker: true, SendLocation: true, SendContact: true,
	SendPoll: true, VotePoll: true,
	Reply: true, Forward: true, React: true, MaxReactionsPerUser: 1,
	Edit: true, EditWindow: 15 * time.Minute,
	DeleteForEveryone: true, DeleteWindow: 48 * time.Hour,
	ReadReceipts: true, Typing: true, Mentions: true, Groups: true,
	Statuses: true, PostStatus: true, StatusViewers: true, StatusAudience: /* §10 */, StatusReact: false,
	HistoryOnDemand: true,
	MaxFileSize: 100 << 20, MaxImageSize: 16 << 20, MaxVideoSize: 64 << 20, MaxTextLen: 65536,
}
```
Por chat: `@newsletter` donde no eres administrador → `ReadOnly()`; grupos «solo administradores»
donde no lo eres (`GroupInfo.IsAnnounce` y no admin) → `ReadOnly()`.
