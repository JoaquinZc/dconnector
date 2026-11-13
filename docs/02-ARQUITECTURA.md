# 02 · Arquitectura

## 1. Forma

```
 adaptadores de canal (salida)        NÚCLEO (internal/core)            adaptadores de entrada
 ┌──────────────────────┐   eventos  ┌─────────────────────────┐  llamadas ┌──────────────────────┐
 │ whatsapp  (×N cuent.)│──────────▶ │ Router → AccountStore   │ ◀──────── │ ui/wails (servicios) │
 │ telegram  (×N)       │            │        → Bus ──────────────────────▶│ eventos en vivo      │
 │ messenger (×N)       │ ◀──────────│ Messaging, Statuses,    │           ├──────────────────────┤
 │ instagram (×N)       │  comandos  │ Campaigns, Scheduler,   │ ◀──────── │ protocol/ws (destin.)│
 └──────────────────────┘            │ Contacts, Stats, Inbox  │ ─Bus────▶ │ outbox → servidor    │
                                     └─────────────────────────┘           └──────────────────────┘
                                         │ port.AppStore / port.AccountStore
                                         ▼
                                     adapter/store/sqlite (app.db, account.db)
```

- **Puerto de canal** (`port.ChannelAdapter`): uno por plataforma; una **instancia por cuenta**.
- **Puerto de almacén** (`port.AppStore`, `port.AccountStore`): SQLite.
- **Adaptadores de entrada**: la interfaz (servicios Wails) y el protocolo (destinos WebSocket). Los
  dos usan los **mismos servicios** del núcleo; ninguno tiene lógica propia.

## 2. Árbol completo

```
dconnector-v2/
  PLAN.md  PROMPT-SESION.md  VERSION  README.md  .gitignore  go.mod  go.sum
  Taskfile.yml                      tareas (Wails v3 usa Task): build, portable, msi, test
  main.go                           arranque: rutas, registro, almacenes, núcleo, Wails, bandeja
  internal/
    buildinfo/buildinfo.go          var Version = "dev" (se fija con -ldflags)
    paths/paths.go                  DataDir(), AccountDir(id), MediaDir(id), AvatarDir(id), LogsDir(), IsPortable()
    logging/logging.go              slog JSON a <data>/logs/dconnector-AAAA-MM-DD.log, borra >14 días
    core/
      model/                        03-DOMINIO.md (código literal)
        ids.go account.go chat.go message.go status.go contact.go campaign.go scheduled.go
        destination.go event.go
      capability/capability.go      03-DOMINIO.md
      port/                         04-PUERTOS.md (código literal)
        channel.go store.go errors.go
      bus/bus.go                    pub/sub en memoria (06-SERVICIOS §1)
      service/
        types.go                    tipos compartidos de las firmas (del orquestador; 13 oleada 0 §9)
        accounts.go                 instancias, ciclo de vida, vinculación
        router.go                   eventos de adaptadores → almacén → bus
        inbox.go                    bandeja unificada, búsqueda global
        messaging.go                enviar, responder, reaccionar, editar, borrar, leído, medios
        statuses.go                 estados
        contacts.go                 contactos unificados y listas
        campaigns.go                campañas (usa campaign/)
        scheduler.go                programados
        stats.go                    estadísticas (usa stats/)
        destinations.go             CRUD de destinos + estado (el transporte está en adapter/protocol)
        settings.go                 ajustes
      campaign/                     lógica pura portada de D'Ventas (sin E/S)
        types.go template.go variants.go pacing.go outreach.go abtest.go optout.go  (+ _test.go)
      stats/                        lógica pura portada de D'Ventas
        types.go conversation.go status.go periods.go  (+ _test.go)
      fake/fake_adapter.go          adaptador falso en memoria para pruebas del núcleo
    adapter/
      store/sqlite/
        open.go migrate.go          abrir con pragmas, migraciones
        app_migrations.go account_migrations.go
        appstore.go                 implementa port.AppStore
        accountstore.go             implementa port.AccountStore
        *_test.go
      channel/
        registry.go                 map[model.Channel]port.AdapterFactory
        whatsapp/  adapter.go convert.go send.go media.go pairing.go history.go status.go (+ _test.go)
        telegram/  adapter.go convert.go send.go media.go auth.go session.go history.go stories.go (+ _test.go)
        meta/      common.go convert.go (+ _test.go)       compartido por messenger e instagram
        messenger/ adapter.go
        instagram/ adapter.go
      protocol/ws/
        client.go                   conexión, hello, reconexión, ping
        outbox.go                   suscripción al bus, filtros, reenvío
        commands.go                 comandos → servicios del núcleo
        media.go                    media.get / media.chunk
        manager.go                  un cliente por destino habilitado
        *_test.go
      ui/wails/
        services.go                 registra todos los servicios en la app Wails
        accounts_svc.go inbox_svc.go messaging_svc.go statuses_svc.go contacts_svc.go
        campaigns_svc.go scheduled_svc.go stats_svc.go destinations_svc.go settings_svc.go files_svc.go
        events.go                   bus → eventos Wails (nombres en 11-INTERFAZ §2)
        media_handler.go            /media/… y /avatar/… para la vista web
        tray.go                     bandeja del sistema
        autostart.go                arranque con Windows (HKCU\…\Run)
  frontend/                         11-INTERFAZ.md
    package.json vite.config.ts tsconfig.json index.html
    bindings/                       GENERADO por wails3 (no se edita a mano)
    src/ main.tsx App.tsx styles.css
      api/  events.ts  types.ts     envoltorios finos de bindings y eventos
      store/  app.ts accounts.ts inbox.ts chat.ts statuses.ts ui.ts
      lib/  format.ts channels.ts webmToOgg.ts caps.ts
      components/  layout/ chat/ status/ accounts/ contacts/ campaigns/ scheduled/ stats/ destinations/ settings/ common/
      pages/  ChatsPage.tsx StatusesPage.tsx CampaignsPage.tsx ScheduledPage.tsx ContactsPage.tsx
              StatsPage.tsx AccountsPage.tsx DestinationsPage.tsx SettingsPage.tsx
  examples/echo-server/main.go
  build/                            iconos, manifest, recursos de Wails
    appicon.png  windows/ icon.ico  info.json  wails.exe.manifest
    windows/msi/ dConnector.wxs  License.rtf
  scripts/  portable.ps1  msi.ps1
  docs/  …  BITACORA.md  MANUAL.md  QA-EN-VIVO.md
  dist/                             salida (ignorado en git)
```

