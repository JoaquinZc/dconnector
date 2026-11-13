# 13 · Oleadas, agentes y criterios de cierre

La sesión de construcción tiene un **orquestador** (la sesión principal) y **agentes** (Agent tool,
`subagent_type: "general-purpose"`) que trabajan en paralelo dentro de cada oleada. Máximo **5
agentes a la vez** por oleada (tope del dueño: nunca más de 10 en total en paralelo).

## Reglas comunes

- Cada agente recibe en su prompt: (1) leer `PLAN.md` y los documentos de su lista, (2) sus
  **archivos propios** (solo puede crear o editar esos), (3) su criterio de cierre, (4) las reglas
  de `PLAN.md` «Reglas para el ejecutor».
- **Prohibido para los agentes**: tocar `go.mod`, `go.sum`, `frontend/package.json`,
  `pnpm-lock.yaml`, `internal/core/model`, `internal/core/capability`, `internal/core/port`, y
  archivos de otro agente. Si necesitan algo de ahí, lo piden en su informe final.
- Cada agente termina con `go build ./...` y `go test ./<sus paquetes>/...` en verde (o
  `pnpm -C frontend exec tsc --noEmit` + `vitest` si es de interfaz) y un informe: archivos tocados,
  pruebas, y lo anotado en `BITACORA.md` (cada agente añade una sección `## <oleada>/<agente>`; para no
  chocar, escribe su sección en `docs/bitacora/<oleada>-<agente>.md` y el orquestador las junta).
- Entre oleadas, el orquestador: junta bitácoras, resuelve dependencias pedidas, ejecuta la
  **puerta** (abajo), arregla lo que rompa la puerta y hace el commit `oleada N: …`.

**Puerta de cada oleada** (todo en verde):
```powershell
go vet ./...
go test ./...
pnpm -C frontend exec tsc --noEmit
pnpm -C frontend exec vitest run
wails3 generate bindings -ts
task build        # o los pasos de 12 §3
bin\dConnector.exe --selftest --out $env:TEMP\dc-selftest.txt   # con DCONNECTOR_DATA a una carpeta temporal
```

## Oleada 0 · Cimientos (solo el orquestador, sin agentes)

1. Herramientas (12 §1). Anotar versiones exactas en `BITACORA.md`.
2. `git init` en `dconnector-v2/`, `.gitignore` (`bin/`, `dist/`, `frontend/node_modules/`,
   `frontend/dist/`, `*.syso`, `docs/bitacora/`).
3. `go mod init dconnector`; `go get` de **todas** las dependencias de 01-DECISIONES (T2–T15),
   incluida la regla de importación de Meta (09 §1) → anotar qué opción salió.
4. Wails: 12 §2 (plantilla, `build/`, `Taskfile.yml`, icono).
5. `frontend/`: Vite + React 19 + TS + Tailwind v4 + todas las librerías de T11, `vitest`,
   `@wailsio/runtime`; alias `@/` → `src/`.
6. Escribir **literal** `internal/core/model/*` (03), `model/preview.go` (07 §6), `capability/` (03,
   con su `MarshalJSON` y prueba), `port/*` (04, con `Base` completa).
7. `internal/buildinfo`, `internal/paths`, `internal/logging`, `internal/core/bus` (06 §1, con pruebas),
   `internal/core/fake` (06 §13).
8. `internal/core/arch_test.go` (02 §3).
9. Esqueletos que compilan (cada método devuelve `errors.New("pendiente")`; nunca `panic`) de **todos**
   los servicios del núcleo con sus firmas de 06, de `ui/wails/*_svc.go` con las firmas de 11 §1, y
   `main.go` con el arranque de 02 §4 y los flags de T16. Así los agentes de interfaz tienen
   enlaces generados desde el principio. Para que compile, se declaran ya **todos los tipos**
   que aparecen en esas firmas:
   - en `service/types.go`: `AccountView`, `CampaignReview`, `CampaignProgress`, `ImportResult`,
     `CSVMapping`, `AppSettings`, `OpenAccountStore`, `OutboxWriter`, `IncomingObserver` (literal de 06);
   - en `core/stats/types.go`: `Range` (`struct{ Days int; From, To string }` — `Days > 0` o fechas
     locales `AAAA-MM-DD`), y como structs vacíos `StatusStats`, `ConversationOverview`,
     `ContactConversationStats`, `GroupsOverview`. **B3** rellena `Range` y `StatusStats`; **C2**
     rellena los de conversaciones (oleadas distintas: no chocan);
   - en `core/campaign/types.go`: `ReviewItem{Level, Code, Message string}` y
     `Estimate{Seconds int64; Days int}`; **C1** puede ampliarlos.
   Los `service/*.go` de cada agente sustituyen a su esqueleto; `service/types.go` es del
   orquestador (un agente que necesite cambiarlo lo pide).
