# dConnector 0.3.3 · notas de la versión

Versión pequeña: documenta y simplifica el uso de la **imagen de Docker Hub** y es la primera que puedes instalar **con el botón de actualizar** desde la 0.3.2.

## Qué cambia

### Actualizar desde la 0.3.2

- Si tienes la **0.3.2**, abre **Ajustes → Actualizaciones** y pulsa **Buscar actualizaciones**: aparecerá la 0.3.3. «Actualizar a 0.3.3» la descarga, verifica su SHA-256, la instala encima sin administrador y vuelve a abrir la aplicación. Tus datos no se tocan.
- Si tienes la 0.3.1 o anterior, instala el MSI de esta página a mano una vez; después ya podrás actualizar desde Ajustes.

### Docker

- La imagen del servidor está publicada en **Docker Hub**: `jassnoob/dconnector-server`, para `linux/amd64` y `linux/arm64`, con las etiquetas `latest` y la de cada versión (`0.3.3`).
- El `docker-compose.example.yml` ahora es **autónomo**: se copia solo, sin el código fuente, y usa `jassnoob/dconnector-server:latest`. Para actualizar: `docker compose pull` y `docker compose up -d`.
- El README explica `docker run` con la imagen publicada y cómo construirla tú mismo.

## Límites conocidos

Los mismos de las versiones anteriores: el escritorio no está firmado (SmartScreen o Smart App Control pueden bloquearlo; el actualizador lo explica si ocurre), no hay versión de 32 bits y nada de red se ha probado con cuentas reales de WhatsApp, Telegram, Messenger ni Instagram. Detalles en el [README](../README.md).

## Descargas

Todo está en [la página de Releases](https://github.com/JoaquinZc/dconnector/releases/tag/v0.3.3). Los nombres de archivo son los de siempre con `0.3.3`; `SHA256SUMS` lista las sumas de todos.