## 3. Dependencias permitidas

| Paquete | Puede importar |
| --- | --- |
| `core/model`, `core/capability` | solo biblioteca estándar |
| `core/port` | `model`, `capability`, estándar |
| `core/bus` | `model`, estándar |
| `core/campaign`, `core/stats` | `model`, estándar |
| `core/service` | `model`, `capability`, `port`, `bus`, `campaign`, `stats`, `paths`, estándar, `ulid` |
| `core/fake` | `model`, `capability`, `port` |
| `adapter/store/sqlite` | `core/*` (no `service`), `modernc.org/sqlite`, `paths` |
| `adapter/channel/*` | `core/model`, `core/capability`, `core/port`, su librería de canal, `paths` |
| `adapter/protocol/ws` | `core/*`, `coder/websocket` |
| `adapter/ui/wails` | `core/*`, Wails v3, `paths` |
| `main.go` | todo |

`go vet` + una prueba `internal/core/arch_test.go` que recorre los imports con `go/parser` y falla si
`core/` importa `adapter/` o una librería de canal.

## 4. Arranque (`main.go`), en este orden

1. `paths.Init()` (decide carpeta de datos; crea `logs/`, `accounts/`).
2. `logging.Init(paths.LogsDir())`; registra versión y carpeta (sin datos personales).
3. `sqlite.OpenApp(paths.AppDBPath())` → migraciones → `appStore`.
4. `bus.New()`.
5. `service.NewAccounts(appStore, bus, channel.Registry, openAccountStore)` donde
   `openAccountStore(id)` abre `accounts\<id>\account.db` con migraciones y devuelve
   `(port.AccountStore, *sql.DB, error)`.
6. Resto de servicios con sus dependencias (06-SERVICIOS).
7. `ws.NewManager(destinations, services, bus, appStore)`.
8. Aplicación Wails v3: `SingleInstance` (al repetir, mostrar ventana), una ventana
   `1280×800` mínima `960×600`, título «dConnector», servicios de `ui/wails`, manejador de medios,
   bandeja.
9. Al evento de aplicación lista: `accounts.StartAll(ctx)` (cada cuenta habilitada en su goroutine),
   `scheduler.Start(ctx)`, `campaigns.Start(ctx)`, `wsManager.Start(ctx)`.
10. Al salir: cancelar el contexto raíz → `wsManager.Stop`, `campaigns.Stop`, `scheduler.Stop`,
    `accounts.StopAll` (máximo 10 s en total), cerrar bases.

## 5. Concurrencia

- Un **contexto raíz** por proceso; cada cuenta tiene su contexto hijo.
- Cada adaptador corre su conexión en goroutines propias y entrega eventos con `sink.Emit(ev)`.
  `Emit` **no bloquea**: el Router tiene un canal con búfer de 4.096 por cuenta y una goroutine que
  los procesa en orden. Si se llena, `Emit` espera hasta 2 s y luego descarta con registro `WARN`.
- El Router procesa un evento así: guardar en `AccountStore` → completar (p. ej. `ChatKey`, contacto
  unificado) → `bus.Publish`.
- `bus`: cada suscriptor tiene su canal con búfer (por defecto 1.024). Si un suscriptor se atrasa,
  el bus descarta para ese suscriptor (no frena a los demás) y cuenta los descartes. **El outbox del
  protocolo no se apoya en el bus para no perder nada**: el Router además llama a
  `OutboxWriter.Enqueue(ev)` de forma síncrona (06-SERVICIOS §2).
- SQLite: un `*sql.DB` por archivo, `SetMaxOpenConns(8)`, WAL, `busy_timeout=10000`,
  `_txlock=immediate`.

## 6. Errores

- Errores del dominio en `port/errors.go` (`ErrUnsupported`, `ErrNotConnected`, `ErrNotFound`,
  `ErrInvalid`, `ErrRateLimited`). Los adaptadores envuelven con `fmt.Errorf("whatsapp: …: %w", err)`.
- Los servicios Wails devuelven `error`; la interfaz muestra `err.message` en un aviso (toast).
- El protocolo traduce: `ErrUnsupported`→`unsupported`, `ErrNotConnected`→`not_connected`,
  `ErrNotFound`→`not_found`, `ErrInvalid`→`invalid`, `ErrRateLimited`→`rate_limited`, otro→`internal`.
- Ningún pánico cruza una goroutine: cada goroutine de larga vida tiene `defer recover()` que
  registra y reinicia el bucle (cuentas: reconexión con espera 2 s → 4 → 8 … máx. 5 min).

## 7. Registro

- `slog` JSON, nivel `INFO` (ajuste «Registro detallado» → `DEBUG`).
- **Nunca** se registra texto de mensajes, nombres de chats ni números completos. Se registran
  `accountId`, canal, tipo de evento, ids nativos abreviados (últimos 4) y errores.
- La librería de cada canal usa su propio registrador adaptado a `slog` con el atributo `channel`.