10. `wails3 generate bindings -ts`; `docs/BITACORA.md` inicial.
11. Puerta (el `--selftest` puede devolver «pendiente» en lo de la cuenta falsa: se completa en la 1).
    Commit `oleada 0: cimientos`.

## Oleada 1 · Núcleo, almacén, WhatsApp e interfaz base (5 agentes en paralelo)

| Agente | Archivos propios | Documentos | Cierre |
| --- | --- | --- | --- |
| **A1 almacén** | `internal/adapter/store/sqlite/**` | 04, 05 | `AppStore` y `AccountStore` completos; pruebas por método con archivo temporal (`t.TempDir()`), incluidas FTS5, idempotencia de `UpsertMessage`, orden de entrega, reacciones de 1 por persona, outbox |
| **A2 núcleo** | `internal/core/service/{router,accounts,inbox,messaging,settings,destinations}.go` + pruebas | 02, 06 §2–5, §11–12 | todo probado con `core/fake` y un `AccountStore` en memoria hecho en el test (o el de A1 si ya está: no esperar; usar un doble propio en `service/testutil_test.go`) |
| **A3 WhatsApp** | `internal/adapter/channel/whatsapp/**`, `internal/adapter/channel/registry.go` | 07 | compila; `convert_test.go` cubre la tabla de 07 §6 y §7; el adaptador completo (vinculación, envío, historial, estados) escrito contra la API real de whatsmeow |
| **A4 interfaz base** | `frontend/src/{main.tsx,App.tsx,styles.css}`, `frontend/src/{api,store,lib}/**`, `frontend/src/components/{layout,common}/**`, `frontend/src/pages/AccountsPage.tsx`, `frontend/src/components/accounts/**` | 11 §2, §5, §6.1, §6.8, §7 | marco con rail, tema claro/oscuro, toasts, modales; Cuentas completa (añadir con los 4 flujos, tarjetas, estados en vivo) contra los enlaces generados |
| **A5 interfaz chats** | `frontend/src/pages/ChatsPage.tsx`, `frontend/src/components/chat/**` | 11 §6.2 | lista unificada con filtro de cuentas, conversación con todos los tipos de burbuja, menú, compositor (incluida la nota de voz con `webmToOgg`), panel de información |

A4 y A5 se coordinan solo por `store/` y `api/`, que son de **A4**: A5 importa de ahí y, si le
falta algo, crea `frontend/src/components/chat/useChatApi.ts` local y lo pide en su informe.

Al cerrar: el orquestador completa `ui/wails` de cuentas, bandeja y chats (`accounts_svc.go`,
`inbox_svc.go`, `messaging_svc.go`, `files_svc.go`, `events.go`, `media_handler.go`, `tray.go`,
`autostart.go`) llamando a los servicios ya hechos, y el `--selftest` completo. Puerta. Commit.

## Oleada 2 · Telegram, protocolo, estados (4 agentes)

| Agente | Archivos propios | Documentos | Cierre |
| --- | --- | --- | --- |
| **B1 Telegram** | `internal/adapter/channel/telegram/**` (+ registrar en `registry.go`: **solo** añadir su línea) | 08 | compila; `convert_test.go` con la tabla de 08 §5; vinculación, envío, historial, historias |
| **B2 protocolo** | `internal/adapter/protocol/ws/**`, `examples/echo-server/**`, `docs/PROTOCOLO-v1.md` (copia de 10) | 10 | pruebas de 10 §9 en verde; servidor de ejemplo funcional contra dConnector con el adaptador falso (prueba de integración en `ws/integration_test.go`) |
| **B3 estados (núcleo)** | `internal/core/service/statuses.go`, `internal/core/stats/{status,periods}.go` + pruebas | 06 §6, §10 | servicio y lógica de estadísticas de estados portada con pruebas |
| **B4 interfaz estados + destinos + ajustes** | `frontend/src/pages/{StatusesPage,DestinationsPage,SettingsPage}.tsx`, `frontend/src/components/{status,destinations,settings}/**` | 11 §6.3, §6.9, §6.10 | pantallas completas contra los enlaces |

