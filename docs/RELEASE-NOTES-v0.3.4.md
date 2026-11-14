# dConnector 0.3.4 · notas de la versión

Corrige un error que **impedía conectar la aplicación de escritorio a un servidor remoto**, y añade el ejemplo de Docker Compose con HTTPS automático.

## Qué cambia

### Corrección: conexión a un servidor remoto

- En **Ajustes → Conexión → Remoto**, «Guardar y conectar» fallaba con el mensaje `DesktopService.SaveRemote expects 3 arguments, got 1`: la interfaz enviaba los datos de conexión como un solo objeto y el escritorio esperaba la dirección, el usuario y la clave por separado. Afectaba a la 0.3.0 hasta la 0.3.3.
- Se añadió una prueba que compara, contra el código real de Go, los argumentos de **cada** llamada de la interfaz a los servicios del escritorio, para que un desajuste así no vuelva a pasar desapercibido.

### Docker

- Nuevo ejemplo para publicar el servidor con **HTTPS y certificado de Let's Encrypt automático** usando Caddy: `deploy/docker-compose.caddy.example.yml` y `deploy/Caddyfile.example`. El servidor por sí solo no obtiene certificados válidos; genera uno autofirmado.

## Actualizar

Si tienes la 0.3.2 o la 0.3.3, abre **Ajustes → Actualizaciones**, pulsa **Buscar actualizaciones** y luego «Actualizar a 0.3.4». Tus datos no se tocan. Con una versión anterior, instala el MSI de esta página a mano una vez.

## Límites conocidos

Los mismos de las versiones anteriores: el escritorio no está firmado (SmartScreen o Smart App Control pueden bloquearlo), no hay versión de 32 bits y nada de red se ha probado con cuentas reales de WhatsApp, Telegram, Messenger ni Instagram. Las cuentas vinculadas en modo local no se trasladan solas a un servidor remoto: hay que vincularlas de nuevo allí. Detalles en el [README](../README.md).

## Descargas

Todo está en [la página de Releases](https://github.com/JoaquinZc/dconnector/releases/tag/v0.3.4). Los nombres de archivo son los de siempre con `0.3.4`; `SHA256SUMS` lista las sumas de todos.
