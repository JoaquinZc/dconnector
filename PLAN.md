# dConnector v2 · plan maestro (11-11-2026)

App de escritorio para Windows, **local y sin cuentas de usuario**, que reúne varias cuentas de
WhatsApp, Telegram, Messenger e Instagram en una sola bandeja con aspecto de WhatsApp, y que habla
con servidores externos por un protocolo propio (WebSocket) para que reciban los mensajes y actúen
por el canal correcto.

- Código nuevo: **esta carpeta** (`dconnector-v2/`).
- `../dconnector/` es la base de D'Ventas (Electron + whatsapp-web.js): **solo se lee** como
  referencia de funciones, lógica e interfaz. Nunca se modifica ni se compila.

## Cómo se usa este plan

El plan está escrito para que quien lo ejecute **no tome decisiones de diseño**: cada tipo, interfaz,
tabla, mensaje del protocolo, pantalla y paso de empaquetado está fijado. Si algo no está escrito,
se resuelve con la regla de la sección «Reglas para el ejecutor» y se anota en `docs/BITACORA.md`.

| Documento | Contenido |
| --- | --- |
| [docs/01-DECISIONES.md](docs/01-DECISIONES.md) | Todas las decisiones, con versión y motivo |
| [docs/02-ARQUITECTURA.md](docs/02-ARQUITECTURA.md) | Árbol completo de carpetas y archivos, dependencias permitidas, ciclo de vida, concurrencia, errores, registro |
| [docs/03-DOMINIO.md](docs/03-DOMINIO.md) | Código Go literal de `core/model` y `core/capability` |
| [docs/04-PUERTOS.md](docs/04-PUERTOS.md) | Código Go literal de `core/port` (adaptadores, almacenes, eventos) |
| [docs/05-ALMACEN.md](docs/05-ALMACEN.md) | SQL de `app.db` y `account.db`, migraciones, rutas de datos |
| [docs/06-SERVICIOS.md](docs/06-SERVICIOS.md) | Servicios del núcleo: qué hace cada método |
| [docs/07-ADAPTADOR-WHATSAPP.md](docs/07-ADAPTADOR-WHATSAPP.md) | whatsmeow → modelo único |
| [docs/08-ADAPTADOR-TELEGRAM.md](docs/08-ADAPTADOR-TELEGRAM.md) | gotd → modelo único |
| [docs/09-ADAPTADOR-META.md](docs/09-ADAPTADOR-META.md) | Messenger e Instagram (messagix) → modelo único |
| [docs/10-PROTOCOLO-v1.md](docs/10-PROTOCOLO-v1.md) | Protocolo dConnector v1 completo |
| [docs/11-INTERFAZ.md](docs/11-INTERFAZ.md) | Servicios Wails (enlaces), eventos en vivo, pantallas y componentes |
| [docs/12-EMPAQUETADO.md](docs/12-EMPAQUETADO.md) | Carpeta portable (.zip) e instalador MSI |
| [docs/13-OLEADAS.md](docs/13-OLEADAS.md) | Oleadas de trabajo, agentes, archivos de cada uno y criterios de cierre |
| [PROMPT-SESION.md](PROMPT-SESION.md) | Prompt para arrancar la sesión de construcción |

## Resultado final

1. `dist/dConnector-<versión>-portable-x64.zip`: carpeta que se descomprime y se ejecuta sin
   instalar (`dConnector.exe` + `portable.txt`; los datos quedan en `data\` junto al .exe).
2. `dist/dConnector-<versión>-x64.msi`: instalador por usuario, con acceso directo, actualización
   encima de versiones anteriores y desinstalación que **conserva los datos**.
3. Las cuatro plataformas como adaptadores, la bandeja unificada, estados, campañas, programados,
   contactos y listas, estadísticas, destinos del protocolo y servidor de ejemplo.
4. `docs/MANUAL.md` (uso) y `docs/QA-EN-VIVO.md` (lista de comprobación que hace el dueño con sus
   cuentas reales).

## Reglas para el ejecutor

1. **El plan manda.** Nombres, firmas, tablas y textos se copian tal cual. Si una librería externa
   tiene otra firma que la supuesta aquí, se adapta **dentro del adaptador** sin cambiar el puerto,
   y se anota en `docs/BITACORA.md`.
2. **Antes de usar una API externa**, se lee su firma real con `go doc <paquete>.<Símbolo>` (o el
   código en `%GOPATH%\pkg\mod`). No se escribe de memoria.
3. El núcleo (`internal/core`) **no importa** nada de `internal/adapter` ni librerías de canal.
4. **Nadie toca `go.mod`, `go.sum`, `frontend/package.json` ni `pnpm-lock.yaml` salvo el
   orquestador.** Un agente que necesite una dependencia nueva lo dice en su informe.
5. Cada agente toca **solo** los archivos que le asigna `docs/13-OLEADAS.md`.
6. Identificadores de código en inglés; comentarios, textos de interfaz, documentos y mensajes de
   error visibles en español.
7. Pruebas: unitarias con `go test` y `vitest`. **No se usan cuentas reales** de WhatsApp, Telegram
   ni Meta: los adaptadores se prueban con sus funciones de traducción (estructuras de la librería →
   modelo) y con el adaptador falso. La prueba en vivo es del dueño (`docs/QA-EN-VIVO.md`).
8. Si algo no está en el plan: se elige lo más simple que cumpla los puertos, se anota en la
   bitácora y se sigue. No se detiene la construcción por eso.
9. Git local en `dconnector-v2/`: un commit al cerrar cada oleada. **Sin remoto y sin push.**
