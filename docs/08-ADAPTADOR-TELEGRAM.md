# 08 · Adaptador Telegram (gotd)

Paquete `internal/adapter/channel/telegram`. Librerías: `github.com/gotd/td/telegram`,
`…/telegram/auth`, `…/telegram/updates`, `…/telegram/message`, `…/telegram/uploader`,
`…/telegram/downloader`, `…/telegram/query`, `…/tg`, y `github.com/gotd/contrib/middleware/floodwait`.

> Antes de cada llamada: `go doc github.com/gotd/td/tg.Client.<Método>` (la API de `tg` se genera
> del esquema de Telegram y los nombres son `MessagesSendMessage`, `StoriesSendStory`, etc.).

## 1. Archivos

| Archivo | Contenido |
| --- | --- |
| `adapter.go` | `Adapter{port.Base; deps; client *telegram.Client; api *tg.Client; gaps *updates.Manager; self *tg.User; peers *peerCache; sink}`, `New`, `Start`, `Stop`, `Logout`, `Capabilities` |
| `session.go` | `kvSession` (implementa `session.Storage` sobre `kv["tg.session"]`), `kvUpdatesState` (implementa `updates.StateStorage` en `kv["tg.state"]` y `kv["tg.chpts.<id>"]`), `kvAccessHasher` (`updates.ChannelAccessHasher` en `kv["tg.hash.<userId>.<channelId>"]`) |
| `auth.go` | `BeginPairing`, `SubmitPairing` |
| `peers.go` | `peerCache`: id nativo ↔ `tg.InputPeerClass` (guarda `access_hash` de usuarios y canales en `kv["tg.peer.<native>"]`); se llena con todas las `tg.Entities` que llegan |
| `convert.go` | **puras**: `convertMessage(m *tg.Message, ents peerNames, selfID int64) model.Message`, `convertService(*tg.MessageService)`, `nativeOf(tg.PeerClass) string`, `kindOf(...)` |
| `send.go` | envío y acciones |
| `media.go` | `DownloadMedia`, `FetchAvatar`, handle de medios |
| `history.go` | `SyncChats`, `RequestHistory` |
| `stories.go` | historias |
| `convert_test.go` | pruebas de traducción con `tg.Message` construidos a mano |

## 2. Configuración y cliente

- `api_id` (int) y `api_hash` de `Account.Config`; si faltan, `Start` emite `pairing` con
  `Detail = "Falta api_id/api_hash"` y no conecta.
- ```go
  gaps := updates.New(updates.Config{Handler: dispatcher, Storage: stateStorage, AccessHasher: hasher, Logger: zapNop})
  client := telegram.NewClient(apiID, apiHash, telegram.Options{
      SessionStorage: kvSession,
      UpdateHandler:  gaps,
      Middlewares:    []telegram.Middleware{floodwait.NewSimpleWaiter()},
      Device: telegram.DeviceConfig{DeviceModel: "dConnector", SystemVersion: "Windows", AppVersion: buildinfo.Version, LangCode: "es", SystemLangCode: "es"},
      Logger: zapNop, // zap.NewNop(); con «Registro detallado», un zap que escribe a slog
  })
  ```
- `Start`: goroutine con `client.Run(ctx, func(ctx) error { … })`. Dentro:
  1. `status, _ := client.Auth().Status(ctx)`; si no autorizado → emitir `pairing` + `Need: phone`
     y esperar en un canal interno a que la vinculación termine.
  2. `self, _ := client.Self(ctx)` → `person.updated` propio (`NativeID = "user:<id>"`,
     `Phone = "+" + self.Phone`, `Username`, nombre).
  3. `gaps.Run(ctx, client.API(), self.ID, updates.AuthOptions{IsBot: false, OnStart: …})` en otra
     goroutine; emitir `connected`; `SyncChats` en segundo plano.
  4. Bloquear en `<-ctx.Done()`.
  Si `Run` devuelve error → `error` (el servicio Accounts reintenta). `auth.ErrUnauthorized` /
  `AUTH_KEY_UNREGISTERED` → `logged_out` y borrar `kv["tg.session"]`.

## 3. Vinculación

- `BeginPairing{Method: "login", Phone}`: `sent, err := client.Auth().SendCode(ctx, phone, auth.SendCodeOptions{})`;
  de `*tg.AuthSentCode` guardar `PhoneCodeHash` en memoria → `pairing` + `Need: login_code`.
- `SubmitPairing{Code}`: `client.Auth().SignIn(ctx, phone, code, hash)`; si
  `errors.Is(err, auth.ErrPasswordAuthNeeded)` → `pairing` + `Need: password`; si
  `*auth.SignUpRequired` → `error` con «Ese número no tiene cuenta de Telegram».
- `SubmitPairing{Password}`: `client.Auth().Password(ctx, password)` → seguir con el paso 2 de §2.
- Códigos incorrectos (`PHONE_CODE_INVALID`, `PASSWORD_HASH_INVALID`) → `pairing` con el mismo
  `Need` y `Detail` «Código incorrecto» / «Contraseña incorrecta».

