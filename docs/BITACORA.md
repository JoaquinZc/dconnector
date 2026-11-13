# Bitácora de construcción · dConnector v2

Registro del orquestador. Las secciones de cada agente viven en `docs/bitacora/<oleada>-<agente>.md`
(ignoradas por git) y se resumen aquí al cerrar cada oleada.

## Oleada 0 · Cimientos (11-11-2026)

### Herramientas instaladas (versiones exactas)

| Herramienta | Versión | Cómo |
| --- | --- | --- |
| Go | 1.27.4 windows/amd64 | `winget install GoLang.Go` |
| .NET SDK | 8.0.428 | `winget install Microsoft.DotNet.SDK.8` |
| Wails CLI (`wails3`) y librería | v3.1.0 (estable; existe desde 01-11-2026) | `go install …/wails3@v3.1.0` |
| Task | 3.54.0 | `go install github.com/go-task/task/v3/cmd/task@latest` |
| WiX | 5.0.2 + `WixToolset.UI.wixext 5.0.2` | `dotnet tool install --global wix --version 5.0.2` |
| Node / pnpm | 22.21.1 / 12.11.2 | ya instalados |
| TypeScript | 7.0.2 (compilador nativo) | pnpm |

Rutas del PATH que hay que sumar en una consola nueva: `%USERPROFILE%\go\bin` y `%USERPROFILE%\.dotnet\tools`
(Go y .NET se añaden solos al PATH de la máquina).

### Dependencias de Go

`go.mod` fija: wails v3.1.0, modernc.org/sqlite v1.62.0, whatsmeow (rama principal, 10-11-2026), gotd/td v0.165.0,
gotd/contrib v0.26.0, coder/websocket, oklog/ulid/v2 v2.1.2, testify v1.12.1, x/image, x/sys, zap, protobuf,
**go.mau.fi/mautrix-meta v0.2610.0**.

`internal/deps/deps.go` (etiqueta `tools`) mantiene en `go.mod` las dependencias que aún nadie importa, para que
`go mod tidy` no las quite mientras los agentes escriben los adaptadores. **Se borra en el cierre.**

### Regla de importación de Meta (09 §1) · resultado

- `go.mau.fi/mautrix-meta@v0.2610.0`, paquete `pkg/messagix`: se importa y compila **sin** `pkg/connector` ni el
  puente de Matrix (arrastra solo bibliotecas `maunium.net/go/mautrix/{id,crypto,bridgev2/networkid…}` como utilidades).
  → librería de **Messenger**.
- **Instagram**: la v0.2610.0 trae además `pkg/instameow` (cliente MQTT de Instagram: `login_web*`, `login_mobile`,
  `login_caa`, `socket`, `streamcontroller`…) y `pkg/igconnector`. `instameow` es importable y compila → opción **2b**
  (paquete importable bajo `pkg/` del mismo módulo). No hace falta `third_party/`.
  Los agentes de Meta (C3) deben decidir con `go doc` si `messagix` aún sirve Instagram (`types.Instagram` existe) o si
  hay que usar `instameow`; el documento 09 manda en lo demás.
- **Hallazgo importante**: mautrix-meta lleva un `replace github.com/imroc/req/v3 => github.com/beeper/req/v3 …` en su
  `go.mod`. Los `replace` de dependencias no se heredan: **se copió a nuestro `go.mod`** (sin él, `messagix` no compila).
- E2EE de Messenger (09 §1.3): se evalúa en la oleada 3 (C3).

### Desviaciones del plan

1. `wails3 init -t react-ts` no existe en v3.1.0: la plantilla se llama `react` (React + TypeScript + Vite). Se usó `-t react`.
2. `wails3 generate syso` se llama desde la raíz del proyecto con rutas `build/windows/…` y el `.syso` se borra tras
   compilar (tarea `syso` del `Taskfile.yml`).
3. TypeScript 7 (compilador nativo) **no admite `baseUrl`**: el alias `@/` se declara con `paths: {"@/*": ["./src/*"]}`
   y con `resolve.alias` en Vite.
4. El plugin de Vite de Wails (`@wailsio/runtime/plugins/vite`) exige que exista `frontend/bindings/`: siempre hay que
   correr `wails3 generate bindings -ts` **antes** de `vite build` / `vitest` (la tarea `frontend` ya depende de `bindings`).
5. `frontend/bindings/` se versiona en git (los agentes de interfaz lo necesitan desde el principio).
6. `service/types.go`: `OutboxWriter` e `IncomingObserver` van como en 06. `Router.Attach` del esqueleto devuelve un
   sink que descarta eventos (nunca `panic`).
7. `ui/wails/*_svc.go` quedaron **completos desde la oleada 0** (son fachadas finas de una línea): solo faltan
   `events.go`, `media_handler.go`, `tray.go` y `autostart.go`, que se escriben en la oleada 1.
8. `ContactsService.List` devuelve `ContactPage{Contacts, Total}` (Wails no enlaza tuplas); el resto de firmas igual que 11 §1.
9. `.npmrc` de la plantilla (`minimum-release-age`) **no** se copió: bloquearía versiones recientes de las librerías.
10. `AccountsService.Capabilities(ctx, chatKey)` acepta una clave de chat (`cuenta|chat`) o solo el id de la cuenta.

### Estado al cerrar la oleada

Puerta en verde (`task gate`): `go vet ./...`, `go test ./...`, `tsc --noEmit`, `vitest run`, `wails3 generate bindings -ts`
(11 servicios, 87 métodos), `task build` (`bin/dConnector.exe`, 11 MB), `--version` = `0.1.0` y `--selftest` = OK (parcial:
el almacén SQLite llega en la oleada 1). La ventana abre sin errores (WebView2 creado).

Tareas disponibles: `task bindings | frontend | build | version | test | selftest | gate`. Las ejecuta una consola con
`%USERPROFILE%\go\bin` y `%USERPROFILE%\.dotnet\tools` en el PATH.

## Oleada 1 · Núcleo, almacén, WhatsApp e interfaz base (11-11-2026)

Agentes: A1 almacén SQLite, A2 núcleo, A3 WhatsApp, A4 interfaz base + Cuentas, A5 Chats. Sus bitácoras completas están en
`docs/bitacora/1-A*.md` (no versionadas). Resumen de lo que afecta a las demás oleadas:

**Almacén (A1).** API pública: `sqlite.Open`, `OpenApp`, `OpenAccount`. FTS5 indexado por `rowid` (no hacer `VACUUM` sobre
`account.db`). `SearchMessages` sanea la consulta. `OutboxPending` suma 1 a `attempts` en cada lectura. `OutboxTrim` deja como
máximo `maxRows` por destino. `CampaignTotals` sale de la cola (`Replied` y `OptOut` los calcula el servicio). Método extra
`AppStore.ReorderAccounts` (fuera del puerto, sin usar: el orden vive en el ajuste `account_order`).

**Núcleo (A2).** Claves de ajustes: `close_to_tray`, `start_with_windows`, `start_minimized`, `notifications`, `theme`,
`verbose_log`, `auto_download_media` (JSON), `optout_keywords`, `optout_footer_text`, `instance_id`, `account_order`,
`pacing:<id>`, `warmup_started_at:<id>`. `theme`, `optout_*` van como texto crudo. El Router tiene un `isOptOut` provisional:
**en la oleada 3 hay que sustituirlo por `campaign.IsOptOut`** (lo hace el orquestador tras C1). El Router publica un
`chat.updated` derivado tras cada mensaje nuevo (la interfaz lo necesita para vista previa y no leídos). `Router.Publish` y
`Messaging.Close` son API extra. `ValidateDestinationURL` y las constantes `Setting*` son públicas.

**WhatsApp (A3).** Dialecto de whatsmeow `"sqlite"`; tablas propias `wa_kv`, `wa_chats`, `wa_msgs`, `wa_polls` en el mismo
`account.db`. `ErrUnsupported`: `SubmitPairing`, `ReactStatus`, `StatusViewers` (las vistas llegan como eventos), todo lo
interactivo en canales `@newsletter`, reenviar encuestas. `StatusAudience=false`: esta versión de whatsmeow no deja elegir la
audiencia de un estado (se usa la privacidad del teléfono). Citar y reenviar usan una caché de los últimos 3.000 mensajes vistos.

**Interfaz (A4, A5).** Contrato `api/`, `store/`, `lib/` en `docs/bitacora/1-A4.md`. `startApp()` inicia los stores (nadie más
llama a `init()`). Los enums de los enlaces no aceptan cadenas sueltas: usar `ChannelId`/`PairNeedId` de `api/types.ts`. B4 debe
llamar a `useUI.getState().setTheme(...)` además de `SettingsService.Save`. A5 usó piezas locales en `components/chat/parts.tsx`
en vez de `components/common/*` (unificar es opcional).

### Integración del orquestador (oleada 1)

- `ui/wails`: `events.go` (puente bus → Wails con lotes de 100 ms y `unread:total` ≤ 1/s, probado), `media_handler.go`
  (`/media`, `/avatar`, `/local` solo dentro de `<data>/tmp`, con `Range`, probado), `tray.go` (bandeja, cerrar a bandeja,
  notificaciones, salir) y `autostart_windows.go` (HKCU\…\Run con `--minimized`).
- `main.go`: almacén SQLite real, `EnsureDefaults`, ajustes aplicados al guardar (arranque con Windows y registro detallado),
  servicio de notificaciones registrado, apagado ordenado (cuentas, `Messaging.Close`). `--selftest` completo: abre `app.db`
  y `account.db`, escribe y lee un chat, y borra la cuenta falsa.
- **Convención nueva:** `Chat.UnreadCount < 0` significa «sin cambio». El almacén conserva el contador (y guarda 0 en un chat
  nuevo); `chatShell` de WhatsApp lo usa en los avisos parciales y manda `0` explícito al marcar leído desde el teléfono. El
  Router ahora publica y encola el chat **completo** tal como quedó guardado, no el parcial del adaptador.
- `UpsertStatus` con `DeletedAt > 0` y sin `PostedAt` fusiona (solo marca el borrado) en vez de reemplazar la fila.
- Dependencia añadida: `git.sr.ht/~jackmordaunt/go-toast/v2` (la trae el servicio de notificaciones de Wails).

### Pendiente / límites conocidos tras la oleada 1

- `go test -race` **no se puede correr en este equipo**: exige CGO y no hay `gcc`. A1 y A2 revisaron las carreras a mano.
  Para D2 (oleada 4) hay que instalar un compilador C (`winget install BrechtSanders.WinLibs.POSIX.UCRT`) o aceptar la excepción.
- La interfaz no se ha visto funcionando con cuentas reales ni revisado visualmente (claro/oscuro): es parte de `QA-EN-VIVO.md`.
- `pages/registry.tsx` solo enlaza Chats y Cuentas; el resto se enlaza al cerrar las oleadas 2 y 3.

## Oleada 2 · Telegram, protocolo, estados (11-11-2026)

Agentes: B1 Telegram, B2 protocolo WebSocket v1, B3 estados + estadísticas de estados, B4 interfaz de Estados, Destinos y Ajustes.
Bitácoras completas en `docs/bitacora/2-B*.md`.

**Telegram (B1).** gotd/td v0.165.0 con su propio `gotd/log` (adaptado a `slog`, solo Warn y Error). La tabla `kv` de `account.db` se
usa por SQL con claves `tg.*`; el adaptador lee en solo lectura la tabla `messages` del almacén (citas, borrados, historial):
si cambian esas columnas hay que ajustar `local.go` e `history.go`. `ErrUnsupported`: `PostStatus` con estado de texto, o si
`StoriesCanSendStory` da error; `Send` de `system`/`unsupported`. Floodwait: espera hasta 20 s y 3 reintentos, luego `ErrRateLimited`.
Limitaciones: si otra persona quita su reacción no hay forma de saberlo; en encuestas anónimas `Voters` lleva marcadores `"anon"`.
Las claves `api_id`/`api_hash` se leen al crear el adaptador: si el usuario las cambia hay que recrearlo.

**Protocolo (B2).** API: `ws.NewManager(dst, ws.Services{Accounts,Inbox,Messaging,Statuses,Contacts}, bus, app, log)` con
`Start/Stop/States/Test/Enqueue/OnState`. `Enqueue` implementa `service.OutboxWriter` y solo toca SQLite. Añadió `chatKind` a los
datos de `message.received/sent`. `clientRef` se resuelve con un «ticket» por chat (espera máx. 3 s) porque el núcleo no lo copia del
`Outgoing` al `Message`. `welcome.resumeFrom` se ignora (10 no dice qué hace). `accounts.list` quita `Config` (lleva `api_hash`) y el QR;
`chats.list` oculta grupos si el destino no tiene `includeGroups`. Una cuenta fuera del filtro devuelve `forbidden`; máx. 16 comandos
simultáneos por conexión. `docs/PROTOCOLO-v1.md` es la copia pública. Servidor de ejemplo en `examples/echo-server`.

