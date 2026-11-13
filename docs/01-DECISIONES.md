# 01 · Decisiones

Todas están cerradas. Si una versión exacta no existe al instalar, se toma la más reciente estable
de esa misma línea y se anota en `BITACORA.md`.

## Producto

| # | Decisión | Motivo |
| --- | --- | --- |
| D1 | Nombre del producto **dConnector**; ejecutable `dConnector.exe`; versión inicial `0.1.0` en el archivo `VERSION` | — |
| D2 | **Sin cuentas de usuario, sin licencia, sin telemetría, sin actualizador remoto**. No hay ninguna llamada de red salvo los canales y los destinos configurados | Pedido explícito: todo local |
| D3 | **Sin contraseña ni cifrado de base** en v1. Los datos quedan en la carpeta del usuario de Windows | Simplicidad; se puede añadir después sin cambiar puertos |
| D4 | Se conserva de D'Ventas: chats, estados, campañas (con anti-bloqueo), programados, contactos y listas, estadísticas de chats | Pedido explícito |
| D5 | **No existe**: catálogo, productos, pedidos, cobros, finanzas, compras, gastos, envíos, opiniones, agenda/citas, IA, Google Calendar, Supabase, PostHog | Pedido explícito |
| D6 | Una sola ventana; al cerrarla **se oculta en la bandeja** y las cuentas siguen conectadas (ajuste, encendido por defecto). «Salir» desde la bandeja apaga todo de forma ordenada | Las cuentas deben seguir recibiendo y enviando al protocolo |
| D7 | Instancia única: abrir el .exe por segunda vez muestra la ventana ya abierta | Dos procesos sobre el mismo SQLite y la misma sesión rompen las sesiones |
| D8 | Idioma de la interfaz: español | — |

## Tecnología

| # | Decisión |
| --- | --- |
| T1 | **Go 1.24 o superior** (`winget install GoLang.Go`). Módulo `dconnector` (ruta de módulo literal `dconnector`) |
| T2 | **Wails v3** (`github.com/wailsapp/wails/v3`), la última etiqueta publicada (estable si existe; si solo hay alfa/beta, la última alfa/beta). Se usa v3 y no v2 porque v3 trae **bandeja del sistema**, instancia única, notificaciones y servicios con enlaces generados. CLI: `go install github.com/wailsapp/wails/v3/cmd/wails3@<misma etiqueta>` |
| T3 | **Sin CGO**: `CGO_ENABLED=0`. SQLite con `modernc.org/sqlite` (Go puro, incluye FTS5) |
| T4 | WhatsApp: `go.mau.fi/whatsmeow` (última versión) |
| T5 | Telegram: `github.com/gotd/td` (última versión), cuenta personal por MTProto |
| T6 | Messenger e Instagram: `messagix` del módulo `go.mau.fi/mautrix-meta` (ver la regla de importación en `09-ADAPTADOR-META.md`) |
| T7 | WebSocket cliente: `github.com/coder/websocket` |
| T8 | IDs: `github.com/oklog/ulid/v2` |
| T9 | Registro: `log/slog` de la biblioteca estándar, en JSON, a archivo |
| T10 | Pruebas Go: biblioteca estándar `testing` + `github.com/stretchr/testify/require` |
| T11 | Interfaz: **React 19 + TypeScript + Vite + Tailwind CSS v4**, gestor **pnpm**. Librerías: `zustand` (estado), `lucide-react` (iconos), `@tanstack/react-virtual` (listas largas), `recharts` (gráficos), `emoji-picker-react` (emojis), `date-fns` (fechas, locale `es`), `qrcode.react` (QR de vinculación), `@wailsio/runtime` (eventos y enlaces de Wails v3, la versión que pida la plantilla de `wails3 init`). Pruebas: `vitest` |
| T15 | Otras de Go: `golang.org/x/image` (webp, escalado de miniaturas), `golang.org/x/sys/windows/registry` (arranque con Windows), `github.com/gotd/contrib` (floodwait), `go.uber.org/zap` (lo pide gotd), `google.golang.org/protobuf` (lo pide whatsmeow) |
| T16 | El ejecutable admite `--version` (imprime y sale), `--minimized` (arranca oculto en la bandeja) y `--selftest` (sin ventana: resuelve la carpeta de datos, abre `app.db`, aplica migraciones, crea y borra una cuenta falsa con su `account.db`, imprime `OK` y sale con código 0; cualquier fallo → mensaje y código 1). `--selftest` es la prueba automática de los paquetes portable y MSI |
| T12 | MSI: **WiX Toolset 5.0.2** como herramienta de .NET (`dotnet tool install --global wix --version 5.0.2`; requiere .NET SDK 8: `winget install Microsoft.DotNet.SDK.8`). Extensión `WixToolset.UI.wixext/5.0.2`. Se fija la 5.0.2 porque es MS-RL sin cuota de mantenimiento |
| T13 | Sin firma de código: Windows SmartScreen avisará; se documenta en el manual |
| T14 | Arquitectura de destino: **windows/amd64**. ARM64 queda fuera de v1 |

