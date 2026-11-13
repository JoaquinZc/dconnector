# 09 · Adaptadores Messenger e Instagram (messagix)

Paquetes `internal/adapter/channel/meta` (común), `…/messenger` y `…/instagram`. Vía **no
oficial**: la librería imita el cliente web/móvil de Meta. Licencia AGPL-3.0.

## 1. Regla de importación (paso 1 de la oleada que construye estos adaptadores)

Lo hace el **orquestador** en la oleada 0 al instalar dependencias, y anota en `BITACORA.md` qué
salió:

1. `go list -m -versions go.mau.fi/mautrix-meta` → tomar la última etiqueta.
   `go get go.mau.fi/mautrix-meta@<etiqueta>` y comprobar que `go.mau.fi/mautrix-meta/pkg/messagix`
   se importa **sin** arrastrar el puente de Matrix (`go build` de un archivo que solo lo importa).
   → Esa es la librería de **Messenger**.
2. Instagram: desde v26.08 mautrix-meta ya no habla con Instagram. En orden:
   a. Si `pkg/messagix` de la última etiqueta todavía tiene la plataforma Instagram funcional
      (buscar `Instagram` en `pkg/messagix/types`), usarla para los dos.
   b. Si no: buscar el repositorio Go del nuevo puente (`github.com/mautrix/instagram`, leer su
      `go.mod` para la ruta de módulo) y un paquete importable bajo `pkg/` con el cliente de
      Instagram. Si existe, usarlo.
   c. Si no es importable (`internal/` o atado al puente): copiar `pkg/messagix` de la etiqueta
      **v0.2607.x** de mautrix-meta (la última con Instagram) a `third_party/messagix-ig/` con su
      `LICENSE`, cambiar su ruta de import a `dconnector/third_party/messagix-ig/...` y usarla solo
      para Instagram.
3. **Cifrado de extremo a extremo de Messenger** (la mayoría de chats personales desde 2024): en
   mautrix-meta se hace con whatsmeow en modo Messenger (buscar en `pkg/connector` los archivos de
   E2EE y el uso de `whatsmeow` con configuración de Messenger). Si esa parte se puede usar o copiar
   (a `third_party/`, con licencia) y compila: `E2EEChats = true`. Si tras implementarlo en la
   oleada correspondiente no compila o no funciona con el adaptador falso de pruebas: se entrega
   `E2EEChats = false` y la interfaz muestra en las cuentas de Messenger el aviso fijo «Los chats
   cifrados de extremo a extremo aún no se muestran».

> Los nombres de tipos y funciones de messagix que siguen son **orientativos**: se leen con
> `go doc` en la versión instalada y se ajustan. Lo que no cambia es lo que cada método del puerto
> tiene que lograr.

## 2. Común (`meta/`)

- `common.go`: `type Platform string` (`"messenger"`, `"instagram"`); `type Client` que envuelve el
  cliente messagix: crear desde cookies, conectar el socket, manejar eventos, tareas.
- Cookies: se aceptan JSON (array de `{name, value}` exportado del navegador, o objeto
  `{"c_user": "...", "xs": "..."}`) o cabecera `nombre=valor; nombre2=valor2`. Se guardan en
  `kv["meta.cookies"]` (JSON) y se cargan en `Start`.
  - Messenger necesita al menos `c_user`, `xs`, `datr`, `fr` (validar y decir cuál falta).
  - Instagram necesita al menos `sessionid`, `ds_user_id`, `csrftoken`, `mid`.
- `Start`: si no hay cookies → `pairing` + `Need: cookies`. Si hay: crear cliente, cargar la primera
  página (`LoadMessagesPage` o equivalente: devuelve hilos y mensajes iniciales en tablas
  `LS…`), convertir a `history.batch`, conectar el socket y emitir `connected`.
- Eventos del socket (tablas `LS…` que vengan en cada publicación): por cada tabla relevante:

| Tabla (orientativa) | Se emite |
| --- | --- |
| `LSDeleteThenInsertThread`, `LSUpdateThreadSnippet`, `LSSyncUpdateThreadName` | `chat.updated` |
| `LSInsertMessage`, `LSUpsertMessage` | `message.received` |
| `LSInsertAttachment`, `LSInsertBlobAttachment`, `LSInsertXmaAttachment`, `LSInsertStickerAttachment` | completar el mensaje con su medio (se juntan por `MessageId` dentro de la misma publicación) |
| `LSEditMessage` | `message.updated{Text, EditedAt}` |
| `LSDeleteMessage`, `LSDeleteThenInsertMessage` con `IsUnsent` | `message.updated{Deleted}` |
| `LSUpsertReaction`, `LSDeleteReaction` | `reaction` |
| `LSUpdateReadReceipt` | `receipt read` |
| `LSUpdateTypingIndicator` | `typing` |
| `LSVerifyContactRowExists`, `LSDeleteThenInsertContact` | `person.updated` |

- `nativeChatID` = id del hilo (`ThreadKey`) como texto. `NativeID` del mensaje = `MessageId`.
  Emisor = `SenderId`; `FromMe` si es el id propio (`c_user` o `ds_user_id`).
- Kind por tipo de adjunto: imagen, vídeo, audio (voz si es nota de voz), archivo, sticker
  (`LSInsertStickerAttachment`), enlace/`Xma` → texto con la URL.
- `Media.Handle` = JSON con la URL de descarga del adjunto (`PlayableUrl`/`PreviewUrl`/`Url` según
  el tipo) y su expiración. `DownloadMedia` = `http.Get` con las cookies de la cuenta. Si la URL
  caducó, se marca el medio como no disponible («El archivo ya no está disponible»).

## 3. Envío y acciones (común)

| Puerto | Tarea (orientativa) |
| --- | --- |
| `Send` texto | `SendMessageTask{ThreadId, Text, Otid, ReplyMetaData (si hay cita), Source, SendType}` |
| `Send` archivo | subir (`SendMercuryUploadRequest` o equivalente con `Filename`, `MimeType`, datos) → id de adjunto → `SendMessageTask{AttachmentFBIds}` |
| `React` | `SendReactionTask{ThreadKey, MessageID, Reaction}` |
| `Edit` | `EditMessageTask{MessageID, Text}` (Messenger; Instagram si existe) |
| `Delete` (para todos) | `DeleteMessageTask{MessageId}` |
| `MarkRead` | `ThreadMarkReadTask{ThreadId, LastReadWatermarkTs}` |
| `SetTyping` | tarea de indicador de escritura |
| `RequestHistory` | `FetchMessagesTask{ThreadKey, ReferenceTimestampMs, ReferenceMessageId}` |
| `SyncChats` | `FetchThreadsTask` (paginado, máx. 500 hilos) |

Lo que el canal no tenga → `ErrUnsupported` (por `port.Base`).

## 4. Inicio de sesión con usuario y contraseña

Si la versión instalada ofrece inicio de sesión con credenciales (Messenger por la app de
Android/iOS desde v26.08), se añade `BeginPairing{Method: "password_login"}` →
`Need: password`/`login_code` según lo que pida, y se guarda lo que devuelva como sesión en
`kv["meta.session"]`. Si no existe en la versión instalada, ese método devuelve `ErrUnsupported` y la
interfaz solo muestra «Pegar cookies».

## 5. Capacidades

```go
// messenger
capability.Set{
	SendText: true, SendImage: true, SendVideo: true, SendAudio: true, SendVoice: true,
	SendDocument: true, SendSticker: false, SendLocation: false, SendContact: false,
	Reply: true, Forward: false, React: true, MaxReactionsPerUser: 1,
	Edit: true, EditWindow: 15 * time.Minute, DeleteForEveryone: true, DeleteWindow: 0,
	ReadReceipts: true, Typing: true, Groups: true,
	HistoryOnDemand: true, E2EEChats: /* §1.3 */,
	MaxFileSize: 25 << 20, MaxImageSize: 25 << 20, MaxVideoSize: 25 << 20, MaxTextLen: 20000,
}
// instagram: igual pero SendDocument: false, Edit: según versión (si no hay tarea, false), Groups: true, E2EEChats: false, MaxTextLen: 1000
```
`Statuses`, `PostStatus`: false en los dos (las historias de Instagram/Facebook no están en este
protocolo de mensajes).

## 6. Pruebas

`meta/convert_test.go`: tablas `LS…` construidas a mano (o JSON de ejemplo guardado en
`testdata/`) → mensajes del modelo. Sin cuentas reales.
