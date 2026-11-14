# dConnector · Manual de uso

Versión 0.3.3 · para Windows 10 y 11 (x64 y arm64) · funciona en tu equipo sin cuentas de usuario, sin licencia y sin telemetría. Desde la 0.3.0 la aplicación puede usar el **servidor local** que lleva incluido o conectarse a un **servidor remoto** ([apartado 18](#18-conexión-local-o-servidor-remoto)).

dConnector reúne varias cuentas de **WhatsApp, Telegram, Messenger e Instagram** en una sola bandeja de chats (con el aspecto de WhatsApp Web), publica y mira estados, hace campañas con envío prudente, programa mensajes, guarda tus contactos y listas, muestra estadísticas y puede **hablar con un servidor tuyo** (un bot, una IA, un almacén de mensajes) por un protocolo propio.

> **Antes de empezar, dos avisos honestos**
>
> 1. **Messenger e Instagram son una vía no oficial.** dConnector imita al navegador de Meta. **Meta puede bloquear o suspender la cuenta.** Úsalo con cuentas que puedas permitirte perder, o con las que asumas ese riesgo.
> 2. **Solo WhatsApp se ha probado a mano con una cuenta real; Telegram, Messenger e Instagram no.** Todo lo que habla con los canales está escrito contra las librerías de cada uno y cubierto con pruebas automáticas y un adaptador falso, pero la prueba en vivo es la que debes hacer tú con `docs/QA-EN-VIVO.md`. Si algo falla, el apartado «Cuando algo falla» dice qué guardar para poder arreglarlo.

---

## Índice

1. [Instalar](#1-instalar)
2. [Dónde se guardan tus datos](#2-dónde-se-guardan-tus-datos)
3. [Recorrido por la ventana](#3-recorrido-por-la-ventana)
4. [Añadir cuentas](#4-añadir-cuentas)
5. [Chats](#5-chats)
6. [Estados](#6-estados)
7. [Campañas](#7-campañas)
8. [Programados](#8-programados)
9. [Contactos](#9-contactos)
10. [Estadísticas](#10-estadísticas)
11. [Destinos y el protocolo](#11-destinos-y-el-protocolo)
12. [Cuentas (la pantalla)](#12-cuentas-la-pantalla)
13. [Ajustes](#13-ajustes)
14. [Bandeja del sistema, cerrar, arranque con Windows](#14-bandeja-del-sistema-cerrar-arranque-con-windows)
15. [Atajos de teclado](#15-atajos-de-teclado)
16. [Cuando algo falla](#16-cuando-algo-falla)
17. [Qué no hace · límites](#17-qué-no-hace--límites)
18. [Conexión: local o servidor remoto](#18-conexión-local-o-servidor-remoto) (incluye «Sin conexión», «Seguir en segundo plano» y avisos con servidor remoto)

---

## 1. Instalar

Hay dos formas. Las dos dan el mismo programa.

| | Instalador `.msi` | Carpeta portable `.zip` |
| --- | --- | --- |
| Archivo (x64; también hay `arm64`) | `dConnector-<versión>-x64.msi` | `dConnector-<versión>-portable-x64.zip` |
| Para quién | Uso normal en tu equipo | Probar sin instalar, o llevarlo en un disco |
| Instala en | Tu usuario de Windows (`%LOCALAPPDATA%\Programs\dConnector`). **No pide permisos de administrador** | Donde descomprimas la carpeta |
| Accesos directos | Menú Inicio y Escritorio, llamados «dConnector» | Ninguno: abres `dConnector.exe` |
| Datos | `%APPDATA%\dConnector` | Carpeta `data\` junto al `.exe` |
| Actualizar | Botón en Ajustes › Actualizaciones, o ejecuta el `.msi` nuevo: se instala **encima** y conserva tus datos | El mismo botón, o sustituye los ejecutables y deja `data\` como está |
| Quitar | «Aplicaciones instaladas» de Windows. **Desinstalar NO borra tus datos** | Borra la carpeta (y con ella `data\`) |

### Instalador (.msi)

1. Haz doble clic en `dConnector-<versión>-x64.msi` (o el `arm64` si tu equipo tiene procesador ARM).
2. Si Windows muestra el aviso azul de SmartScreen, mira el apartado siguiente.
3. Acepta la licencia y pulsa el botón de instalar.
4. Abre «dConnector» desde el menú Inicio o el Escritorio.

### Carpeta portable (.zip)

1. Haz clic derecho en el `.zip` › «Extraer todo…». Elige una carpeta que **no sea de solo lectura** (por ejemplo `D:\dConnector`, no «Archivos de programa»).
2. Dentro hay una carpeta `dConnector-<versión>-portable-x64` con: `dConnector.exe`, `dConnector-server.exe` (el servidor local; no lo borres ni lo muevas), `portable.txt`, `LEEME.txt` y `LICENCIAS.txt`.
3. Abre `dConnector.exe`. La presencia de `portable.txt` junto al `.exe` es lo que hace que los datos vayan a la carpeta `data\` de al lado.
4. Para cambiarla de sitio: cierra dConnector con «Salir» (clic derecho en el icono de la bandeja; si activaste «Seguir en segundo plano», comprueba que el servidor local también se detuvo: [apartado 18](#18-conexión-local-o-servidor-remoto)) y mueve **toda** la carpeta, incluida `data\`.

### El aviso de SmartScreen («Windows protegió su PC»)

dConnector **no está firmado** con un certificado de código (cuesta dinero y no hay empresa editora detrás). Por eso Windows lo trata como «aplicación desconocida» la primera vez. Es normal y no significa que haya un virus.

1. En la ventana azul pulsa el enlace **«Más información»**.
2. Aparece un botón nuevo: **«Ejecutar de todas formas»**. Púlsalo.

Pasa lo mismo con el `.msi` y con el `.exe` portable. Solo lo ves la primera vez de cada archivo.

### Actualizar desde la aplicación

**Ajustes › Actualizaciones.** Al abrir dConnector (unos 10 segundos después, y como máximo una vez cada 24 horas) mira en la Release de GitHub si hay una versión más nueva; si la hay, aparece un aviso discreto con **«Actualizar»** y **«Más tarde»** (no vuelve a salir para esa versión en esa sesión). También puedes pulsar **«Buscar actualizaciones»**, o apagar la búsqueda automática con **«Buscar al abrir»** (los fallos de la búsqueda automática no se avisan).

**«Actualizar a X.Y.Z»** hace, en este orden: 1) descarga el `.msi` (si instalaste con MSI) o el `.zip` (portable) de tu arquitectura, solo desde `https` y desde servidores de GitHub; 2) **verifica su SHA-256** con `SHA256SUMS` de la misma Release (si no coincide, lo borra y no instala nada); 3) cierra dConnector y su servidor local, aunque tuvieras «Seguir en segundo plano»; 4) instala encima **sin pedir administrador**; 5) vuelve a abrir dConnector. Tus datos no se tocan. No se envía ningún dato tuyo a GitHub (solo `dConnector/<versión>` como identificación del programa).

**Sin firma, qué esperar de Windows.** Los instaladores y ejecutables no están firmados. Windows 11 con *Smart App Control* o *SmartScreen*, o tu antivirus, puede bloquearlos. dConnector no intenta saltarse esas protecciones: si algo falla verás un cuadro con la causa probable, la ruta del instalador descargado (`datos\updates\<versión>`) y la del registro (`datos\logs\desktop\update-<fecha>.log`), y se abre la carpeta; en Ajustes también está **«Abrir carpeta»**. Con el instalador a mano puedes seguir el apartado de [SmartScreen](#el-aviso-de-smartscreen-windows-protegió-su-pc). Las copias que no son MSI ni portable (desarrollo) solo ofrecen abrir la página de la versión.

### Requisitos

- Windows 10 u 11: x64 o arm64, con el instalador de tu arquitectura (no hay versión de 32 bits).
- **WebView2** (el motor que dibuja la ventana). Windows 10 y 11 ya lo traen instalado.
- Conexión a internet para los canales. dConnector **no hace ninguna otra llamada de red** que no sea a WhatsApp, Telegram, Meta, a los destinos que tú configures, al servidor remoto que elijas o a GitHub para saber si hay una versión nueva (desactivable, sin enviar datos tuyos: ver [Actualizar](#actualizar-desde-la-aplicación)).
- En el modo local, un puerto de tu propio equipo (127.0.0.1, elegido al azar): no hay que abrir nada en el cortafuegos y no es visible desde la red.

### Una sola ventana, una sola copia

Si abres `dConnector.exe` otra vez mientras ya está en marcha, no se abre una segunda copia: te muestra la ventana que ya estaba abierta (aunque estuviera oculta en la bandeja). Es a propósito: dos copias sobre los mismos datos romperían las sesiones.

---

## 2. Dónde se guardan tus datos

| Cómo lo abres | Carpeta de datos |
| --- | --- |
| Instalado con el `.msi` | `%APPDATA%\dConnector` (por ejemplo `C:\Users\tu_usuario\AppData\Roaming\dConnector`) |
| Portable | `data\` junto a `dConnector.exe` |

Para llegar sin buscar: **Ajustes › Datos › «Abrir carpeta»**. Ahí también se ve la ruta exacta.

Qué hay dentro:

| Qué | Dónde |
| --- | --- |
| Cuentas, contactos, listas, campañas, programados, destinos y ajustes | `app.db` |
| Sesión del canal, chats, mensajes y estados de **cada cuenta** | `accounts\<id de la cuenta>\account.db` |
| Fotos, vídeos y archivos descargados | `accounts\<id>\media\` |
| Fotos de perfil | `accounts\<id>\avatars\` |
| Registro de errores | `logs\` (el del servidor local) y `logs\desktop\` (el de la ventana); ver [apartado 16](#16-cuando-algo-falla) |
| Qué servidor usa la ventana (local o remoto), «Seguir en segundo plano» y las huellas de certificados confiados | `desktop.json` (la clave de un servidor remoto va **cifrada con DPAPI de Windows**) |
| Adjuntos de campañas, programados y estados | `campaign-media\`, `scheduled-media\`, `status-media\` |
| Temporales (se vacía al arrancar) | `tmp\` |

Cosas que conviene saber:

- **No hay contraseña ni cifrado de la base de datos.** Quien tenga acceso a tu usuario de Windows (o a la carpeta `data\` del portable) puede leer tus chats y copiar tus sesiones. Cuida el equipo y las copias de seguridad.
- **Desinstalar el `.msi` no borra nada de esto.** Si quieres borrar todo, elimina la carpeta a mano (con dConnector cerrado).
- **Copia de seguridad:** con dConnector cerrado (menú de la bandeja › «Salir») y, si usas «Seguir en segundo plano», con el servidor local detenido, copia la carpeta de datos entera. La base trabaja en modo WAL: una copia con el servidor encendido puede salir a medias.
- **Con un servidor remoto**, las cuentas, los chats y los medios viven **en el servidor**, no aquí: esta carpeta solo guarda `desktop.json` y el registro de la ventana. Lo que se explica aquí sobre los datos aplica entonces a la carpeta de datos del servidor.
- Todas las horas se guardan en UTC y se muestran en la hora local de tu equipo.

---

## 3. Recorrido por la ventana

La ventana abre a **1280 × 800** (mínimo 960 × 600). A la izquierda hay una barra de 80 píxeles con iconos y su nombre debajo; cada uno lleva su atajo al pasar el ratón por encima. En la barra se ve el estado de la conexión («Conectado», «Reconectando» o «Sin conexión»).

De arriba abajo:

| Icono | Pantalla | Atajo |
| --- | --- | --- |
| Burbujas | **Chats** (con un número si hay mensajes sin leer) | `Ctrl+1` |
| Círculo | **Estados** | `Ctrl+2` |
| Megáfono | **Campañas** | `Ctrl+3` |
| Reloj de calendario | **Programados** | `Ctrl+4` |
| Personas | **Contactos** | `Ctrl+5` |
| Gráfico | **Estadísticas** | `Ctrl+6` |
| Enchufe | **Destinos** | `Ctrl+8` |
| *(abajo)* Teléfono | **Cuentas** (con un punto de color) | `Ctrl+7` |
| *(abajo)* Engranaje | **Ajustes** | `Ctrl+9` |

> Los atajos siguen el orden de la lista de abajo (Cuentas es el 7 y Destinos el 8), no el orden en que se dibujan los iconos.

**El punto de la pantalla Cuentas** resume la salud de todas tus cuentas con el **peor** estado: verde (todo conectado), ámbar (alguna conectando o esperando vinculación) o rojo (alguna con error o con la sesión cerrada).

**Avisos.** Los errores y confirmaciones salen como un aviso breve (rojo o verde) que desaparece solo; los errores duran un poco más. Cuando dConnector te dice que algo no se pudo, el texto del aviso explica el motivo en español.

**Borradores.** Los formularios largos (asistente de cuentas, campañas, programados, contactos, destinos, ajustes) guardan un borrador mientras escribes y lo recuperan si cierras la ventana a medias.

**Todo texto largo** se corta con «…»; al pasar el ratón por encima aparece completo.

---

## 4. Añadir cuentas

Pantalla **Cuentas** (`Ctrl+7`) › botón **«Añadir cuenta»**. El asistente tiene cuatro pasos: **Canal › Datos › Vincular › Listo**.

1. **Canal:** elige una de las cuatro tarjetas (WhatsApp, Telegram, Messenger, Instagram). Las de Meta llevan la nota «Vía no oficial».
2. **Datos:** escribe una **Etiqueta** (por ejemplo «Ventas», «Personal»; hasta 40 caracteres) y elige un **Color**. La etiqueta y el color acompañan a cada chat y mensaje de esa cuenta para que nunca confundas desde dónde escribes. Según el canal pide además:
   - Telegram: `api_id` y `api_hash`.
   - Messenger e Instagram: marcar la casilla **«Entiendo»** bajo el aviso **«Vía no oficial: Meta puede bloquear la cuenta»**. Sin la casilla no se puede seguir.
   Pulsa **«Continuar»**: se crea la cuenta y empieza la vinculación.
3. **Vincular:** depende del canal (abajo).
4. **Listo:** aparece **«Conectada ✓»** con el nombre y el número o usuario de la cuenta. Pulsa **«Ir a chats»** o **«Cerrar»**.

Si cierras el asistente en el paso 3, la cuenta se queda creada en «Esperando vinculación»; no se pierde. Puedes seguir luego con **«Volver a vincular»** en su tarjeta.

Puedes tener **varias cuentas del mismo canal** (por ejemplo, dos WhatsApp). Cada una es independiente.

### 4.1 WhatsApp

Tiene dos pestañas en el paso 3. Las dos vinculan dConnector como «dispositivo vinculado» de tu WhatsApp del teléfono (el teléfono sigue siendo el principal).

**Pestaña «Código QR»** (la más sencilla)

1. En el teléfono abre WhatsApp › **Ajustes** › **Dispositivos vinculados** › **Vincular un dispositivo**. (Debajo del QR la pantalla te recuerda: «WhatsApp › Dispositivos vinculados › Vincular un dispositivo».)
2. Escanea el QR grande que aparece en dConnector. El QR **se renueva solo** cada cierto tiempo; si caduca o falla aparece un mensaje en rojo y el botón **«Nuevo código QR»**.
3. Espera unos segundos: pasa solo a «Conectada ✓».

**Pestaña «Con número»** (si no puedes escanear el QR)

1. Escribe tu número con prefijo de país, por ejemplo `+593 99 123 4567`, y pulsa **«Obtener código»**.
2. Aparece un código de **8 caracteres** en grande, con la forma `XXXX-XXXX`.
3. En el teléfono: abre WhatsApp › Ajustes › Dispositivos vinculados › Vincular un dispositivo › elige **«Vincular con el número de teléfono»** y escribe ese código.
4. **«Usar otro número»** vuelve al paso anterior.

Qué pasa después: dConnector descarga el historial reciente que WhatsApp le envía. **Puede tardar un rato** y los chats van apareciendo poco a poco.

### 4.2 Telegram

Telegram no deja usar su red sin identificarte como programa. Por eso **cada cuenta pone su propio `api_id` y `api_hash`**, que se piden gratis en el sitio de Telegram para desarrolladores. Solo se hace una vez por cuenta.

**Cómo conseguir `api_id` y `api_hash` (paso a paso)**

1. Abre **https://my.telegram.org** en el navegador (en el paso 2 del asistente hay un enlace directo, «my.telegram.org»).
2. Escribe el **número de teléfono de esa cuenta de Telegram**, con prefijo de país (por ejemplo `+593…`), y pulsa para continuar.
3. Telegram te manda un código **a tu app de Telegram** (no por SMS). Escríbelo en la página.
4. Entra en **«API development tools»**.
5. Si es la primera vez, rellena el formulario «Create new application»: «App title» (por ejemplo `dConnector`), «Short name» (letras y números, por ejemplo `dconnector`) y, en «Platform», lo que quieras (por ejemplo «Desktop»). Los demás campos pueden quedar vacíos. Pulsa «Create application».
6. La página muestra **«App api_id»** (un número, por ejemplo `1234567`) y **«App api_hash»** (32 letras y números). Cópialos.
7. En dConnector, paso 2 del asistente: pega el número en **api_id** y el texto en **api_hash**. El `api_id` debe ser solo dígitos.

> Trátalos como una contraseña: identifican tu aplicación ante Telegram. No los compartas. Si la página da un error genérico al crear la aplicación, vuelve a intentarlo más tarde.

**Vincular (paso 3)**

1. **Número de teléfono** (con prefijo, por ejemplo `+593 99 123 4567`) › **«Enviar código»**.
2. **Código recibido:** Telegram lo manda a tu app de Telegram en otro dispositivo (o por SMS si no tienes otra sesión). Escríbelo y pulsa **«Continuar»**. Si te equivocas, aparece «Código incorrecto» y vuelves a escribirlo; si caduca, se vuelve al paso del teléfono.
3. **Contraseña:** solo si la cuenta tiene verificación en dos pasos. Escribe la contraseña de Telegram y **«Continuar»**. (Si falla: «Contraseña incorrecta».)
4. «Conectada ✓».

Si el número no tiene cuenta de Telegram, el asistente te devuelve al paso del teléfono con un aviso. Dentro de dConnector no se crean cuentas nuevas de Telegram.

**Si te equivocaste con los datos de la aplicación:** en la tarjeta de la cuenta › **«Editar»** puedes corregir `api_id` y `api_hash`; al guardar, la cuenta se reconecta con los datos nuevos.

### 4.3 Messenger e Instagram (pegando cookies)

> **Vía no oficial: Meta puede bloquear la cuenta.** dConnector entra como si fuera tu navegador, con las «cookies» de una sesión ya iniciada. Meta no lo permite oficialmente. Posibles consecuencias: te pida verificar tu identidad, cierre la sesión, limite la cuenta o la suspenda. **No lo uses con una cuenta que no puedas perder.**

Una *cookie* es un dato pequeño que el navegador guarda cuando inicias sesión, y que dice «esta persona ya entró». **Quien tenga tus cookies puede entrar como tú**: trátalas como una contraseña, no las pegues en ninguna página y no las compartas.

**Cookies que hacen falta**

| Canal | Obligatorias | También sirven (opcionales) |
| --- | --- | --- |
| Messenger | `c_user`, `xs`, `datr` | `fr` |
| Instagram | `sessionid`, `ds_user_id`, `csrftoken` | `mid` |

La ayuda desplegable de la pantalla («Cómo copiar las cookies») lista también `fr` y `mid` como si fueran necesarias; **no hace falta** que estén. Si falta una obligatoria, dConnector te dice cuál.

**Cómo copiarlas desde las herramientas del navegador (Chrome, Edge)**

1. Abre **messenger.com** (para Messenger) o **instagram.com** (para Instagram) e **inicia sesión** con la cuenta.
2. Pulsa **`F12`** (se abren las herramientas para desarrolladores).
3. Arriba, entra en la pestaña **«Aplicación»** (si no la ves, pulsa `»` para ver más pestañas). En Firefox se llama **«Almacenamiento»**.
4. A la izquierda: **«Almacenamiento»** › **«Cookies»** › la dirección del sitio (por ejemplo `https://www.messenger.com`).
5. En la lista, busca cada cookie por su **«Nombre»** y copia su **«Valor»** (doble clic en el valor para seleccionarlo, `Ctrl+C`).
6. Escríbelas juntas en un bloc de notas con este formato, una detrás de otra, separadas por `;`:

   `c_user=1000123456789; xs=45%3Aabcd...; datr=AbCdEf...`

   (Cada cookie es `nombre=valor`. No quites caracteres raros como `%3A` del valor: se copian tal cual.)
7. Copia todo ese texto y **pégalo en la caja grande «Pega aquí las cookies»** del paso 3. Pulsa **«Vincular»**.

**Alternativa más cómoda:** una extensión como *Cookie-Editor* con **«Exportar › JSON»**. dConnector acepta ese JSON tal cual, y también el formato `nombre=valor; nombre2=valor2`.

**Messenger tiene además una pestaña «Usuario y contraseña»** (solo Messenger; Instagram solo admite cookies). Es un método alternativo que, **según las notas de construcción, todavía no se ha probado**. Si el método no está disponible, la pestaña se oculta sola y se vuelve a «Pegar cookies». Si te pide un código, aparece el paso «Código recibido». Cosas que dConnector no puede mostrar (captcha, aprobar el inicio desde otro dispositivo) te devuelven a las cookies con una explicación.

**Consejos**

- **No cierres la sesión en el navegador después de copiar las cookies**: al cerrar sesión, esas cookies dejan de valer. Cierra solo la pestaña (o usa un perfil o ventana aparte para esa cuenta).
- Si más tarde la cuenta pasa a «Sesión cerrada», es que Meta cerró esa sesión: copia cookies nuevas y usa **«Volver a vincular»**.
- **«Cerrar sesión» en dConnector solo borra la sesión local.** Meta no deja cerrar la sesión web desde fuera: la sesión puede seguir apareciendo en «Seguridad» / «Actividad de inicio de sesión» de la cuenta hasta que la cierres tú allí.
- **Chats cifrados de extremo a extremo de Messenger (E2EE):** son la mayoría de los chats personales desde 2024. dConnector intenta mostrarlos, pero esa parte **no se ha podido probar** y es de lo más frágil de esta versión. Si falla, se desactiva con un cambio y la tarjeta de la cuenta mostrará el aviso fijo «Los chats cifrados de extremo a extremo aún no se muestran». Los chats cifrados **antiguos** (el historial anterior a conectar) no se pueden traer en ningún caso. Una sesión iniciada con «Usuario y contraseña» no usa chats cifrados.

### 4.4 Qué significan los estados de una cuenta

| Texto en la tarjeta | Significa |
| --- | --- |
| **Conectada** | Todo bien: recibe y envía |
| **Conectando…** | Está abriendo la conexión |
| **Esperando vinculación** | Falta escanear el QR, poner el código, o pegar las cookies. Botón **«Volver a vincular»** |
| **Desconectada** | La apagaste con «Desconectar» o aún no arrancó |
| **Sesión cerrada** | El canal cerró la sesión (la cerraste desde el teléfono, caducó, la revocó Meta…). Hay que **volver a vincular** |
| **Error** | Falló algo (red, servidor…). dConnector **reintenta solo** con espera creciente (2 s, 4 s, 8 s… hasta 5 min). El motivo está debajo en gris |
| **Desactivada** | Marcaste la cuenta como no activa en «Editar» |

Si el teléfono con WhatsApp lleva mucho tiempo apagado o sin internet, o abres la sesión en demasiados sitios, WhatsApp puede cerrar la sesión de dConnector. Se arregla con «Volver a vincular».

---

## 5. Chats

Pantalla **Chats** (`Ctrl+1`). Tres columnas: la lista de chats (360 px), la conversación y el panel de información (360 px, que se pliega solo si la ventana es estrecha). Con ninguna conversación abierta ves el logotipo «dConnector».

### 5.1 La lista

- **«Buscar chat»:** filtra por el título del chat.
- **Fila de chips de cuentas:** **«Todas»**, un chip por cada canal presente (WhatsApp, Telegram…) y un chip por cada cuenta (con su color y etiqueta). Puedes marcar **varios a la vez**. Se recuerda entre sesiones. Es la forma de ver solo «Ventas» o solo Telegram.
- **Pestañas:** **«Todos»**, **«No leídos»** (con el contador), **«Grupos»**, **«Archivados»**.
- **Cada fila:** la foto de la persona con la insignia del canal en la esquina; el título; la hora a la derecha (hoy `HH:mm`, ayer «Ayer», esta semana el día, y si no `dd/MM/aa`); la vista previa (con «Tú: » si fue tuyo y su marca de entrega) y la **píldora de cuenta** (color y etiqueta, hasta 12 caracteres); el contador de no leídos, el símbolo de fijado y el de silenciado.
- **Clic derecho en un chat:** «Fijar chat» / «Desfijar chat», «Archivar chat» / «Desarchivar chat», «Silenciar 8 horas», «Silenciar 1 semana», «Silenciar siempre» y «Activar sonido». (En Messenger e Instagram fijar, archivar y silenciar **no están disponibles**: dan un aviso de «no admitido».)
- La lista carga más chats al bajar.
- **`Ctrl+K`** abre **«Buscar en todos los mensajes»**: busca en los mensajes guardados de **todas** las cuentas a la vez. Pulsa un resultado para abrir ese chat (no salta hasta el mensaje exacto). `Esc` lo cierra.

### 5.2 La conversación

**Cabecera:** foto, título, insignia del canal y píldora de cuenta, y «escribiendo…» / «grabando audio…» cuando la otra persona lo hace (en grupos, «Ana está escribiendo…»). Botones: **«Buscar en el chat»** (busca solo entre los mensajes ya cargados; con «Anterior», «Siguiente» y **«Buscar más atrás»** para cargar más), **«Archivar»**/«Desarchivar», **«Silenciar»**/«Activar sonido» y **«Información»**.

**Mensajes.** Aparecen separados por día. Cada tipo se ve así:

| Tipo | Cómo se ve |
| --- | --- |
| Texto | Con formato de WhatsApp: `*negrita*`, `_cursiva_`, `~tachado~`, `` `código` ``. Los enlaces son clicables y se abren en tu navegador |
| Foto | Miniatura que pasa a la foto completa al cargar; clic = visor a pantalla grande (con «Guardar como», anterior/siguiente y «Cerrar») |
| Vídeo | Reproductor con controles |
| Nota de voz / audio | Reproductor con velocidades **1×, 1,5×, 2×** |
| Documento | Icono, nombre, tamaño y botón **«Guardar como»** |
| Sticker | Grande, sin burbuja |
| Ubicación | Tarjeta con las coordenadas y el enlace **«Ver en OpenStreetMap»** (abre el navegador; no hay mapa dentro de la app) |
| Contacto | Tarjeta con el nombre y el teléfono, y **«Abrir chat»** si esa persona tiene chat |
| Encuesta | Opciones con barras, número de votos y, si el canal lo permite, puedes **votar** pulsando una opción |
| Mensajes de sistema | Centrados y en gris (alguien entró al grupo, cambió el asunto…) |
| «Mensaje no compatible» | Algo que dConnector no sabe mostrar |
| «Mensaje eliminado» | Se borró para todos |

En grupos, el nombre del que escribe va arriba, con un color propio. Si un mensaje cita otro, la cita va arriba (clic = ir al mensaje, si está cargado). Los reenviados llevan la marca «Reenviado». Debajo, las reacciones en una píldora.

**Marcas de entrega** (junto a la hora de tus mensajes): reloj = pendiente de enviar; una marca = enviado; dos marcas grises = entregado; dos marcas **azules** = leído; triángulo rojo = falló. «editado» aparece si el mensaje se modificó.

**Menú de un mensaje** (clic derecho sobre él, o la flechita que aparece al pasar el ratón, «Opciones del mensaje»):

- Barra de seis reacciones rápidas (👍 ❤️ 😂 😮 😢 🙏) y un botón **«Más emojis»**. Volver a pulsar la tuya la quita.
- **«Responder»**, **«Copiar»**, **«Reenviar»**, **«Editar»**, **«Info.»**, **«Eliminar»**.
- Solo aparece lo que ese canal permite en ese momento (por ejemplo, **«Editar»** solo en tus mensajes de texto y mientras no haya pasado el plazo: 15 minutos en WhatsApp, Messenger e Instagram; 48 horas en Telegram).
- **«Eliminar»** pregunta «¿Eliminar mensaje?» con **«Eliminar para todos»** (si el canal lo permite y estás a tiempo: en WhatsApp, hasta 48 horas), **«Eliminar para mí»** o **«Cancelar»**. «Para mí» solo oculta el mensaje en dConnector; no lo borra en el canal.
- **«Reenviar a…»** abre un buscador con los chats de la misma cuenta del mensaje (no se puede reenviar a otra cuenta).
- **«Info.»** muestra fecha, estado, quién lo envió, tipo y reacciones.

**Escribir y enviar** (zona de abajo):

- Caja **«Escribe un mensaje»**. **Enter** envía, **Mayús+Enter** hace un salto de línea. Cada chat guarda su propio borrador.
- **«Emojis»**: selector de emojis.
- **«Adjuntar»** (clip): menú con lo que ese chat admite: **«Fotos y vídeos»**, **«Documento»**, **«Sticker»**, **«Ubicación»**, **«Contacto»**, **«Encuesta»**. Al elegir archivos ves una **vista previa**; en cada uno puedes poner un **pie** («Añade un pie») y quitar, **«Añadir más»**, **«Cancelar»** o enviar.
  - Ubicación: escribes **Latitud** y **Longitud** a mano (más nombre y dirección opcionales); no hay botón «usar mi ubicación».
  - Contacto: **Nombre** y **Teléfono** (+593…).
  - Encuesta: **Pregunta**, de 2 a 12 opciones y la casilla **«Permitir varias respuestas»**.
  - Stickers: en WhatsApp solo se aceptan archivos `.webp`.
- **Arrastrar y soltar** archivos sobre la conversación («Soltar para adjuntar»), y **pegar** una imagen con `Ctrl+V`. Cada archivo soltado o pegado puede pesar hasta 50 MB; para archivos mayores usa el clip.
- **Micrófono** («Grabar nota de voz»): pulsa para grabar (aparece un contador de tiempo); **«Descartar»** o **«Enviar nota de voz»**. Hasta 15 minutos. Windows te pedirá permiso para el micrófono la primera vez; si lo niegas, un aviso lo explica.
- Sobre la caja aparece una barra cuando **respondes** (cita del mensaje) o **editas** («Editando mensaje»); la **X** cancela. En edición el botón de enviar pasa a **«Guardar cambios»**.
- Si el archivo supera el tamaño máximo del canal, un aviso lo dice antes de enviar.
- Si el chat es de solo lectura (canal de difusión, grupo de «solo administradores»), en lugar de la caja verás **«Solo los administradores pueden enviar mensajes»**.

dConnector manda «escribiendo…» mientras tecleas (como mucho cada 4 s) y marca como **leído** el chat al abrirlo si la ventana tiene el foco.

### 5.3 Panel de información

Se abre con **«Información»**. Muestra la foto grande, el título, el canal y la cuenta, y:

- **Contacto:** si esa persona ya es un contacto, su nombre y las **otras cuentas** donde también está, cada una con **«Abrir chat»**. Si no lo es, el botón **«Crear contacto»** (solo en chats individuales).
- **Listas:** chips para meter o sacar el contacto de una lista.
- **Etiquetas** (con «Añadir etiqueta») y **Notas**.
- **Archivos:** cuadrícula de fotos y vídeos del chat y los documentos.
- **Estadísticas:** enviados, recibidos y total (solo si hay datos).

### 5.4 Historial

- Al **vincular** una cuenta, el canal manda un historial inicial y los chats se llenan solos.
- Al **subir** en una conversación hasta el final, dConnector pide al canal mensajes más antiguos («Cargando…»). Pide como mucho una vez cada 5 segundos por chat, y el canal decide cuánto entrega.
- Los archivos (fotos, audios…) se **descargan automáticamente** según **Ajustes › Descargas automáticas** (por tipo y tamaño). Los demás se descargan al abrirlos o al pulsar «Guardar como».

---

## 6. Estados

Pantalla **Estados** (`Ctrl+2`): los estados de WhatsApp y las historias de Telegram. Messenger e Instagram **no tienen** esta función en dConnector. Si no hay ninguna cuenta con estados verás «Sin cuentas con estados».

**Columna izquierda**

- Selector de **cuenta** (solo las que admiten estados).
- **«Mis estados»** con tu anillo y el botón **«+»** («Nuevo estado»).
- **«Recientes»** (no vistos, con anillo segmentado) y **«Vistos»**. Pulsa una persona para abrir el visor.

**Zona principal.** Dos pestañas, **«Mis estados»** y **«Estadísticas»**, y el botón **«Nuevo estado»**.

**El visor (pantalla completa)**

- Barras de progreso arriba; 5 s por foto o texto y la duración del vídeo.
- `←` y `→` (o tocar los lados) cambian de estado; mantener pulsado hace **pausa**; `Esc` o **«Cerrar»** salen.
- **Reaccionar** (seis emojis) solo existe en Telegram. En WhatsApp **no** se puede reaccionar a un estado desde dConnector.
- **«Responder»** abre el chat de esa persona con el estado como cita. (Que la respuesta llegue como cita de estado no se ha probado.)
- Cada estado ajeno se marca como **visto** al mostrarse. Eso avisa a su autor.

**Publicar: «Nuevo estado»**

- Pestaña **«Texto»**: el texto, 12 colores de fondo («Color»), 4 tipos de letra («Letra») y una vista previa. Hay contador de caracteres.
- Pestaña **«Foto o vídeo»**: **«Elegir foto o vídeo»** (se ve su nombre y tamaño; sin vista previa) y un **«Pie»** opcional.
- **«Audiencia»:**
  - **WhatsApp:** texto fijo «Se usa la privacidad de estados del teléfono». dConnector **no puede** elegir quién lo ve.
  - **Telegram:** **«Mis contactos»**, **«Solo compartir con…»** o **«Mis contactos excepto…»**, eligiendo **listas** y **contactos**.
- Botones **«Publicar»**, **«Programar»** (aparece «Fecha y hora» y **«Confirmar»**: queda en la pantalla Programados) y **«Cancelar»**.
- **Telegram no admite historias solo de texto:** si lo intentas, un aviso lo explica. Además, publicar historias solo funciona si tu cuenta de Telegram puede publicarlas (por ejemplo, con Premium o dentro del límite); si no, el aviso dice el motivo.

**«Mis estados»** muestra una cuadrícula de tus estados de las últimas 24 horas. Pulsa uno para abrir **«Mi estado»**: lista de **«Vistas»** (quién, a qué hora, con qué reacción), contador y **«Borrar»** (pregunta «Borrar estado»; se borra para todos).

**«Estadísticas»**: periodo de **7, 30 o 90 días**, con **«Publicados»**, **«Vistas medias»** y **«Alcance»** (en **número de personas**, no en porcentaje), con la variación frente al periodo anterior, y dos curvas: **«Vistas»** por día y **«Vistas desde la publicación»**.

dConnector refresca las vistas de tus estados cada 10 minutos (la primera vez, al minuto de arrancar) y también las recibe en vivo cuando el canal las avisa.

---

## 7. Campañas

Pantalla **Campañas** (`Ctrl+3`): envías un mismo mensaje (con variantes) a muchas personas **de forma prudente**, para no ser bloqueado.

> **Solo se pueden hacer campañas con cuentas de WhatsApp y de Telegram.** Messenger e Instagram no hacen campañas.

La lista muestra cada campaña con su cuenta, el **estado** (Borrador, Programada, En curso, En pausa, Frenada, Terminada, Cancelada), el progreso **enviados/total** y lo que pasa ahora: «Fuera de horario», «Límite por hora», «Límite del día», «Descanso», «Cuenta desconectada», «Próximo envío 14:32» o «Frenada: <motivo>». Arriba: **«Ritmo de la cuenta»** y **«Nueva campaña»**.

### 7.1 El asistente «Nueva campaña» (5 pasos)

1. **Nombre y cuenta:** el **Nombre** y la cuenta (solo WhatsApp o Telegram activas; si no están conectadas el motor espera y lo dice).
2. **Audiencia:** marca **Listas** y/o **Contactos** sueltos (con buscador). A la derecha ves cuántos **Destinatarios** serán, repartidos por riesgo, y los **Omitidos** (ver abajo).
3. **Mensaje:** el texto, las **variantes** (pestañas A, B, C, D, E, con «+» para añadir), un **adjunto** opcional («Adjuntar») y el interruptor **«Pie de baja»**. A la derecha, el panel **«Revisión»** se actualiza mientras escribes.
4. **Envío:** **Inicio** («Ahora» o «Fecha y hora»), **Ventana horaria** (de qué hora a qué hora) y **días** (Lun…Dom), **«Prueba A/B»** y **«Ritmo de la cuenta»** con el botón **«Ajustar»**.
5. **Resumen:** todo junto, con la **Duración** estimada, y el botón **«Lanzar»**.

Abajo siempre hay **«Guardar borrador»**, **«Atrás»** y **«Siguiente»**. La campaña no sale hasta que pulsas **«Lanzar»**; antes puedes cerrar y seguir luego. Una campaña ya guardada y programada o en pausa se edita con **«Guardar cambios»** (en ese caso la cola ya generada no se rehace).

> **Ojo con la ventana horaria.** Por defecto es de **09:00 a 18:00, de lunes a viernes**. Fuera de ese horario la campaña queda esperando («Fuera de horario»). Si quieres probar a otra hora, amplía la ventana en el paso 4 (por ejemplo de 00:00 a 23:59 y todos los días). La hora de inicio debe ser menor que la de fin: no se admiten ventanas que crucen la medianoche.

### 7.2 Etiquetas en el texto

- `{nombre}` se sustituye por el **primer nombre** del contacto (botón **«{nombre}»**).
- `{hola|buenas|qué tal}` elige **una opción al azar por persona** (botón **«{a|b}»**).
- La vista previa usa «Ana» y la primera opción, y muestra debajo el pie de baja.

### 7.3 Variantes y revisión (semáforo del mensaje)

Cada persona recibe **una** variante, repartidas por igual y en orden mezclado. El panel **«Revisión»** tiene un semáforo con tres niveles:

| Nivel | Texto del panel | Qué significa |
| --- | --- | --- |
| Verde | «Todo en orden» | Nada que corregir |
| Amarillo | «Revisa los avisos» | Hay avisos, pero **puedes lanzar** |
| Rojo | «Corrige antes de lanzar» | **«Lanzar» queda bloqueado** hasta corregir |

Lo que revisa: enlaces acortados (rojo), enlaces en general (amarillo), MAYÚSCULAS, exceso de emojis, palabras típicas de spam («gratis», «urgente»…), mensajes casi iguales a los de otras campañas recientes (30 días), variantes demasiado parecidas entre sí, falta de cuenta, mensaje vacío y cero destinatarios. **Desde 30 destinatarios hacen falta al menos 3 mensajes distintos** (variantes u opciones `{a|b}`); con menos no es obligatorio.

### 7.4 Destinatarios, riesgo (verde, amarillo, rojo) y omitidos

En el paso 2, los destinatarios se cuentan por **riesgo de bloqueo**, con un punto de color:

| Punto | Etiqueta | Significa |
| --- | --- | --- |
| Verde | **Conocidos** | Esa persona te ha escrito (hay mensajes suyos entre los últimos 200 del chat) |
| Amarillo | **Sin respuesta** | Hay chat, pero solo mensajes tuyos |
| Rojo | **Fríos** | Nunca habéis hablado: no hay chat, o está vacío. **Es el motivo principal de bloqueo** |

Los **Omitidos** son los que no recibirán nada, y se guardan en la cola como «omitidos» con su motivo:

- **Baja:** pidió no recibir más mensajes (o está marcado «No recibe campañas»).
- **Sin identidad:** ese contacto no tiene datos en la cuenta de la campaña. Un contacto creado a mano o desde un CSV **no tiene identidad** hasta que dConnector lo asocia a un chat de la cuenta (con **«Importar de una cuenta»**, o con **«Crear contacto»** desde el chat; ver [Contactos](#9-contactos)).
- **Frecuencia:** ya recibió demasiadas campañas en pocos días (por defecto, 3 en 7 días, contando todas las campañas).

### 7.5 «Modo de envío seguro»: el ritmo anti-bloqueo

dConnector envía **de uno en uno, con pausas, como lo haría una persona**. Los valores por defecto se llaman **«valores seguros»**. Se editan en **«Ritmo de la cuenta»** (cabecera de Campañas, y en el paso 4 con «Ajustar»): eliges la cuenta y ves seis grupos. **«Restablecer valores seguros»** los devuelve a los de fábrica; **«Guardar»** aparece cuando cambias algo. El ritmo es **por cuenta**.

| Grupo | Campo | Valor seguro | Qué hace |
| --- | --- | --- | --- |
| **Esperas** | Mínima / Máxima | 20 / 45 s | Pausa al azar entre envíos a conocidos y sin respuesta |
| | Mínima / Máxima (fríos) | 60 / 120 s | Pausa mayor con quienes nunca hablaron contigo |
| | Escribiendo… | 300 ms por letra | Antes de cada envío muestra «escribiendo…» (con tope de 8 s) |
| **Límites** | Por hora | 60 | Máximo de envíos de campaña en una hora (0 = sin límite) |
| | Por día | 200 | Máximo al día (0 = sin límite) |
| | Fríos por día | 20 | Máximo de contactos nuevos al día (0 = sin límite propio) |
| **Tandas** | Tamaño | 25 envíos | Después de esta cantidad, descansa |
| | Descanso | 10 min | Entre tandas, con ±30 % de variación (0 = sin tandas) |
| **Calentamiento** | Activado | Sí | Una cuenta «nueva» sube el ritmo poco a poco |
| | Primer día | 20 envíos | Límite del primer día |
| | Duración | 14 días | Sube en línea recta hasta el límite por día |
| **Freno** | Activado | Sí | Frena la campaña si algo va mal |
| | Fallos | 30 % | Frena si fallan tantos envíos (se mide desde 10 intentos) |
| | Mínimo de envíos | 30 | Antes de comprobar «Sin leer» |
| | Sin leer | 95 % | Frena si, con al menos 30 enviados y 2 horas, casi nadie los lee y nadie responde |
| | Bajas | 3 | Frena al llegar a este número de bajas durante la campaña |
| **Frecuencia** | Campañas por contacto | 3 | Máximo de campañas a la misma persona (0 = sin límite) |
| | En | 7 días | ...dentro de este periodo |

Cómo se comporta el motor:

- **Un solo envío a la vez por cuenta**, aunque haya varias campañas en curso (se atienden por orden de creación).
- Los límites por hora y por día cuentan **todos** los envíos de campaña de esa cuenta.
- El **calentamiento** empieza a contar desde el **primer envío correcto** de campaña de la cuenta.
- Si la cuenta se **desconecta**, la campaña espera («Cuenta desconectada»); no cuenta como fallo.
- Si el canal responde que te pasaste de ritmo, descansa como una tanda (mínimo 1 minuto).
- Antes de enviar a alguien, vuelve a comprobar que sigue sin haberse dado de baja.
- Si cierras dConnector, **la campaña se detiene**; al abrir, retoma lo que faltaba (las esperas empiezan de cero). Para que corra necesita que la app esté abierta (puede estar en la bandeja) y la cuenta conectada.

### 7.6 Freno automático

Cuando se cumple una regla de freno la campaña pasa a **«Frenada»** y muestra el motivo, por ejemplo: «Freno automático: fallaron 12 de 30 envíos (40 %). Revisa los números o la conexión.», «…3 personas pidieron no recibir más mensajes…» o «…casi nadie lo leyó…». Para seguir, pulsa **«Reanudar»** (se reinician los contadores del freno) o **«Cancelar»**. Antes de reanudar, **revisa por qué** frenó.

### 7.7 Pie de baja y respuestas

- Con **«Pie de baja»** activado, cada mensaje termina con el texto de **Ajustes › Campañas › «Texto del pie»** (por defecto, «Responde BAJA para no recibir más mensajes.»).
- Si alguien contesta con **un mensaje que es exactamente** una de las **palabras de baja** (por defecto: baja, stop, no más, cancelar, cancelar suscripción, unsubscribe, no quiero recibir), dConnector lo marca **«No recibe campañas»** automáticamente, aunque escriba en mayúsculas, sin tildes o con signos. Una frase que solo *contiene* la palabra (por ejemplo «quiero la baja del plan») no cuenta.
- Esas bajas aparecen en el contador **«Bajas»** de la campaña, y también cuentan para el freno.

### 7.8 Prueba A/B

Con **«Prueba A/B»** activada (hacen falta al menos dos mensajes escritos), dConnector hace esto:

1. Envía primero una **muestra** (20 % de cada variante).
2. Espera unas **4 horas**.
3. Elige las **2 mejores** variantes (puntúan más las respuestas, algo las lecturas, y restan las bajas) y reparte el resto entre ellas.

Mientras tanto, el resto de destinatarios queda «retenido» en la cola. Los valores (20 %, 4 h, 2 ganadoras) son fijos: no se pueden cambiar en esta versión.

### 7.9 Detalle, cola, pausar y reanudar

Pulsa una campaña para abrir su detalle (flecha **«Volver»**). Arriba: nombre, cuenta, estado, lo que pasa ahora y los botones que valgan en ese estado:

- **«Editar»** (borrador, programada o en pausa), **«Pausar»** (en curso o programada), **«Reanudar»** (en pausa o frenada), **«Cancelar»** (deja de enviar lo que falta; pide confirmación) y **«Eliminar»** (borra la campaña con su cola; no está disponible mientras corre).

En el cuerpo: **«enviados/total»** con su barra y seis contadores: **Pendientes, Enviados, Fallidos, Omitidos, Respuestas, Bajas**. Debajo el **Mensaje** y la **cola** con pestañas **Pendientes, Enviados, Fallidos y Omitidos**. La cola identifica a cada persona por su número o id (sin el `@…`), con el punto de riesgo; en los fallidos y omitidos se ve el motivo. Se muestran hasta 200 filas por pestaña.

### 7.10 Adjuntos

Una campaña puede llevar **un** adjunto (imagen, vídeo o documento); el texto va como pie. Se copia a la carpeta de datos, así que puedes mover o borrar el original.

---

## 8. Programados

Pantalla **Programados** (`Ctrl+4`): mensajes (y estados) que se envían solos a una hora. Es una tabla con **Fecha, Cuenta, Destino, Tipo, Contenido y Estado**.

- Filtros arriba con su recuento: **«Todos»**, **«Pendientes»**, **«Enviados»**, **«Fallidos»**, **«Cancelados»**.
- **«Nuevo»:** elige **Cuenta** (Elige una cuenta) › **Chat** (buscador «Buscar chat»; no ofrece chats de solo lectura) › **Mensaje** (texto, y **«Adjuntar»** un archivo) › **Fecha y hora** › **«Programar»**. Se controla según lo que admite el canal.
- Acciones de cada fila: **«Editar»** (si está pendiente o fallido, y es un mensaje), **«Enviar ahora»**, **«Cancelar»** (solo pendientes) y **«Borrar»**.
- Los programados de tipo **Estado** (creados desde Estados › «Programar») se ven con destino «Estados»; solo se pueden enviar ahora, cancelar o borrar.
- La tabla se recarga sola cada 15 segundos.

Cómo se comporta: dConnector revisa **cada 15 segundos**. Si a la hora de enviar la cuenta **no está conectada**, el programado **espera**; si pasan **6 horas** desde la hora prevista sin poder enviarlo, queda como **«Fallido»** («La cuenta no estuvo conectada»). Un fallido no se reintenta solo: lo corriges con «Editar» (vuelve a pendiente) o lo mandas con «Enviar ahora». **Para que envíe, dConnector tiene que estar abierto** (en la bandeja vale).

---

## 9. Contactos

Pantalla **Contactos** (`Ctrl+5`). Un **contacto** es una persona, y puede tener varias **identidades** (su número en WhatsApp, su usuario de Telegram, su perfil de Messenger…). Así sabes que «Ana» de WhatsApp y «Ana» de Telegram son la misma.

**Izquierda: «Listas».** **«Todos»** y tus listas (con el número de miembros). El **+** («Nueva lista») crea una (nombre y color); el lápiz la edita y la papelera la elimina (los contactos **no** se borran). Pulsar una lista filtra la tabla.

**Cabecera:** el total, **«Buscar»** (nombre o teléfono), **«Etiqueta»** (filtra por etiqueta; también puedes pulsar la etiqueta de una fila), **«Nuevo contacto»** y el menú de tres puntos con **«Importar de una cuenta»**, **«Importar CSV»** y **«Sugerencias de unión»**.

**La tabla:** Nombre, Teléfono, Canales (iconos), Listas y Etiquetas. Un símbolo de campana tachada indica **«No recibe campañas»**.

**La ficha (panel de la derecha)** al pulsar un contacto:

- **Identidades:** una por cuenta, con **«Abrir chat»** y **«Separar»** (convierte esa identidad en otro contacto; no se puede separar la única).
- Nombre, Teléfono, **Etiquetas**, **Notas**, **Listas** y el interruptor **«No recibe campañas»**.
- **«Unir con otro»** (abre «Unir contactos»: buscas, ves los dos lado a lado, y puedes invertir cuál se conserva), **«Eliminar»** (sus chats no se tocan) y **Guardar / Descartar** (aparece «Cambios sin guardar»).

**Teléfonos:** se normalizan a formato internacional. Un número ecuatoriano de 10 dígitos que empieza por 0 (por ejemplo `099 123 4567`) pasa a `+593 99 123 4567`. Para otros países escribe siempre el `+` y el prefijo.

### Importar contactos

- **«Importar de una cuenta»:** eliges una cuenta **conectada** y dConnector trae sus contactos. Volver a importar **no duplica**: omite los que ya existen o actualiza los que cambiaron. Si el teléfono ya es de un contacto, le **añade la identidad** en lugar de crear otro. Muestra un resumen al final (creados, actualizados, omitidos). Si el canal no ofrece su agenda, avisa de que no está admitido (es el caso de Messenger e Instagram).
- **«Importar CSV»:** **«Elegir archivo»** (CSV), casilla **«Tiene encabezado»**, y para cada dato (**Nombre, Teléfono, Etiquetas, Notas**) el **número de columna** (1…20, o «No importar»). **«Añadir a la lista»** mete a todos los importados en una lista. Acepta separador `;` o `,`, archivos con acentos de Excel y hasta 50 MB; las etiquetas pueden ir separadas por `,`, `;` o `|`. **No hay vista previa de las filas**: cuenta tú las columnas con el archivo abierto en Excel. Volver a importar el mismo archivo da todo «omitido».
- **«Sugerencias de unión»:** pares de contactos que comparten teléfono, para **«Unir»** o **«Ignorar»**.

> **Importante para campañas:** los contactos creados a mano o desde un CSV nacen **sin identidad en ninguna cuenta**, y una campaña los omite como «Sin identidad». Para que reciban campañas, después usa **«Importar de una cuenta»** (los asocia por teléfono) o crea el contacto desde su chat con **«Crear contacto»**.

---

## 10. Estadísticas

Pantalla **Estadísticas** (`Ctrl+6`). Todo se calcula con los mensajes guardados en tu equipo.

- **Periodo:** **«7 días»**, **«30 días»**, **«90 días»** o **«Personalizado»** (dos fechas, «Desde» y «Hasta»; no admite fechas futuras).
- **Cuentas:** si tienes más de una, chips **«Todas»** y una por cuenta (se recuerda).
- **Pestaña «Conversaciones»:** KPIs con su variación frente al periodo anterior: **Enviados, Recibidos, Chats con actividad, Tu tiempo de respuesta, Respondes en menos de 1 h, Te responden en, Conversaciones que inicias, Conversaciones que inician** (una conversación nueva es la que empieza tras 8 horas o más sin mensajes). Gráficos: **«Mensajes por día»**, **«Horas de más actividad»** (mapa de calor día × hora, con «Qué se envía»), **«Tiempos de respuesta»**, **«Quién inicia»**, **«Chats más activos»** y **«Chats más frecuentes»** (clic = abrir el chat).
- **Pestaña «Grupos»:** **Mensajes en grupos, Enviados, Recibidos, Grupos activos**, **«Grupos más activos»** y **«Personas más activas»**.

Si no hay mensajes aparece «Sin mensajes en este periodo» / «Sin actividad en grupos». Las estadísticas solo cuentan chats individuales (los grupos van a la pestaña Grupos); los canales de difusión y el chat contigo mismo no cuentan.

---

## 11. Destinos y el protocolo

### 11.1 Qué es un destino

Un **destino** es un **servidor tuyo** (un bot, un almacén de mensajes, una IA) al que dConnector le envía, en tiempo real, lo que pasa en tus cuentas, y que —si tú lo permites— puede **actuar por ti** a través de dConnector (mandar mensajes, reaccionar…), **sin tener que hablar con WhatsApp, Telegram o Meta directamente**.

- **dConnector es el cliente**: abre una conexión **WebSocket** hacia la dirección que le des. Tu servidor solo tiene que aceptarla.
- Habla el **protocolo v1** (documentado en `docs/PROTOCOLO-v1.md`, pensado para quien programa el servidor).
- Los eventos se guardan antes de enviarse y se reenvían hasta que el servidor los **confirma** (al menos una vez). Si tu servidor está apagado, esperan hasta 7 días o 50.000 eventos por destino, y salen cuando vuelve.
- Los **códigos QR y los códigos de vinculación nunca salen** por el protocolo, ni tampoco la configuración de las cuentas (api_hash, cookies…).

### 11.2 La pantalla Destinos

`Ctrl+8`. Lista de destinos, cada uno con su **estado en vivo**, el icono de terminal si permite comandos, el interruptor de activar y los botones **«Editar»** y **«Eliminar»**.

| Estado | Significa |
| --- | --- |
| **Desactivado** | Apagado con su interruptor |
| **Conectando…** | Intentando conectar (reintenta con esperas de 1 s hasta 60 s) |
| **En línea** | Conectado; a la derecha se ve cuántos eventos están **pendientes** de confirmar |
| **Sin conexión** | Se cortó la conexión |
| **Error** | Falló; el motivo va debajo en rojo. **«Token rechazado»** (el servidor respondió 401 o 403) **no se reintenta**: hay que editar el destino. **«El servidor pidió esperar…»** (el servidor está apagado o preparándose y pidió volver más tarde) reintenta solo cuando pasa ese tiempo, hasta 1 hora; si ya lo encendieron, abre el destino y pulsa «Guardar» (aunque no cambies nada): reconecta al instante |

**«Nuevo destino»** abre el formulario:

- **Nombre** y **Dirección** (`wss://…`). **Debe empezar por `wss://`**, salvo `ws://localhost` y `ws://127.0.0.1` (para probar en tu propio equipo).
- **Token:** una clave que dConnector manda en cada conexión (`Authorization: Bearer <token>`) para que tu servidor sepa que es él. Si lo dejas vacío, **se genera uno largo solo** al guardar (es el que debes copiar en tu servidor; el ojo «Mostrar token» lo enseña). También puedes escribir el tuyo (por ejemplo `secreto`, para pruebas). Se guarda en tu equipo y **nunca** se escribe en el registro.
- **Cuentas** y **Canales:** qué cuentas y canales se reenvían (vacío o «Todas»/«Todos» = todos).
- **Eventos:** casillas con los once eventos del protocolo (Mensaje recibido, Mensaje enviado, Mensaje modificado, Reacción, Acuse de entrega, Chat actualizado, Persona actualizada, Estado de la cuenta, Capacidades de la cuenta, Estado publicado, Estado visto). Con todos marcados se envía todo; hay que dejar al menos uno.
- **«Activo»**; **«Incluir los míos»** (por defecto **apagado**: así no te llegan los mensajes que tú mismo envías, ni sus modificaciones); **«Incluir grupos»** (por defecto **apagado**: ni grupos ni canales); y **«Permitir comandos»**.
- Botones: **«Probar conexión»**, **«Cancelar»**, **«Guardar»**, y el enlace **«Ver el protocolo»** (muestra dentro de la aplicación el documento del protocolo v1, con búsqueda y botón de copiar; también está en `docs/PROTOCOLO-v1.md`).

### 11.3 «Permitir comandos» y su riesgo

Apagado por defecto. Al encenderlo aparece el aviso: **«El servidor podrá enviar mensajes en tu nombre»**.

Con comandos activados, **quien controle ese servidor puede**, a través de tus cuentas: listar tus chats, leer tus mensajes y contactos, **enviar mensajes** (también archivos y estados), reaccionar, editar, **borrar**, reenviar, votar encuestas y marcar leído. Todo por el canal correcto y **con las mismas reglas que si lo hicieras tú** (capacidades y ventanas de tiempo de cada canal). No se saltan el ritmo de las campañas: los comandos de envío no pasan por el motor de campañas.

Reglas de seguridad que dConnector aplica:

- Sin «Permitir comandos», cualquier comando recibe un error «forbidden».
- Solo se atienden cuentas que pasan el filtro del destino.
- Un servidor **no** puede pedir que se envíe un archivo de **tu disco**: solo por una dirección `https` o subiéndolo por el propio protocolo.
- Cada comando queda en el registro (qué destino, qué comando, qué cuenta, si salió bien o no) **sin su contenido**.
- Máximo 16 comandos en curso por conexión.

**Recomendación:** activa «Permitir comandos» **solo** en servidores que controles tú, con `wss://` (conexión cifrada) y un token largo, y desactívalo cuando no lo necesites.

### 11.4 Probar un destino

1. **Guarda** el destino y pulsa **«Probar conexión»** (si el formulario es nuevo o tiene cambios, lo guarda primero y luego prueba). dConnector se conecta, envía su saludo y cierra.
2. Resultado: aviso verde «Conexión correcta» o un aviso rojo con el motivo.
3. Mira el estado en la lista de destinos.

Para una prueba completa sin escribir un servidor, usa el **servidor de ejemplo** (`echo-server`), descrito paso a paso en `docs/QA-EN-VIVO.md`. En resumen: ejecutas `echo-server-<versión>-windows-x64.exe` (o el de tu sistema; viene en las descargas), creas un destino con dirección `ws://127.0.0.1:8787/` y el token `secreto`, activas «Permitir comandos», y desde otro teléfono escribes a tu cuenta: el servidor imprime el evento y contesta «Recibido ✅».

---

## 12. Cuentas (la pantalla)

`Ctrl+7`. Una tarjeta por cuenta, con una franja de su color. En cada tarjeta:

- El **asa** de la izquierda («Arrastrar para ordenar»): arrástrala para cambiar el orden de las cuentas, o con el teclado selecciona el asa y usa **↑ / ↓**.
- Canal, etiqueta, tu nombre y número o usuario, el **estado** (con detalle debajo), y los mensajes sin leer.
- **«Conectar»** / **«Desconectar»** (o **«Volver a vincular»** si hay que vincular). *«Desconectar» no apaga la cuenta para siempre: vuelve a conectar al abrir dConnector.*
- **«Editar»:** **Etiqueta**, **Color**, **api_id/api_hash** (Telegram) y la casilla **«Cuenta activa»** (desmarcarla la desconecta y la deja apagada).
- **Menú «Más acciones»:** **«Volver a vincular»**, **«Cerrar sesión»** (cierra la sesión en el canal si puede, y hay que volver a vincular) y **«Eliminar»**.
- **«Eliminar»** pide escribir la etiqueta de la cuenta para confirmar y **borra sus chats y mensajes guardados en este equipo**, además de intentar cerrar la sesión en el canal.

---

## 13. Ajustes

`Ctrl+9`. Varias secciones a la izquierda; los cambios se guardan con el botón **«Guardar»** de la barra de abajo (que aparece con «Cambios sin guardar»; **«Descartar»** los deshace). **«Conexión»** guarda con sus propios botones (ver [apartado 18](#18-conexión-local-o-servidor-remoto)).

| Sección | Qué hay |
| --- | --- |
| **Conexión** | **Estado** del enlace con el servidor, versión del servidor y de su API, **Modo** (**Local** o **Remoto**), los datos del servidor remoto y **«Seguir en segundo plano»**. Explicado en el [apartado 18](#18-conexión-local-o-servidor-remoto) |
| **General** | **«Cerrar a la bandeja»** (por defecto sí), **«Iniciar con Windows»**, **«Iniciar minimizado»** y **«Tema»**: **«Del sistema»**, **«Claro»** u **«Oscuro»** |
| **Notificaciones** | **«Notificaciones de escritorio»**, **«Sonido»** y **«Mostrar el texto del mensaje»**; el **permiso de Windows**, el estado del modo **«No molestar»** de Windows, **«Probar notificación»** y la **última decisión** (por qué un mensaje avisó o no). Detalle en el [apartado 14](#14-bandeja-del-sistema-cerrar-arranque-con-windows) |
| **Descargas automáticas** | Por tipo: **Fotos, Audios, Notas de voz, Vídeos, Documentos, Stickers**, con su interruptor y el tamaño máximo en MB. Por defecto: fotos, audios, notas de voz y stickers sí (20, 20, 20 y 5 MB); vídeos y documentos no (50 y 20 MB) |
| **Campañas** | **«Palabras de baja»** (separadas por comas) y **«Texto del pie»** |
| **Datos** | La ruta de la carpeta de datos, **«Abrir carpeta»**, **«Abrir registros»** y **«Registro detallado»** |
| **Actualizaciones** | **Versión instalada**, **«Buscar actualizaciones»**, las notas de la versión nueva, **«Actualizar a X.Y.Z»** con su barra de progreso y el interruptor **«Buscar al abrir»**. Ver [Actualizar desde la aplicación](#actualizar-desde-la-aplicación) |
| **Acerca de** | **Versión**, **Protocolo** (v1) y la lista de **licencias de terceros** |

**«Registro detallado»** hace que el registro de errores guarde más información (útil para diagnosticar un fallo). Actívalo solo mientras reproduces un problema.

Si dejas vacías las palabras de baja o el pie, vuelven a sus valores por defecto.

---

## 14. Bandeja del sistema, cerrar, arranque con Windows

**Icono en la bandeja** (junto al reloj de Windows; puede estar en «Mostrar iconos ocultos»):

- Pasar el ratón: **«dConnector»**, o **«dConnector · N sin leer»**.
- **Clic izquierdo** (o doble clic): abre la ventana.
- **Clic derecho:** menú con **«Abrir dConnector»**, una línea por cada cuenta con su estado (por ejemplo «● Ventas (WhatsApp) — Conectada»; las líneas son informativas) y **«Salir»**.

**Cerrar la ventana (la X).** Con **«Cerrar a la bandeja»** activado (por defecto), la X **solo oculta la ventana**: dConnector **sigue abierto** y las cuentas siguen conectadas, recibiendo y enviando (campañas, programados y destinos incluidos). La primera vez, Windows muestra una notificación «dConnector sigue abierto en la bandeja». Con la opción apagada, la X cierra el programa.

**Salir de verdad:** menú de la bandeja › **«Salir»**. Apaga la ventana y, en modo local, el servidor local de forma ordenada (como mucho 10 segundos), **salvo que tengas activado «Seguir en segundo plano»**: en ese caso el servidor local sigue vivo y las cuentas conectadas ([apartado 18](#18-conexión-local-o-servidor-remoto)). Antes de copiar la carpeta de datos, actualizar el programa o mover la carpeta portable, apaga también el servidor (desactiva esa opción y vuelve a salir).

**Notificaciones.** Desde la 0.3.0 la decisión de avisar la toma **el servidor** y la ventana solo **muestra** el aviso:

- Un mensaje **nuevo** entrante de un chat **no silenciado** ni archivado avisa con una notificación de Windows con el título «chat · etiqueta de la cuenta» y, si **«Mostrar el texto del mensaje»** está activo, el principio del mensaje (apagado, dice «Mensaje nuevo»). Al pulsarla se abre ese chat.
- **No** avisa de mensajes propios, de historial viejo, ni cuando estás mirando ese chat con la ventana enfocada. Con **varias ventanas conectadas al mismo servidor** (por ejemplo dos ordenadores), no se avisa si **alguna** de ellas tiene ese chat abierto y enfocado.
- **Sonido propio:** un sonido breve suena con cada mensaje nuevo que merece aviso, aunque el aviso no se vea (por ejemplo con «No molestar» de Windows). Se apaga con **«Sonido»**.
- **Contador en la barra de tareas:** círculo rojo con `1`…`9` y `9+` sobre el icono, con el total de mensajes sin leer.
- Los ajustes («Notificaciones de escritorio», «Sonido» y «Mostrar el texto del mensaje») se guardan **en el servidor**, así que valen para todas las ventanas conectadas a él; el **permiso de Windows** y el modo **«No molestar»** son de **este** equipo y se ven en Ajustes › Notificaciones.
- Si Windows está en **«No molestar»** (campana con una «z») o tiene las notificaciones apagadas para la aplicación, los avisos no se ven aunque lleguen; el sonido propio sí suena. **«Probar notificación»** y **«Última decisión»** (Ajustes › Notificaciones) dicen qué pasó con el último mensaje.

**Arranque con Windows.** Con **«Iniciar con Windows»** dConnector se abre solo al iniciar sesión, **oculto en la bandeja** (con `--minimized`), para que las cuentas estén conectadas desde el principio. Se activa y se desactiva desde Ajustes; en el portable apunta a la ruta actual del `.exe` (si lo mueves, vuelve a guardar el ajuste).
Nota: «Iniciar minimizado» solo afecta al arranque con Windows: si «Iniciar con Windows» está activo y «Iniciar minimizado» también, el programa arranca oculto en la bandeja; si abres dConnector a mano, la ventana siempre se abre.

**Una sola copia.** Abrir `dConnector.exe` otra vez muestra la ventana ya abierta (aunque estuviera en la bandeja).

---

## 15. Atajos de teclado

| Atajo | Qué hace |
| --- | --- |
| `Ctrl+1` | Chats |
| `Ctrl+2` | Estados |
| `Ctrl+3` | Campañas |
| `Ctrl+4` | Programados |
| `Ctrl+5` | Contactos |
| `Ctrl+6` | Estadísticas |
| `Ctrl+7` | Cuentas |
| `Ctrl+8` | Destinos |
| `Ctrl+9` | Ajustes |
| `Ctrl+K` | Búsqueda en todos los mensajes (te lleva a Chats si estabas en otra pantalla) |
| `Esc` | Cierra el diálogo o la ventana emergente que esté arriba (también el visor de estados y la búsqueda) |
| `Enter` / `Mayús+Enter` | En un chat: enviar / salto de línea |
| `←` / `→` | En el visor de estados: anterior / siguiente |

---

## 16. Cuando algo falla

### 16.1 El registro de errores

dConnector escribe un registro **sin texto de mensajes**, sin nombres de chats y sin números completos: solo qué cuenta, qué canal, qué tipo de evento, los **últimos 4 caracteres** de los ids y los errores.

- Carpeta: **Ajustes › Datos › «Abrir registros»**, o `logs\` dentro de la carpeta de datos.
- Un archivo por día: `dconnector-AAAA-MM-DD.log` (por ejemplo `dconnector-2026-11-12.log`). Se guardan **14 días**.
- Es texto con una línea por suceso (formato JSON). Si algo falla, **copia el archivo del día** (o el de ese día y el anterior) y envíalo junto con la descripción de lo que hiciste. Con «Registro detallado» activado trae más detalle.
- Aunque el registro no lleva el texto de los mensajes, **sí puede llevar los ids acortados y los errores de cada canal**; revísalo si te preocupa.

### 16.2 Problemas típicos

| Qué ves | Qué hacer |
| --- | --- |
| Windows muestra «Windows protegió su PC» | Es normal: «Más información» › «Ejecutar de todas formas» ([apartado 1](#el-aviso-de-smartscreen-windows-protegió-su-pc)) |
| La ventana sale en blanco o no abre | Falta WebView2 (muy raro en Windows 10/11 actualizados). Instala «Microsoft Edge WebView2 Runtime» desde la web de Microsoft |
| Abro dConnector y «no pasa nada» | Quizá ya está abierto en la bandeja: mira junto al reloj. Una segunda copia solo muestra la primera |
| Cerré con la X y las cuentas siguen conectadas | Es lo previsto («Cerrar a la bandeja»). Para salir de verdad: bandeja › «Salir» |
| WhatsApp: el QR no avanza | Comprueba que el teléfono tenga internet y que escaneas desde «Dispositivos vinculados». Pulsa «Nuevo código QR». Alternativa: pestaña «Con número» |
| WhatsApp: «Sesión cerrada» | Cerraste el dispositivo desde el teléfono, o WhatsApp lo cerró por inactividad. «Volver a vincular» |
| WhatsApp: la misma persona aparece como dos chats (uno con su número y otro con un id raro) | Puede ocurrir con los identificadores internos de WhatsApp (`@lid`), que dConnector intenta unir al número. Anótalo y guarda el registro (ver `QA-EN-VIVO.md`) |
| Telegram: «Falta api_id/api_hash» | Edita la cuenta y pon los dos datos ([apartado 4.2](#42-telegram)) |
| Telegram: «Telegram pidió esperar N s…» | Telegram frena a quien manda demasiado rápido (floodwait). Espera ese tiempo y reintenta; no insistas |
| Telegram: «Código incorrecto» / «Contraseña incorrecta» | Reescríbelos. Si el código caduca, vuelve a pedirlo |
| Messenger/Instagram: «Error» o «Sesión cerrada» | La cookie caducó o Meta cerró la sesión. Copia cookies nuevas y «Volver a vincular». Si Meta pide verificar tu identidad, hazlo en el navegador |
| Messenger/Instagram: dice que falta una cookie | El aviso nombra cuál; cópiala y pega todas de nuevo |
| Un archivo dice «El archivo ya no está disponible» | En Messenger e Instagram los enlaces de los archivos caducan; si no se descargó a tiempo, ya no se puede |
| La cuenta aparece «Error» | dConnector reintenta solo (2 s, 4 s, 8 s… hasta 5 min). Si no se arregla, «Desconectar» y «Conectar», y mira el registro |
| Una campaña dice «Fuera de horario» | Estás fuera de la ventana horaria de la campaña (por defecto 09:00–18:00 de lunes a viernes). Edita la campaña o espera |
| Una campaña dice «Frenada» | Lee el motivo, revisa y pulsa «Reanudar» o «Cancelar» ([apartado 7.6](#76-freno-automático)) |
| Un programado quedó «Fallido» | La cuenta no estuvo conectada 6 horas, o el canal dio un error (pasa el ratón por el estado para leerlo). «Editar» o «Enviar ahora» |
| Un destino pone «Token rechazado» | El token no coincide con el de tu servidor. Edítalo y guarda (así vuelve a intentar) |
| Un destino pone «Error» y la dirección no se acepta | Debe ser `wss://…` (o `ws://127.0.0.1` / `ws://localhost` para probar) |
| No suena ni sale la notificación | Revisa Ajustes › «Notificaciones», que el chat no esté silenciado y las notificaciones de Windows |
| El micrófono no graba | Windows › Configuración › Privacidad › Micrófono: permite el acceso a las aplicaciones de escritorio |
| Quiero empezar de cero | Cierra con «Salir» y borra la carpeta de datos (haz antes una copia si dudas) |

### 16.3 Qué anotar si algo falla

1. Qué hiciste (paso a paso) y qué esperabas.
2. Qué ves: el texto exacto del aviso o del estado, y una captura si puedes (sin datos privados).
3. Qué cuenta y qué canal.
4. El archivo `dconnector-AAAA-MM-DD.log` de ese día.

---

## 17. Qué no hace · límites

Esta lista es deliberadamente honesta: sale de las notas de construcción y de leer el código.

### 17.1 General

- **Messenger e Instagram son una vía no oficial** y pueden dejar de funcionar sin aviso, o la cuenta puede ser limitada. Son las dos integraciones más frágiles.
- **Solo WhatsApp se ha probado con una cuenta real** (a mano, durante la serie 0.2.x). Telegram, Messenger e Instagram no se han probado con cuentas reales. Vincular, enviar y recibir de cada tipo, el historial, los estados y el envío de campañas son lo que cubre `QA-EN-VIVO.md`.
- La interfaz **no se ha revisado sistemáticamente** en claro/oscuro ni con selectores de fecha y diálogos de archivos reales.
- **No hay contraseña ni cifrado** de los datos guardados en tu equipo (ni en el servidor).
- **No hay** catálogo, pedidos, cobros, IA, agenda, Google Calendar, telemetría ni actualizador automático. En el modo local no hay cuentas de usuario; un servidor remoto sí tiene usuarios y claves.
- Escritorio solo para Windows (x64 y arm64; la versión de 32 bits no se ofrece porque Wails v3.1.0 no arranca en 32 bits). El servidor existe también para Linux, pero **Linux y arm64 no se han ejecutado todavía en el equipo de construcción**.
- Para recibir, enviar campañas, enviar programados y hablar con los destinos tiene que estar **en marcha el servidor** que lleva tus cuentas: en modo local, la aplicación abierta (puede estar oculta en la bandeja) o el servidor local en segundo plano; en modo remoto, el servidor remoto (la ventana puede estar cerrada). Si no hay servidor en marcha, no se reciben mensajes en vivo (el historial que el canal conserve puede llegar al volver a abrir).

### 17.2 Lo que cada canal NO admite

Cuando algo no está admitido, el botón no aparece o sale un aviso en español; en el protocolo el servidor recibe el error `unsupported`.

| Función | WhatsApp | Telegram | Messenger | Instagram |
| --- | --- | --- | --- | --- |
| Enviar texto, fotos, vídeos, audio, notas de voz | Sí | Sí | Sí | Sí |
| Enviar documentos | Sí | Sí | Sí | **No** |
| Enviar sticker, ubicación, contacto, encuesta | Sí | Sí | **No** | **No** |
| Recibir sticker, ubicación, contacto, encuesta | Sí | Sí | Sticker sí; ubicación solo en chats cifrados; contactos y encuestas como enlace o «Mensaje no compatible» | Lo que no sepa mostrar aparece como «Mensaje no compatible» |
| Reenviar | Sí (menos encuestas) | Sí | **No** | **No** |
| Votar en encuestas | Sí | Sí | **No** | **No** |
| Editar mensajes | Sí (15 min) | Sí (48 h, solo texto) | Sí (15 min) | Sí (15 min) |
| Eliminar para todos | Sí (48 h) | Sí | Sí | Sí |
| Eliminar solo para mí | Sí | Sí | Sí (no en chats cifrados) | **No** |
| Reacciones | Sí | Sí | Sí | Sí |
| Archivar, fijar, silenciar chats | Sí | Sí | **No** | **No** |
| Estados / historias (ver y publicar) | Sí | Sí (ver; publicar según la cuenta; no de solo texto) | **No** | **No** |
| Elegir audiencia de un estado | **No** (usa la privacidad del teléfono) | Sí | — | — |
| Reaccionar a un estado | **No** | Sí | — | — |
| Campañas | Sí | Sí | **No** | **No** |
| Historial antiguo bajo demanda | Sí | Sí | Sí (no en chats cifrados antiguos) | Sí |
| Longitud máxima de un mensaje | 65.536 caracteres | 4.096 | 20.000 | 1.000 |
| Tamaño máximo de archivo | 100 MB (foto 16, vídeo 64) | 2 GB (foto 10) | 25 MB | 25 MB |

### 17.3 Detalles y límites conocidos, canal por canal

**WhatsApp**

- **Canales de difusión (`@newsletter`):** solo lectura (salvo que seas administrador); no se puede reaccionar, editar, borrar, marcar leído, archivar, fijar ni silenciar, ni pedir historial; y dConnector **no recibe sus actualizaciones en vivo**.
- **Citar y reenviar** funcionan con los últimos 3.000 mensajes vistos en vivo; un mensaje más antiguo (por ejemplo del historial) da un aviso de «no encontrado».
- Los vídeos recibidos o enviados salen **sin duración ni miniatura** (no hay ffmpeg).
- «Escribiendo…» de otras personas solo llega mientras dConnector se declara «disponible» (los 45 segundos siguientes a que tú escribas).
- **Estados:** sin elección de audiencia ni reacciones; la vista «Responder a un estado» no se ha probado.
- Los identificadores internos `@lid` de WhatsApp se traducen al número de teléfono cuando se conoce; en cuentas ya migradas puede haber casos raros.
- Por seguridad, si WhatsApp responde que «la sesión fue reemplazada» o que el cliente «está desactualizado», dConnector pasa a «Sesión cerrada» sin borrarla; vuelve a vincular.

**Telegram**

- Solo cuentas **personales** (no bots), por su protocolo propio (MTProto). Cada cuenta necesita su `api_id` y `api_hash`.
- **Sin historias de solo texto**; publicar historias depende de que Telegram deje a tu cuenta (Premium, límites).
- **Ediciones solo de texto.** No hay mensajes programados de Telegram, temas de foro ni chats secretos.
- Si otra persona **quita** su reacción, dConnector no puede saberlo y la reacción queda hasta resincronizar. En encuestas anónimas solo se ven los recuentos.
- Se sincronizan hasta **1.000 chats** (incluyendo la carpeta de archivados dentro de ese tope).
- Las historias ocultas y las de canales no se piden activamente.
- Las menciones (`@`) que mandes se envían como texto plano.
- Si Telegram te frena («floodwait»), dConnector espera hasta 20 segundos y reintenta 3 veces; si pide más, da el aviso «Telegram pidió esperar N s».

**Messenger e Instagram**

- **Estados e historias: no.** (No están en este protocolo de mensajes.)
- **Reenviar, votar encuestas, archivar, fijar y silenciar: no.** Enviar stickers, ubicaciones, contactos y encuestas: no.
- **Messenger:** los **chats cifrados antiguos** no se pueden traer; el soporte de chats cifrados (E2EE) no se ha podido probar. **Instagram:** no envía documentos; no puede «eliminar solo para mí».
- **Varios adjuntos en un mensaje:** el primero va en el mensaje y los demás aparecen como mensajes aparte; si borras el primero, los demás se quedan.
- Los enlaces de archivos de Meta **caducan**: «El archivo ya no está disponible».
- «Cerrar sesión» solo borra la sesión local; la sesión web sigue figurando en Meta hasta que la cierres allí.
- Instagram usa una librería **distinta** a la de Messenger (otro protocolo interno), y ninguna de las dos se ha probado con cuentas reales.

### 17.4 Contactos, campañas, programados y estadísticas

- **Importar CSV sin vista previa** de las filas (solo eliges columnas por número).
- Los contactos de CSV o creados a mano **no tienen identidad** hasta asociarse a una cuenta (las campañas los omiten).
- **Importar de una cuenta** no existe para Messenger ni Instagram.
- **Campañas:** solo WhatsApp y Telegram; la **Prueba A/B** usa valores fijos (20 %, 4 h, 2 ganadoras); el freno, las esperas y los contadores «se rehacen» al reiniciar la app (las esperas empiezan de cero); una campaña **no sigue enviando** con dConnector cerrado; no se puede cambiar la cuenta de una campaña ya lanzada; la cola identifica a las personas por su número o id (no por su nombre).
- **Programados:** no hay aviso en vivo (la tabla se recarga cada 15 s); los de estado no se editan.
- **Estadísticas:** sin minutos de voz; no incluyen campañas ni pedidos; el alcance de estados es un número de personas y no un porcentaje; la «Estadística» del panel de información de un chat usa solo ese chat.
- «Iniciar minimizado» solo se aplica al arranque con Windows; al abrir a mano siempre se ve la ventana (ver apartado 14).
- **Reenviar**: solo a chats de la misma cuenta aunque el selector ofrezca otras.

### 17.5 Si un canal se rompe

Meta, WhatsApp y Telegram cambian sus sistemas con el tiempo. Si un canal deja de funcionar tras una actualización suya, dConnector necesitará una versión nueva: el resto de canales no se ve afectado.

---

## 18. Conexión: local o servidor remoto

Desde la 0.3.0 la ventana de dConnector es un **cliente**: siempre habla con un **servidor** que es quien mantiene tus cuentas conectadas y guarda los datos. Hay dos opciones y se elige en **Ajustes › Conexión**.

### 18.1 Modo local (por defecto)

- Al abrir dConnector se lanza solo, **oculto**, el **servidor local** (`dConnector-server.exe`, que viene junto a la aplicación). No tienes que hacer nada ni gestionar usuarios o claves.
- Escucha **solo en tu propio equipo** (`127.0.0.1`, un puerto elegido al azar, sin TLS) y la ventana se identifica con un código aleatorio que se genera en cada arranque. Desde la red nadie puede verlo.
- Usa la **misma carpeta de datos** de siempre ([apartado 2](#2-dónde-se-guardan-tus-datos)), de modo que las cuentas de la 0.2.x se abren sin hacer nada.
- Si el servidor local se cae, la ventana lo **reinicia sola** con esperas crecientes (1, 2, 4, 8, 16 y 30 segundos) y mientras tanto muestra «Reconectando» o «Sin conexión».
- Si dConnector dice que **no encuentra `dConnector-server`**, es que falta ese archivo junto a `dConnector.exe`: reinstala o restáuralo de la carpeta portable.

### 18.2 «Seguir en segundo plano»

**Ajustes › Conexión › «Seguir en segundo plano»** (solo en modo local):

- **Apagado (por defecto):** al salir de dConnector (bandeja › «Salir») se apaga también el servidor local de forma ordenada (hasta 10 segundos). Mientras la aplicación esté cerrada **no se reciben mensajes**.
- **Encendido:** al salir, el servidor local **sigue vivo** y las cuentas siguen conectadas (campañas, programados y destinos incluidos). Al volver a abrir dConnector, la ventana **se reconecta** al servidor que seguía en marcha.
- Para apagarlo del todo: abre dConnector, desactiva «Seguir en segundo plano» y vuelve a salir. Si necesitas copiar o mover la carpeta de datos, o actualizar dConnector, hazlo con el servidor apagado.
- Recuerda que este servidor corre con tu usuario de Windows y que, con él encendido, tus cuentas siguen conectadas aunque no veas ninguna ventana.

### 18.3 Modo remoto

Sirve para llevar las cuentas en una máquina que está siempre encendida (un servidor propio, un VPS) y abrir la ventana desde uno o varios equipos. Primero hay que instalar `dconnector-server` allí: la guía está en el [README](../README.md#instalar-el-servidor).

1. **Ajustes › Conexión › Modo: Remoto.**
2. **Dirección del servidor:** por ejemplo `https://dconnector.ejemplo.com` o `https://servidor:8443`. Debe ser `https`; con otra cosa la aplicación no deja continuar (la clave no puede viajar sin cifrar; solo se admite `http` en el propio equipo).
3. **Usuario** y **Clave** que creó el administrador del servidor (la clave del primer arranque se muestra una sola vez en el servidor; se recupera con `dconnector-server user passwd`).
4. **«Guardar y conectar».**

**Huella del certificado.** Si el servidor usa su certificado propio (no uno de una autoridad), la aplicación muestra **«Certificado sin confirmar»** con la **huella SHA-256**. Compárala con la que el servidor imprimió al arrancar y pulsa **«Confiar en este certificado»** solo si coinciden. La huella queda fijada para ese servidor. Si más adelante cambia, la aplicación **no se conecta** y avisa **«El certificado del servidor cambió»**: confía en la nueva únicamente si tú o el administrador renovaron el certificado. La huella fijada se ve en Ajustes › Conexión.

**Qué cambia con un servidor remoto:**

- Las **cuentas, chats, mensajes, campañas, programados y destinos** viven en el servidor y son los mismos desde cualquier equipo conectado. Los datos de tu carpeta local no se tocan ni se borran.
- Para **adjuntar archivos** (mensajes, campañas, programados, estados, importar un CSV), la ventana **sube primero el archivo al servidor** (hasta 100 MiB por archivo); los archivos subidos se borran del servidor a las 24 horas si no se usaron. «Guardar como» descarga el medio del servidor y lo guarda con el diálogo de tu equipo.
- Las opciones que hablan de **este equipo** («Abrir carpeta» de datos y de registros) solo existen en modo local. «Cerrar a la bandeja» e «Iniciar con Windows» siguen siendo de esta máquina.
- **Cerrar sesión** (en Ajustes › Conexión) borra la sesión de este equipo; **«Usar este equipo»** vuelve al servidor local.
- La clave del servidor se guarda en `desktop.json` **cifrada con DPAPI** de Windows.
- Si el administrador cambia tu clave o desactiva tu usuario, se cierran tus sesiones y la ventana te pide **«Iniciar sesión»** otra vez.

### 18.4 Sin conexión

Si el servidor no responde (se cayó, hay un corte de red, el proxy falla), la aplicación **no se queda en blanco**:

- Aparece la pantalla **«Sin conexión»** con el motivo en español, la dirección del servidor y los botones **«Reintentar»** y, en modo remoto, **«Usar este equipo»**. Con la sesión ya abierta, una banda avisa «Sin conexión con el servidor» y la barra lateral pasa a «Reconectando» o «Sin conexión».
- Reintenta **sola** (1, 2, 4, 8, 16 y 30 segundos, con un poco de azar) y, al volver, **recarga** chats, programados, destinos y contactos desde el servidor, que es la fuente de la verdad. No se reenvían eventos perdidos: se vuelve a leer el estado.
- Si el motivo es el certificado, ves el cuadro de confianza de la huella (arriba). Si es el inicio de sesión («Usuario o clave incorrectos»), revisa los datos; **tras demasiados intentos fallidos el servidor bloquea un rato** («Demasiados intentos fallidos. Espere antes de volver a intentarlo.»).
- Qué mirar: ¿está encendido el servidor? ¿responde `https://…/api/v1/health` en un navegador? ¿el cortafuegos o el proxy dejan pasar el WebSocket `/api/v1/events`? Ver el [README](../README.md#tls-certificado-propio-certificado-externo-y-proxy-inverso).

### 18.5 Varias ventanas a la vez

Puedes abrir dConnector en varios equipos contra el mismo servidor remoto (hasta 8 conexiones por usuario). Todos ven lo mismo en tiempo real. Los **avisos** los decide el servidor: se avisa de un mensaje solo si **ninguna** de las ventanas lo está mirando (chat abierto con la ventana enfocada); en las que no lo miran, suena y sale el aviso de Windows de cada equipo.

### 18.6 Problemas típicos de la conexión

| Qué ves | Qué hacer |
| --- | --- |
| «No se pudo conectar de forma segura» y una huella | Certificado sin confirmar o cambiado: compara la huella con la del servidor y confía solo si coincide |
| «Usuario o clave incorrectos» | Revisa los datos; si no recuerdas la clave, el administrador usa `dconnector-server user passwd <usuario>` en el servidor |
| «Demasiados intentos fallidos» | Espera unos minutos; el bloqueo crece con cada reincidencia (hasta 1 hora) |
| «Este servidor usa otra versión de la API» | Actualiza el servidor o la aplicación para que sean de la misma versión |
| Entra, pero los chats no se actualizan solos | Falla el WebSocket de eventos: revisa el proxy (`Upgrade`/`Connection`, tiempos) |
| Falla al adjuntar un archivo grande | El proxy limita el cuerpo: debe permitir al menos 128 MiB en `/api/v1/files` |
| No encuentra `dConnector-server` | Falta el archivo junto a `dConnector.exe` (modo local) |
| Las cuentas no conectan al abrir la ventana tras salir | Comprueba si «Seguir en segundo plano» estaba apagado: el servidor se apagó al salir y arranca de nuevo al abrir |
