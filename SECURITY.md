# Política de seguridad

dConnector guarda las sesiones de cuentas de mensajería y conversaciones privadas. Tomamos en serio los informes de seguridad.

## Cómo informar de una vulnerabilidad

**No abras un issue público ni publiques los detalles.** Usa los **avisos privados de seguridad de GitHub** del repositorio:

1. Entra en la pestaña **Security** del repositorio (enlace directo: https://github.com/JoaquinZc/dconnector/security/advisories/new).
2. Pulsa **«Report a vulnerability»** (informar de una vulnerabilidad).
3. Describe el problema con los pasos para reproducirlo, la versión afectada y el impacto.



Incluye, si puedes: versión (`--version`), sistema operativo y arquitectura, cómo se ejecuta el servidor (servicio, Docker, proxy) y una prueba mínima. **No incluyas claves, tokens, cookies, bases de datos ni conversaciones reales**: reproduce con datos de prueba.

Qué puedes esperar: acuse de recibo y una primera valoración lo antes posible; la corrección se prepara en privado y se publica con una versión nueva y un aviso (con crédito, si lo deseas). Es un proyecto sin equipo de guardia: no hay plazos garantizados.

## Versiones soportadas

| Versión | Soporte de seguridad |
| --- | --- |
| 0.3.x (la última) | Sí |
| 0.2.x y anteriores | No: actualiza a 0.3.x (el escritorio 0.2.x no tiene servidor ni autenticación) |

## Alcance

Se consideran vulnerabilidades, por ejemplo:

- Saltarse la autenticación del servidor (acceder a `/api/v1/…` sin sesión válida, usar un token caducado, de otro usuario o revocado, o ampliar el alcance de un `media-token` o de un ticket de WebSocket).
- Saltarse o eludir el límite de intentos de inicio de sesión.
- Lectura o escritura de archivos fuera de lo permitido (recorrido de rutas en `/files`, `/media`, `/avatar` o en `upload://`), o lectura de archivos del servidor mediante rutas locales por parte de un cliente remoto.
- Ejecución de código remoto, inyección SQL, o filtración de claves, tokens, cookies de sesión de canales o contenido de mensajes (en respuestas, en el registro o en el protocolo de destinos).
- Debilidades de TLS: aceptar un certificado cambiado sin avisar, rebajar la verificación o servir sin TLS fuera de loopback sin que el operador lo haya pedido con `--insecure`.
- Origen de WebSocket o CORS mal validado que permita a una web ajena actuar como el usuario.
- Que el protocolo de destinos permita a un servidor sin «Permitir comandos» ejecutar comandos, o actuar sobre cuentas fuera de su filtro.
- Escalada de privilegios en el instalador, el servicio de Windows o la unidad de systemd.

## Lo que NO se considera una vulnerabilidad

- **Cualquier cosa que requiera ya acceso a la máquina o a la carpeta de datos.** Los datos locales (`app.db`, `account.db`, medios) **no están cifrados**; esto está documentado. Quien lee esa carpeta lee las conversaciones y puede copiar las sesiones.
- Que el servidor, con `--insecure`, escuche sin TLS fuera de loopback: es una opción explícita y avisa cada minuto.
- Que el certificado propio del servidor sea autofirmado: se verifica con su huella, no con una autoridad.
- Que, detrás de un proxy inverso, el límite de intentos por IP vea la dirección del proxy (el servidor no confía en `X-Forwarded-For` a propósito; está documentado).
- Que los usuarios del servidor sean todos administradores en esta versión.
- Que **Meta, WhatsApp o Telegram bloqueen o limiten una cuenta** por usar clientes no oficiales o envío masivo (Messenger e Instagram usan una vía no oficial y lo advierten), o cambios de esos servicios que rompan un canal.
- Ataques de denegación de servicio por volumen que un proxy o cortafuegos debe frenar, o por una cuenta autenticada con acceso de administrador.
- Falta de firma de código del instalador y de los ejecutables (SmartScreen avisa; se publican sumas SHA-256).
- Vulnerabilidades de una dependencia ya corregidas en una versión más nueva que dConnector aún no incluye: avísanos igualmente, pero como actualización de dependencias.
- Informes automáticos sin una demostración de impacto concreto.

## Modelo de amenazas del servidor (resumen)

**Qué protege el servidor:** las sesiones de las cuentas de mensajería (WhatsApp, Telegram, Messenger e Instagram), los chats, contactos y medios, y la capacidad de enviar mensajes en nombre de esas cuentas.

**Quién se supone que es de confianza:** el operador del servidor (acceso al sistema y a la carpeta de datos) y los usuarios con clave (todos son administradores en esta versión). **No** es de confianza: la red, otros equipos de la red, otras páginas web abiertas en el navegador de un usuario ni los servidores externos configurados como destinos (que solo reciben lo que su filtro permite y solo actúan con «Permitir comandos»).

**Autenticación.** Usuarios con clave de al menos 12 caracteres, guardada solo como hash **argon2id** (64 MiB, 3 pasadas, 2 hilos, sal de 16 bytes). El inicio de sesión devuelve un token opaco de 32 bytes aleatorios; el servidor guarda únicamente su SHA-256. El token caduca tras 24 horas sin actividad (30 días como máximo) y deja de valer al cambiar la clave, cerrar sesión o desactivar al usuario. El `?token=` en una URL solo vale con un `media-token` de 15 minutos y de solo lectura, en tres rutas de descarga; el WebSocket usa un ticket de un solo uso de 30 segundos para que el token de sesión nunca viaje en una URL. El mensaje de «usuario o clave incorrectos» es el mismo exista o no el usuario y se iguala el tiempo de respuesta.

**Fuerza bruta.** 5 fallos por IP y 10 por usuario en 15 minutos, con bloqueo que se duplica en cada reincidencia (hasta 1 hora) y respuesta `429` con `Retry-After`; 600 peticiones por minuto por sesión; el cómputo de argon2id está acotado en paralelo para que no sea un medio de agotar la memoria.

**Transporte.** HTTPS por defecto (TLS 1.2 como mínimo, HSTS con TLS). Fuera de loopback el servidor se niega a arrancar sin TLS salvo `--insecure`. Con el certificado propio, el cliente de escritorio fija la huella SHA-256 la primera vez y se niega a conectar si cambia. Con un proxy, el TLS lo termina el proxy y el servidor escucha en loopback.

**Superficie de la API.** El cuerpo de las llamadas RPC se limita a 1 MiB; las subidas, a 100 MiB por archivo y 1 GiB por usuario (se borran a las 24 horas); los identificadores de archivo son ULID validados; los segmentos de las rutas de medios se validan (sin `..`, `/`, `\` ni NUL); un cliente remoto solo puede referirse a archivos subidos (`upload://<id>`), nunca a rutas del servidor. Se comprueba el `Origin` del WebSocket (mismo host, `wails.localhost` o `--allow-origin`). Todas las respuestas llevan `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer` y `Cache-Control: no-store`; medios y archivos, además, `Content-Security-Policy: sandbox`.

**Modo local del escritorio.** El servidor local escucha solo en loopback con un token aleatorio de un solo uso por arranque, pasado por variable de entorno (nunca por argumento) y aceptado solo si la conexión llega por loopback y sin cabeceras de reenvío.

**Qué datos guarda.** En la carpeta de datos, **sin cifrar**: usuarios (nombre y hash argon2id), sesiones (hash del token), las sesiones de cada canal, chats, mensajes, contactos, medios y ajustes, y los tokens de los destinos. En el escritorio, la clave de un servidor remoto se guarda cifrada con DPAPI de Windows. **El registro** nunca lleva claves, tokens, tickets ni cadenas de consulta, ni el texto de los mensajes ni nombres de chats, y solo los últimos caracteres de los identificadores; la clave generada del administrador se imprime una vez por la salida estándar y no por el registro.

**Riesgos que asume el operador:** cifrado en reposo (usa el cifrado de disco del sistema), copias de seguridad (contienen todo lo anterior), exposición a Internet (usa TLS y un firewall) y el riesgo de cuenta con los canales no oficiales.

**Actualizador del escritorio.** Consulta `GET https://api.github.com/repos/JoaquinZc/dconnector/releases/latest` (sin autenticación y sin datos de la persona), descarga solo por `https` y solo desde `api.github.com`, `github.com`, `objects.githubusercontent.com` y `release-assets.githubusercontent.com` (se comprueba cada redirección), con tope de tamaño y tiempo, valida el nombre del asset contra el patrón esperado y **verifica el SHA-256 contra `SHA256SUMS` de la misma Release** antes de ejecutar o extraer nada (el zip portable se extrae con protección contra zip-slip y enlaces, y se repite la verificación en el ayudante). **Limitación:** si la cuenta de GitHub del proyecto se compromete, el atacante controla la Release y las sumas, y la verificación no lo detecta; por eso conviene firmar el instalador y los ejecutables en el futuro.

## Divulgación

Se prefiere la divulgación coordinada: danos tiempo razonable para corregir antes de publicar. Cuando haya una corrección, se anunciará en las notas de la versión.
