# 14 · API del servidor (v1)

Contrato entre `dconnector-server` y sus clientes (la app de escritorio y cualquier otro). Los agentes de la
oleada 1 se apoyan **solo** en este documento. Todo cambio es **aditivo** dentro de `/api/v1`.

Convenciones: JSON en UTF-8; fechas y horas como en el modelo (`int64` en milisegundos Unix); nombres de
campos exactamente como los serializa hoy `core/model` y `core/service` (los mismos tipos que ven los enlaces
de Wails: `frontend/src/api/types.ts` es la referencia); mensajes de error en español.

## 1. Visión general

| Ruta | Método | Auth | Uso |
| --- | --- | --- | --- |
| `/api/v1/health` | GET | no | estado y versión |
| `/api/v1/auth/login` | POST | no | usuario + clave → token |
| `/api/v1/auth/logout` | POST | sí | invalida el token |
| `/api/v1/auth/me` | GET | sí | usuario actual |
| `/api/v1/auth/password` | POST | sí | cambiar la clave propia |
| `/api/v1/auth/ws-ticket` | POST | sí | ticket de un solo uso para el WebSocket |
| `/api/v1/auth/media-token` | POST | sí | token de solo lectura de medios (para `?token=`) |
| `/api/v1/rpc/{servicio}/{método}` | POST | sí | llamada a un método del núcleo |
| `/api/v1/events` | GET (WebSocket) | ticket | eventos en vivo |
| `/api/v1/files` | POST | sí | subir un archivo (multipart) |
| `/api/v1/files/{id}` | GET | sí / media-token | descargar un archivo subido |
| `/api/v1/media/{accountId}/{mediaId}` | GET | sí / media-token | medio de un chat (descarga si falta; `Range`) |
| `/api/v1/avatar/{accountId}/{archivo}` | GET | sí / media-token | avatar guardado |

El prefijo `/api/v1` es la versión de la API. Un cambio incompatible llevaría `/api/v2` y el servidor serviría
ambas durante una versión. El cliente manda `X-DConnector-Client: <nombre>/<versión>` (informativo); el servidor
ignora campos desconocidos y el cliente también.

## 2. Errores

Toda respuesta de error es JSON con el código HTTP indicado:

```json
{"error": {"code": "invalid", "message": "El número no es válido."}}
```

| `code` | HTTP | Cuándo |
| --- | --- | --- |
| `invalid` | 400 | argumentos mal formados o inválidos (`port.ErrInvalid`) |
| `unauthorized` | 401 | falta el token, caducó o no es válido; usuario o clave erróneos |
| `forbidden` | 403 | token sin permiso para esa ruta (p. ej. media-token en `/rpc`), origen no permitido |
| `not_found` | 404 | no existe el recurso o el método (`port.ErrNotFound`) |
| `not_connected` | 409 | la cuenta no está conectada (`port.ErrNotConnected`) |
| `too_large` | 413 | cuerpo o archivo por encima del límite |
| `unsupported` | 422 | el canal no admite la operación (`port.ErrUnsupported`) |
| `rate_limited` | 429 | demasiados intentos o peticiones; lleva la cabecera `Retry-After` (segundos) (`port.ErrRateLimited`) |
| `internal` | 500 | cualquier otro error; el mensaje es genérico, el detalle va al registro del servidor |
| `unavailable` | 503 | servidor arrancando o apagándose |
| `not_available` | 404 | método solo del escritorio (ver §4.3) |

`message` es siempre un texto en español, accionable y **apto para mostrar tal cual**. Nunca contiene claves,
tokens, rutas del servidor ni trazas. Un error de un servicio del núcleo conserva su mensaje (ya está en español).

## 3. Autenticación

- **Usuarios**: tabla propia en `app.db` (`users`: id, username, password_hash, role, created_at, disabled_at).
  Claves con **argon2id** (m=64 MiB, t=3, p=2, salt de 16 bytes, hash de 32 bytes; parámetros dentro de la cadena
  PHC para poder subirlos). Mínimo 12 caracteres. `role`: `admin` (el único usado en v1; el campo queda para el futuro).
