# Protocolo dConnector v1

Este documento es para quien escribe un servidor (un bot, un almacén de mensajes, una IA) que se
conecta con dConnector. Con él, el servidor recibe los mensajes de todas las cuentas de dConnector
(WhatsApp, Telegram, Messenger, Instagram) y puede actuar por el canal correcto, **sin hablar con
cada red**.

- **dConnector es el cliente.** Tu servidor solo acepta una conexión WebSocket.
- Todo viaja en frames de texto JSON (UTF-8) con el mismo sobre.
- Los eventos llegan **al menos una vez**: tu servidor confirma con `ack` y debe deduplicar por `id`.
- Hay un servidor de ejemplo completo en `examples/echo-server/` (más abajo, §10).

## 1. Conexión

dConnector abre `GET <url>` por WebSocket, con estas cabeceras:

| Cabecera | Valor |
| --- | --- |
| `Authorization` | `Bearer <token>` (el token que dConnector generó para ese destino) |
| `X-DConnector-Id` | id de la instalación de dConnector (el mismo que `hello.data.instanceId`) |
| `X-DConnector-Protocol` | `1` |
| `User-Agent` | `dConnector/<versión>` |

- La dirección debe ser `wss://`. Solo se admite `ws://` con `localhost` o `127.0.0.1` (para probar en
  tu equipo).
- Si respondes **401 o 403** al abrir, dConnector marca el destino con el error «Token rechazado» y
  **no vuelve a intentarlo** hasta que el usuario edite el destino.
- Cualquier otro fallo (red caída, servidor apagado, cierre de la conexión) se reintenta con espera
  creciente: 1 s, 2, 4, 8 … hasta 60 s, con ±20 % de variación. La espera vuelve a 1 s cuando una
  conexión dura más de 10 s.
- **Pedir que vuelva más tarde (`Retry-After`):** si al abrir respondes con un código de error (por
  ejemplo `503`) y la cabecera `Retry-After` (segundos enteros o fecha HTTP), dConnector espera **como
  mínimo** ese tiempo antes de reintentar, **hasta un máximo de 1 hora** (si pides más, espera 1 h).
  Si la espera creciente de arriba es mayor, se usa esa. Un valor inválido o negativo se ignora. Sirve
  para que un destino apagado o en preparación no reciba intentos cada minuto; el usuario puede
  despertarlo en cualquier momento guardando el destino. Es un cambio compatible: las versiones
  anteriores de dConnector ignoran la cabecera y siguen con la espera creciente.
- **Ping:** dConnector manda un ping WebSocket cada 25 s. Si no recibe pong en 10 s, cierra y
  reconecta. Cualquier biblioteca WebSocket responde al ping sin código extra; solo tienes que estar
  leyendo la conexión.
- **Tamaño máximo** de un frame de texto: 1 MiB, en los dos sentidos. Configura el límite de lectura de
  tu biblioteca por encima de 1 MiB (muchas traen 32 KiB por defecto y cierran la conexión al pasarlo).

## 2. Sobre

Cada frame es **un** objeto JSON:

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

| Campo | Significado |
| --- | --- |
| `v` | versión del protocolo (`1`) |
| `id` | identificador único del frame (ULID). En un `event` es el id del evento y **es el mismo en los reenvíos**. En un `command` lo pones tú; el `result` lleva **el mismo** `id` |
| `kind` | tipo de frame |
| `name` | nombre del evento o del comando |
| `ts` | momento en milisegundos desde 1970 |
| `account` | en eventos, siempre; en comandos, tú indicas la cuenta con `account.id` |
| `data` | contenido |

Los campos que no conozcas **se ignoran** (así el protocolo puede crecer sin romper tu servidor).

## 3. Saludo

1. dConnector envía `hello`:

```json
{ "v": 1, "id": "…", "kind": "hello", "ts": 1762900000000, "data": {
  "instanceId": "01jc…", "version": "0.1.0", "protocol": 1,
  "accounts": [
    { "id": "wa-…", "channel": "whatsapp", "label": "Ventas", "color": "#25D366",
      "state": "connected", "selfId": "593999…@s.whatsapp.net", "selfName": "Ana", "selfHandle": "+593999…",
      "capabilities": { "sendText": true, "sendImage": true, "…": "…" } }
  ],
  "filter": { "accountIds": [], "channels": [], "events": [], "includeFromMe": false, "includeGroups": false,
              "contactScope": { "mode": "all", "includeListIds": [], "excludeListIds": [] } },
  "allowCommands": true
} }
```

