# Plan: servidor independiente + app de escritorio como cliente (0.3.0)

Estado (13-11-2026): **oleadas 0, 1 y 2 hechas y fusionadas en `feat/servidor-0.3.0`** (servidor, API, seguridad, cliente de escritorio, frontend y pruebas de extremo a extremo con el servidor real). **La oleada 3 está en curso**: la documentación es esta rama; los builds y los workflows de GitHub los hacen otros agentes y aún no están fusionados. **Falta**: builds finales, revisión adversarial, publicación y QA en vivo. Marcas: ✔ = existe en el código fusionado (o, para la documentación, en la rama de documentación de la oleada 3); ☐ = pendiente.

## Decisiones tomadas (12-11-2026)

- **Modo local = proceso aparte.** La app de escritorio lanza el `dconnector-server` incluido en el MSI, en loopback y con un token propio, y habla con él igual que con uno remoto. Un único camino de código.
- **Licencia: AGPL-3.0** (mautrix-meta es AGPL-3.0 y libsignal GPL-3.0, ambos enlazados).
- **Publicación:** repo público y Release en GitHub con `gh`, autorizado por el responsable del proyecto. Antes de subir: revisar que no haya datos personales, claves ni bases de datos; mostrar nombre del repo y lista de archivos.

## Entregables (todo desde el mismo código)

| Artefacto | Plataformas | Formato |
|---|---|---|
| App de escritorio (cliente) | Windows x86, x64, arm64 | MSI por arquitectura + zip portable |
| Servidor | Windows x64, arm64 · Linux x64, arm64 | zip / tar.gz, unidad systemd, Dockerfile |
| echo-server (ejemplo del protocolo) | las mismas | zip / tar.gz |
| `SHA256SUMS` | — | texto |

CGO desactivado en todo (SQLite es `modernc.org/sqlite`): se compila todo desde una sola máquina.

## Arquitectura

```
cmd/dconnector-server   ← sin ventana: núcleo + adaptadores de canal + almacén + API
cmd/dconnector          ← escritorio (Wails): UI + adaptadores de Windows (avisos, bandeja, insignia, autoarranque)
internal/adapter/api/   ← NUEVO: HTTP(S) + WebSocket, autenticación, subida/descarga de archivos
internal/core           ← sin cambios de firma (solo se añade)
```

- **API.** `POST /api/v1/rpc/{servicio}/{método}` con cuerpo JSON (array de argumentos) y respuesta JSON, generado por reflexión a partir de las mismas fachadas de `adapter/ui/wails` (≈108 métodos): un solo lugar donde viven. Eventos de `bus` → `GET /api/v1/events` (WebSocket) con el mismo catálogo de nombres que hoy emite Wails. Medios: `GET /api/v1/media/...` autenticado.
- **Archivos locales.** En modo remoto, adjuntos, campañas y estados suben el archivo a `POST /api/v1/files` y usan el id devuelto en lugar de una ruta. En modo local se sigue usando la ruta.
- **Autenticación.** Usuarios con clave (argon2id), sesiones con token de expiración, límite de intentos por IP/usuario, TLS obligatorio fuera de loopback (certificado propio, o detrás de proxy inverso). Primer arranque: crea el administrador y muestra la clave una sola vez. Nunca se registra una clave ni un token.
- **Frontend.** `frontend/src/api/index.ts` y `events.ts` pasan a tener dos transportes (bindings locales de Wails ya no; local y remoto usan el mismo cliente HTTP/WS). Pantalla «Conexión»: Local (por defecto) o Remoto (URL, usuario, clave, probar). Estado de conexión visible y reconexión automática con reintentos.
- **Lado Windows en el cliente.** `winnotify`, `taskbarbadge`, bandeja y autoarranque pasan a alimentarse del flujo de eventos del servidor (hoy escuchan el bus en proceso). El servicio `Notifications` se parte: la decisión (silenciado, visto…) sigue en el servidor y el cliente solo muestra.
- **Rutas de datos.** `internal/paths` se generaliza: Windows `%APPDATA%\dConnector`, Linux `$XDG_DATA_HOME/dconnector` o `/var/lib/dconnector` con `--data`; configuración por indicadores y variables de entorno (`DCONNECTOR_*`).
- **Servicio.** Linux: unidad systemd + Dockerfile (usuario sin privilegios, volumen de datos). Windows: modo servicio con `golang.org/x/sys/windows/svc` (`--service install|uninstall`).

## Fases

Lo que sigue es el estado real. ✔ solo donde el código existe en la rama integrada; no se marca nada por intención.

### 1. Servidor y API (cimientos) · hecha

- ✔ `cmd/dconnector-server` (arranque, apagado ordenado, `--version`, `--selftest`, subcomando `healthcheck`) y `internal/server`; `main.go` dividido en `cmd/dconnector` y `internal/app` (núcleo compartido).
- ✔ Rutas de datos multiplataforma (`internal/paths`: Windows `%APPDATA%\dConnector`, Linux `$XDG_DATA_HOME/dconnector` o `--data`) y configuración por indicadores y `DCONNECTOR_*` (`internal/config`).
- ✔ API (`internal/adapter/api`): RPC por reflexión sobre las fachadas neutras (`facade`), WebSocket de eventos con `hello`/`seq`/`notify`, medios, avatares, subida y descarga de archivos (`upload://<id>`), `/health`. Contrato en `docs/14-API-SERVIDOR.md`.
- ✔ Seguridad (`internal/adapter/api/auth`, `tlsconf`): usuarios con argon2id, sesiones con token, tickets de WebSocket y media-token, límite de intentos por IP y usuario, TLS con certificado propio autogenerado o externo (recarga con SIGHUP en Linux), reglas de loopback/`--no-tls`/`--insecure`, primer arranque con clave mostrada una vez, CLI `user add|passwd|list|disable|enable`.
- ✔ Pruebas de contrato y de seguridad (fuerza bruta, token caducado o manipulado, sin sesión, recorrido de rutas, origen del WebSocket, cabeceras, que no se registren claves) con el adaptador falso.
- ✔ Servicio de Windows (`--service install|uninstall|start|stop`), unidad de systemd, `Dockerfile`, `docker-compose` y variables de ejemplo en `deploy/`.