- **Primer arranque** (sin usuarios): se crea el administrador (`--admin-user`, por defecto `admin`) con una clave
  aleatoria de 24 caracteres que se imprime **una sola vez** en la salida estándar. Si existe
  `DCONNECTOR_ADMIN_PASSWORD` se usa esa clave y no se imprime nada. Nunca se escribe en el archivo de registro.
- **Login**: `POST /api/v1/auth/login` con `{"username":"…","password":"…"}` →
  `{"token":"…","expiresAt":1760000000000,"user":{"id":"…","username":"admin","role":"admin"}}`.
  El token es opaco (32 bytes aleatorios en base64url); el servidor guarda solo su SHA-256. Caduca a las
  **24 horas** de inactividad (ventana deslizante) y, como máximo, a los **30 días**. Un cambio de clave o
  `logout` invalida todos los tokens del usuario.
- **Uso**: cabecera `Authorization: Bearer <token>`. El `?token=` en la URL se admite **solo** en
  `/api/v1/media/…`, `/api/v1/avatar/…` y `/api/v1/files/{id}` (GET), y solo con un **media-token**.
- **media-token**: `POST /api/v1/auth/media-token` → `{"token":"…","expiresAt":…}`; vale 15 minutos, solo
  lectura de esas tres rutas (en cualquier otra da `forbidden`). El cliente lo renueva antes de que caduque.
- **ticket del WebSocket**: `POST /api/v1/auth/ws-ticket` → `{"ticket":"…","expiresAt":…}`; un solo uso y
  30 segundos. Se pasa como `?ticket=` al abrir `/api/v1/events`. Así el token de sesión nunca va en una URL.
- **Límite de intentos**: 5 fallos de login por IP en 15 minutos y 10 por usuario en 15 minutos; al superarlo,
  `429 rate_limited` con `Retry-After`, y el bloqueo se duplica con cada reincidencia (tope 1 hora). Los éxitos
  no cuentan. El mensaje de login fallido es el mismo exista o no el usuario (`Usuario o clave incorrectos.`) y el
  tiempo de respuesta se iguala. Límite general: 3000 peticiones por minuto por token de sesión remoto (el token local del escritorio no se limita; al alcanzarlo, el servidor registra las rutas más llamadas). Un `429` se produce **antes** de ejecutar la llamada: repetirla es siempre seguro.
- **Modo local**: el escritorio lanza el servidor con `DCONNECTOR_LOCAL_TOKEN=<aleatorio>` (por entorno, nunca por
  argumento). Ese valor se acepta como `Bearer` con el usuario `local` (rol `admin`), **solo si la conexión llega por
  loopback** y el servidor escucha en loopback. No crea usuarios. Sirve también para media y ticket.
- **CLI**: `dconnector-server user add <nombre>`, `user passwd <nombre>`, `user list`, `user disable <nombre>`;
  la clave se pide por entrada estándar sin eco (o `--password-stdin`). Operan sobre la misma carpeta de datos.
- **Registro**: nunca se registran claves, tokens, tickets ni cadenas de consulta (`?token=`, `?ticket=`).

## 4. RPC

### 4.1 Formato

`POST /api/v1/rpc/{servicio}/{método}`, `Content-Type: application/json`.

- **Cuerpo**: un array JSON con los argumentos en el orden de la firma Go, **sin** el `context.Context`.
  Sin argumentos: `[]`. Un argumento struct es un objeto con las etiquetas JSON que ya tiene.
- **Respuesta 200**: el valor de retorno serializado (`application/json`); métodos sin valor devuelven `null`.
  Una lista vacía puede llegar como `null` (igual que en Wails); el cliente la convierte a `[]`.
  Métodos con tupla `(T, error)` devuelven `T`. Los que devuelven varios valores (`(a, b, error)`) los devuelven
  como array en orden.