Solo van las cuentas que pasan el filtro del destino. `state` es uno de `disconnected`,
`connecting`, `pairing`, `connected`, `logged_out`, `error`. `capabilities` dice qué admite cada cuenta
(los nombres son los de `capability.Set`; `editWindowMs` y `deleteWindowMs` están en milisegundos).

2. Tu servidor puede responder con `welcome` (es opcional):

```json
{ "v": 1, "id": "…", "kind": "welcome", "data": { "name": "Mi bot", "events": ["message.received"] } }
```

`events` (opcional) reduce aún más los eventos que quieres recibir: dConnector da por entregado lo que
no esté en la lista. **Si no mandas `welcome`, dConnector espera 10 s antes de empezar a enviar**, así que
conviene mandarlo enseguida. (`resumeFrom`, si lo mandas, se ignora en la versión 1.)

3. dConnector empieza a enviar el **outbox pendiente** en orden de creación y después los eventos nuevos.

## 4. Eventos (dConnector → servidor)

`kind: "event"`, con `name` uno de:

| `name` | `data` |
| --- | --- |
| `account.state` | estado de la cuenta (`accountId`, `state`, `need`, `detail`, `since`). **Nunca** lleva el QR ni el código de vinculación |
| `account.capabilities` | las capacidades de la cuenta (como en `hello`) |
| `chat.updated` | el chat (`key`, `accountId`, `channel`, `nativeId`, `kind`, `title`, `unreadCount`, `lastMessageAt`, …) |
| `message.received` | el mensaje: entrante, o saliente hecho desde otro dispositivo (`fromMe` lo distingue) |
| `message.sent` | mensaje saliente hecho desde dConnector (por la interfaz o por un comando). Si lo originó un comando con `clientRef`, `data.clientRef` lo devuelve |
| `message.updated` | `{ "update": MessageUpdate, "message": Message }`: editado, borrado, entrega, votos, medio descargado |
| `reaction` | `{ "messageKey", "reaction": { "senderNativeId", "senderName", "emoji", "at" } }` (`emoji` vacío = reacción quitada) |
| `receipt` | `{ "chatKey", "messageKeys", "delivery", "readerId", "at" }` |
| `person.updated` | la persona (`nativeId`, `name`, `pushName`, `phone`, `username`, …) |
| `status.posted` | un estado/historia |
| `status.viewed` | quién vio un estado: `{ "statusKey", "viewerId", "viewerName", "at", "reaction" }` |

Los mensajes (`Message`) traen, entre otros: `key`, `chatKey`, `accountId`, `channel`, `nativeId`,
`chatNativeId`, `senderNativeId`, `senderName`, `fromMe`, `timestamp` (ms), `kind` (`text`, `image`,
`video`, `audio`, `voice`, `document`, `sticker`, `location`, `contact`, `poll`, `system`,
`unsupported`), `text`, `media`, `location`, `contacts`, `poll`, `quotedNativeId`, `delivery`.

**Dos campos extra** en `message.received` y `message.sent`:

- `chatKind`: el tipo del chat (`direct`, `group`, `channel`, `self`), para que distingas grupos sin
  pedir el chat. Puede faltar si el chat aún no se conocía.
- `clientRef`: solo en `message.sent`, el que mandaste en el comando (§5).

En `message.media`, `url` **no sirve** fuera de dConnector: el archivo se pide con `media.get` (§6).
Las miniaturas (`thumb`, data URL) se quitan si el frame no cabe en 1 MiB.

### Qué no llega

No viajan nunca: `typing` («escribiendo…»), el historial que dConnector descarga al vincular
(`history.batch`), ni los trozos de archivo por el outbox.

### Filtro del destino

Quien administra dConnector define, por destino, qué te llega (el `filter` del `hello` te lo dice):

- `accountIds`: solo esas cuentas (vacío = todas). `channels`: solo esos canales (vacío = todos).
- `events`: solo esos nombres (vacío = todos).
- `includeFromMe = false` quita `message.received` con `fromMe`, todos los `message.sent` y los
  `message.updated` de mensajes propios. Con `includeFromMe = true` (en la app: «Incluir los míos» del
  destino) recibes también **todo lo que envía el usuario**, por las dos vías: `message.received` con
  `fromMe: true` (lo que escribe desde el teléfono u otro dispositivo, o lo que llega sincronizado) y
  `message.sent` (lo que escribe desde dConnector o lo que envía un comando tuyo, con su `clientRef`). Así el
  servidor ve los dos lados de la conversación. Si además usas `events`, lista ambos nombres
  (`message.received` y `message.sent`); un nombre que no esté en `events` no se envía.
