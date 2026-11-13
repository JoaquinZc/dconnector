# dConnector 0.3.0 · notas de la versión

**dConnector deja de ser solo una aplicación de escritorio: ahora hay un servidor independiente** que mantiene tus cuentas de WhatsApp, Telegram, Messenger e Instagram conectadas las 24 horas, y la aplicación de escritorio es su cliente. Se publica por primera vez como código abierto bajo la licencia **AGPL-3.0**.

> **Antes de nada:** Messenger e Instagram son una vía no oficial y Meta puede bloquear la cuenta. Un servidor con cuentas de mensajería es sensible: no lo expongas sin TLS ni con claves débiles. Léelo todo en el [README](../README.md) y en [`SECURITY.md`](../SECURITY.md).

## Qué hay de nuevo

### Servidor independiente (`dconnector-server`)

- Ejecutable sin ventana para **Windows y Linux, x64 y arm64**, sin dependencias (SQLite incluido, sin CGO).
- **API HTTPS** con llamadas a los mismos servicios de la interfaz, **eventos en vivo por WebSocket**, subida de archivos y descarga autenticada de medios. Contrato en [`docs/14-API-SERVIDOR.md`](14-API-SERVIDOR.md).
- **Usuarios y claves** (argon2id), sesiones con caducidad (24 horas sin actividad, 30 días como máximo), **límite de intentos** por IP y usuario, y comando `user add|passwd|list|disable|enable`.
- **Primer arranque:** crea el administrador y muestra su clave **una sola vez** (o usa `DCONNECTOR_ADMIN_PASSWORD`).
- **TLS por defecto:** certificado propio autogenerado con huella SHA-256, certificado externo (recarga con `SIGHUP` en Linux) o detrás de un proxy inverso (ejemplos de Caddy y nginx en el README). Fuera de loopback, el servidor se niega a arrancar sin TLS salvo `--insecure`.
- **Cómo se instala:** unidad de systemd con usuario sin privilegios (Linux), servicio de Windows (`--service install|uninstall|start|stop`) y Dockerfile con volumen `/data` y `HEALTHCHECK`.

### Aplicación de escritorio

- **Dos modos** (Ajustes › Conexión): **Local** (por defecto: lanza el servidor incluido, oculto, solo en tu equipo y con un token propio) o **Remoto** (te conectas por HTTPS a un servidor tuyo con usuario y clave).
- **Huella TLS fijada** («Confiar en este certificado») y aviso si el certificado de un servidor cambia. La clave del servidor remoto se guarda cifrada con DPAPI.
- **«Seguir en segundo plano»:** el servidor local sigue vivo al cerrar la aplicación, con las cuentas conectadas.
- **«Sin conexión» y reconexión automática:** si el servidor cae, la aplicación lo dice y reintenta sola, y al volver recarga el estado; nunca se queda en blanco.
- **Varios escritorios** contra el mismo servidor. Las **notificaciones** las decide el servidor (silenciado, ya visto, chat abierto en cualquiera de las conexiones) y cada escritorio solo muestra el aviso, suena y actualiza el contador de la barra de tareas.
- **Subida de archivos** al servidor cuando es remoto (hasta 100 MiB por archivo): mensajes, campañas, programados, estados e importación de CSV.
- **Windows x64 y arm64**: un instalador MSI y un portable por arquitectura. El escritorio de 32 bits (x86) no se ofrece en esta versión: Wails v3.1.0 no arranca en 32 bits.

### Código abierto

- Licencia **AGPL-3.0** (`LICENSE`), avisos de terceros (`THIRD-PARTY-NOTICES`), `SECURITY.md`, `CONTRIBUTING.md`, README completo y manual actualizado (sección «Conexión: local o servidor remoto»).

## Descargas

