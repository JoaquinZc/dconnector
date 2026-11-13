# dConnector

**dConnector reúne varias cuentas de WhatsApp, Telegram, Messenger e Instagram en una sola bandeja de chats**, con aspecto de WhatsApp Web, y las pone al alcance de tus propios servidores (un bot, una IA, un almacén de mensajes) mediante un protocolo abierto por WebSocket.

Desde la versión **0.3.0** son dos piezas del mismo código:

- **dConnector (escritorio, Windows):** la aplicación con ventana. Funciona sola (arranca su propio servidor local) o como cliente de un servidor remoto.
- **dconnector-server (Windows y Linux):** el servidor sin ventana. Mantiene las cuentas conectadas las 24 horas, guarda los datos y ofrece una API HTTPS protegida con usuario y clave. Pensado para una máquina que está siempre encendida: un VPS, un servidor propio, Docker.

Es para ti si atiendes clientes o comunidades por varios canales y quieres verlos juntos, programar mensajes, hacer campañas con envío prudente o conectar tus mensajes a un servidor tuyo, sin entregar tus conversaciones a un servicio de terceros: **los datos viven en tu equipo o en tu servidor**. No hay telemetría; las únicas llamadas de red son las de los canales, las de los destinos que configures y la consulta opcional de versiones nuevas a GitHub (se puede desactivar).

<!-- capturas: pendiente. Se añadirán a docs/img/ solo cuando haya capturas sin datos personales (ni chats, ni nombres, ni números). -->

> **Advertencia: Messenger e Instagram son una vía no oficial.** dConnector imita al navegador de Meta con las cookies de una sesión ya iniciada (librerías `messagix` e `instameow` de mautrix-meta). **Meta puede bloquear o suspender la cuenta.** Úsalo solo con cuentas que puedas permitirte perder. La interfaz lo avisa al añadir la cuenta (casilla «Entiendo» obligatoria).

