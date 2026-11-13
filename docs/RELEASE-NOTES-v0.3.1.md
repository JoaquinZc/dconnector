# dConnector 0.3.1 · notas de la versión

Corrige el instalador de Windows (MSI) de la 0.3.0 y quita el parpadeo al abrir una pestaña por primera vez. **Si instalaste la 0.3.0 con el MSI y te pidió administrador, esta versión es la que lo arregla.**

## Qué cambia

### Instalador de Windows (MSI)

- **Se instala por usuario de verdad, sin administrador.** La 0.3.0 intentaba escribir en `C:\Program Files\dConnector` y mostraba «el instalador no dispone de privilegios suficientes». Ahora instala en `%LOCALAPPDATA%\Programs\dConnector`, crea el acceso directo y no toca tus datos (`%APPDATA%\dConnector`).
- Desinstalar elimina la carpeta del programa y los accesos directos y conserva tus datos.
- Probado instalando y desinstalando de verdad en Windows 11 (x64). El MSI de arm64 se verificó solo por sus tablas.
- El script de empaquetado comprueba ahora que la carpeta de instalación cuelga de `LocalAppData` y nunca de `Program Files`.

### Interfaz

- **Sin parpadeo al abrir una pestaña por primera vez.** El código de todas las páginas se descarga en segundo plano justo después de arrancar, y las páginas que ya visitaste se mantienen en memoria (volver a ellas es instantáneo). Programados y Ajustes se cierran al salir porque consultan al servidor por su cuenta.
- **Esqueletos de carga a la medida de cada pestaña** (Chats, Cuentas, Estados, Contactos, Campañas, Destinos, Programados, Estadísticas y Ajustes): el marco de la página aparece al instante y solo los datos esperan, sin saltos de maquetación al llegar. Los estados vacíos («Sin chats», «0 contactos»…) solo se muestran cuando la carga terminó de verdad.
- Si una carga falla, se muestra el error con «Reintentar» en lugar de quedarse cargando.
- Chats oculta ya no marca mensajes como leídos ni suprime avisos mientras miras otra pestaña.

## Actualizar desde la 0.3.0

Instala el MSI de la 0.3.1 encima (mismo identificador de producto) o descomprime el portable en otra carpeta. Si instalaste la 0.3.0 con permisos de administrador en `Program Files`, desinstálala primero desde «Aplicaciones instaladas».

## Límites conocidos

Los mismos de la 0.3.0: el escritorio sigue sin firmar (SmartScreen y, en Windows 11, Smart App Control pueden bloquearlo), no hay versión de 32 bits, y nada de red se ha probado con cuentas reales. Detalles en el [README](../README.md) y en `docs/QA-EN-VIVO.md`.

## Descargas

Todo está en [la página de Releases](https://github.com/JoaquinZc/dconnector/releases/tag/v0.3.1). Los nombres de archivo son los de la 0.3.0 con `0.3.1`; `SHA256SUMS` lista las sumas de todos.