## 4. Ids

| Telegram | `nativeChatID` | `ChatKind` |
| --- | --- | --- |
| `PeerUser` | `user:<id>` | `direct` (`self` si es el propio usuario: «Mensajes guardados») |
| `PeerChat` | `chat:<id>` | `group` |
| `PeerChannel` con `Megagroup` | `channel:<id>` | `group` |
| `PeerChannel` sin `Megagroup` | `channel:<id>` | `channel` (`ReadOnly` si no eres admin con permiso de publicar) |

`NativeID` del mensaje = `strconv.Itoa(msg.ID)` (los ids son por chat en canales y por cuenta en
privados/grupos; con la clave compuesta chat+id no chocan). Emisor: `msg.FromID` (si nil en privados,
el otro usuario o yo según `Out`).

## 5. Entrada

`dispatcher := tg.NewUpdateDispatcher()` con:

| Manejador | Se emite |
| --- | --- |
| `OnNewMessage`, `OnNewChannelMessage` | `message.received` (`*tg.Message`) o `system` (`*tg.MessageService`: altas, bajas, título) |
| `OnEditMessage`, `OnEditChannelMessage` | `message.updated{Text, EditedAt: EditDate}` |
| `OnDeleteMessages` (sin chat: buscar el chat del id en la base con `kv` índice `tg.msgchat.<id>`), `OnDeleteChannelMessages` | `message.updated{Deleted}` |
| `OnReadHistoryOutbox`, `OnReadChannelOutbox` | `receipt` `read` hasta `MaxID` |
| `OnUserTyping`, `OnChatUserTyping`, `OnChannelUserTyping` | `typing` |
| `OnMessageReactions` (`UpdateMessageReactions`) | `reaction` (las recientes de `RecentReactions`) |
| `OnMessagePoll` | `message.updated{Poll}` con recuentos |
| `OnStory` | `status.posted` |
| `OnUserName`, `OnUser` | `person.updated` |

Cada manejador recibe `tg.Entities`: se pasan al `peerCache` y a los nombres de `convertMessage`.

`convertMessage`:
- `Message` → text; `Entities` de tipo mención → `Mentions`; `ReplyTo.(*tg.MessageReplyHeader).ReplyToMsgID` → cita; `FwdFrom != nil` → `Forwarded`.
- `Media`: `*tg.MessageMediaPhoto` → image (tamaño mayor de `Sizes`); `*tg.MessageMediaDocument` →
  según atributos: `DocumentAttributeSticker` → sticker; `DocumentAttributeAudio{Voice}` → voice,
  si no audio; `DocumentAttributeVideo` (con `RoundMessage` también) → video;
  `DocumentAttributeAnimated` → video; otro → document (`DocumentAttributeFilename`).
  `*tg.MessageMediaGeo`/`GeoLive`/`Venue` → location; `*tg.MessageMediaContact` → contact;
  `*tg.MessageMediaPoll` → poll (las opciones con su `Option []byte` en base64 dentro de
  `kv["tg.poll.<chat>.<msg>"]` para votar); otros → unsupported con el texto que traiga.
- `Media.Handle` = JSON `{"t":"photo"|"doc","id":…,"ah":…,"fr":"<base64>","ts":"<thumbSize>","dc":…}`.

## 6. Envío

- `sender := message.NewSender(api)`; `b := sender.To(inputPeer)`; con cita `b = b.Reply(id)`.
- texto → `b.Text(ctx, text)` (menciones: v1 envía texto plano).
- archivos → `up := uploader.NewUploader(api)`; `f, _ := up.FromPath(ctx, path)`:
  imagen ≤ 10 MB → `b.Media(ctx, message.UploadedPhoto(f, styling.Plain(caption)))`; imagen mayor →
  como documento; vídeo → `message.UploadedDocument(f).MIME(mime).Video()` (atributos de vídeo);
  voz → `message.UploadedDocument(f).MIME("audio/ogg").Voice()` o atributo `DocumentAttributeAudio{Voice: true, Duration}`;
  audio → `.Audio()`; documento → `.Filename(name).MIME(mime)`; sticker (.webp) → documento
  `image/webp` con `DocumentAttributeSticker`.
- ubicación → `message.Media(&tg.InputMediaGeoPoint{GeoPoint: &tg.InputGeoPoint{Lat, Long}})`;
  contacto → `&tg.InputMediaContact{PhoneNumber, FirstName, Vcard}`; encuesta → `&tg.InputMediaPoll{Poll: tg.Poll{Question, Answers: [{Text, Option: []byte{'0'+i}}], MultipleChoice}}`.
- La respuesta es `tg.UpdatesClass`: sacar el `UpdateMessageID`/`UpdateNewMessage` para el id real;
  devolver el `model.Message` con `Delivery = sent`.