### 2. Cliente · hecha

- ✔ Frontend con transporte HTTP/WebSocket (`frontend/src/api`), subida de archivos en lugar de rutas en modo remoto, pantallas «Conexión» (Ajustes) / «Sin conexión» / «Iniciar sesión», huella TLS y reconexión automática con recarga del estado.
- ✔ Cliente Go del servidor (`internal/adapter/desktop/client`), administrador de conexión local/remoto con clave cifrada con DPAPI y huella TLS fijada (`connection`), lanzador del servidor local con ciclo de vida, reinicio y «seguir en segundo plano» (`localserver`).
- ✔ Lado Windows alimentado por eventos del servidor (`bridge`: avisos con `winnotify`, contador `taskbarbadge`, bandeja y arranque con Windows); la decisión de avisar vive en el servidor (`service.Notifications`).
- ✔ Pruebas con el servidor real (ciclo del lanzador, cliente remoto con TLS autofirmado, carpeta 0.2.x sintética).

### 3. Builds y publicación · pendiente (en curso en la oleada 3)

- ✔ Matriz de `Taskfile.yml` y scripts: escritorio x86, x64 y arm64; servidor Windows y Linux x64 y arm64; `echo-server`; `SHA256SUMS` (agente de builds, aún no fusionado).
- ✔ MSI por arquitectura (WiX 5.0.2, por usuario, mismo `UpgradeCode`) que incluya el servidor local; zips portables; paquetes del servidor (zip y tar.gz con unidad de systemd y ejemplo de configuración); imagen de Docker publicada (la receta ya está en `deploy/`).
- ✔ (escritos y validados con actionlint; falta verlos correr en GitHub) Workflows de GitHub Actions: `ci.yml` (Linux x64 y arm64 y Windows) y `release.yml` (al etiquetar `v*`).
- ✔ `task release` produciendo todo `dist/`, y que las comprobaciones automáticas de los paquetes estén en verde.
- ⚠ Escritorio x86: compila pero Wails v3.1.0 entra en pánico al arrancar en 32 bits (ver `docs/BITACORA.md`, oleada 3); `DESKTOP_ARCHS` por defecto es `x64 arm64`. Pendiente de decisión del dueño.

### 4. Documentación · hecha en la rama de la oleada 3

- ✔ `README.md` completo, `LICENSE` (AGPL-3.0, texto oficial), `SECURITY.md`, `CONTRIBUTING.md`, `THIRD-PARTY-NOTICES` y `LICENCIAS.txt` regenerados con los módulos reales (81 de Go y 10 de la interfaz) por `scripts/licencias.ps1`, `LEEME.txt`, `License.rtf` del MSI, manual (sección 18), lista de QA (sección M) y notas de la versión `docs/RELEASE-NOTES-v0.3.0.md`.
- ✔ Nombre del repositorio: `JoaquinZc/dconnector` (confirmado por el dueño el 13-11-2026).
- ☐ Capturas de pantalla sin datos personales (hay un marcador comentado en el README).

### 5. Revisión previa a publicar y publicación · pendiente

- ✔ Oleada 4: revisión adversarial (seguridad del servidor, concurrencia con `-race`, fugas de goroutines, errores, compatibilidad de datos 0.2.x), `task gate`, `staticcheck`, builds cruzados de las siete combinaciones, ejecución real de los `.exe` x86 y x64 y del servidor en Windows; subir `VERSION`, `info.json` y `package.json` a 0.3.0.
- ☐ Auditoría de privacidad del árbol y de TODO el historial (correos, teléfonos, ids de cuenta, tokens, rutas con el usuario de Windows, bases de datos, medios, capturas) y de los documentos de proceso (`PLAN.md`, `PROMPT-SESION.md`, `docs/PROMPT-SERVIDOR.md`), que mencionan rutas y nombres locales y no deben publicarse tal cual.
- ☐ Crear el repositorio público y subir la etiqueta, el Release `v0.3.0` con todos los artefactos y las notas; vigilar las Actions (Linux y arm64 se verifican ahí por primera vez).
- ☐ **QA en vivo** (`docs/QA-EN-VIVO.md`, secciones A–M): ninguna parte de red con cuentas reales de Telegram, Messenger e Instagram; servidor en Linux/arm64, TLS con proxy, dos clientes y notificaciones con cliente remoto sin probar en vivo.

## Riesgos y límites

- Un servidor en la nube con cuentas de mensajería es sensible: autenticación y TLS desde el primer día; el README advierte de no exponerlo sin TLS.
- Wails v3 con x86 y arm64 sin CGO: se compila, pero **arm64 y Linux no se pueden ejecutar en esta máquina**; esas pruebas corren en GitHub Actions (runners Linux x64 y arm64). El x86 de 32 bits sí se ejecuta aquí.
- No se prueba nunca con cuentas reales de WhatsApp/Telegram/Meta; solo adaptador falso y pruebas.
- La versión 0.2.x sigue funcionando: la migración de datos (carpeta `%APPDATA%\dConnector`) se conserva y el servidor local la reutiliza (verificado con una carpeta 0.2.x sintética; falta confirmarlo con una copia de datos reales en la QA en vivo).
- Licencia AGPL implica publicar el código de quien ofrezca el servidor como servicio; se documenta en el README.