- **Error**: ver §2. Un error de Go no `nil` nunca da 200.
- Límite de cuerpo: **1 MiB** (los archivos van por `/files`, §6). Método distinto de POST: `405`.
- Servicio o método inexistente: `404 not_found`. Número de argumentos o tipos incorrectos: `400 invalid`.
- Cada llamada usa el contexto de la petición (se cancela si el cliente se desconecta).
- El catálogo es el de las fachadas `internal/adapter/ui/wails` (los mismos nombres de servicio y de método que
  los enlaces actuales; ver `frontend/bindings/`). La API se genera **por reflexión sobre esas fachadas movidas
  a un paquete neutro** (`internal/adapter/api/facade`), que no importa Wails.

### 4.2 Servicios

`Accounts`, `Inbox`, `Messaging`, `Statuses`, `Contacts`, `Campaigns`, `Scheduled`, `Stats`, `Destinations`,
`Settings`, `History`, `Notifications`. El nombre del servicio en la URL es el del tipo Go sin el sufijo
`Service`, con la primera letra tal cual (`/rpc/Messaging/Send`).

### 4.3 Métodos que cambian en el servidor

Son los que dependen de la máquina del usuario. El servidor **no** los expone por RPC (`404 not_available`) y
el cliente los resuelve por su cuenta:

| Método de Wails | En el servidor | Qué hace el cliente |
| --- | --- | --- |
| `Files.PickFiles` | no existe | diálogo del cliente + subida a `/files` (modo remoto) |
| `Files.SaveTemp` | no existe | subida a `/files` (§6) |
| `Files.Info` | no existe | la subida devuelve la ficha |
| `Messaging.SaveMediaAs` | no existe | descarga `/media/…` y guarda con el diálogo del cliente |
| `Settings.OpenDataDir`, `Settings.OpenLogsDir` | no existe | solo en modo local, el escritorio abre la carpeta |
| `Notifications.Status`, `RequestPermission`, `SendTest`, `OpenSystemSettings`, `MarkAsked` | no existe | los resuelve el cliente de Windows |

`Notifications.SetFocus(chatKey, focused)` **sí** existe: informa de qué chat tiene abierto **esa conexión** y si la
ventana tiene el foco; la decisión de avisar usa el estado de todas las conexiones del usuario (se avisa si
ninguna tiene ese chat abierto y enfocado).

`Settings.DataDir()` devuelve la carpeta de datos del **servidor** (informativo). `Settings.Version()` la versión del servidor.

### 4.4 Argumentos de archivo

Donde un método hoy recibe una **ruta** local (adjuntos de `Messaging.Send`, medios de campañas, programados y
estados, `Contacts.ImportCSV`, etc.), el cliente remoto manda en su lugar la cadena **`upload://<id>`** con el id
que devolvió `/files`. El servidor la resuelve a un archivo bajo `<data>/uploads/` y rechaza cualquier otra ruta
cuando la petición viene de un cliente **remoto**. En modo local (token propio) se siguen aceptando rutas absolutas.

## 5. Eventos (WebSocket)

`GET /api/v1/events?ticket=<ticket>` con `Upgrade: websocket`. Sin ticket válido: `401` antes del upgrade.
Verificación de `Origin`: se admite sin cabecera `Origin` (clientes nativos), el mismo host, `wails.localhost`
y los orígenes de `--allow-origin`; otro origen → `403 forbidden`. Máximo 8 conexiones por usuario.

Cada mensaje del servidor es un marco de texto JSON:

```json
{"event":"chat:upsert","seq":1042,"data":[ … ]}
```

- `seq`: contador creciente por conexión. **No hay reenvío**: tras una reconexión el cliente vuelve a cargar su
  estado por RPC (el servidor es la fuente de verdad).
- Primer mensaje al conectar: `{"event":"hello","seq":0,"data":{"version":"0.3.0","apiVersion":1,"protocol":1,"serverTime":…,"user":"admin","connId":"…"}}`. El cliente manda `X-DConnector-Conn: <connId>` en sus llamadas RPC para que el servidor sepa de qué conexión vienen (lo usa `Notifications.SetFocus`).
- El servidor envía un *ping* de WebSocket cada **25 s** y cierra si no hay *pong* en 60 s. El cliente no envía
  datos (si lo hace, se ignora). Cierre por apagado: código `1001`; por token revocado: `4401`.