- `includeGroups = false` quita todo lo de chats `group` y `channel`.
- `contactScope` (campo opcional: si falta, vale `{ "mode": "all" }`) limita **de qué contactos** te llega
  algo, según las listas de contactos de dConnector. Primero la inclusión y después la exclusión:
  - `mode`: `"all"` (todos los contactos) o `"lists"` (solo los contactos de las listas de
    `includeListIds`). `includeListIds` solo cuenta con `mode = "lists"`.
  - `excludeListIds`: los contactos de estas listas **nunca** pasan, con cualquiera de los dos modos.
    Ejemplo: `mode = "all"` y `excludeListIds = ["<id de Familia>"]` = todos menos la lista Familia.
  - Un evento pasa si (`mode = "all"` o el contacto está en alguna lista incluida) **y** el contacto no está
    en ninguna lista excluida.
  - Un chat se vincula a un contacto por la identidad de la otra persona del chat directo. Un chat sin
    contacto, un grupo, un canal y el chat contigo mismo **no tienen contacto**: pasan solo con
    `mode = "all"` (no están en ninguna lista excluida) y nunca con `mode = "lists"`.
  - `person.updated`, `status.posted` y `status.viewed` se evalúan por la persona (la autora del
    estado, o quien lo vio). `account.state` y `account.capabilities` no llevan datos de contactos y
    pasan siempre.
  - Los ids son los de las listas de dConnector (el servidor no los necesita para nada más que para
    saber qué filtro tiene aplicado). Una lista que ya no existe se ignora.
  - dConnector recuerda la pertenencia a listas unos 30 s: si el usuario cambia a alguien de lista, el
    cambio puede tardar ese tiempo en notarse. Lo que ya está en el outbox no se vuelve a filtrar.

### Entrega y `ack`

- dConnector guarda cada evento que pasa el filtro en un **outbox persistente** (por destino) *antes* de
  intentar enviarlo. Si apagas tu servidor, los eventos esperan y salen cuando vuelve a conectar.
- Confirmas con `ack`:

```json
{ "v": 1, "kind": "ack", "id": "…", "data": { "ids": ["<id del evento>", "…"] } }
```

- **Ventana:** como mucho 200 eventos enviados sin `ack`. Si no confirmas, dConnector deja de enviar
  hasta que lo hagas.
- Al reconectar, dConnector reenvía desde el más antiguo sin `ack`. Por eso el servidor **debe
  deduplicar por `id`** (y confirmar también los repetidos).
- Limpieza: el outbox borra lo que tenga más de 7 días y lo que pase de 50.000 eventos por destino (lo
  más antiguo primero), con aviso en el registro de dConnector.

## 5. Comandos (servidor → dConnector)

Solo funcionan si el destino tiene activado **«Permitir comandos»**. Si no, todo comando recibe un
`result` con `error.code = "forbidden"`. La cuenta de un comando debe pasar el filtro del destino
(si no, `forbidden`). Con `contactScope` pasa lo mismo con los chats: un comando sobre un chat que queda
fuera del alcance de contactos (incluido el `toChatId` de `message.forward`) recibe `forbidden`, y
`chats.list` y `contacts.find` solo devuelven lo que está dentro. `media.get` sigue el chat del
archivo.

```json
{ "v": 1, "id": "cmd-1", "kind": "command", "name": "message.send",
  "account": { "id": "wa-…" },
  "data": { "chatId": "593999…@s.whatsapp.net", "message": { "kind": "text", "text": "Hola" }, "clientRef": "abc" } }
```

Respuesta (mismo `id`):

```json
{ "v": 1, "id": "cmd-1", "kind": "result", "name": "message.send",
  "data": { "ok": true, "result": { "…": "el Message enviado" } } }

{ "v": 1, "id": "cmd-1", "kind": "result", "name": "message.send",
  "data": { "ok": false, "error": { "code": "unsupported", "message": "no admitido por este canal" } } }
```

