# 10 · Protocolo dConnector v1

Para conectar dConnector con cualquier servidor (un bot, un almacén de mensajes, una IA) sin que el
servidor hable con cada red. Paquete `internal/adapter/protocol/ws`. Este documento es también la
especificación pública: se copia a `docs/PROTOCOLO-v1.md` del producto tal cual.

## 1. Conexión

- **dConnector es el cliente**. Por cada destino habilitado abre `GET <url>` WebSocket con cabeceras:
  - `Authorization: Bearer <token>`
  - `X-DConnector-Id: <instance_id>`
  - `X-DConnector-Protocol: 1`
  - `User-Agent: dConnector/<versión>`
- `wss://` obligatorio; `ws://` solo para `localhost` / `127.0.0.1` / `[::1]`.
- Si el servidor responde 401/403 → estado `error` «Token rechazado» y **no** se reintenta hasta que
  se edite el destino. Otros fallos → reintento con espera 1 s, 2, 4, 8 … máx. 60 s, con ±20 %.
- Si el servidor responde con `Retry-After` (segundos o fecha HTTP), la espera es **como mínimo** ese
  tiempo, con tope de 1 hora (`maxRetryAfter`, `client.go`): `espera = max(creciente, Retry-After)`, con
  variación solo hacia arriba (0–20 %) para no quedar por debajo de lo pedido. Un valor inválido o
  negativo se ignora. Editar o guardar el destino (aunque no cambie nada) cancela la espera y reconecta al instante. El estado `error`
  dice «El servidor pidió esperar (código HTTP 503); se reintentará en X».
- Ping del WebSocket cada 25 s; si no hay pong en 10 s, se cierra y se reconecta.
- Tamaño máximo de frame de texto: 1 MiB en los dos sentidos.

## 2. Sobre

Cada frame es **un** objeto JSON (UTF-8):

```json
{
  "v": 1,
  "id": "01jc8x…",
  "kind": "hello | welcome | event | ack | command | result",
  "name": "message.received",
  "ts": 1762900000000,
  "account": { "id": "wa-01jc…", "channel": "whatsapp", "label": "Ventas" },
  "data": { }
}
```

- `id`: ULID único del frame. En `event` es el id del evento (el mismo en reenvíos). En `command`
  lo pone el servidor; el `result` lleva **el mismo** `id`.
- `account`: en eventos siempre; en comandos, el servidor indica la cuenta con `account.id`.
- Campos desconocidos se ignoran (compatibilidad hacia delante).

## 3. Saludo

1. dConnector → `hello`:
```json
{ "v":1, "id":"…", "kind":"hello", "ts":…, "data": {
  "instanceId": "01jc…", "version": "0.1.0", "protocol": 1,
  "accounts": [
    { "id":"wa-…", "channel":"whatsapp", "label":"Ventas", "color":"#25D366",
      "state":"connected", "selfId":"593999…@s.whatsapp.net", "selfName":"Ana", "selfHandle":"+593999…",
      "capabilities": { …capability.Set… } }
  ],
  "filter": { …DestinationFilter… },
  "allowCommands": true
} }
```
Solo van las cuentas que pasan el filtro del destino.
2. Servidor → `welcome` (opcional; si no llega en 10 s se sigue igual):
```json
{ "v":1, "id":"…", "kind":"welcome", "data": { "name": "Mi bot", "events": ["message.received"], "resumeFrom": "" } }
```
`events` (opcional) reduce aún más los eventos que quiere recibir.
3. dConnector empieza a enviar el **outbox pendiente** en orden de creación y luego los nuevos.

## 4. Eventos (dConnector → servidor)

`kind: "event"`, `name` uno de:

| name | data |
| --- | --- |
| `account.state` | `AccountStatus` (sin `qrCode` ni `pairCode`: nunca salen por el protocolo) |
| `account.capabilities` | `capability.Set` |
| `chat.updated` | `Chat` |
| `message.received` | `Message` (entrante, o saliente hecho desde otro dispositivo; `fromMe` lo distingue) |
| `message.sent` | `Message` (saliente hecho desde dConnector, por la interfaz o por un comando; lleva `clientRef` si el comando lo trajo, en `data.clientRef`) |
| `message.updated` | `{ "update": MessageUpdate, "message": Message }` |
| `reaction` | `ReactionEvent` |
| `receipt` | `ReceiptEvent` |
| `person.updated` | `Person` |
| `status.posted` | `Status` |
| `status.viewed` | `StatusView` |

Los objetos son los de `03-DOMINIO.md` con sus etiquetas JSON. En `Message.media`, `url` no sirve
fuera de dConnector: el archivo se pide con `media.get` (§6).

Filtro del destino (`DestinationFilter`): cuentas, canales, eventos; `includeFromMe = false` quita
`message.received` con `fromMe`, `message.sent` y sus `message.updated`; `includeGroups = false`
quita lo de chats `group`/`channel`.

### Entrega

- Cada evento que pasa el filtro se guarda en `outbox` (por destino) **antes** de intentar enviarlo.
- El servidor confirma con `ack`:
```json
{ "v":1, "kind":"ack", "id":"…", "data": { "ids": ["<id evento>", "…"] } }
```
- Ventana: como mucho **200** eventos enviados sin `ack`. Al reconectar se reenvían desde el más
  antiguo sin `ack`. El servidor **debe** deduplicar por `id`.
- Limpieza: se borran del outbox los de más de 7 días o lo que pase de 50.000 filas por destino
  (lo más antiguo primero), con aviso en el registro.

## 5. Comandos (servidor → dConnector)