- Reconexión del cliente: espera 1, 2, 4, 8, 16 y 30 s (tope) con ±20 % de azar; en cada intento pide un
  ticket nuevo; si el ticket da `401`, vuelve a iniciar sesión (o pide credenciales).
- Los eventos agrupados (`chat:upsert`, `message:upsert`) mantienen el lote de 100 ms del puente actual;
  `unread:total` sale como máximo una vez por segundo.

### 5.1 Catálogo

Los **mismos nombres y cargas que emite hoy Wails** (`internal/adapter/ui/wails/events.go`; tipos en
`frontend/src/api/types.ts`, `EventPayloads`):

| Evento | Carga |
| --- | --- |
| `account:status` | `AccountStatus` |
| `account:caps` | `{accountId, capabilities}` |
| `chat:upsert` | `Chat[]` |
| `chat:removed` | `{chatKey}` |
| `message:upsert` | `Message[]` |
| `message:update` | `Message` |
| `receipt` | `ReceiptEvent` |
| `typing` | `TypingEvent` |
| `person:upsert` | `Person` |
| `status:upsert` | `Status` |
| `status:view` | `StatusView` |
| `history:progress` | `{accountId, progress}` |
| `history:sync` | `HistoryStatus` |
| `campaign:progress` | `CampaignProgressEvent` |
| `destination:state` | `DestinationState` |
| `unread:total` | `{total, byAccount}` |

Eventos **nuevos** (aditivos):

| Evento | Carga | Uso |
| --- | --- | --- |
| `hello` | ver arriba | primer mensaje |
| `notify` | `{chatKey, accountId, channel, title, body, preview, silent, sound, reason}` | el servidor ya decidió (silenciado, visto, enfocado…) que **hay que avisar**; el cliente solo muestra el aviso, suena y, al hacer clic, abre el chat. `silent=true` y `sound=true` significan «solo sonido, sin ventana» (permiso de aviso bloqueado) |
| `notify:decision` | `{chatKey, shown, reason}` | por qué un mensaje no avisó (alimenta «Última decisión» de Ajustes) |

`ui:open-chat` deja de ser del servidor: lo genera el cliente al hacer clic en un aviso.

## 6. Archivos subidos

`POST /api/v1/files`, `multipart/form-data` con un campo `file` (nombre y tipo del archivo). Respuesta:

```json
{"id":"01J…","name":"foto.jpg","mime":"image/jpeg","size":123456}
```

- Se guarda en `<data>/uploads/<id>` (el nombre original no se usa como ruta: solo se guarda en la ficha).
- Límites: **100 MiB** por archivo, **1 GiB** de uploads en total por usuario; superarlo da `413 too_large`.
- `mime` se detecta por contenido y extensión (nunca se confía solo en el del cliente).
- Se borran a las **24 horas** de su subida (o al arrancar), salvo los que un envío ya copió a su sitio definitivo.
- `GET /api/v1/files/{id}` devuelve el contenido (con `Content-Type` detectado y las cabeceras de §8).
- `id` es un ULID; cualquier id con otro formato da `400 invalid`. Sin recorridos de ruta posibles.

## 7. Medios

- `GET /api/v1/media/{accountId}/{mediaId}`: igual que el `/media/…` actual del manejador de Wails (descarga si falta,
  `Range`, `Content-Type`). `GET /api/v1/avatar/{accountId}/{archivo}` igual que `/avatar/…`.
- Los segmentos se validan (`safeSegment`): sin `..`, `/`, `\` ni NUL; fuera de la carpeta de la cuenta → `400 invalid`.
- Las URL dentro de los modelos (`Media.URL`, `avatarUrl`) siguen siendo **relativas** (`/media/<cuenta>/<id>`);
  el cliente las convierte en `{base}/api/v1/media/<cuenta>/<id>?token=<media-token>`.
- `/local?path=` (vista previa de archivos locales) no existe en el servidor: las vistas previas de subidas
  usan `GET /api/v1/files/{id}`.

## 8. Cabeceras, límites y TLS

- Toda respuesta: `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `Cache-Control: no-store`
  (medios: `private, max-age=3600`); medios y archivos además `Content-Security-Policy: sandbox`;
  con TLS: `Strict-Transport-Security: max-age=31536000`.