| `name` | `data` | `result` |
| --- | --- | --- |
| `accounts.list` | `{}` | `[{ account, status, capabilities }]` (solo las cuentas del filtro; sin credenciales) |
| `chats.list` | `ChatFilter` (`tab`, `query`, `before`, `limit`, `includeEmpty`…); `accountIds` se ignora: se usa `account.id` si viene, si no todas las del filtro. Por defecto solo salen conversaciones con actividad: los chats directos sin ningún mensaje (contactos de la libreta que el canal lista como conversaciones vacías) se omiten, salvo los fijados; los grupos y canales salen siempre. `includeEmpty: true` (opcional, `false` por defecto) incluye también esos directos vacíos | `[Chat]` |
| `messages.list` | `{ chatId, beforeTs, limit }` | `{ messages, hasMore }` (de más antiguo a más reciente; `limit` máx. 200) |
| `message.send` | `{ chatId, message: Outgoing, clientRef }` | `Message` |
| `message.reply` | `{ chatId, replyTo: messageId, message: Outgoing, clientRef }` | `Message` |
| `message.react` | `{ chatId, messageId, emoji }` (`emoji` vacío = quitar) | `{}` |
| `message.edit` | `{ chatId, messageId, text }` | `{}` |
| `message.delete` | `{ chatId, messageId, forEveryone }` | `{}` |
| `message.forward` | `{ chatId, messageId, toChatId }` | `Message` |
| `poll.vote` | `{ chatId, messageId, options: [..] }` | `{}` |
| `chat.mark_read` | `{ chatId }` | `{}` |
| `chat.typing` | `{ chatId, typing }` | `{}` |
| `status.post` | `OutgoingStatus` (archivo por `fileUrl` o `uploadId`) | `Status` |
| `media.get` | `{ mediaId }` (con `account.id`) | ver §6 |
| `media.upload` | `{ fileName, mime, size }` | `{ uploadId }`, y luego frames `media.chunk` tuyos |
| `contacts.find` | `{ query, limit }` | `[Contact]` (solo con las identidades de las cuentas del filtro, y solo los contactos que pasan `contactScope`) |

**Identificadores.** `chatId` es el id nativo del chat (el `chatNativeId` de los eventos) y la cuenta va
en `account.id`. También se acepta la clave `"<cuenta>|<chatId>"` (el `chatKey` de los eventos); en ese
caso `account` puede omitirse. `messageId` y `replyTo` son ids nativos de mensaje (`nativeId`).

**`Outgoing`** (`data.message`): `kind` (`text`, `image`, `video`, `audio`, `voice`, `document`,
`sticker`, `location`, `contact`, `poll`), `text` (texto o pie de foto), `fileName`, `mime`, `location`,
`contact`, `poll`, `mentions`. Para los tipos con archivo, el archivo llega por **`fileUrl`** (una dirección
`https`; dConnector la descarga con un tope de tamaño según la cuenta y 60 s de espera) o por
**`uploadId`** (de un `media.upload` previo). **`filePath` nunca se acepta por el protocolo.**

`message.reply` busca el mensaje `replyTo` en la base de dConnector (si no existe: `not_found`) y lo
pone como cita.

**`clientRef`** es un texto tuyo para casar la respuesta: el `message.sent` que provoca el comando lo
devuelve en `data.clientRef`. (Los comandos con `clientRef` al mismo chat se atienden de uno en uno.)

### Errores

`result.data.error.code`:

| Código | Cuándo |
| --- | --- |
| `unsupported` | el canal o el chat no admiten la acción |
| `not_connected` | la cuenta no está conectada |
| `not_found` | no existe (cuenta, mensaje, medio…) |
| `invalid` | datos no válidos o incompletos, comando desconocido |
| `rate_limited` | límite de envío alcanzado, o demasiados comandos a la vez (máx. 16 en curso por conexión) |
| `forbidden` | destino sin «Permitir comandos», o cuenta o chat fuera del filtro del destino |
| `internal` | cualquier otro fallo, o «Tiempo agotado» (un comando tarda como máximo 120 s) |

`error.message` va en español y sin datos personales.

Los comandos usan **los mismos servicios** que la interfaz de dConnector: mismas validaciones y mismas
capacidades. Cada comando queda en el registro de dConnector (destino, nombre, cuenta, ok o error),
**nunca su contenido**.

Si una respuesta no cabe en 1 MiB (por ejemplo, `messages.list` con demasiados mensajes), recibes
`invalid` y debes pedir menos (`limit`).