Todo está en [la página de Releases](https://github.com/JoaquinZc/dconnector/releases/tag/v0.3.0).

| Qué | Plataforma | Archivo |
| --- | --- | --- |
| Escritorio, instalador | Windows x64 / arm64 | `dConnector-0.3.0-x64.msi` · `dConnector-0.3.0-arm64.msi` |
| Escritorio, portable | Windows x64 / arm64 | `dConnector-0.3.0-portable-x64.zip` · `…-portable-arm64.zip` |
| Servidor | Windows x64 / arm64 | `dConnector-server-0.3.0-windows-x64.zip` · `…-windows-arm64.zip` |
| Servidor | Linux x64 / arm64 | `dConnector-server-0.3.0-linux-x64.tar.gz` · `…-linux-arm64.tar.gz` |
| echo-server (ejemplo del protocolo) | Windows y Linux, x64 / arm64 | `echo-server-0.3.0-<os>-<arq>…` |
| Sumas de comprobación | — | `SHA256SUMS` |

El MSI y el portable del escritorio **incluyen el servidor local**. Comprueba las descargas con `SHA256SUMS`. El instalador y los ejecutables **no están firmados**: SmartScreen avisará la primera vez («Más información» › «Ejecutar de todas formas»).

## Cómo actualizar desde 0.2.x

1. **Copia tu carpeta de datos** (`%APPDATA%\dConnector`, o `data\` si usas el portable) con dConnector cerrado («Salir» en la bandeja). Es la red de seguridad por si algo falla.
2. Instala el `.msi` de tu arquitectura **encima** (conserva los datos), o reemplaza los archivos del portable dejando `data\`.
3. Abre dConnector. Se lanza el servidor local y **abre tus cuentas tal cual**: no hay pasos manuales. En `app.db` se añaden tablas nuevas (usuarios y sesiones) sin tocar las existentes.
4. Si quieres usar un servidor aparte, instálalo con la guía del README y conéctalo en Ajustes › Conexión › Remoto. **Los datos de la carpeta local no se mueven solos al servidor remoto**: el servidor remoto empieza con su propia carpeta de datos y sus propias cuentas, que vinculas allí.

Desinstalar el MSI sigue sin borrar tus datos.

## Límites conocidos y qué no se probó

Dicho sin maquillaje (más detalle en el [README](../README.md#límites-conocidos-y-qué-no-se-probó) y en [`docs/BITACORA.md`](BITACORA.md)):

- **Cuentas reales:** solo WhatsApp se probó a mano con una cuenta real (serie 0.2.x). **Telegram, Messenger e Instagram no se han probado con cuentas reales.** Lo más frágil: chats cifrados de extremo a extremo de Messenger, Instagram (`instameow`), inicio con usuario y contraseña de Messenger, *floodwait* y sesión de Telegram, y los `@lid` de WhatsApp.
- **Linux y arm64:** el servidor se **compila** para ellos, pero no se había ejecutado allí al preparar esta versión; sus pruebas corren en la integración continua.
- **Interfaz:** revisada con pruebas automáticas y poco en una ventana real (claro/oscuro, selectores de fecha, diálogos de archivos, arrastrar y soltar, notas de voz).
- **Notificaciones de Windows** y el **MSI instalado de verdad** (actualizar encima, desinstalar conservando datos) tienen poca prueba manual.
- **Servidor:** los datos no están cifrados en disco; todos los usuarios son administradores; detrás de un proxy, el límite de intentos por IP se comparte entre todos (el servidor no confía en `X-Forwarded-For`); el certificado externo se recarga con `SIGHUP` solo en Linux.
- Riesgos conocidos del protocolo para destinos: `message.updated` tras descargar un medio no llega; `messages.list` y `message.send` en grupos ignoran `includeGroups=false`; `fileUrl` sigue redirecciones.
- Los estados de WhatsApp que ya existían al vincular solo se leen en el momento de vincular.
- Sin firma de código.
- **Licencia:** AGPL-3.0. Si ofreces dConnector como servicio en red a terceros, debes ofrecerles el código fuente de la versión que ejecutas (ver el README).

## Marcas

WhatsApp, Telegram, Messenger e Instagram son marcas de sus dueños. dConnector es independiente y no está afiliado a ellos; su uso con esos servicios es responsabilidad de cada usuario, que debe respetar sus términos.