- CORS: solo para los orígenes permitidos de §5 (el escritorio carga la interfaz desde `wails.localhost`);
  `Access-Control-Allow-Headers: Authorization, Content-Type, X-DConnector-Client`; las respuestas a `OPTIONS` no requieren token.
- Tiempos: cabeceras 10 s, cuerpo de RPC 30 s, subidas sin tope de duración pero con progreso mínimo (60 s sin datos corta).
- **TLS**: por defecto el servidor sirve HTTPS (TLS 1.2 como mínimo, 1.3 preferido).
  - Sin `--tls-cert`: genera un certificado propio en `<data>/tls/` (ECDSA P-256, 1 año, SAN `localhost`, `127.0.0.1`,
    `::1` y los nombres e IP de la máquina), lo renueva 30 días antes y **imprime su huella SHA-256** al arrancar.
    El escritorio la fija en la primera conexión (confianza al primer uso) y avisa si cambia.
  - Con `--tls-cert/--tls-key`: certificado externo (PEM), recargado al recibir `SIGHUP` (Linux) o al reiniciar.
  - **Obligatorio fuera de loopback**: si `--listen` no es loopback y no hay TLS, el servidor se niega a arrancar
    con un error en español, salvo `--insecure` (aviso ruidoso cada minuto en el registro). `--no-tls` solo se
    acepta en loopback o junto a `--insecure` (detrás de un proxy inverso que ya pone TLS).
- **Modo local**: `--listen 127.0.0.1:0 --no-tls` con `DCONNECTOR_LOCAL_TOKEN`. Al estar listo, el servidor escribe
  **una línea JSON** en la salida estándar: `{"event":"listening","addr":"127.0.0.1:51234","tls":false,"version":"0.3.0","pid":1234}`.
  Es lo único que el escritorio lee de esa salida.
- **Apagado ordenado**: `SIGINT`/`SIGTERM` (o parada del servicio de Windows) → deja de aceptar, cierra los
  WebSocket con `1001`, apaga núcleo y cuentas (máximo 10 s) y sale con 0.

## 9. `GET /api/v1/health`

Sin autenticación, sin datos sensibles:

```json
{"status":"ok","version":"0.3.0","apiVersion":1,"protocol":1,"tls":true}
```

`status` es `starting` (503) mientras el núcleo arranca, `ok` (200) listo, `stopping` (503) al apagar.
El cliente la usa para «Probar conexión» (junto con el login) y para detectar un servidor más nuevo o más viejo
(`apiVersion` distinto → aviso «Este servidor usa otra versión de la API»).

## 10. Compatibilidad de datos

El servidor usa la carpeta de datos de 0.2.x tal cual (`%APPDATA%\dConnector` en Windows). Las tablas nuevas
(`users`, sesiones, huellas de uploads) se añaden con **migraciones aditivas** de `app.db`; nunca se tocan las
existentes. Abrir una carpeta 0.3.0 con 0.2.x debe seguir funcionando (las tablas nuevas se ignoran).

## 11. Pruebas exigidas

- **Contrato** (agente A): cada método del catálogo por HTTP real contra el adaptador falso (`core/fake`);
  errores y códigos de §2; subida y descarga; eventos de §5 con `seq` y `hello`.
- **Seguridad** (agente B): fuerza bruta y bloqueo; token caducado, manipulado, de otro usuario, tras `logout` y
  tras cambio de clave; media-token fuera de su alcance; ticket reutilizado y caducado; sin sesión en todas las
  rutas; recorrido de rutas en `/files`, `/media`, `/avatar` y `upload://`; origen del WebSocket; cabeceras; que
  ningún registro contenga claves, tokens ni consultas.
