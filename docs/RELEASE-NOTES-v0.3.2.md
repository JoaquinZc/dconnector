# dConnector 0.3.2 · notas de la versión

Añade el **botón para actualizar la aplicación** desde esta misma página de Releases y corrige un error de «Demasiadas peticiones» que podía bloquear hasta un envío de mensaje.

## Qué hay de nuevo

### Actualizar con un solo botón (escritorio de Windows)

- **Ajustes → Actualizaciones**: muestra la versión instalada y un botón **Buscar actualizaciones**. Si hay una versión nueva, «Actualizar a X.Y.Z» la descarga, **verifica su SHA-256** contra el archivo `SHA256SUMS` de esa misma Release, cierra la aplicación y el servidor local, instala encima **sin administrador** y vuelve a abrir dConnector. Tus datos no se tocan.
- Funciona con el instalador (MSI) y con la versión portable (conserva su carpeta de datos). Si dConnector está en otra ubicación (por ejemplo, compilado por ti), solo ofrece abrir la página de la versión.
- **Al abrir** la aplicación comprueba una vez, como máximo cada 24 horas, si hay una versión nueva y muestra un aviso discreto con «Actualizar» y «Más tarde». Se puede desactivar en Ajustes («Buscar al abrir»). La consulta va a la API pública de GitHub y no envía ningún dato tuyo.
- Seguridad: solo HTTPS y solo desde los servidores de GitHub; el archivo se verifica antes de ejecutarlo o extraerlo y se descarta si no coincide; el portable se extrae con protección contra rutas maliciosas y con marcha atrás si algo falla.
- **Sin firma digital todavía.** Windows (SmartScreen o Smart App Control) puede bloquear el instalador o el programa actualizado. Si pasa, la aplicación explica qué ocurrió y deja el instalador descargado con un botón «Abrir carpeta». Firmar los ejecutables queda pendiente.

### Corrección: «Demasiadas peticiones»

- La aplicación de escritorio compartía un único cupo de 600 peticiones por minuto con todas sus llamadas locales, y al agotarlo rechazaba incluso enviar un mensaje. Ahora **el token local del escritorio no se limita** (el límite sigue protegiendo a los clientes remotos, con un tope de 3000 por minuto).
- Si aun así el servidor responde «demasiadas peticiones» con una espera corta, la interfaz **reintenta sola y en silencio** (es seguro: el servidor rechaza antes de ejecutar nada). Los avisos de error idénticos seguidos se agrupan en uno.
- Si se alcanza el tope, el servidor registra qué rutas lo consumieron (solo rutas, nunca datos) para poder localizar el origen.

## Actualizar desde la 0.3.1 o anterior

La 0.3.1 y las anteriores no tienen el botón, así que **esta vez hay que instalar a mano**: descarga el MSI de tu arquitectura (o el portable) desde esta página e instálalo encima. Desde la 0.3.2 en adelante podrás actualizar desde Ajustes.

## Límites conocidos

Los mismos de la 0.3.0 y la 0.3.1: el escritorio no está firmado, no hay versión de 32 bits y nada de red se ha probado con cuentas reales de WhatsApp, Telegram, Messenger ni Instagram. El actualizador se probó de punta a punta contra un servidor de pruebas, con instalador y portable, pero **todavía no contra una Release real de GitHub** (esta es la primera con botón). Detalles en el [README](../README.md) y en `docs/QA-EN-VIVO.md`.

## Descargas

Todo está en [la página de Releases](https://github.com/JoaquinZc/dconnector/releases/tag/v0.3.2). Los nombres de archivo son los de siempre con `0.3.2`; `SHA256SUMS` lista las sumas de todos.