**Estados (B3).** `Statuses` completo con `Start(ctx)`/`Stop()` (refresco de vistas cada 10 min, primera pasada al minuto). `StatusStats`
real en `core/stats/status.go` (KPIs actual/anterior, `changes`, `perDay`, `perStatus`, `curve` de 25 puntos, `loyalty`). `model.Status`
no guarda la audiencia, así que el alcance es un recuento de personas y no un porcentaje (desviación de D'Ventas). Con listas la
audiencia pasa a `allow` salvo que se pida `deny`. Rango inválido = 30 días.

**Interfaz (B4).** Páginas Estados, Destinos y Ajustes. «Ver el protocolo» abre un `blob:` con un resumen local (no hay método Go para
abrir el documento). «Probar conexión» guarda primero el formulario (hace falta el id). Responder a un estado abre el chat del autor con
una cita sintetizada: que el adaptador la envíe como cita de estado no está probado. El archivo elegido para un estado no tiene vista previa.

### Integración del orquestador (oleada 2)

- `main.go`: `ws.Manager` creado en `buildCore` y conectado con `Router.SetOutbox`; `OnState` → evento `destination:state`;
  arranque `statuses.Start` y `protocol.Start` tras las cuentas; apagado `protocol.Stop` y `statuses.Stop` antes de `accounts.StopAll`.
  `DestinationStates` y `DestinationTest` del `ui/wails` apuntan al manager.
- `pages/registry.tsx` enlaza Estados, Destinos y Ajustes. Enlaces regenerados (12 servicios, 99 métodos; el servicio de notificaciones de
  Wails aparece como servicio).
- `go mod tidy` ejecutado: `gotd/log` pasa a dependencia directa.
- `stats/types.go` ya no define `StatusStats` (lo define B3); en la oleada 3 C2 hará lo mismo con los tipos de conversaciones.

### Pendiente tras la oleada 2

- `Contacts`, `Campaigns`, `Scheduler` y `Stats` siguen como esqueletos (oleada 3): `contacts.find` y las pantallas
  Campañas, Programados, Contactos y Estadísticas no existen todavía.
- Comprobar en la integración final que `/media/<cuenta>/<id>` sirve también medios de estados (el almacén los guarda con un chat
  reservado; `Messaging.MediaPath` usa `GetMedia`) y que `Post` deja `Media.URL` vacío en los estados propios.
- A3 y B1: `PostStatus` debe devolver `NativeID` y las vistas deben traer `ViewerID` (el refresco las ignora si no).

## Oleada 3 · Contactos, campañas, programados, estadísticas, Meta (12-11-2026)

Agentes: C1 campañas y programados, C2 contactos y estadísticas de conversaciones, C3 Messenger e Instagram, C4 interfaz de campañas y
programados, C5 interfaz de contactos y estadísticas. Bitácoras completas en `docs/bitacora/3-C*.md`.

**Campañas (C1).** `core/campaign` portado 1:1 con pruebas; `SeededRandom` verificado contra Node con 5 valores de 3 semillas (una con ñ y emoji).
`AssignVariants` devuelve `[]Assignment` en orden mezclado. Añadió `InWindow`, `NextWindowStart`, `EstimateSeconds`, `TypingMs`.
Decisiones: un envío por tick y cuenta aunque haya varias campañas `running`; A/B con `ABDefaults` (20 %, 4 h, 2 ganadoras) porque
`model.Campaign` no guarda configuración A/B; `Totals.Replied/OptOut` no los guarda el almacén y `Campaigns` los reconstruye una vez por
arranque; tope de «escribiendo…» 8 s; los saltados quedan en la cola con `state "skipped"` y el motivo en `Error`; `Resume` reinicia la base del
freno (en memoria); Messenger/Instagram dan `ErrUnsupported`. Programados: adjuntos copiados a `<data>/scheduled-media/<id>/`.
Detalle de las 32 desviaciones en `docs/bitacora/3-C1.md`.

**Contactos y estadísticas (C2).** `NormalizePhoneE164` (exportada; `00` = `+`; descarta <7 o >15 dígitos). `ImportCSV` detecta `;`/`,`, tolera BOM y
lee Latin-1 si no es UTF-8. `Merge` conserva `OptedOut` si alguno lo tenía; `SplitIdentity` devuelve el contacto nuevo. `Stats.Chat` mira solo ese chat.
No hay `voiceMinutes` (`StatRow` no trae duración). `Contacts` no puede publicar `chat.updated`: `Chat.ContactID` se actualiza con el siguiente
evento del chat. Las pruebas de `core` no pueden importar `adapter/...` (ni en `_test.go`): usan dobles propios. `CSVMapping` y `ImportResult`
no tienen etiquetas JSON (salen `Name/Phone/…`); el valor cero de `CSVMapping` es la columna 0, la interfaz manda `-1` explícito.

**Meta (C3).** Messenger usa `pkg/messagix`; **Instagram usa `pkg/instameow`** (messagix no configura endpoints de Instagram: el CHANGELOG de v26.08 lo
quitó), con otro protocolo (bandeja «mailbox» + avisos «slide»), por eso hay dos traductores (`meta/convert.go` para tablas LS e
`instagram/convert.go` para `slidetypes`). **E2EE de Messenger: `E2EEChats = true`** (messagix registra el dispositivo, whatsmeow recibe/envía; cubre
texto, imagen, vídeo, audio/voz, documento, sticker, ubicación solo recibida, reacciones, ediciones, borrado para todos, leído, escribiendo, recibos).
No cubre el historial cifrado antiguo (`RequestHistory` en chat cifrado → `ErrUnsupported`). **Riesgo:** el registro del dispositivo y la conexión cifrada
no se pudieron probar; si fallan en QA, el interruptor es que `newE2EE` (en `messenger/e2ee.go`) devuelva `nil` y `E2EEChats` queda en `false`.
Cookies obligatorias: Messenger `c_user`, `xs`, `datr`; Instagram `sessionid`, `ds_user_id`, `csrftoken` (09 pedía además `fr` y `mid`: se
aceptan pero no se exigen, igual que la librería). `DownloadMedia` no manda cookies al CDN. El id de chat de Instagram es `thread_fbid` y el id propio es
el de mensajería. Varios adjuntos: el primero en el mensaje y los demás como mensajes `<id>#2`, `<id>#3`. `Logout` solo borra la sesión local
(Meta no deja cerrar la sesión web de las cookies: sigue en «Seguridad»). `ErrUnsupported`: Forward, VotePoll, Archive/Pin/Mute, estados e historias,
enviar sticker/ubicación/contacto/encuesta; Instagram además enviar documentos y borrar solo para mí. `meta/local.go` lee columnas de `messages` y
`reactions` del almacén.

**Interfaz (C4, C5).** Campañas (asistente de 5 pasos, ritmo de la cuenta) y Programados (se recarga cada 15 s: no hay evento). Contactos y Estadísticas.
Limitaciones: el asistente de CSV no tiene vista previa de filas (no existe método Go que lea el archivo); la cola de campaña muestra `chatNativeId`
(`QueueItem` no trae nombre); la ventana por defecto del asistente es 09:00–18:00 L–V. «Lanzar» solo existe en borradores; una campaña programada
o en pausa muestra «Guardar cambios».

### Integración del orquestador (oleada 3)

- Router: `isOptOut` ahora delega en `campaign.IsOptOut` (se borró `foldOptOut`; las pruebas del Router siguen en verde).
- `pages/registry.tsx` enlaza las nueve páginas. Enlaces regenerados.
- `AccountCard`: aviso fijo «Los chats cifrados de extremo a extremo aún no se muestran» en cuentas de Messenger cuando `capabilities.e2eeChats` es falso.
- `main.go` ya arrancaba y paraba `campaigns` y `scheduler` y reenviaba `Campaigns.Progress()` como `campaign:progress`.
- `go mod tidy` ejecutado. El ejecutable pasó de 11 MB a **77 MB** con los cuatro canales (Meta arrastra `bridgev2` como biblioteca): aceptable, pero
  conviene vigilarlo en el empaquetado (el `.zip` comprime bien).

### Pendiente tras la oleada 3

- Nada de red está probado con cuentas reales: WhatsApp, Telegram, Messenger e Instagram quedan para `docs/QA-EN-VIVO.md`.
- La interfaz no se ha revisado en vivo (claro/oscuro, `datetime-local` en WebView2, diálogos de archivos).
- Opcional: `ContactsService.PreviewCSV(path, n)` para la vista previa del CSV; `SettingsService.OpenProtocolDoc()` para abrir el documento del protocolo.

## Oleada 4 · Empaquetado, revisión y documentos (12-11-2026)

Agentes: D1 empaquetado, D2 revisión, D3 documentos. Bitácoras completas en `docs/bitacora/4-D*.md` (D2 trae la lista completa de diferencias
documento ↔ código, clasificada).

**Empaquetado (D1).** `task portable`, `task msi`, `task echo-server`, `task release` (las tres) y `task licencias`. Comprobaciones automáticas en verde:
portable (zip con una carpeta raíz; descomprimido en limpio, `--version`, `--selftest` OK con `portable: true`, `data\app.db`) y MSI (tablas
leídas con `WindowsInstaller.Installer`: `UpgradeCode`, versión, por usuario sin `ALLUSERS`, sin componentes en AppData, extracción administrativa
con `msiexec /a` y `--selftest` OK). Desviaciones: WiX 5.0.2 rechaza `ARPNOMODIFY` duplicado (lo define `WixUI_Minimal`; se quitó esa línea);
`-culture es-ES` funciona (diálogos en español); `-pdbtype none`; el zip se escribe con `System.IO.Compression` y rutas con `/` (en
PowerShell 5.1 `Compress-Archive` guarda `\`); `.ps1` y `LEEME.txt` en UTF-8 con BOM (si un editor lo quita, PowerShell 5.1 rompe las tildes).
`LICENCIAS.txt` se genera de los módulos reales (79 de Go y 10 paquetes de la interfaz, con el texto de cada licencia).
**Aviso legal (decisión del dueño, no resuelta):** dConnector enlaza `go.mau.fi/mautrix-meta` (AGPL-3.0) y `go.mau.fi/libsignal` (GPL-3.0); además
MPL-2.0 en whatsmeow, `go.mau.fi/util`, `zeroconfig` y `mautrix`. Distribuir el binario puede obligar a ofrecer el código fuente de dConnector
a quien lo reciba (AGPL/GPL). Está anotado en `LICENCIAS.txt` y `License.rtf`; conviene una decisión antes de repartirlo fuera del uso propio.
El MSI instala ahora también `LICENCIAS.txt` junto al exe (cambio del orquestador).

**Revisión (D2).** `go vet`, `go test`, `go test -race` (primera vez en todo el proyecto: **cero carreras**, con dos pruebas de estrés nuevas),
`staticcheck` v0.8.2 sin hallazgos (`staticcheck.conf` desactiva solo `ST1005`: errores con mayúscula porque son textos visibles en español), `tsc`
y `vitest` en verde. Errores arreglados: `Media.URL` vacía en eventos en vivo y en estados (`completeMedia`/`withMediaURL`); `history:progress` sin
consumidor (la bandeja y la conversación abierta releen); fijar/silenciar en Messenger e Instagram se perdía con cada `chat.updated`
(`keepLocalMarks`); `PatchChat` ya no guarda `UnreadCount` negativo; registrador de whatsmeow sin Debug; `recover` en escuchas y conexiones;
`ws://[::1]`; cabeceras `nosniff` + `Content-Security-Policy: sandbox` en lo que se sirve (`/media`, `/avatar`, `/local`); código muerto y
símbolos obsoletos fuera. Riesgos que **quedan** (detalle y `archivo:línea` en `4-D2.md`): `message.updated` tras descargar un medio no llega (se
usa el id del medio como id del mensaje; la foto se ve igual por URL); `message:update` puede adelantarse al `message:upsert` agrupado; `messages.list`
y `message.send` sobre grupos ignoran `includeGroups=false` en el protocolo; `fileUrl` (https) sigue redirecciones y no bloquea direcciones privadas;
`statuses.go` usa `SelfID` desactualizado tras el alta; el archivo de registro no rota a medianoche con la app abierta; archivar en local un chat de
Messenger/Instagram se pierde con el siguiente `chat.updated`; WhatsApp declara `StatusViewers` pero devuelve `ErrUnsupported`.

**Documentos (D3).** `docs/MANUAL.md`, `docs/QA-EN-VIVO.md` (casillas, tabla «Resultado», comprobaciones marcadas **[RIESGO]**) y `README.md`.
Dejó 16 contradicciones documento ↔ código en `4-D3.md` (manda el código).

### Integración y arreglos del orquestador (oleada 4 y cierre)

- Borrado `internal/deps/deps.go` (todo lo usa ya código real) y `go mod tidy`. Se quitó `dconnector.exe` de la raíz, que yo mismo había dejado con
  un `go build .` y se coló en los commits de las oleadas 1 y 2 (historial local sin tocar; ahora `/dconnector.exe` está en `.gitignore`).
- «Iniciar minimizado» no tenía efecto: ahora el arranque con Windows lleva `--minimized` solo si ese ajuste está activo; se re-sincroniza en cada
  inicio (por si cambió la ruta tras actualizar). Abrir a mano siempre muestra la ventana. Manual y QA actualizados.
- El selector de «Reenviar» solo ofrece chats de la misma cuenta (`Messaging.Forward` no cruza cuentas); la ayuda de cookies pide solo las que
  exige el código (Messenger `c_user, xs, datr`; Instagram `sessionid, ds_user_id, csrftoken`).
- Contradicciones de D3 que se **dejan** (comportamiento válido, ya documentado en manual y QA): los contactos creados a mano o por CSV no tienen
  identidad y las campañas los omiten como «Sin identidad» (hay que «Crear contacto» desde el chat o «Importar de una cuenta»); el formulario de Destinos
  genera un token aleatorio si se deja vacío (el echo-server de ejemplo usa `-token secreto`); la ventana horaria por defecto de una campaña es 09:00–18:00 L–V.
- Equipo de construcción: se instaló WinLibs (gcc 16.2) con winget para poder correr `-race`; no hace falta para compilar (`CGO_ENABLED=0`).

## Cierre · dConnector 0.1.0 (12-11-2026)

Puerta completa en verde sobre el commit de la oleada 4 + los arreglos de cierre: `gofmt`, `go vet`, `go test`, `go test -race` (cero carreras),
`staticcheck`, `tsc`, `vitest` (589 pruebas), `task build`, `--version` y `--selftest`. `task release` en verde con sus comprobaciones automáticas.

| Artefacto | Tamaño | SHA-256 (primeros 16) |
| --- | --- | --- |
| `dist/dConnector-0.1.0-portable-x64.zip` | 25,8 MB | `48B7BC1517ED8EAF` |
| `dist/dConnector-0.1.0-x64.msi` | 20,3 MB | `64BC376BF140252F` |
| `dist/echo-server-0.1.0-x64.exe` (para la prueba del protocolo) | 7,1 MB | — |

Verificación independiente del portable: zip con una carpeta raíz (`dConnector.exe` 81 MB, `LEEME.txt`, `LICENCIAS.txt`, `portable.txt`); descomprimido en
una carpeta nueva, `--selftest` = OK con `portable: true` y `data\app.db` creada; sin procesos vivos. MSI: tablas, extracción administrativa y `--selftest` del exe
extraído en verde.

### Lo que quedó limitado o sin probar (ver también cada sección anterior)

- **Ninguna parte de red se ha probado con cuentas reales** (WhatsApp, Telegram, Messenger, Instagram): solo traducciones, máquinas de estado y dobles. Es la
  prueba en vivo de `docs/QA-EN-VIVO.md`, que marca con **[RIESGO]** lo más frágil: E2EE de Messenger (interruptor en `messenger/e2ee.go`), `instameow` de
  Instagram, inicio con usuario y contraseña de Messenger, floodwait y sesión de Telegram, `@lid` de WhatsApp.
- **La interfaz nunca se ha visto en una ventana con datos:** claro/oscuro, `datetime-local`/`time`/`date` en WebView2, diálogos de archivos, arrastrar y
  soltar, nota de voz (MediaRecorder + Opus), vídeo con audio en el visor.
- **Instalación real del MSI** (ruta por usuario, accesos directos, actualizar encima, desinstalar conservando `%APPDATA%\dConnector`): solo comprobada por sus tablas.
- `ErrUnsupported` por canal: ver las secciones de cada oleada y la sección 17 de `docs/MANUAL.md` («Qué no hace / límites»).
- Sin firma de código: SmartScreen avisará (T13).
- **Licencias:** AGPL-3.0 (mautrix-meta) y GPL-3.0 (libsignal) enlazadas: decisión del dueño antes de repartir el binario (ver Oleada 4).

## Ronda 5 · dConnector 0.2.0 (12-11-2026)

Cambios pedidos tras la primera prueba con una cuenta real de WhatsApp. Detalle por agente en `docs/bitacora/5-*.md` (carpeta fuera de git).

- **Contactos solos (A):** cada persona o chat directo de cualquier canal crea un contacto con identidad (`source = auto`); al arrancar, `SyncAll` rellena lo ya
  guardado (la libreta de WhatsApp: 275 personas en la cuenta de prueba). Al eliminar una cuenta se borran los automáticos que se quedan sin identidades; los
  manuales e importados se conservan. Un automático que el usuario edita pasa a manual. Con identidad, las campañas ya los incluyen.
- **Vistas previas vacías (A):** el historial trae la marca de tiempo del chat antes que la del último mensaje y la vista previa se quedaba en blanco. El almacén
  la rellena al guardar y la migración 2 de `account.db` repara los chats ya guardados. Los chats sin ningún mensaje (63 de 77 en la cuenta de prueba) quedan
  sin vista previa hasta que se pida su historial.
- **«Mensaje no compatible» (B):** WhatsApp ahora traduce vídeo redondo, botones, listas, plantillas, interactivos, invitaciones, eventos, pedidos, llamadas y
  «ver una vez»; los mensajes solo de metadatos (álbum padre, fijar…) ya no se guardan; lo desconocido se registra una vez por tipo en el log
  («tipo de mensaje de WhatsApp no soportado: …»). Los mensajes ya guardados como no compatibles no se reparan solos.
- **Fotos y stickers (B + F2):** las URL del historial caducan (403). Ahora se pide al teléfono que vuelva a subir el archivo (`SendMediaRetryReceipt`, espera de
  30 s, máximo 3 a la vez). La interfaz muestra «Descargando…», «No disponible» y «Reintentar».
- **Historial completo (C + F2):** servicio `History` copiado del diseño de v1: por chat, hacia atrás, reanudable (`history_sync`), lento a propósito (WhatsApp
  2,5 s entre peticiones; Telegram 1 s; Messenger/Instagram 6 s, 8 páginas por chat y una tanda automática cada 6 h), con paradas ante silencio del teléfono o
  bloqueos. Botón por cuenta, «Cargar mensajes anteriores» por chat y ajuste «Guardar el historial automáticamente». Nuevas vinculaciones de WhatsApp piden
  sincronización completa (`RequireFullSync`); las cuentas ya vinculadas solo reciben lo que el teléfono envíe por chat. **Sin probar con teléfono real:** el
  ancla sintética para chats sin mensajes (único punto de cambio en `whatsapp/history.go`).
- **Programados (D + F1):** botón «Programar» en el chat, varios por chat y recurrentes (día, semana con días, mes, año, personalizado; fin por fecha o por
  número), panel «Programados de este chat», vista previa de las próximas fechas, serie cancelable. Cada ocurrencia es una fila; atraso mayor de 1 h se omite.
- **Destinos (G):** el documento del protocolo se ve completo dentro de la app (con búsqueda y copiar); el filtro de un destino gana «Contactos»: todos o solo
  ciertas listas, y luego excluir listas (p. ej. todos menos «Familia»). Se aplica a eventos y a comandos; los cambios de lista tardan hasta 30 s en notarse.
- Versión 0.2.0 (para que el MSI actualice encima de 0.1.0). Puerta completa en verde.

### 0.2.1 (12-11-2026)

- Instagram: la pestaña «Usuario y contraseña» (que ya existía en el servidor) ahora también sale para Instagram y es la primera; «Pegar cookies» queda como
  alternativa si Meta pide un paso extra (captcha, verificación). Sin probar con cuenta real.
- Estados: «Nuevo estado» indica en qué canal y cuenta se publica y deja cambiar entre las cuentas que pueden publicar; si la cuenta elegida no puede (Instagram,
  Messenger), la cabecera lo explica en vez de ocultar el botón. Publicar historias de Instagram sigue sin ser posible (la librería no sube historias).

### 0.2.2 (12-11-2026)

- **Notificaciones de Windows (N):** puerto `port.Notifier` en el núcleo, adaptador `adapter/desktop/winnotify` (permiso leído del registro, sonido, clic abre el
  chat) y servicio `service.Notifications` que escucha el bus: silenciado o archivado → nada; notificaciones activas y permitidas → aviso; apagadas o bloqueadas
  → solo suena; chat abierto con la ventana enfocada → nada. Sección «Notificaciones» en Ajustes con estado de Windows y botón de prueba. Sin ver en ventana
  real: el aviso, el sonido y que `ms-settings:` abra la pantalla correcta.
- **Lista de chats solo con actividad (H):** los 63 contactos de la libreta que WhatsApp lista como conversaciones vacías ya no salen en Chats (grupos y fijados
  sí). **«Nuevo chat»** (botón junto al buscador y «Escribir» en la ficha del contacto) abre o crea el chat vacío; con el primer mensaje aparece en la lista.
  WhatsApp además permite escribir a un número fuera de la libreta (`IsOnWhatsApp`, sin probar con cuenta real). Messenger e Instagram no inician chats nuevos.
- **Protocolo:** documentado que `includeFromMe` envía lo que escribe el usuario por dos vías (`message.received` con `fromMe` y `message.sent`).

### 0.2.3 (12-11-2026)

- **Notificaciones, causa real de «no funciona» (P):** Windows tenía «No molestar» activado (campana con «z»): los avisos llegan a Windows pero no se ven ni suenan.
  Ahora el estado de No molestar se detecta (estado WNF `0x0D83063EA3BF1C75`, verificado activo en el equipo de prueba; `SHQueryUserNotificationState` no sirve:
  da «acepta» con No molestar activo) y se muestra en Ajustes › Notificaciones; «Probar notificación» dice la verdad.
- **Sonido propio:** un «pop» generado en memoria que suena con cada mensaje nuevo, aunque el aviso esté suprimido; el aviso va siempre silencioso para no duplicarlo.
- **Contador en la barra de tareas:** círculo rojo con `1`…`9` y `9+` sobre el icono (adaptador `taskbarbadge`, sobre la ventana principal). Se reaplica al mostrar,
  restaurar o enfocar la ventana y cada 30 s si hay sin leer (Wails 3.1.0 no deja enganchar `TaskbarButtonCreated`). Cuenta el total de no leídos sin separar silenciados.
- Mensajes propios en el chat «Tú» (desde otro dispositivo) avisan y suenan; «Última decisión» explica por qué un mensaje no avisó.

### 0.2.4 (12-11-2026)

- Sonido de mensaje elegido por el usuario (`livechat-129007`, convertido a WAV mono de 44,1 kHz y ~1 s, con el volumen bajado 5 dB y fundido final) incrustado en
  `internal/adapter/desktop/winnotify/message.wav`. Si no fuera válido, suena el «pop» generado. Para cambiarlo: sustituir `message.wav` (PCM de 16 bits, menos de 3 s).
  El archivo de origen es de Pixabay (licencia de Pixabay: uso libre, sin atribución obligatoria); confirmar la licencia si se reparte la app.

### 0.2.5 (12-11-2026)

- **Rediseño de la bandeja (aprobado en maqueta):** filtros sin scroll horizontal (chip «Todas», un icono por canal y menú «Cuentas» con casillas por cuenta), pestañas
  subrayadas que caben en 360 px, botón «Nuevo chat» en la cabecera, filas con la cuenta como texto pequeño (ya no una píldora que se cortaba), fijado y silenciado con
  iconos, fila seleccionada con barra de acento. Scroll global fino y sin flechas con `::-webkit-scrollbar` (WebView2 ignora esas reglas si `scrollbar-width` está puesto,
  y en motores antiguos dibujaba la barra clásica): el pulgar solo se ve al pasar el cursor. Tokens nuevos en `styles.css` (`--accent-text`, `--badge-*`, `--sel`, `--ch-*`…)
  con verde de mayor contraste para texto y relleno.
- **Barra lateral:** 80 px con el nombre bajo cada icono, marca arriba, insignia de no leídos en Chats y de estados sin ver en Estados, e indicador «Conectado /
  Reconectando / Sin conexión».
- **Estados de todos los canales juntos:** lista por secciones (canal › cuenta › Recientes / Vistos, plegables), aro segmentado por persona, «Ocultar vistos» y visor
  en línea en el panel derecho (el modal queda disponible). El visor usa la cuenta de cada autor para cargar, marcar visto y reaccionar. WhatsApp y Telegram son los
  únicos con estados; Instagram y Messenger se avisan con una nota al pie. «Mis estados» y «Estadísticas» eligen cuenta con chips.
- Sin ver en ventana real: todo el diseño (solo `tsc` y pruebas con jsdom); ni el menú de cuentas, ni el aro, ni el scroll en WebView2, ni claro/oscuro.

### 0.2.6 (12-11-2026)

- Barra lateral: los nombres «Programados», «Campañas» y «Estadísticas» se salían del botón. Ahora la barra mide 80 px, el botón 72 px y la etiqueta 10,5 px, de modo que
  caben enteras también en activo (negrita). Sin ver en ventana real.
- **Estados de WhatsApp que no aparecían:** en la base real no había ningún estado ajeno guardado. Causa encontrada: al vincular una cuenta, WhatsApp manda los estados que ya
  existían en `StatusV3Messages` (sincronización `INITIAL_STATUS_V3`) y el adaptador no leía ese campo (`history_status.go`, `statusesFromHistory`: vigentes, sin repetidos,
  solo texto/foto/vídeo). Eso solo ocurre al vincular, así que en la cuenta ya vinculada NO se recuperan los que ya existían. Los estados nuevos llegan en vivo; para saber
  si llegan se añadió registro de diagnóstico (sin texto ni números): `estado recibido` (tipo de contenido y si se aceptó), `no se pudo descifrar un estado` y
  `estados del historial`. Hipótesis sin comprobar: la presencia «no disponible» (necesaria para que el teléfono siga avisando) podría hacer que WhatsApp no entregue
  estados al dispositivo vinculado; no se cambió.

## 0.3.0 · Oleada 0 · servidor independiente (13-11-2026)

- **Spike (sin CGO):** el escritorio Wails v3.1.0 compila para `windows/386`, `windows/arm64` y `windows/amd64`; el núcleo, los almacenes y los
  cuatro canales compilan para `linux/amd64`, `linux/arm64` y `windows/arm64`. No hubo que preguntar nada.
- **Esqueleto:** `main.go` → `cmd/dconnector` (mismo `dConnector.exe`); `selftest.go` y el cableado del núcleo → `internal/app` (`Build`, `OpenAccountStore`,
  `RunSelftest`, compartidos por escritorio y servidor); `frontend/embed.go` incrusta `frontend/dist` (un `go:embed` no puede subir de carpeta);
  `cmd/dconnector-server` (por ahora `--version` y `--selftest`); `internal/config` (indicadores + `DCONNECTOR_*`); `internal/paths` multiplataforma
  (`--data`/`SetOverride`, Linux `$XDG_DATA_HOME/dconnector`). Taskfile apunta a `./cmd/dconnector`; los enlaces de Wails se generan idénticos (120 métodos).
- **Contrato:** `docs/14-API-SERVIDOR.md`.

### 0.3.0 · Oleada 1 · servidor, seguridad, API, cliente y escritorio (13-11-2026)

Agentes A (API), B (seguridad), C (servidor y servicio), D (frontend), E (escritorio), cada uno en su propio worktree de git fuera de OneDrive.
- **API (A):** `internal/adapter/api` + `facade` (fachadas neutras copiadas de `ui/wails`, sin Wails). RPC por reflexión, WebSocket con `hello`/`seq`, `/files`, `/media`, `/avatar`,
  `/health`, CORS. `upload://<id>` se resuelve a `<data>/uploads/<ID>/<nombre>`. Eventos nuevos `notify` y `notify:decision`.
- **Seguridad (B):** `auth` (argon2id, sesiones 24 h deslizantes/30 d, ticket WS, media-token, límites de intentos, token local por loopback, CLI `user add|passwd|list|disable|enable`) y
  `tlsconf` (certificado propio ECDSA P-256 con huella, externo con SIGHUP). Migración 3 de `app.db` (`users`, `auth_tokens`), aditiva.
- **Servidor (C):** `cmd/dconnector-server` + `internal/server` (arranque/apagado como el escritorio, línea JSON `listening`, `/health`, servicio de Windows `dConnectorServer`,
  `deploy/` con systemd, Dockerfile y compose).
- **Frontend (D, F):** transporte HTTP/WS, conexión local/remota, huella TLS («Confiar en este certificado»), estado `desktop:connection`, subida de archivos, tipos explícitos
  (`src/api/svcTypes.ts` y `src/api/core/*`: los modelos de `core/*` ya no se generan con Wails).
- **Escritorio (E, H):** `localserver` (proceso aparte, token por entorno, reinicio, «seguir en segundo plano», readopción), `client`, `bridge` (insignia, avisos, bandeja desde el
  flujo de eventos), `secret` (DPAPI), `connection` (`desktop.json`), `DesktopService`. Servicios Wails viejos borrados.

### 0.3.0 · Oleada 2 · integración (13-11-2026)

- Arreglos de integración: selftest del servidor (migraciones antes de `auth`), `ListenNonLoopback` (con `--insecure` fuera de loopback el token local no vale), `SetNotifications`
  (foco por conexión y `notify:decision`), cierre 4401 del WebSocket al revocar tokens, `healthcheck` acepta indicadores después del subcomando, 413/400 en cuerpos grandes o rotos,
  `--no-accounts` / `DCONNECTOR_NO_ACCOUNTS=1` (aditivo; `Accounts.LoadAll` en el núcleo).
- Pruebas: E2E del servidor real (HTTP, WebSocket, TLS con huella, modo local, dos clientes, reconexión, tokens caducados, recorridos de ruta, slowloris, ejecutable real) y del
  lanzador del escritorio contra el servidor real compilado. `go test -race` sin carreras; vitest 1054.
- **Datos reales 0.2.x:** se abrió una COPIA de `%APPDATA%\dConnector` con el servidor 0.3.0 (`--no-accounts`, sin conectar nada): las cuentas, los totales y la migración aditiva
  funcionan. La copia se borró.
- **Incidente menor (agente C):** su primer `--selftest` usó la carpeta real una vez (creó y borró una cuenta falsa y un `.log` vacío); los datos existentes no se tocaron.
- Límites: no se pudo probar ventana Wails real, WebView2 con TLS autofirmado, servicio de Windows real, Linux, Docker ni arm64 (todo eso va a Actions o a QA en vivo).

### 0.3.0 · Oleada 3 · builds, CI y documentación (13-11-2026)

- **Builds (I):** `Taskfile.yml` reescrito (`release:windows`, `release:linux`, `checksums`, `docker`, `release`, `gate`, `check:cross`…); `scripts/dist.go` (empaquetado portable Windows/Linux, zip/tar.gz, `SHA256SUMS`); MSI por arquitectura con el mismo
  UpgradeCode, que cierra `dConnector.exe` y `dConnector-server.exe` antes de reemplazar. VERSION se lee con `go run`, sin PowerShell.
- **CI (J):** `.github/workflows/ci.yml` (Linux x64 y arm64 nativos, Windows, Docker) y `release.yml` (al etiquetar `v*`).
- **Documentación (K):** README, LICENSE (AGPL-3.0), SECURITY, CONTRIBUTING, THIRD-PARTY-NOTICES, notas de la versión, manual y QA actualizados.
- **Hallazgo (I): el escritorio de 32 bits no arranca.** Wails v3.1.0 compila para `windows/386`, pero `dConnector.exe` entra en pánico al iniciar (`compileCallback: argument size is larger than uintptr`
  en `internal/webview2/pkg/combridge`: los callbacks COM de WebView2 reciben argumentos de 64 bits). El servidor x86 sí funciona. `DESKTOP_ARCHS` es `x64 arm64` por defecto; `task release DESKTOP_ARCHS="x64 x86 arm64"` lo arma.

### 0.3.0 · Oleada 4 · revisión adversarial y cierre técnico (13-11-2026)

Cuatro revisores independientes (R1 seguridad del servidor, R2 concurrencia y datos, R3 frontend, R4 builds y despliegue). Hallazgos arreglados, cada uno con su prueba:
- **R1:** el límite de intentos de login no frenaba una ráfaga simultánea (se comprobaba antes de argon2 y los fallos se contaban después); tabla de límites llena; IPv6 contada por dirección (ahora /64);
  rutas UNC y flujos NTFS en `FilePath` remoto (fuga de hash NTLM en Windows); cupo de subidas sin reserva; WebSocket abierto tras `user disable` por CLI; nombres reservados de Windows; plazo de lectura del login;
  endurecimiento de la unidad systemd.
- **R2:** no había candado por carpeta de datos (ahora `dconnector-server.lock`: «Ya hay un servidor usando esta carpeta de datos»); servidor local huérfano si el escritorio moría a mala fuerza (objeto de trabajo con
  `KILL_ON_JOB_CLOSE`; con «seguir en segundo plano» solo sobrevive al cierre ordenado); se mataba por pid sin comprobar que fuera nuestro servidor; apagado de más de 10 s; `paths.Init` vaciaba `tmp/` del servidor vivo;
  apertura concurrente de bases nuevas; `desktop.json` dañado se sobrescribía.
- **R3:** reintento de 503 en POST (podía duplicar envíos de mensajes: ahora solo en lecturas); sin error boundary (pantalla en blanco); dos WebSocket abiertos por carrera de reconexión; token de un servidor arrastrado a otro;
  `mediaUrl` con `javascript:`; CSP y `no-referrer`; aviso de `http:` fuera de loopback; límite de 100 MiB en el cliente.
- **R4:** el flujo de release no instalaba `WixToolset.Util.wixext`; publicaba desde una etiqueta de cualquier rama; Release público a medias; cachés en el build que se publica; MSI x86 sobre x64; echo-server sin licencias;
  Dockerfile sin `.dockerignore`.
- **Orquestador:** `staticcheck` v0.8.2 sin hallazgos (Windows y Linux); `SetFocus` ignora conexiones sin id; LEEME neutro (MSI y portable); versión 0.3.0 (`VERSION`, `package.json`, `info.json`).
- **Privacidad:** el árbol no contiene nombre propio, rutas locales, correos, teléfonos ni ids de cuenta reales; `PROMPT-SESION.md` y `docs/PROMPT-SERVIDOR.md` dejaron de versionarse (siguen en disco, en `.gitignore`).

**Puerta final** (`task gate`, EXIT 0): `gofmt`, `go vet`, `go test`, `tsc`, `vitest` (78 archivos), build, `--version`, `--selftest` con servidor, compilación cruzada de las 8 combinaciones. `go test -race` sin carreras en api, auth, server, e2e, desktop y core.

| Artefacto (`dist/`) | Tamaño |
| --- | --- |
| `dConnector-0.3.0-x64.msi` | 25,4 MB |
| `dConnector-0.3.0-arm64.msi` | 23,2 MB |
| `dConnector-0.3.0-portable-x64.zip` | 31,2 MB |
| `dConnector-0.3.0-portable-arm64.zip` | 28,0 MB |
| `dConnector-server-0.3.0-windows-x64.zip` | 25,3 MB |
| `dConnector-server-0.3.0-windows-arm64.zip` | 22,8 MB |
| `dConnector-server-0.3.0-linux-x64.tar.gz` | 24,9 MB |
| `dConnector-server-0.3.0-linux-arm64.tar.gz` | 22,8 MB |
| `echo-server-0.3.0-windows-x64.zip` / `-arm64.zip` | 3,2 / 2,8 MB |
| `echo-server-0.3.0-linux-x64.tar.gz` / `-arm64.tar.gz` | 3,1 / 2,8 MB |
| `SHA256SUMS` | verificado (12 archivos) |

**No probado:** ejecución en arm64 y en Linux (el servidor de Linux x64 sí se ejecutó en WSL durante la revisión R4); instalación real del MSI (actualizar encima de 0.2.x, desinstalar conservando datos); ventana Wails real,
CSP y TLS autofirmado en WebView2; servicio de Windows real; Docker (el demonio estaba apagado); escritorio x86 (no arranca, ver arriba); canales reales (WhatsApp, Telegram, Messenger, Instagram).

### 0.3.1 (en preparación) · MSI por usuario de verdad, carga sin parpadeo (13-11-2026)

- **Bug del MSI (desde la 0.1.0, visto al instalarlo por primera vez en una máquina real):** el instalador intentaba escribir en `C:\Program Files\dConnector` y pedía administrador. `Scope="perUser"` no basta:
  `ProgramFiles6432Folder` en un MSI de 64 bits apunta a `Program Files` (Windows solo redirige a `%LOCALAPPDATA%\Programs` la carpeta de 32 bits). Hasta entonces el MSI solo se había comprobado leyendo sus tablas.
  Arreglo: `LocalAppDataFolder\Programs\dConnector` (con `RemoveFolder` de ambas carpetas). `scripts/msi.ps1` comprueba ahora que `INSTALLFOLDER` cuelga de `LocalAppDataFolder` y no de `ProgramFiles`.
  **Prueba real:** instalación silenciosa por usuario (código 0, archivos en `%LOCALAPPDATA%\Programs\dConnector`, acceso directo en el menú Inicio, nada en Program Files) y desinstalación (código 0, carpeta y acceso directo
  eliminados, `%APPDATA%\dConnector` intacta: 36 archivos, 238,9 MB). Los MSI de la Release 0.3.0 tienen este defecto; la 0.3.1 lo corrige.
- **Parpadeo al abrir una pestaña por primera vez:** precarga inmediata del código de las 9 páginas, páginas visitadas en memoria (Programados y Ajustes se desmontan por tener temporizador propio; Chats sabe cuándo está oculta
  para no marcar leído ni suprimir avisos), y esqueletos a la medida de cada página (`frontend/src/pages/skeletons/`), usados como fallback de carga y como estado de primera carga dentro de cada página.