## Datos

| # | Decisión |
| --- | --- |
| A1 | Carpeta de datos: `%APPDATA%\dConnector` (`os.UserConfigDir()` + `dConnector`). Modo portable si existe `portable.txt` junto al .exe → `<carpeta del exe>\data`. Variable de entorno `DCONNECTOR_DATA` gana a ambos (para pruebas) |
| A2 | `app.db`: lo común (cuentas, contactos, listas, campañas, programados, destinos, outbox, ajustes) |
| A3 | `accounts\<accountId>\account.db`: la sesión del canal (tablas de la librería) + chats, personas, mensajes, reacciones, estados de esa cuenta. `accounts\<accountId>\media\`: archivos descargados. `accounts\<accountId>\avatars\` |
| A4 | Borrar una cuenta = cerrar sesión en el canal (si se puede) + borrar su carpeta + sus filas en `app.db` |
| A5 | Instantes en **UTC**, guardados como enteros (milisegundos Unix). La interfaz muestra la hora local del equipo |

## Canales

| # | Decisión |
| --- | --- |
| C1 | WhatsApp vincula con **QR** o con **código de 8 caracteres** (número de teléfono) |
| C2 | Telegram: cada cuenta pone su propio `api_id` y `api_hash` (my.telegram.org) al añadirla; se guardan en `accounts.config_json`. Inicio con teléfono → código → contraseña 2FA si la tiene |
| C3 | Messenger e Instagram: inicio pegando las **cookies** de la sesión del navegador (formato JSON o `nombre=valor; …`). Si la versión de messagix instalada ofrece inicio con usuario y contraseña, se añade como segunda opción en la misma pantalla |
| C4 | Al vincular Messenger o Instagram se muestra una vez el aviso «Vía no oficial: Meta puede bloquear la cuenta» con casilla «Entiendo» obligatoria |
| C5 | Los ids nativos no se reescriben: WhatsApp usa el JID como texto (`123@s.whatsapp.net`, `xxx@g.us`, `xxx@lid`), Telegram `user:<id>`, `chat:<id>`, `channel:<id>`, Meta el id numérico del hilo |
| C6 | WhatsApp `@lid` ↔ número: el adaptador resuelve siempre al JID de teléfono (`@s.whatsapp.net`) cuando whatsmeow lo conoce (`client.Store.LIDs`), para que una persona sea un solo chat |

## Protocolo

| # | Decisión |
| --- | --- |
| P1 | dConnector es **cliente** WebSocket de cada destino. `wss://` obligatorio salvo `ws://localhost` y `ws://127.0.0.1` |
| P2 | JSON por frame, sobre v1 (`10-PROTOCOLO-v1.md`). Entrega al menos una vez con outbox y `ack` |
| P3 | Cada destino tiene token propio, filtro de cuentas y eventos, y un interruptor **«Permitir comandos»** (apagado por defecto) |
| P4 | Se incluye `examples/echo-server/` en Go: imprime los eventos y responde «Recibido ✅» a cada `message.received` que no es tuyo |