Solo si el destino tiene **«Permitir comandos»**; si no, todo comando recibe `result` con
`error.code = "forbidden"`. La cuenta debe pasar el filtro del destino.

```json
{ "v":1, "id":"cmd-1", "kind":"command", "name":"message.send",
  "account": { "id":"wa-…" },
  "data": { "chatId":"593999…@s.whatsapp.net", "message": { "kind":"text", "text":"Hola" }, "clientRef":"abc" } }
```
Respuesta:
```json
{ "v":1, "id":"cmd-1", "kind":"result", "name":"message.send",
  "data": { "ok": true, "result": { …Message… } } }
{ "v":1, "id":"cmd-1", "kind":"result", "name":"message.send",
  "data": { "ok": false, "error": { "code":"unsupported", "message":"no admitido por este canal" } } }
```

| name | data | result |
| --- | --- | --- |
| `accounts.list` | `{}` | `[{account, status, capabilities}]` (filtrado) |
| `chats.list` | `ChatFilter` (sin `accountIds`: se usa `account.id` si viene, si no todas las del filtro) | `[Chat]` |
| `messages.list` | `{ chatId, beforeTs, limit }` | `MessagePage` |
| `message.send` | `{ chatId, message: Outgoing, clientRef }` | `Message` |
| `message.reply` | `{ chatId, replyTo: messageId, message: Outgoing, clientRef }` | `Message` |
| `message.react` | `{ chatId, messageId, emoji }` | `{}` |
| `message.edit` | `{ chatId, messageId, text }` | `{}` |
| `message.delete` | `{ chatId, messageId, forEveryone }` | `{}` |
| `message.forward` | `{ chatId, messageId, toChatId }` | `Message` |
| `poll.vote` | `{ chatId, messageId, options: [..] }` | `{}` |
| `chat.mark_read` | `{ chatId }` | `{}` |
| `chat.typing` | `{ chatId, typing }` | `{}` |
| `status.post` | `OutgoingStatus` (archivo por `fileUrl` o `uploadId`) | `Status` |
| `media.get` | `{ mediaId }` | ver §6 |
| `media.upload` | `{ fileName, mime, size }` | `{ uploadId }` y luego frames `media.chunk` del servidor |
| `contacts.find` | `{ query, limit }` | `[Contact]` |

`Outgoing` con archivo, desde el servidor: o bien `fileUrl` (https; dConnector lo descarga a
`tmp/` con límite de tamaño de la capacidad, timeout 60 s) o `uploadId` de un `media.upload` previo.
`filePath` **nunca** se acepta por el protocolo. En `commands.go`, `data.message` se decodifica a
`struct{ model.Outgoing; FileURL string \`json:"fileUrl"\`; UploadID string \`json:"uploadId"\` }`, se
vacía `FilePath` y después se rellena con la ruta local de `tmp/` resultante.

`message.reply` es azúcar: busca el mensaje `replyTo` en la base y lo pone como `ReplyTo`.

Códigos de error: `unsupported`, `not_connected`, `not_found`, `invalid`, `rate_limited`,
`forbidden`, `internal`. Timeout de un comando: 120 s (`internal` «Tiempo agotado»).

Los comandos usan **los mismos servicios** que la interfaz (`Messaging`, `Inbox`, `Statuses`,
`Contacts`): mismas validaciones y capacidades.

## 6. Archivos

**Bajar** (`media.get`): dConnector asegura el archivo (`Messaging.MediaPath`, que descarga si hace
falta) y responde `result` con `{ "ok": true, "result": { "transferId":"…", "fileName", "mime", "size", "chunks": N } }`,
seguido de N frames:
```json
{ "v":1, "id":"…", "kind":"event", "name":"media.chunk",
  "data": { "transferId":"…", "seq": 0, "last": false, "b64":"…" } }
```
Trozos de 512 KiB (antes de base64). Los `media.chunk` **no** pasan por el outbox ni piden `ack`.
Máximo 100 MB por archivo por el protocolo.

**Subir** (`media.upload`): tras el `result` con `uploadId`, el servidor manda frames
`kind:"command", name:"media.chunk"` con `{ uploadId, seq, last, b64 }`; dConnector los escribe en
`tmp/<uploadId>` y al recibir `last` responde un único `result` a ese último trozo con `{ ok }`.
Un `uploadId` caduca a los 10 min.

## 7. Seguridad

- El token se guarda en `app.db` y nunca se escribe en el registro.
- Comandos apagados por defecto. Cada comando queda en el registro: destino, nombre, cuenta, ok/error
  (sin contenido).
- Los QR y códigos de vinculación nunca salen por el protocolo.

## 8. Servidor de ejemplo (`examples/echo-server/main.go`)

- `go run ./examples/echo-server -addr 127.0.0.1:8787 -token secreto`.
- Acepta WebSocket en `/` con el token; imprime `hello` y cada evento en una línea legible;
  responde `ack` a todo; a cada `message.received` con `fromMe == false` en chat `direct`, envía
  `message.reply` con `{ kind:"text", text:"Recibido ✅" }` e imprime el `result`.
- Usa `github.com/coder/websocket` y solo la biblioteca estándar.

## 9. Pruebas (`ws/*_test.go`)

Con `httptest.Server` + `coder/websocket`: saludo, filtro, outbox y reenvío tras cortar la
conexión (el servidor de prueba no da `ack` a la primera y sí tras reconectar: no se pierde ni se
duplica en su lado tras deduplicar), comandos con permiso y sin él, `media.get` por trozos y
`media.upload`, 401 sin reintento.