Al cerrar: el orquestador completa `ui/wails/{statuses,destinations,settings}_svc.go`, conecta
`ws.Manager` en `main.go` y `Router.SetOutbox`. Puerta. Commit.

## Oleada 3 · Contactos, campañas, programados, estadísticas, Meta (5 agentes)

| Agente | Archivos propios | Documentos | Cierre |
| --- | --- | --- | --- |
| **C1 campañas (núcleo)** | `internal/core/campaign/**`, `internal/core/service/{campaigns,scheduler}.go` + pruebas | 06 §8, §9 | lógica pura portada con sus pruebas (incluida la de `SeededRandom` contra valores de Node); motor probado con `core/fake` y reloj inyectable (`now func() time.Time`) |
| **C2 contactos + estadísticas (núcleo)** | `internal/core/service/{contacts,stats}.go`, `internal/core/stats/conversation.go` + pruebas | 06 §7, §10 | importaciones, uniones, CSV, estadísticas portadas con pruebas |
| **C3 Meta** | `internal/adapter/channel/{meta,messenger,instagram}/**`, `third_party/**` si hace falta (+ sus líneas en `registry.go`) | 09 | compila; pruebas de traducción; E2EE según 09 §1.3 |
| **C4 interfaz campañas + programados** | `frontend/src/pages/{CampaignsPage,ScheduledPage}.tsx`, `frontend/src/components/{campaigns,scheduled}/**` | 11 §6.4, §6.5 | asistente completo, detalle, ritmo de la cuenta, programados |
| **C5 interfaz contactos + estadísticas** | `frontend/src/pages/{ContactsPage,StatsPage}.tsx`, `frontend/src/components/{contacts,stats}/**` | 11 §6.6, §6.7 | pantallas completas |

Al cerrar: el orquestador completa los `_svc.go` que faltan y los conecta en `main.go`. Puerta. Commit.

## Oleada 4 · Revisión, empaquetado y documentos (3 agentes)

| Agente | Archivos propios | Cierre |
| --- | --- | --- |
| **D1 empaquetado** | `scripts/**`, `build/windows/msi/**`, tareas `portable` y `msi` del `Taskfile.yml`, `LEEME.txt`, `LICENCIAS.txt` | 12 §4 y §5 completos, con sus comprobaciones automáticas en verde |
| **D2 revisión** | puede **editar cualquier archivo de Go** para arreglar; no añade funciones | recorre 03–11 contra el código y deja una lista de diferencias en `docs/bitacora/4-D2.md`; arregla las que sean errores; `go test -race ./...`, `go vet`, y `staticcheck ./...` (`go run honnef.co/go/tools/cmd/staticcheck@latest`) sin hallazgos |
| **D3 documentos** | `docs/MANUAL.md`, `docs/QA-EN-VIVO.md`, `README.md` | manual de uso por pantalla; QA en vivo: lista de comprobación por canal (vincular, recibir de cada tipo, enviar de cada tipo, reaccionar, editar, borrar, estados, campaña de prueba a 3 contactos propios con mensajes [PRUEBA], destino con el echo-server) con casillas para que el dueño marque |

D2 corre **después** de D1 y D3 si sus arreglos tocan lo mismo; si no, en paralelo.

## Cierre final (orquestador)

1. Puerta completa + `task portable` + `task msi`.
2. Verificar los dos artefactos de `dist/` con sus comprobaciones (12 §4 y §5).
3. `docs/BITACORA.md` final con: versiones exactas, decisiones tomadas por la regla 8, lo que quedó en
   `ErrUnsupported` y por qué (con referencia a la regla de su documento).
4. Commit `release: dConnector 0.1.0`, etiqueta `v0.1.0`. Sin push.
5. Informe al dueño: rutas de los dos artefactos, cómo probar (`docs/QA-EN-VIVO.md`), y lo que la
   bitácora marca como limitado.