> **Estado, con honestidad:** todo está cubierto con pruebas automáticas y un adaptador falso. WhatsApp se probó a mano con una cuenta real durante la serie 0.2.x; **Telegram, Messenger e Instagram no se han probado con cuentas reales**, y **el servidor no se ha ejecutado todavía en Linux ni en arm64** (solo se compila para ellos; sus pruebas corren en la integración continua). Los detalles están en [Límites conocidos](#límites-conocidos-y-qué-no-se-probó).

## Contenido

1. [Qué incluye](#qué-incluye)
2. [Descargas](#descargas)
3. [Instalar el escritorio (Windows)](#instalar-el-escritorio-windows)
4. [Modo local y modo remoto](#modo-local-y-modo-remoto)
5. [Instalar el servidor](#instalar-el-servidor)
6. [Primer arranque y clave del administrador](#primer-arranque-y-clave-del-administrador)
7. [Conectar el escritorio a un servidor remoto](#conectar-el-escritorio-a-un-servidor-remoto)
8. [TLS: certificado propio, certificado externo y proxy inverso](#tls-certificado-propio-certificado-externo-y-proxy-inverso)
9. [Seguridad y buenas prácticas](#seguridad-y-buenas-prácticas)
10. [Indicadores y variables de entorno](#indicadores-y-variables-de-entorno)
11. [Actualizar](#actualizar)
12. [Copias de seguridad](#copias-de-seguridad)
13. [Protocolo para servidores externos](#protocolo-para-servidores-externos)
14. [API del servidor](#api-del-servidor)
15. [Compilar desde el código](#compilar-desde-el-código)
16. [Límites conocidos y qué no se probó](#límites-conocidos-y-qué-no-se-probó)
17. [Licencia](#licencia)
18. [Marcas](#marcas)
19. [Documentación](#documentación)

## Qué incluye

- **Chats:** bandeja unificada de todas las cuentas, con filtro por cuenta y canal; texto, fotos, vídeo, notas de voz, audio, documentos, stickers, ubicación, contactos y encuestas; citas, reacciones, editar, borrar, reenviar, «escribiendo», leído y búsqueda global (`Ctrl+K`).
- **Estados** de WhatsApp y Telegram: ver, publicar, programar y borrar, con vistas y estadísticas.
- **Campañas** (WhatsApp y Telegram) con envío prudente: ritmo anti-bloqueo, tandas, calentamiento, freno automático, pie de baja, variantes y prueba A/B.
- **Programados** (también recurrentes), **Contactos y listas** (importar de una cuenta o de un CSV, unir y separar identidades) y **Estadísticas**.
- **Destinos:** protocolo `dConnector v1` por WebSocket, con filtros, `ack`, entrega al menos una vez (cola persistente) y comandos opcionales («Permitir comandos»). Incluye un servidor de ejemplo, `echo-server`.
- **Servidor independiente:** API HTTPS con usuarios (argon2id), sesiones con caducidad, límite de intentos, TLS y eventos en vivo por WebSocket.
- **Escritorio:** bandeja del sistema, cerrar a la bandeja, notificaciones de Windows con sonido propio, contador en la barra de tareas, arranque con Windows e instancia única.

Qué no es: no hay catálogo, pedidos, cobros, IA ni agenda; solo chats, estados, campañas, programados, contactos y estadísticas.

## Descargas

Todo se publica en [Releases](https://github.com/JoaquinZc/dconnector/releases/latest). En la lista, `<versión>` es el número de versión (por ejemplo `0.3.0`).

| Qué | Plataforma | Archivo |
| --- | --- | --- |
| **Escritorio**, instalador | Windows x64 | `dConnector-<versión>-x64.msi` |
| | Windows arm64 | `dConnector-<versión>-arm64.msi` |
| **Escritorio**, portable | Windows x64 / arm64 | `dConnector-<versión>-portable-x64.zip` · `…-portable-arm64.zip` |
| **Servidor** | Windows x64 / arm64 | `dConnector-server-<versión>-windows-x64.zip` · `…-windows-arm64.zip` |
| | Linux x64 / arm64 | `dConnector-server-<versión>-linux-x64.tar.gz` · `…-linux-arm64.tar.gz` |
| **echo-server** (ejemplo del protocolo) | Windows y Linux, x64 / arm64 | `echo-server-<versión>-<os>-<arq>…` |
| Sumas de comprobación | — | `SHA256SUMS` |

El MSI y el portable del escritorio **incluyen el servidor local**: no hace falta descargar nada más para usarlo en un solo equipo.

Comprueba la descarga con `SHA256SUMS`:

```powershell
# Windows (PowerShell)
Get-FileHash .\dConnector-<versión>-x64.msi -Algorithm SHA256
```

```sh
# Linux
sha256sum -c SHA256SUMS --ignore-missing
```

## Instalar el escritorio (Windows)

Requisitos: Windows 10 u 11 y **WebView2** (ya viene en Windows 10 y 11 al día). Hay un MSI y un portable por cada arquitectura: elige el de la tuya (en caso de duda, x64; los equipos con procesador Snapdragon u otro ARM de Windows usan arm64).

**Instalador (`.msi`).** Doble clic. Se instala **por usuario** en `%LOCALAPPDATA%\Programs\dConnector` y **no pide permisos de administrador**. Crea accesos directos «dConnector» en el menú Inicio y el Escritorio.

**Portable (`.zip`).** Descomprímelo en una carpeta donde puedas escribir (no «Archivos de programa») y abre `dConnector.exe`. La presencia de `portable.txt` junto al `.exe` hace que los datos vayan a la carpeta `data\` de al lado. Para moverlo, sal de dConnector y mueve la carpeta entera.

**SmartScreen (sin firma de código).** Ni el instalador ni los ejecutables están firmados: Windows mostrará «Windows protegió su PC» la primera vez. Pulsa **«Más información» › «Ejecutar de todas formas»**. Es normal; si prefieres no confiar a ciegas, compara la suma SHA-256 del archivo con `SHA256SUMS` o compila tú mismo desde el código.

**Dónde quedan tus datos.** Con el MSI, en `%APPDATA%\dConnector`; con el portable, en `data\`. Allí están las bases de datos (`app.db`, y una `account.db` por cuenta), los medios descargados y los registros. **No están cifrados**: quien tenga acceso a tu usuario de Windows puede leerlos.

**Actualizar.** Ejecuta el `.msi` nuevo: se instala encima y conserva los datos. También puedes actualizar desde la propia aplicación (**Ajustes › Actualizaciones**). Ver [Actualizar](#actualizar).

**Desinstalar.** «Configuración › Aplicaciones › Aplicaciones instaladas › dConnector › Desinstalar». **No borra tus datos** (`%APPDATA%\dConnector`): si los quieres borrar, elimina esa carpeta a mano con dConnector cerrado. En el portable, borra la carpeta.

Línea de comandos del ejecutable de escritorio:

| Opción | Qué hace |
| --- | --- |
| `--version` | Imprime la versión y sale |
| `--minimized` | Arranca oculto en la bandeja (lo usa «Iniciar con Windows») |
| `--selftest` | Sin ventana: resuelve la carpeta de datos, arranca el servidor local, lo comprueba, imprime `OK` y sale con 0 |
| `--require-server` | Con `--selftest`, falla si no encuentra `dConnector-server` junto al ejecutable (paquetes de instalación) |
| `--server-exe <ruta>` | Ruta del servidor local, para desarrollo (también `DCONNECTOR_SERVER_EXE`) |
| `--out <archivo>` | Con `--version` o `--selftest`, escribe la salida en un archivo (el `.exe` no tiene consola) |

La variable `DCONNECTOR_DATA` fuerza la carpeta de datos (para pruebas) y gana al modo portable y a `%APPDATA%`.

## Modo local y modo remoto

La aplicación de escritorio siempre habla con **un servidor**; lo único que cambia es cuál.

- **Local (por defecto).** Al abrir dConnector se lanza, oculto, el **servidor local** (`dConnector-server.exe`, incluido junto a la aplicación). Escucha **solo en loopback** (`127.0.0.1`, puerto efímero, sin TLS) y la aplicación se identifica con un **token aleatorio** que le pasa por variable de entorno (nunca por argumento) y que no se guarda. Usa la misma carpeta de datos de siempre, así que las cuentas de 0.2.x se abren sin pasos manuales. No hay usuarios ni claves que gestionar. Al salir de dConnector («Salir» en la bandeja) el servidor local se apaga, salvo que actives **«Seguir en segundo plano»** (Ajustes › Conexión): entonces sigue vivo, con las cuentas conectadas, aunque cierres la aplicación.
- **Remoto.** La aplicación se conecta por HTTPS a un `dconnector-server` que tú instalaste en otra máquina (ver [Instalar el servidor](#instalar-el-servidor)). Entonces las cuentas, los chats y los envíos viven en el servidor; el escritorio es solo la ventana y los avisos de Windows. Los datos locales no se tocan ni se borran: puedes volver a «Usar este equipo» cuando quieras.

Se elige en **Ajustes › Conexión**. Si el servidor cae o se corta la red, la aplicación muestra **«Sin conexión»** y reintenta sola (1, 2, 4, 8, 16 y 30 segundos), sin quedarse en blanco.

Varios escritorios pueden conectarse al mismo servidor a la vez. Las notificaciones las decide el servidor (silenciado, ya visto, chat abierto en cualquiera de las conexiones) y cada escritorio solo muestra el aviso y suena.

## Instalar el servidor

El servidor es un único ejecutable (`dconnector-server`) sin dependencias (SQLite incluido, sin CGO). Guarda todo en una **carpeta de datos**: `--data <carpeta>` o `DCONNECTOR_DATA`. Por defecto: en Windows `%APPDATA%\dConnector`; en Linux `$XDG_DATA_HOME/dconnector` o `~/.local/share/dconnector`. En un servidor conviene fijarla explícitamente (`/var/lib/dconnector`, `/data` en Docker, `%ProgramData%\dConnector` en el servicio de Windows).

Por defecto escucha en `127.0.0.1:8443` y sirve **HTTPS**. Para aceptar conexiones de otras máquinas hay que decirlo con `--listen 0.0.0.0:8443` (y entonces el TLS es obligatorio; ver [TLS](#tls-certificado-propio-certificado-externo-y-proxy-inverso)).

### Linux: tar.gz y systemd

Se explica con un usuario del sistema sin privilegios. Sustituye `<versión>` y, en un equipo ARM, `x64` por `arm64`. Los comandos de `sudo` son los únicos que necesitan permisos de administrador.

```sh
# 1. Descargar y comprobar
tar -xzf dConnector-server-<versión>-linux-x64.tar.gz
cd dConnector-server-<versión>-linux-x64        # (el nombre de la carpeta puede variar)

# 2. Usuario sin privilegios y sin shell
sudo useradd --system --home-dir /var/lib/dconnector --shell /usr/sbin/nologin dconnector

# 3. Ejecutable
sudo install -m 0755 dconnector-server /usr/local/bin/dconnector-server

# 4. Configuración (solo la lee root y el grupo dconnector)
sudo install -d -m 0750 -o root -g dconnector /etc/dconnector
sudo install -m 0640 -o root -g dconnector dconnector.env.example /etc/dconnector/dconnector.env

# 5. Antes del primer arranque, edita /etc/dconnector/dconnector.env:
#    - pon DCONNECTOR_ADMIN_PASSWORD con una clave larga (12 caracteres o más), o déjala vacía
#      y el servidor generará una y la mostrará UNA vez (paso 7);
#    - DCONNECTOR_LISTEN, DCONNECTOR_TLS_CERT / DCONNECTOR_TLS_KEY, etc. (ver la tabla de variables).
sudoedit /etc/dconnector/dconnector.env

# 6. Unidad de systemd y arranque
sudo install -m 0644 dconnector.service /etc/systemd/system/dconnector.service
sudo systemctl daemon-reload
sudo systemctl enable --now dconnector

# 7. Estado, huella TLS y (si no pusiste clave) la clave generada del administrador
systemctl status dconnector
sudo journalctl -u dconnector --no-pager | head -40
```

`dconnector.service` y `dconnector.env.example` también están en la carpeta [`deploy/`](deploy/) del repositorio (en el paquete van junto al ejecutable). La unidad guarda los datos en `/var/lib/dconnector` (la crea systemd con dueño `dconnector`), endurece el proceso (`NoNewPrivileges`, `ProtectSystem=strict`, sin capacidades, etc.), reinicia ante fallos y recarga el certificado externo con `systemctl reload dconnector` (señal `SIGHUP`). Para escuchar en el puerto 443 sin root, descomenta en la unidad las dos líneas de `CAP_NET_BIND_SERVICE` y usa `--listen 0.0.0.0:443`.

Comprobar que responde (con el certificado propio, `-k` omite la verificación; la huella la ves en el registro):

```sh
curl -k https://127.0.0.1:8443/api/v1/health
# {"status":"ok","version":"…","apiVersion":1,"protocol":1,"tls":true}
```

Registro: `sudo journalctl -u dconnector -f`. Además el servidor escribe un archivo por día en `/var/lib/dconnector/logs/`.

### Windows: zip y servicio

1. Descomprime `dConnector-server-<versión>-windows-x64.zip` (o `arm64`) en una carpeta fija, por ejemplo `C:\dConnector-server`.
2. Abre **PowerShell como administrador** en esa carpeta.
3. Crea el administrador **antes** de instalar el servicio (el servicio no tiene consola donde mostrar una clave generada):

   ```powershell
   .\dconnector-server.exe --data "C:\ProgramData\dConnector" user add admin
   ```

   Te pide la clave dos veces, sin eco (12 caracteres o más).
4. Instala, inicia y comprueba:

   ```powershell
   .\dconnector-server.exe --service install --listen 0.0.0.0:8443
   .\dconnector-server.exe --service start
   ```

   Sin `--data`, el servicio usa `%ProgramData%\dConnector`. Los parámetros que pasaste a `--service install` (`--listen`, `--data`, `--tls-cert`, `--tls-key`, `--no-tls`, `--insecure`, `--verbose`, `--admin-user`, `--allow-origin`) quedan guardados en la línea de comandos del servicio; el token local y las claves **nunca** se guardan.
5. El servicio se llama `dConnectorServer` («dConnector Servidor»), arranca con Windows (inicio automático retrasado) y se reinicia solo si falla. Detener y quitar:

   ```powershell
   .\dconnector-server.exe --service stop
   .\dconnector-server.exe --service uninstall     # no toca los datos
   ```

6. La **huella del certificado** propio queda en el registro de la carpeta de datos (`C:\ProgramData\dConnector\logs\`, línea «huella TLS»). Abre el puerto 8443 en el Firewall de Windows solo hacia quien deba conectarse.

También puedes ejecutarlo en primer plano, sin servicio: `.\dconnector-server.exe --listen 0.0.0.0:8443` (cierra con `Ctrl+C`).

### Docker

La imagen está publicada en **Docker Hub**: [`jassnoob/dconnector-server`](https://hub.docker.com/r/jassnoob/dconnector-server), para `linux/amd64` y `linux/arm64` (Docker elige la tuya), con las etiquetas `latest` y la de cada versión (por ejemplo `0.3.2`). Es una imagen mínima `distroless` (sin shell), con usuario sin privilegios `65532`, volumen `/data` y `HEALTHCHECK` incluido. También puedes construirla desde el código con el [`deploy/Dockerfile`](deploy/Dockerfile).

```sh
docker run -d --name dconnector --restart unless-stopped \
  -p 8443:8443 \
  -v dconnector-data:/data \
  -e DCONNECTOR_ADMIN_PASSWORD='una-clave-larga-y-unica' \
  -e TZ=America/Guayaquil \
  jassnoob/dconnector-server:latest
```

Para construirla tú:

```sh
docker build -f deploy/Dockerfile --build-arg VERSION=0.3.2 -t dconnector-server:0.3.2 .

docker run -d --name dconnector --restart unless-stopped \
  -p 8443:8443 \
  -v dconnector-data:/data \
  -e DCONNECTOR_ADMIN_PASSWORD='una-clave-larga-y-unica' \
  -e TZ=America/Guayaquil \
  dconnector-server:0.3.2

docker logs dconnector            # huella TLS y, si no pusiste clave, la clave generada
```

Con Compose, usa [`deploy/docker-compose.example.yml`](deploy/docker-compose.example.yml) (copia, ajusta y `docker compose up -d`): trae el volumen, `read_only`, `cap_drop: ALL` y `no-new-privileges`. Dentro del contenedor el servidor escucha en `0.0.0.0:8443` con TLS propio; publica el puerto solo en la interfaz que necesites (`127.0.0.1:8443:8443` si hay un proxy en la misma máquina). Para administrar usuarios: `docker exec -it dconnector /dconnector-server user passwd admin`.

## Primer arranque y clave del administrador

Si la carpeta de datos **no tiene usuarios**, el primer arranque crea un administrador:

- El nombre es `admin`, o el de `--admin-user` / `DCONNECTOR_ADMIN_USER`.
- Si defines **`DCONNECTOR_ADMIN_PASSWORD`** (12 caracteres o más), esa es la clave y no se imprime nada.
- Si no, el servidor **genera una clave aleatoria de 24 caracteres** y la imprime **una sola vez** en la salida estándar (consola, `journalctl`, `docker logs`):

  ```
  Administrador creado. Usuario: admin  Clave: …
  Guarde esta clave ahora: no se volverá a mostrar.
  ```

  La clave **nunca se escribe en el archivo de registro** del servidor (aunque con systemd y Docker la salida estándar sí queda en el registro del sistema o del contenedor: borra o rota esos registros tras anotarla, o usa `DCONNECTOR_ADMIN_PASSWORD`).
- En el servicio de Windows no hay consola: crea el administrador antes con `user add`.
- El servidor guarda solo el hash argon2id; no puede recuperar la clave.

Para cambiar la clave o recuperar el acceso, en la misma máquina y con la misma carpeta de datos (el servidor puede estar encendido):

```sh
dconnector-server --data /var/lib/dconnector user passwd admin       # pide la clave nueva, sin eco
dconnector-server --data /var/lib/dconnector user add maria          # otro usuario
dconnector-server --data /var/lib/dconnector user list
dconnector-server --data /var/lib/dconnector user disable maria      # y user enable maria
```

(En Linux con systemd: `sudo -u dconnector dconnector-server --data /var/lib/dconnector user passwd admin`.) Los indicadores van **antes** de `user`. `--password-stdin` lee la clave de una línea de la entrada estándar, útil en scripts. Cambiar la clave o desactivar un usuario **cierra todas sus sesiones**. Todos los usuarios son administradores en esta versión y ven las mismas cuentas.

## Conectar el escritorio a un servidor remoto

1. En el escritorio abre **Ajustes › Conexión** y elige **Remoto**.
2. **Dirección del servidor:** `https://servidor:8443` (o `https://dconnector.ejemplo.com` si está detrás de un proxy). Sin esquema se asume `https`. `http` solo se acepta en loopback: la clave no viaja sin cifrar.
3. **Usuario** y **Clave** del servidor.
4. Pulsa **«Guardar y conectar»**.
5. **Huella del certificado.** Si el servidor usa su certificado propio (autofirmado), la aplicación no puede verificarlo con el sistema y muestra **«Certificado sin confirmar»** con su **huella SHA-256**. Compárala con la que el servidor imprimió al arrancar (línea «Huella TLS (SHA-256)») y, si coincide, pulsa **«Confiar en este certificado»**. A partir de ahí la huella queda fijada para ese servidor (confianza al primer uso). Si más adelante cambia, la aplicación **no se conecta** y avisa: «El certificado del servidor cambió»; confía en la nueva solo si fuiste tú quien lo renovó. Con un certificado emitido por una autoridad reconocida (por ejemplo con proxy y Let's Encrypt) no hay nada que confirmar.
6. Para volver al servidor del propio equipo: **«Usar este equipo»**.

La clave del servidor remoto se guarda en `desktop.json`, dentro de la carpeta de datos del escritorio, **cifrada con DPAPI de Windows** (solo la puede abrir tu usuario de Windows en ese equipo).

## TLS: certificado propio, certificado externo y proxy inverso

Reglas del servidor (las comprueba al arrancar y se niega si no se cumplen):

- Por defecto sirve **HTTPS** (TLS 1.2 como mínimo).
- Si `--listen` **no es loopback** (cualquier cosa que no sea `127.0.0.1`, `::1` o `localhost`), el TLS es **obligatorio**. Con `--no-tls` el servidor **se niega a arrancar**, salvo que añadas `--insecure`.
- `--no-tls` sin `--insecure` solo funciona en loopback (`--listen 127.0.0.1:8443 --no-tls`): es lo que se usa con un **proxy inverso en la misma máquina**.
- `--insecure` permite escuchar fuera de loopback sin TLS (por ejemplo un proxy en otro contenedor o equipo de una red interna de confianza) y escribe un **aviso cada minuto** en el registro. No lo uses para exponer el servidor a Internet.
- `--no-tls` no se combina con `--tls-cert`/`--tls-key`, y estas dos van siempre juntas.

### Certificado propio (por defecto)

Sin `--tls-cert`, el servidor genera un certificado propio en `<datos>/tls/` (ECDSA P-256, válido 1 año, con `localhost`, `127.0.0.1`, `::1` y los nombres e IP de la máquina), lo renueva 30 días antes de caducar e **imprime su huella SHA-256 en cada arranque**. Los clientes fijan esa huella la primera vez ([Conectar el escritorio](#conectar-el-escritorio-a-un-servidor-remoto)). Si renueva el certificado, la huella cambia y hay que volver a confiar.

### Certificado externo

```sh
dconnector-server --listen 0.0.0.0:8443 \
  --tls-cert /etc/dconnector/tls/fullchain.pem \
  --tls-key  /etc/dconnector/tls/privkey.pem
```

(o `DCONNECTOR_TLS_CERT` y `DCONNECTOR_TLS_KEY`). Son archivos PEM. En Linux, `SIGHUP` (`systemctl reload dconnector`) recarga el certificado sin cortar el servicio; en Windows hay que reiniciar el servicio. Si falla la recarga se conserva el certificado anterior. Quien ejecute el servidor debe poder leer la clave privada.

### Proxy inverso

Un proxy da certificados de una autoridad reconocida (Let's Encrypt), un nombre de dominio y el puerto 443. El proxy termina el TLS y el servidor escucha solo en loopback:

```sh
dconnector-server --data /var/lib/dconnector --listen 127.0.0.1:8443 --no-tls
```

(En Docker con el proxy en otro contenedor: `DCONNECTOR_NO_TLS=true` y `DCONNECTOR_INSECURE=true`, sin publicar el puerto 8443 al exterior.)

Dos requisitos del proxy, comunes a cualquiera:

- **WebSocket** en `/api/v1/events` (eventos en vivo): hay que dejar pasar `Upgrade`/`Connection`, y no cortar conexiones ociosas demasiado pronto (el servidor manda un ping cada 25 s).
- **Cuerpo de hasta 100 MiB** en `/api/v1/files` (la subida de archivos): configura un límite de cuerpo de **al menos 128 MiB**, y que el proxy no guarde el cuerpo entero en memoria o disco antes de reenviarlo. El resto de la API usa cuerpos de 1 MiB como máximo.
- Conserva la cabecera `Host` original: el servidor comprueba que el `Origin` del WebSocket coincide con el `Host` (o con `--allow-origin`).

#### Caddy

Caddy obtiene y renueva el certificado solo (el dominio debe apuntar al servidor y los puertos 80 y 443 estar abiertos). `Caddyfile`:

```caddyfile
dconnector.ejemplo.com {
	# Subida de archivos: el servidor admite hasta 100 MiB por archivo.
	request_body {
		max_size 128MiB
	}

	# reverse_proxy ya reenvía el WebSocket de /api/v1/events, conserva el Host y no corta por tiempo
	# las conexiones largas; flush_interval -1 envía los datos sin esperar (medios y eventos).
	reverse_proxy 127.0.0.1:8443 {
		flush_interval -1
	}
}
```

#### nginx

El certificado puede ser el de Let's Encrypt (`certbot`). En el bloque `http { … }` (por ejemplo en `/etc/nginx/conf.d/dconnector.conf`, fuera de `server`):

```nginx
map $http_upgrade $connection_upgrade {
	default upgrade;
	''      close;
}

server {
	listen 80;
	server_name dconnector.ejemplo.com;
	return 301 https://$host$request_uri;
}

server {
	listen 443 ssl;
	http2 on;                       # nginx 1.25.1 o superior; en versiones anteriores: listen 443 ssl http2;
	server_name dconnector.ejemplo.com;

	ssl_certificate     /etc/letsencrypt/live/dconnector.ejemplo.com/fullchain.pem;
	ssl_certificate_key /etc/letsencrypt/live/dconnector.ejemplo.com/privkey.pem;
	ssl_protocols       TLSv1.2 TLSv1.3;

	# Subida de archivos: hasta 100 MiB por archivo (+ margen del formulario).
	client_max_body_size 128m;
	proxy_request_buffering off;

	location / {
		proxy_pass http://127.0.0.1:8443;
		proxy_http_version 1.1;

		proxy_set_header Host              $host;
		proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
		proxy_set_header X-Forwarded-Proto $scheme;

		# WebSocket de /api/v1/events
		proxy_set_header Upgrade    $http_upgrade;
		proxy_set_header Connection $connection_upgrade;

		proxy_read_timeout 3600s;
		proxy_send_timeout 3600s;
		proxy_buffering off;
	}
}
```

> **Detrás de un proxy, el límite de intentos ve la IP del proxy.** El servidor identifica al cliente por la dirección de la conexión y **no** confía en `X-Forwarded-For`. Con un proxy, todos los intentos de inicio de sesión salen de la misma IP, así que el límite «5 fallos por IP en 15 minutos» se comparte entre todos tus usuarios (el límite por usuario sigue funcionando). Si te preocupa la fuerza bruta, añade tu propio límite en el proxy para `/api/v1/auth/login`.

## Seguridad y buenas prácticas

El servidor guarda **las sesiones de tus cuentas de mensajería**: quien llegue a él puede leer tus conversaciones y escribir como tú. Trátalo como a un gestor de contraseñas.

- **No lo expongas sin TLS.** El servidor lo impide, pero revisa que lo que ve Internet sea HTTPS (certificado propio con huella fijada, certificado externo o proxy).
- **Claves largas y únicas** (mínimo 12 caracteres; mejor una frase larga). Un usuario por persona; desactiva (`user disable`) a quien deje de usarlo.
- **Firewall:** abre solo el puerto necesario, solo a quien deba conectarse; si hay proxy, el servidor escucha en `127.0.0.1` y el firewall no abre el 8443.
- **Límite de intentos incluido:** 5 fallos de inicio de sesión por IP y 10 por usuario en 15 minutos, con bloqueo creciente (hasta 1 hora) y 600 peticiones por minuto por sesión. Las sesiones caducan a las 24 horas sin actividad (30 días como máximo).
- **Copias de seguridad cifradas o en un lugar protegido** ([Copias de seguridad](#copias-de-seguridad)): contienen las sesiones de las cuentas y los chats, sin cifrar.
- **No registres claves.** El servidor nunca escribe claves, tokens ni cadenas de consulta en el registro; no lo cambies. No pongas la clave del administrador en historiales de la consola, repositorios ni tickets. `DCONNECTOR_ADMIN_PASSWORD` solo se usa en el primer arranque: después puedes quitarla del archivo de entorno.
- **Mantén el sistema y dConnector al día**, y no ejecutes el servidor como root (la unidad de systemd, el servicio con cuenta restringida o el contenedor sin privilegios lo evitan).
- **Destinos y «Permitir comandos»:** un servidor externo con comandos activados puede enviar mensajes en tu nombre. Actívalos solo en servidores que controles, con `wss://` y un token largo.
- **Tus cuentas, tu riesgo:** WhatsApp, Telegram, Meta no ven con buenos ojos los clientes no oficiales ni el envío masivo; ver [Marcas](#marcas).

Cómo informar de una vulnerabilidad: [`SECURITY.md`](SECURITY.md).

## Indicadores y variables de entorno

Un indicador de la línea de comandos gana a la variable de entorno; la variable gana al valor por defecto. Archivo de ejemplo para systemd: [`deploy/dconnector.env.example`](deploy/dconnector.env.example).

| Indicador | Variable | Por defecto | Qué hace |
| --- | --- | --- | --- |
| `--listen` | `DCONNECTOR_LISTEN` | `127.0.0.1:8443` | Dirección y puerto de escucha. Fuera de loopback exige TLS (o `--insecure`) |
| `--data` | `DCONNECTOR_DATA` | la de la plataforma | Carpeta de datos |
| `--tls-cert` | `DCONNECTOR_TLS_CERT` | vacío (certificado propio) | Certificado TLS externo (PEM); va junto a `--tls-key` |
| `--tls-key` | `DCONNECTOR_TLS_KEY` | vacío | Clave privada del certificado externo |
| `--no-tls` | `DCONNECTOR_NO_TLS` | no | Sirve sin TLS: solo en loopback, o con `--insecure` detrás de un proxy |
| `--insecure` | `DCONNECTOR_INSECURE` | no | Permite escuchar fuera de loopback sin TLS (aviso cada minuto). No recomendado |
| `--admin-user` | `DCONNECTOR_ADMIN_USER` | `admin` | Nombre del administrador del primer arranque |
| — | `DCONNECTOR_ADMIN_PASSWORD` | vacío (se genera) | Clave del administrador del primer arranque (12 caracteres o más) |
| `--allow-origin` | `DCONNECTOR_ALLOW_ORIGIN` | vacío | Orígenes extra permitidos para el WebSocket y CORS, separados por comas |
| `--verbose` | `DCONNECTOR_VERBOSE` | no | Registro detallado |
| `--service` | — | — | Solo Windows: `install`, `uninstall`, `start` o `stop` |
| `--version` | — | — | Imprime la versión y sale |
| `--selftest` | — | — | Comprobación sin red: abre los almacenes, imprime `OK` y sale |
| `--out <archivo>` | — | — | Con `--version` o `--selftest`, escribe la salida en un archivo |
| — | `DCONNECTOR_LOCAL_TOKEN` | — | **Solo para la aplicación de escritorio** (modo local). En un servidor no se define: con ese valor el servidor no crea usuarios |

Subcomandos: `user add|passwd|list|disable|enable` (administración de usuarios, ver arriba) y `healthcheck` (consulta `/api/v1/health` del propio servidor; lo usa el `HEALTHCHECK` de Docker).

La zona horaria del servidor es la del sistema (`TZ` en Docker y systemd).

## Actualizar

**Escritorio, con un botón.** En **Ajustes › Actualizaciones** (y con un aviso discreto al abrir) dConnector busca la última versión en la Release de GitHub y, si hay una nueva, **«Actualizar a X.Y.Z»**:

1. Descarga el `.msi` (instalación con MSI) o el `.zip` (portable) de tu arquitectura y **verifica su SHA-256 contra `SHA256SUMS` de esa misma Release**. Si no coincide, lo borra y no instala nada.
2. Cierra la aplicación y el servidor local (aunque tuvieras «Seguir en segundo plano»), instala encima **sin pedir administrador** y vuelve a abrir dConnector (minimizado, si lo tenías así).
3. Tus datos (`%APPDATA%\dConnector` o `data\` del portable) no se tocan.

Solo se descarga desde `https` y desde `api.github.com`, `github.com` y `*.githubusercontent.com` (`objects` y `release-assets`). **No se envía ningún dato tuyo**: es una petición `GET` sin autenticar con el encabezado `User-Agent: dConnector/<versión>`. La búsqueda automática ocurre al abrir (unos 10 s después, como máximo cada 24 h) y se apaga con el interruptor **«Buscar al abrir»**; sus fallos son silenciosos. **«Buscar actualizaciones»** la hace a mano. Las copias de desarrollo o ubicadas fuera del instalador y del portable solo muestran el enlace a la página de la versión.

**Sin firma.** Los ejecutables no están firmados: Windows 11 con *Smart App Control* o *SmartScreen*, o un antivirus, puede bloquear el instalador o el programa nuevo. dConnector no intenta saltarse ninguna protección: si algo falla, un cuadro de Windows explica la causa probable y dónde quedaron el instalador descargado (`<datos>\updates\<versión>`) y el registro (`<datos>\logs\desktop\update-<fecha>.log`), y abre la carpeta (también está el botón **«Abrir carpeta»**). Entonces puedes instalar a mano el `.msi` o el `.zip`.

**A mano.** MSI: ejecuta el nuevo; se instala encima conservando los datos. Portable: reemplaza los ejecutables, no `data\`. Antes, sal de dConnector con «Salir» (también del servidor local si lo dejaste en segundo plano).

**Desde 0.2.x.** La carpeta `%APPDATA%\dConnector` se reutiliza tal cual: el servidor local la abre y añade tablas nuevas a `app.db` (usuarios y sesiones) sin tocar las que ya existían. Por precaución, **copia la carpeta antes** de la primera vez que abras 0.3.0 ([Copias de seguridad](#copias-de-seguridad)). La versión 0.3.0 no cambia cómo se añaden las cuentas ni sus datos.

**Servidor.** Detén el servicio, haz una copia de la carpeta de datos, reemplaza el ejecutable y arranca:

```sh
sudo systemctl stop dconnector
sudo cp -a /var/lib/dconnector /var/lib/dconnector.copia
sudo install -m 0755 dconnector-server /usr/local/bin/dconnector-server
sudo systemctl start dconnector
```

En Windows: `--service stop`, reemplaza el `.exe`, `--service start`. En Docker: construye la imagen nueva, `docker stop`, `docker rm` y `docker run` de nuevo con el mismo volumen `/data`. Las migraciones de la base son aditivas; no hay actualizador automático. Si la versión de la API del servidor difiere de la del cliente, el escritorio lo avisa («Este servidor usa otra versión de la API»).

## Copias de seguridad

Todo lo que importa está en la **carpeta de datos**: `app.db`, `accounts\<id>\account.db` (con la sesión de cada canal), `accounts\<id>\media\`, `desktop.json` (solo escritorio) y, en el servidor, `tls\` (certificado propio y su clave privada).

1. **Para el servicio o sal de la aplicación** (`systemctl stop dconnector`, `--service stop`, `docker stop dconnector`, o «Salir» en el escritorio). SQLite trabaja en modo WAL: con el servidor encendido, los archivos `-wal` y `-shm` pueden tener datos que aún no están en `app.db`, y una copia a medias puede salir inconsistente.
2. Copia la carpeta **entera** (con sus archivos `-wal` y `-shm`, si hay).
3. Arranca de nuevo.

Para restaurar: con el servidor parado, vuelve a poner la carpeta y arranca. Guarda las copias en un sitio protegido: contienen sesiones de mensajería y conversaciones sin cifrar.

## Protocolo para servidores externos

Un **destino** es un servidor tuyo al que dConnector le manda en tiempo real lo que pasa en tus cuentas y que, si lo permites, puede actuar por ti. **dConnector es el cliente**: abre un WebSocket hacia tu dirección (`wss://`, o `ws://` solo en `localhost`/`127.0.0.1`) con `Authorization: Bearer <token>`; todo viaja en frames JSON; los eventos se entregan **al menos una vez** (cola persistente, confirmación con `ack`, deduplicación por `id`); los códigos QR y las configuraciones de las cuentas nunca salen.

- Especificación completa: [`docs/PROTOCOLO-v1.md`](docs/PROTOCOLO-v1.md).
- Servidor de ejemplo, listo para ejecutar: [`examples/echo-server`](examples/echo-server) (también en las descargas como `echo-server-…`). Imprime cada evento y contesta «Recibido ✅» a los mensajes directos: `echo-server -addr 127.0.0.1:8787 -token secreto`.

## API del servidor

El servidor ofrece `POST /api/v1/rpc/{servicio}/{método}` (los mismos servicios de la interfaz), eventos en vivo por WebSocket (`/api/v1/events`), subida de archivos, descarga autenticada de medios, autenticación con usuario y clave y comprobación de estado (`/api/v1/health`). El contrato completo (rutas, errores, autenticación, límites, TLS y versionado) está en [`docs/14-API-SERVIDOR.md`](docs/14-API-SERVIDOR.md).

## Compilar desde el código

Todo se compila desde una sola máquina Windows, **sin CGO** (`CGO_ENABLED=0`; SQLite es `modernc.org/sqlite`). Estas son las versiones con las que se construyó (ver [`docs/BITACORA.md`](docs/BITACORA.md)):

| Herramienta | Versión | Instalación |
| --- | --- | --- |
| Go | 1.27.4 (lo pide `go.mod`) | `winget install GoLang.Go` |
| Wails v3 (`wails3`) y librería | v3.1.0 | `go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0` |
| Task | 3.54.0 | `go install github.com/go-task/task/v3/cmd/task@latest` |
| .NET SDK | 8.0.428 | `winget install Microsoft.DotNet.SDK.8` |
| WiX Toolset | 5.0.2 + `WixToolset.UI.wixext 5.0.2` | `dotnet tool install --global wix --version 5.0.2` y `wix extension add -g WixToolset.UI.wixext/5.0.2` |
| Node / pnpm | 22.21.1 / 12.11.2 | — |
| TypeScript | 7.0.2 (compilador nativo) | vía pnpm |

Añade al `PATH` `%USERPROFILE%\go\bin` y `%USERPROFILE%\.dotnet\tools`. Interfaz: React 19, TypeScript, Vite y Tailwind v4. `wails3 doctor` comprueba el entorno. `go test -race` exige CGO (un compilador de C, por ejemplo WinLibs); para compilar no hace falta.


```powershell
git clone https://github.com/JoaquinZc/dconnector.git
cd dconnector
task gate        # pruebas + compilación + --version + --selftest
task release     # todo lo de la tabla de descargas, en dist\
```

### Tareas principales (`Taskfile.yml`)

| Tarea | Qué hace |
| --- | --- |
| `task bindings` | Genera los enlaces TypeScript de los servicios del escritorio |
| `task frontend` | Instala dependencias y compila la interfaz (incluye `tsc` y `vitest`) |
| `task build` | Compila `bin\dConnector.exe` (windows/amd64) |
| `task test` | `go vet`, `go test`, `tsc --noEmit` y `vitest run` |
| `task gate` | La puerta de cada oleada: `test` + `build` + `version` + `selftest` |
| `task release` | Produce en `dist\` los instaladores, portables, paquetes del servidor, `echo-server` y `SHA256SUMS` |
| `task licencias` | Regenera `LICENCIAS.txt` y `THIRD-PARTY-NOTICES` (solo cuando cambian las dependencias) |

Los nombres de los artefactos de `dist\` son los de la [tabla de descargas](#descargas). La lista exacta de tareas está en `Taskfile.yml` (`task --list`). La versión sale del archivo `VERSION`.

### Compilar solo el servidor, para cualquier plataforma

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -buildvcs=false \
  -ldflags "-s -w -X dconnector/internal/buildinfo.Version=0.3.2" \
  -o dconnector-server ./cmd/dconnector-server
```

`GOOS` puede ser `linux` o `windows` y `GOARCH` `amd64` o `arm64`. El escritorio solo existe para Windows (`windows/386`, `windows/amd64` y `windows/arm64`).

### Pruebas

```powershell
go vet ./...
go test ./...                            # núcleo, almacén, adaptadores, protocolo, API y seguridad del servidor
pnpm -C frontend exec tsc --noEmit
pnpm -C frontend exec vitest run
```

Las pruebas **no usan cuentas reales** de WhatsApp, Telegram ni Meta: solo pruebas unitarias, el adaptador falso y pruebas de integración locales. Más detalle para colaborar en [`CONTRIBUTING.md`](CONTRIBUTING.md).

### Mapa de la carpeta

```
dconnector/
  README.md  LICENSE  SECURITY.md  CONTRIBUTING.md  THIRD-PARTY-NOTICES  LICENCIAS.txt  LEEME.txt
  VERSION  Taskfile.yml  go.mod  go.sum
  cmd/dconnector/            aplicación de escritorio (cliente, Wails)
  cmd/dconnector-server/     servidor sin ventana
  internal/
    core/                    NÚCLEO: dominio, puertos y servicios; no importa adaptadores
    adapter/channel/         WhatsApp, Telegram, Messenger, Instagram
    adapter/store/sqlite/    app.db y account.db
    adapter/protocol/ws/     protocolo v1 (destinos)
    adapter/api/             API HTTP(S) + WebSocket, autenticación, TLS, archivos y medios
    adapter/desktop/         lado Windows del cliente: conexión, servidor local, avisos, bandeja
    server/  config/  paths/ arranque del servidor, configuración, rutas de datos
  frontend/                  interfaz (React + TypeScript)
  examples/echo-server/      servidor de ejemplo del protocolo
  deploy/                    unidad de systemd, Dockerfile, ejemplos de configuración
  build/  scripts/           iconos, MSI, scripts de empaquetado y licencias
  docs/                      documentación
```

## Límites conocidos y qué no se probó

Copia fiel de lo que dice [`docs/BITACORA.md`](docs/BITACORA.md); no se ha maquillado.

- **Cuentas reales.** WhatsApp se probó a mano con una cuenta durante 0.2.x. **Nada de Telegram, Messenger ni Instagram se ha probado con cuentas reales**: solo traducciones, máquinas de estado y dobles. Lo más frágil: los **chats cifrados de extremo a extremo de Messenger** (el registro del dispositivo y la conexión cifrada no se pudieron probar; el interruptor para apagarlos está en `messenger/e2ee.go`), **Instagram** con `instameow`, el inicio con usuario y contraseña de Messenger, el *floodwait* y la sesión de Telegram y los identificadores `@lid` de WhatsApp. La lista de pruebas pendientes está en [`docs/QA-EN-VIVO.md`](docs/QA-EN-VIVO.md).
- **Messenger e Instagram** son una vía no oficial y pueden dejar de funcionar sin aviso; la cuenta puede ser limitada. «Cerrar sesión» solo borra la sesión local: Meta no deja cerrar la sesión web de las cookies.
- **Linux y arm64 no se ejecutaron en el equipo de construcción**: solo se compilan. Esas pruebas corren en la integración continua (runners Linux x64 y arm64). **El escritorio de 32 bits (Windows x86) no se ofrece en la 0.3.0**: compila, pero Wails v3.1.0 falla al arrancar en 32 bits; queda para cuando Wails lo corrija.
- **La interfaz** se ha revisado con pruebas automáticas y poco en una ventana real con datos: el claro/oscuro, los selectores de fecha en WebView2, los diálogos de archivos, arrastrar y soltar, la nota de voz y el vídeo con audio siguen sin revisión sistemática.
- **Instalación real del MSI** (ruta por usuario, accesos directos, actualizar encima, desinstalar conservando los datos): comprobada por sus tablas y con extracción administrativa; la comprobación manual está en la lista de QA.
- **Notificaciones de Windows:** el aviso, el sonido y que `ms-settings:` abra la pantalla correcta se probaron poco en ventana real. Con «No molestar» de Windows activo los avisos no se ven ni suenan (el sonido propio sí).
- **Estados de WhatsApp:** los estados que ya existían al vincular solo se leen en el momento de vincular; en una cuenta ya vinculada no se recuperan los anteriores.
- **Riesgos conocidos de la revisión** (detalle en la bitácora): `message.updated` tras descargar un medio no llega al protocolo; `messages.list` y `message.send` sobre grupos ignoran `includeGroups=false` en el protocolo; `fileUrl` (https) sigue redirecciones y no bloquea direcciones privadas; archivar en local un chat de Messenger o Instagram se pierde con el siguiente `chat.updated`; WhatsApp declara `StatusViewers` pero devuelve «no soportado».
- **Servidor:** sin cifrado de la base de datos; todos los usuarios son administradores; detrás de un proxy, el límite de intentos por IP se comparte; no hay renovación de certificado externo sin recargar o reiniciar.
- **Sin firma de código:** SmartScreen avisará en Windows.
- **Licencias:** ver [Licencia](#licencia).

## Licencia

dConnector se publica bajo la **GNU Affero General Public License v3.0 (AGPL-3.0)**: el texto íntegro está en [`LICENSE`](LICENSE). Los avisos de los componentes de terceros están en [`THIRD-PARTY-NOTICES`](THIRD-PARTY-NOTICES) (el mismo contenido viaja como `LICENCIAS.txt` en el instalador y en el paquete portable).

Por qué AGPL: dConnector enlaza `go.mau.fi/mautrix-meta` (AGPL-3.0) y `go.mau.fi/libsignal` (GPL-3.0), y esas licencias obligan a que el conjunto se ofrezca con su código fuente bajo licencias compatibles.

Qué implica, dicho en llano (no es asesoría legal):

- **Puedes usarlo, estudiarlo, modificarlo y repartirlo** libremente, también en un negocio.
- Si **repartes** dConnector (modificado o no), debes dar el código fuente correspondiente bajo la misma licencia.
- Si **ofreces dConnector como servicio en red** (por ejemplo un servidor al que otras personas se conectan y usan, aunque no les entregues el programa), la AGPL te obliga a **ofrecer el código fuente completo de la versión que ejecutas, con tus modificaciones**, a esos usuarios. Usarlo para ti o dentro de tu propia organización, sin ofrecerlo a terceros, no activa esa obligación.
- El protocolo `dConnector v1` y tus propios servidores externos (los destinos) **no quedan sujetos** a la AGPL por hablar con dConnector a través de WebSocket; solo lo está el código que enlaza el programa.
- Los componentes de terceros conservan sus licencias (MPL-2.0 en whatsmeow, `go.mau.fi/util`, `zeroconfig` y `mautrix`; MIT, BSD, ISC y Apache-2.0 en otros).

## Marcas

WhatsApp, Telegram, Messenger e Instagram son marcas de sus respectivos dueños (Meta Platforms, Inc. y Telegram FZ-LLC). dConnector es un proyecto independiente: **no está afiliado, respaldado ni patrocinado por ninguno de ellos**. El uso de dConnector con esos servicios es **responsabilidad de cada usuario**, que debe respetar los términos de servicio y las políticas de cada uno (incluidas las que limitan los clientes no oficiales, la automatización y el envío masivo de mensajes). Las cuentas pueden ser suspendidas.

## Documentación

| Documento | Contenido |
| --- | --- |
| [`docs/MANUAL.md`](docs/MANUAL.md) | Manual de uso por pantalla (incluye «Conexión: local o servidor remoto») |
| [`docs/QA-EN-VIVO.md`](docs/QA-EN-VIVO.md) | Lista de comprobación en vivo, con casillas |
| [`docs/14-API-SERVIDOR.md`](docs/14-API-SERVIDOR.md) | API del servidor (v1) |
| [`docs/PROTOCOLO-v1.md`](docs/PROTOCOLO-v1.md) | Protocolo para servidores externos |
| [`docs/PLAN-SERVIDOR.md`](docs/PLAN-SERVIDOR.md) | Plan y estado de la versión 0.3.0 |
| [`docs/RELEASE-NOTES-v0.3.2.md`](docs/RELEASE-NOTES-v0.3.2.md) | Notas de la versión 0.3.2 |
| [`docs/RELEASE-NOTES-v0.3.1.md`](docs/RELEASE-NOTES-v0.3.1.md) | Notas de la versión 0.3.1 |
| [`docs/RELEASE-NOTES-v0.3.0.md`](docs/RELEASE-NOTES-v0.3.0.md) | Notas de la versión 0.3.0 |
| [`docs/BITACORA.md`](docs/BITACORA.md) | Bitácora de construcción: versiones exactas, desviaciones y límites |
| [`docs/01-DECISIONES.md`](docs/01-DECISIONES.md) … [`docs/13-OLEADAS.md`](docs/13-OLEADAS.md) | Diseño: decisiones, arquitectura, dominio, puertos, almacén, servicios, adaptadores de canal, protocolo, interfaz y empaquetado |
| [`SECURITY.md`](SECURITY.md) · [`CONTRIBUTING.md`](CONTRIBUTING.md) | Seguridad y colaboración |

Cuando un documento de diseño y el código discrepan, **manda el código**.