- `React` → `api.MessagesSendReaction(ctx, &tg.MessagesSendReactionRequest{Peer, MsgID, Reaction: []tg.ReactionClass{&tg.ReactionEmoji{Emoticon: e}}})` (vacío = lista vacía).
- `Edit` → `api.MessagesEditMessage(ctx, &tg.MessagesEditMessageRequest{Peer, ID, Message: text})`.
- `Delete` → canales: `api.ChannelsDeleteMessages`; resto: `api.MessagesDeleteMessages{ID, Revoke: forEveryone}`.
- `Forward` → `api.MessagesForwardMessages{FromPeer, ID: []int{id}, ToPeer, RandomID}`.
- `VotePoll` → `api.MessagesSendVote{Peer, MsgID, Options}` (bytes de `kv["tg.poll…"]`).
- `MarkRead` → `api.MessagesReadHistory{Peer, MaxID}` o `api.ChannelsReadHistory`.
- `SetTyping` → `api.MessagesSetTyping{Peer, Action: &tg.SendMessageTypingAction{}}` (o `Cancel`).
- `ArchiveChat` → `api.FoldersEditPeerFolders` (carpeta 1 / 0). `PinChat` → `api.MessagesToggleDialogPin`.
  `MuteChat` → `api.AccountUpdateNotifySettings{Peer: &tg.InputNotifyPeer{Peer}, Settings: {MuteUntil}}`.

## 7. Historial y chats

- `SyncChats`: `query.GetDialogs(api).BatchSize(100).Iter()` → cada elemento: `Peer`, `Dialog`
  (`UnreadCount`, `Pinned`, `FolderID == 1` → archivado, `NotifySettings.MuteUntil`), `Last`
  (último mensaje → `message.received` sin sumar no leídos: se emite dentro de un `history.batch`),
  `Entities` → títulos y personas. Máximo 1.000 diálogos en v1.
- `RequestHistory(chat, before, count)`: `api.MessagesGetHistory{Peer, OffsetID: before.NativeID, Limit: count}`
  → `history.batch`.

## 8. Medios

`DownloadMedia`: `downloader.NewDownloader().Download(api, location).Stream(ctx, pipeWriter)` con
`location` de foto `&tg.InputPhotoFileLocation{ID, AccessHash, FileReference, ThumbSize}` o de
documento `&tg.InputDocumentFileLocation{ID, AccessHash, FileReference}`. Si falla con
`FILE_REFERENCE_EXPIRED`: volver a pedir el mensaje (`api.MessagesGetMessages` /
`api.ChannelsGetMessages`), actualizar el handle y reintentar una vez.
`FetchAvatar`: `&tg.InputPeerPhotoFileLocation{Peer, PhotoID, Big: false}`.

## 9. Historias

- Al conectar y cada 15 min: `api.StoriesGetAllStories(ctx, &tg.StoriesGetAllStoriesRequest{})` →
  `status.posted` por cada historia vigente (`StoryItem` → `Kind` según `Media`, `Caption`,
  `Date`, `ExpireDate`).
- `PostStatus`: primero `api.StoriesCanSendStory(ctx, &tg.InputPeerSelf{})`; si da error → `ErrUnsupported`
  con el motivo. Si se puede: subir el archivo y `api.StoriesSendStory{Peer: Self, Media, Caption, PrivacyRules, RandomID, Period: 86400}`.
  Privacidad: `contacts` → `InputPrivacyValueAllowContacts`; `allow` → `InputPrivacyValueAllowUsers{Users}`;
  `deny` → `AllowContacts` + `DisallowUsers`. Texto solo no existe en Telegram: estados de texto →
  `ErrUnsupported` («Telegram no admite historias solo de texto»).
- `DeleteStatus` → `api.StoriesDeleteStories`. `MarkStatusViewed` → `api.StoriesReadStories{Peer, MaxID}`.
  `ReactStatus` → `api.StoriesSendReaction`. `StatusViewers` → `api.StoriesGetStoryViewsList{Peer: Self, ID, Limit: 100}`.
- `PostStatus` en capacidades = resultado de `StoriesCanSendStory` al conectar (se vuelve a emitir
  `account.capabilities` si cambia).

## 10. Personas

`ListPersons` → `api.ContactsGetContacts(ctx, 0)` (usuarios: nombre, `Phone`, `Username`).
`ResolvePerson` → `api.UsersGetUsers` con el `InputUser` del caché.

## 11. Capacidades

```go
capability.Set{
	SendText: true, SendImage: true, SendVideo: true, SendAudio: true, SendVoice: true,
	SendDocument: true, SendSticker: true, SendLocation: true, SendContact: true,
	SendPoll: true, VotePoll: true,
	Reply: true, Forward: true, React: true, MaxReactionsPerUser: 1,
	Edit: true, EditWindow: 48 * time.Hour,
	DeleteForEveryone: true, DeleteWindow: 0,
	ReadReceipts: true, Typing: true, Mentions: true, Groups: true,
	Statuses: true, PostStatus: /* §9 */, StatusViewers: true, StatusAudience: true, StatusReact: true,
	HistoryOnDemand: true,
	MaxFileSize: 2000 << 20, MaxImageSize: 10 << 20, MaxVideoSize: 2000 << 20, MaxTextLen: 4096,
}
```
Canales sin permiso de publicar → `ReadOnly()`.