## 6. Archivos

### Bajar: `media.get`

```json
{ "v": 1, "id": "cmd-2", "kind": "command", "name": "media.get", "account": { "id": "wa-…" },
  "data": { "mediaId": "<message.media.id>" } }
```

dConnector se asegura de tener el archivo (lo descarga del canal si hace falta) y responde:

```json
{ "v": 1, "id": "cmd-2", "kind": "result", "name": "media.get",
  "data": { "ok": true, "result": { "transferId": "…", "fileName": "informe.pdf", "mime": "application/pdf", "size": 1363148, "chunks": 3 } } }
```

Y después manda `chunks` frames:

```json
{ "v": 1, "id": "…", "kind": "event", "name": "media.chunk",
  "data": { "transferId": "…", "seq": 0, "last": false, "b64": "…" } }
```

- Trozos de 512 KiB (antes de base64), en orden, desde `seq` 0; el último lleva `last: true`.
- Los `media.chunk` **no pasan por el outbox ni piden `ack`**.
- Máximo 100 MB por archivo por el protocolo (más: `invalid`).

### Subir: `media.upload`

1. Mandas el comando `media.upload` con `{ fileName, mime, size }` (el tamaño exacto, máx. 100 MB).
   dConnector responde `{ "uploadId": "…" }`.
2. Mandas los trozos como **comandos** `media.chunk`, en orden:

```json
{ "v": 1, "id": "k0", "kind": "command", "name": "media.chunk",
  "data": { "uploadId": "…", "seq": 0, "last": false, "b64": "…" } }
```

   Trozos de hasta 512 KiB (antes de base64), `seq` desde 0, el último con `last: true`.
3. Solo el **último** trozo recibe `result` (`{ "ok": true }`). Los demás no reciben nada, salvo que algo
   falle: entonces ese trozo recibe `result` con el error y la subida se descarta (trozo fuera de orden,
   tamaño distinto del anunciado, base64 inválido, `uploadId` desconocido o caducado).
4. Con el `uploadId` ya puedes mandar `message.send` (o `status.post`) con `"uploadId"` en lugar de
   `fileUrl`. Un `uploadId` solo lo puede usar la conexión que lo creó y **caduca a los 10 minutos**.

## 7. Seguridad

- El token se guarda en dConnector y nunca se escribe en su registro. Úsalo para identificar a
  dConnector y compara en tiempo constante.
- Los comandos están **apagados por defecto**; el usuario los enciende destino por destino.
- Los QR y los códigos de vinculación **nunca** salen por el protocolo, y la configuración de las
  cuentas (credenciales) tampoco.
- Usa siempre `wss://` fuera de tu equipo.

## 8. Lo que tiene que hacer un servidor

1. Aceptar WebSocket, comprobar `Authorization: Bearer <token>` (si no, 401).
2. Subir el límite de lectura por encima de 1 MiB.
3. Leer el `hello`; contestar `welcome` (aunque sea `{}`) para no esperar 10 s.
4. Por cada `event` (salvo `media.chunk`): guardar o procesar **y** mandar `ack` con su `id`.
5. Deduplicar por `id` (los reenvíos llevan el mismo).
6. Para actuar, mandar `command` con un `id` propio y esperar el `result` con ese `id`.

## 9. Compatibilidad

La versión del protocolo es `1` (`X-DConnector-Protocol` y `hello.data.protocol`). Los campos nuevos se
añaden sin cambiar la versión: ignora lo que no conozcas (por ejemplo, `filter.contactScope` del `hello`
se añadió así). Un cambio incompatible subiría la versión.

## 10. Servidor de ejemplo

`examples/echo-server/main.go` (usa `github.com/coder/websocket` y la biblioteca estándar):

```
go run ./examples/echo-server -addr 127.0.0.1:8787 -token secreto
```

- Acepta WebSocket en `/` con el token; imprime el `hello` y cada evento en una línea legible.
- Responde `welcome` y `ack` a todo, y deduplica por `id`.
- A cada `message.received` con `fromMe == false` en un chat `direct`, envía `message.reply` con
  `{ "kind": "text", "text": "Recibido ✅" }` e imprime el `result`. Para eso, en dConnector crea un
  destino con la dirección `ws://127.0.0.1:8787/` y el token `secreto`, y activa «Permitir comandos».
