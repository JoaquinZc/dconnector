# dConnector · QA en vivo

Lista de comprobación para que **quien opera dConnector** pruebe la versión 0.3.0 con sus propias cuentas y su propio servidor. Marca cada casilla (`- [x]`) cuando salga bien. Al final hay una tabla «Resultado» para resumir.

**Por qué existe este documento.** Todo lo que habla con WhatsApp, Telegram, Messenger e Instagram está escrito contra las librerías de cada canal y cubierto con pruebas automáticas y un adaptador falso, pero **solo WhatsApp se probó a mano con una cuenta real (serie 0.2.x)**; Telegram, Messenger e Instagram no. La interfaz se ha revisado poco en una ventana real (claro/oscuro, selectores y diálogos). Y lo nuevo de la 0.3.0 (el servidor independiente, la conexión remota con TLS, Linux y arm64) **no se ha ejecutado todavía en vivo**: la sección **M** lo cubre. Aquí se descubre qué falla de verdad.

Para saber cómo se usa cada pantalla, mira `docs/MANUAL.md`. Para el protocolo, `docs/PROTOCOLO-v1.md`; para el servidor, el `README.md` y `docs/14-API-SERVIDOR.md`.

---

## 0. Cómo usar este documento

### Convenciones

- `- [ ]` es algo que **comprobar**. Si sale bien, márcalo. Si falla, déjalo sin marcar y anótalo (ver más abajo).
- **[RIESGO]** marca lo que las notas de construcción señalan como **no probado o frágil**. Ahí es donde más probable es que algo falle. En esos puntos, **activa «Registro detallado» antes** (Ajustes › Datos) y apunta la hora.
- «**la cuenta**» es la cuenta que estás probando en dConnector. «**el otro teléfono**» (o la otra cuenta) es la que usas para escribirle y para ver lo que dConnector envía.
- «**Esperado**» dice qué debería verse. Si ves otra cosa, es un fallo.

### Qué necesitas preparar

- [ ] Una cuenta de prueba por canal que quieras probar (WhatsApp, Telegram, Messenger, Instagram). **Messenger e Instagram: usa cuentas que puedas permitirte perder** (vía no oficial, Meta puede bloquearlas).
- [ ] Un **segundo teléfono o segunda cuenta de cada canal** para escribirle a la cuenta de prueba y ver lo que dConnector envía.
- [ ] **Tres contactos PROPIOS** de WhatsApp (o de Telegram) a los que sí puedes mandar mensajes de prueba (por ejemplo tus otros teléfonos). **Nunca uses contactos de clientes para estas pruebas.**
- [ ] Un **grupo de prueba** del canal con al menos tú, la cuenta de prueba y otra persona.
- [ ] Archivos de prueba en una carpeta: una foto `.jpg`, un vídeo corto `.mp4` (menos de 10 MB), un audio `.mp3`, un documento `.pdf`, un sticker `.webp` y otra imagen para pegar con `Ctrl+V`.
- [ ] Un micrófono funcionando.
- [ ] Los archivos de la versión que pruebas (de la página de Releases o de `dist\`): el instalador `dConnector-<versión>-x64.msi`, el zip `dConnector-<versión>-portable-x64.zip`, el servidor (`dConnector-server-<versión>-windows-x64.zip` y, si tienes Linux, `dConnector-server-<versión>-linux-x64.tar.gz`), `echo-server-<versión>-windows-x64.exe` y `SHA256SUMS`.
- [ ] Para la sección M: una **segunda máquina** (o una máquina virtual) para el servidor remoto —idealmente Linux— y, si puedes, un **segundo equipo Windows** como segundo cliente.
- [ ] Una carpeta para guardar **copias del registro** de cada fallo (por ejemplo `Escritorio\QA-dConnector`).

### Qué hacer cuando algo falla

1. **Déjalo sin marcar** y escribe en la tabla de **Notas** (al final): canal, hora (HH:mm), qué hiciste y qué viste (el texto exacto del aviso).
2. **Copia el registro** de ese día: Ajustes › Datos › **«Abrir registros»**, y copia el archivo `dconnector-AAAA-MM-DD.log` a tu carpeta de QA (renómbralo, por ejemplo `falla-whatsapp-foto.log`). Si pasó cerca de medianoche, copia también el del día anterior.
3. Los registros **no llevan el texto de los mensajes** ni nombres de chats ni números completos (solo los últimos 4 caracteres de los ids). Puedes enviarlos sin miedo, pero revísalos si dudas.
4. Si puedes, una **captura de pantalla** (sin datos privados).
5. **No sigas insistiendo** con Messenger/Instagram si falla: repetir intentos aumenta el riesgo de bloqueo de la cuenta.

### Orden recomendado

A (instalar y mirar la interfaz) → B (WhatsApp) → C (Telegram) → D (Messenger) → E (Instagram) → F (campaña de prueba) → G (programado) → H (contactos) → I (estadísticas) → J (destino con echo-server) → K (sistema) → L (MSI y portable) → **M (conexión local y remota, servidor)** → Resultado.

---

## A. Instalar y mirar la interfaz por primera vez

> La interfaz **nunca se ha visto en vivo**. Este es el momento de abrirla y mirar con ojos de usuario.

### A.1 Primera apertura

- [ ] Instalas con el `.msi` (sección L) o descomprimes el `.zip` y abres `dConnector.exe`. Con SmartScreen: «Más información» › «Ejecutar de todas formas».
- [ ] La ventana abre sin quedarse en blanco, con el título «dConnector» y una barra lateral izquierda con 9 iconos.
- [ ] Sin cuentas, **Chats** muestra el logotipo «dConnector» y **Cuentas** muestra «Sin cuentas» con el botón «Añadir cuenta».
- [ ] Pasa el ratón por cada icono de la barra: aparece su nombre y su atajo (`Ctrl+1` … `Ctrl+9`). Comprueba que `Ctrl+1` … `Ctrl+9` cambian de pantalla (Cuentas es `Ctrl+7`, Destinos `Ctrl+8`).
- [ ] Redimensiona la ventana al mínimo (960 × 600): nada se corta ni se solapa de forma fea.
- [ ] **Ajustes › Datos › «Abrir carpeta»** abre la carpeta de datos en el Explorador (y la ruta se ve en esa misma pantalla).

### A.2 Tema claro y oscuro (mirar TODAS las pantallas)

Ve a **Ajustes › General › Tema** y prueba **«Claro»**, **«Oscuro»** y **«Del sistema»** (cambia el modo de Windows para probar este último). Recuerda pulsar **«Guardar»**. En **cada** tema, recorre cada pantalla y comprueba: se lee todo (contraste), no hay texto cortado ni cajas desbordadas, los botones se distinguen, los avisos se ven.

| Pantalla | Qué mirar | Claro | Oscuro |
| --- | --- | --- | --- |
| Chats (con datos: haz esto con una cuenta ya conectada) | Lista, píldoras de cuenta, burbujas entrantes y salientes, marcas azules, reacciones, citas, panel de información, menú de mensaje, selector de emojis | - [ ] | - [ ] |
| Estados | Anillos, visor a pantalla completa, modal «Nuevo estado» (12 colores de fondo), gráficos de estadísticas | - [ ] | - [ ] |
| Campañas | Lista, píldoras de estado, asistente de 5 pasos, panel «Revisión» (verde/amarillo/rojo), «Ritmo de la cuenta», detalle y cola | - [ ] | - [ ] |
| Programados | Tabla, píldoras de estado, formulario | - [ ] | - [ ] |
| Contactos | Tabla, listas, ficha lateral, importadores | - [ ] | - [ ] |
| Estadísticas | KPIs, **gráficos**, **mapa de calor** (los colores se distinguen) | - [ ] | - [ ] |
| Destinos | Tarjetas con punto de estado, formulario | - [ ] | - [ ] |
| Cuentas | Tarjetas, **código QR** (debe verse sobre fondo blanco aunque el tema sea oscuro), código de 8 caracteres | - [ ] | - [ ] |
| Ajustes | Las cinco secciones, la barra «Cambios sin guardar» | - [ ] | - [ ] |

- [ ] **Del sistema:** al cambiar Windows entre claro y oscuro, dConnector sigue el cambio (puede requerir reabrir; anótalo).
- [ ] El tema elegido **se conserva** al cerrar con «Salir» y volver a abrir.
- [ ] Las **letras** son las del sistema (Segoe UI) y son legibles al 100 %, 125 % y 150 % de escala de Windows.

### A.3 Selectores y diálogos del sistema (WebView2)

Estos controles son los más propensos a comportarse distinto dentro de una ventana embebida.

- [ ] **`datetime-local`** en tres sitios: Estados › «Nuevo estado» › «Programar» › «Fecha y hora»; Programados › «Nuevo» › «Fecha y hora»; Campañas › paso 4 › «Fecha y hora». Se abre el selector, puedes elegir día **y** hora con el teclado y con el ratón, y el valor se queda.
- [ ] **`time`** (horas) en Campañas › paso 4 › «Desde» / «Hasta».
- [ ] **`date`** en Estadísticas › «Personalizado» › «Desde» / «Hasta».
- [ ] **Diálogo de archivos de Windows** (debe abrirse y devolver el archivo elegido) desde: Chats › «Adjuntar» › «Fotos y vídeos», «Documento» y «Sticker» (con su filtro de tipos); Campañas › paso 3 › «Adjuntar»; Programados › «Adjuntar»; Estados › «Elegir foto o vídeo»; Contactos › «Importar CSV» › «Elegir archivo» (filtro CSV).
- [ ] **«Guardar como»** (diálogo de guardar) desde un documento y desde el visor de fotos: el archivo se guarda donde eliges y aparece el aviso «Archivo guardado».
- [ ] **Arrastrar y soltar:** arrastra una foto desde el Explorador sobre una conversación: aparece «Soltar para adjuntar» y luego la vista previa. **[RIESGO]** la ventana de Wails podría interceptar el arrastre; anota si no funciona.
- [ ] **Pegar una imagen** (`Ctrl+V` con una imagen copiada) en la caja de escribir: aparece la vista previa para enviar.
- [ ] **Nota de voz con el micrófono:** pulsa el micrófono («Grabar nota de voz»). Windows pide permiso la primera vez; permítelo. Habla 5 segundos (el contador avanza), pulsa «Enviar nota de voz». Sin permiso debe salir un aviso que lo explica. **[RIESGO]** la grabación en formato Opus de WebView2 y su conversión nunca se han probado; anota si dice «Este equipo no puede grabar audio en Opus».
- [ ] **Enlaces:** un enlace dentro de un mensaje (`https://…`) se abre en **tu navegador**, no dentro de dConnector.
- [ ] **Visor de vídeo con audio:** un vídeo recibido se reproduce **con sonido**, y puedes saltar a otro punto del vídeo (adelantar) sin que se reinicie. **[RIESGO]**

---

## B. WhatsApp

Cuenta: ______________  (etiqueta en dConnector: ______________)

> **[RIESGO] general de WhatsApp.** Los identificadores `@lid` (identificadores internos de WhatsApp en lugar del número): dConnector intenta traducirlos siempre al número de teléfono para que una persona sea **un solo chat**. En cuentas ya migradas a `@lid` puede fallar. Mira siempre que **no** aparezcan dos chats para la misma persona.

### B.1 Vincular

- [ ] **Por QR:** Cuentas › «Añadir cuenta» › WhatsApp › etiqueta y color › «Continuar». El QR grande aparece, lo escaneas desde WhatsApp › Dispositivos vinculados › Vincular un dispositivo, y llega a «Conectada ✓» con tu nombre y número.
- [ ] El QR **se renueva solo** si tardas; si caduca sale un mensaje en rojo con «Nuevo código QR» y funciona.
- [ ] **Por código de 8 caracteres** (con otra cuenta de WhatsApp, o tras «Cerrar sesión» de la anterior): pestaña «Con número» › escribes el número con prefijo › «Obtener código» › sale un código `XXXX-XXXX` › en el teléfono eliges «Vincular con el número de teléfono» y lo escribes › «Conectada ✓».
- [ ] Un número mal escrito en «Con número» da un aviso comprensible (no se queda girando para siempre).
- [ ] **Persistencia:** «Salir» (bandeja) y volver a abrir: la cuenta vuelve a «Conectada» **sin pedir el QR** otra vez.
- [ ] «Desconectar» y «Conectar» funcionan.
- [ ] En WhatsApp del teléfono, la sesión aparece en «Dispositivos vinculados» como dConnector (o similar).
- [ ] El historial inicial llega: aparecen tus chats recientes con su última conversación (puede tardar varios minutos). **[RIESGO]**
- [ ] «Cerrar sesión» cierra la sesión (en el teléfono ya no aparece el dispositivo). Después «Volver a vincular» la reconecta.
- [ ] «Eliminar» (escribiendo la etiqueta) borra la cuenta y sus chats; la carpeta `accounts\<id>` desaparece.

### B.2 Recibir de cada tipo (envíalos desde el otro teléfono a la cuenta)

Para cada uno: llega al instante, aparece en la lista con su vista previa, el contador de no leídos sube, y la burbuja se ve bien.

- [ ] **Texto** (con `*negrita*`, `_cursiva_`, `~tachado~` y un enlace).
- [ ] **Foto** (con pie de foto): miniatura, se abre en el visor.
- [ ] **Vídeo**: se reproduce con sonido. (Vídeos grandes no se bajan solos: se bajan al abrirlos.)
- [ ] **Nota de voz**: se reproduce; cambian las velocidades 1× / 1,5× / 2×.
- [ ] **Audio** (archivo mp3).
- [ ] **Documento** (PDF): icono, nombre, tamaño, «Guardar como».
- [ ] **Sticker**.
- [ ] **Ubicación**: tarjeta con coordenadas y «Ver en OpenStreetMap» (abre el navegador).
- [ ] **Contacto** (tarjeta de contacto): nombre y teléfono, «Abrir chat» si existe.
- [ ] **Encuesta**: opciones y votos; **vota** desde dConnector y mira que el otro teléfono ve el voto.
- [ ] **Mensaje con cita** (responder a un mensaje): la cita sale arriba; clic lleva al mensaje.
- [ ] **Mensaje reenviado**: lleva la marca «Reenviado».
- [ ] **Mensaje de un grupo**: sale el nombre de quien escribe, con su color.
- [ ] **Mensajes de sistema de grupo** (alguien entra o sale, cambia el asunto): salen centrados en gris.
- [ ] **Mensaje que desaparece** o «ver una vez»: si lo recibes, anota cómo se muestra.
- [ ] **Mensaje no compatible** (algo raro): sale como «Mensaje no compatible», sin romper el chat.

### B.3 Enviar de cada tipo (desde dConnector; compruébalo en el otro teléfono)

- [ ] **Texto** (con Enter), y salto de línea con Mayús+Enter.
- [ ] **Foto** con pie, con la vista previa antes de enviar.
- [ ] **Varias fotos a la vez** (el menú «Fotos y vídeos» con 2 o 3 archivos): salen todas.
- [ ] **Vídeo** corto.
- [ ] **Nota de voz** (micrófono): en el otro teléfono se oye bien y se ve como nota de voz. **[RIESGO]** (formato y duración).
- [ ] **Audio** (archivo).
- [ ] **Documento** (PDF).
- [ ] **Sticker** (`.webp`).
- [ ] **Ubicación** (latitud y longitud a mano).
- [ ] **Contacto** (nombre y teléfono).
- [ ] **Encuesta** (pregunta y 2 o más opciones; una con «Permitir varias respuestas»).
- [ ] **Responder** a un mensaje (cita): en el otro teléfono sale citado.
- [ ] **Reenviar** un mensaje a otro chat **de la misma cuenta**: llega con la marca de reenviado. Reenviar a un chat de **otra cuenta** debe dar un aviso de error (comportamiento conocido). **[RIESGO]** citar/reenviar solo funciona con los últimos 3.000 mensajes vistos; con uno muy antiguo debe dar un aviso de «no encontrado».
- [ ] Un archivo **demasiado grande** (mayor que el límite) da un aviso antes de enviar.
- [ ] Un mensaje se ve con ✓ (enviado), luego ✓✓ (entregado) y ✓✓ azul (leído) cuando el otro lo lee.

### B.4 Reaccionar, editar, borrar, escribiendo, leído

- [ ] **Reaccionar** con un emoji de la barra rápida a un mensaje del otro: se ve en el otro teléfono. Volver a pulsarlo la quita.
- [ ] **Reacción de otra persona** a un mensaje tuyo: aparece debajo de la burbuja, y si cambia o la quita, se actualiza.
- [ ] **Editar** un mensaje tuyo de texto (menú › «Editar»): en el otro teléfono sale «editado». Pasados 15 minutos, la opción desaparece o da aviso.
- [ ] **Eliminar para todos** un mensaje reciente: desaparece en el otro teléfono y en dConnector queda como «Mensaje eliminado».
- [ ] **Eliminar para mí**: solo se oculta en dConnector.
- [ ] **Escribiendo (de ti hacia el otro):** mientras tecleas en dConnector, el otro ve «escribiendo…». **[RIESGO]** WhatsApp lo entrega solo mientras dConnector se declara «disponible» (los 45 s siguientes a teclear).
- [ ] **Escribiendo (del otro hacia ti):** cuando el otro teclea, la cabecera dice «escribiendo…» (y «grabando audio…» si graba). **[RIESGO]** solo llega en esa misma ventana de 45 s.
- [ ] **Leído:** al abrir un chat con mensajes sin leer (con la ventana enfocada), el contador baja a 0 y en el otro teléfono sus mensajes pasan a ✓✓ azul. Y al leer un chat **desde el teléfono**, dConnector también lo marca como leído.
- [ ] **Entregado:** los dos ticks grises aparecen aunque la cuenta no esté «en línea». **[RIESGO]**
- [ ] **Silenciar / Archivar / Fijar** un chat (clic derecho) funciona y se refleja en el teléfono (puede tardar).
- [ ] **Buscar:** `Ctrl+K` encuentra un mensaje por una palabra, y «Buscar en el chat» dentro de una conversación también.

### B.5 Medios descargados

- [ ] Las **fotos, audios, notas de voz y stickers** se descargan solos (según Ajustes › Descargas automáticas) y se ven al abrir el chat.
- [ ] Un **vídeo** o **documento** (que no se bajan solos por defecto) se descarga al abrirlo o al pulsar «Guardar como».
- [ ] En la carpeta `accounts\<id>\media\` aparecen los archivos con nombres coherentes.
- [ ] Cambiar un límite de Descargas automáticas (por ejemplo, vídeos sí con 50 MB) y recibir un vídeo: se baja solo.
- [ ] Los **avatares** (fotos de perfil) aparecen en la lista; si no hay, salen iniciales de colores.

### B.6 Estados

- [ ] **Estados › cuenta de WhatsApp:** aparece «Mis estados».
- [ ] **Publicar un estado de texto** («Nuevo estado» › «Texto» con un color y una letra › «Publicar»): se ve en el otro teléfono.
- [ ] **Publicar una foto con pie.** Y un **vídeo** corto.
- [ ] **Audiencia:** el modal dice «Se usa la privacidad de estados del teléfono» (no hay elección).
- [ ] **Programar un estado** (Programar › fecha y hora › Confirmar): aparece en Programados con destino «Estados» y se publica a su hora.
- [ ] **Ver los estados de otros** (que tus contactos publiquen): salen en «Recientes» con anillo; se abren en el visor (5 s por foto; `←` `→`; mantener = pausa; `Esc`). Pasan a «Vistos» y el autor ve que los viste.
- [ ] **«Responder»** a un estado abre el chat con la cita. **[RIESGO]** que llegue como cita de estado no se ha probado.
- [ ] **Vistas de mis estados:** tras verlo desde el otro teléfono, aparece en «Mi estado» la lista de «Vistas» con hora (puede tardar un poco; se refresca cada 10 min).
- [ ] **Borrar** un estado propio: desaparece en el teléfono.
- [ ] **Estadísticas** (pestaña): «Publicados», «Vistas medias», «Alcance», curvas.
- [ ] No hay botón para reaccionar a estados de otros (WhatsApp no lo admite en dConnector): es lo esperado.

### B.7 Historial y grupos

- [ ] **Historial al abrir un chat:** abre un chat antiguo y sube hasta el principio: aparece «Cargando…» y se traen mensajes más antiguos; «Buscar más atrás» (en la búsqueda del chat) también trae más. **[RIESGO]**
- [ ] **Grupos:** recibes un mensaje de grupo; envías uno desde dConnector; la pestaña «Grupos» los separa.
- [ ] **Grupo de solo administradores** (si no eres admin): en lugar de la caja verás «Solo los administradores pueden enviar mensajes».
- [ ] **Canal de difusión (newsletter):** si sigues alguno, aparece en solo lectura y sin actualizaciones en vivo (esperado).
- [ ] **Reabrir la app** y comprobar que los chats, mensajes y medios siguen ahí.

---

## C. Telegram

Cuenta: ______________

### C.1 Vincular

- [ ] Consigues `api_id` y `api_hash` en https://my.telegram.org (ver `MANUAL.md`, apartado 4.2).
- [ ] Añadir cuenta › Telegram › `api_id` (solo números) y `api_hash` › «Continuar» › teléfono con prefijo › «Enviar código» › llega a tu app de Telegram › lo escribes › «Continuar».
- [ ] Si tienes **contraseña de dos pasos**: el asistente pide la contraseña y entra. Una contraseña mala da «Contraseña incorrecta» y deja reintentar.
- [ ] Un **código mal escrito** da «Código incorrecto» y deja reintentar.
- [ ] Un `api_id`/`api_hash` **inventado**: la vinculación falla con un aviso comprensible (algo de api_id inválido). Corriges con «Editar» y funciona.
- [ ] **Persistencia:** «Salir» y volver a abrir: la cuenta reconecta **sin pedir código otra vez**. **[RIESGO]** (sesión guardada entre reinicios, nunca probada).
- [ ] Aparecen los chats (hasta 1.000), con sus fotos.
- [ ] En Telegram › Ajustes › Dispositivos, aparece la sesión.
- [ ] «Cerrar sesión» y «Volver a vincular» funcionan; «Eliminar» borra todo.

### C.2 Recibir de cada tipo

- [ ] Texto (con formato) y enlace.
- [ ] Foto (con pie).
- [ ] Vídeo, y **vídeo redondo** (mensaje de vídeo).
- [ ] **Nota de voz**.
- [ ] **Audio** (música, con título y artista).
- [ ] **Documento**.
- [ ] **Sticker** (y uno animado).
- [ ] **GIF**.
- [ ] **Ubicación** y **lugar** (venue).
- [ ] **Contacto**.
- [ ] **Encuesta** (normal y anónima; en la anónima solo se ven los recuentos).
- [ ] **Mensaje con cita**, **reenviado**, y **mensaje con mención** (`@usuario`).
- [ ] **Mensaje de un grupo**, de un **supergrupo** y de un **canal** (solo lectura).
- [ ] **Mensajes de servicio** (alguien se une al grupo, cambia el título…): salen centrados.

### C.3 Enviar de cada tipo

- [ ] Texto, foto (con pie), vídeo, nota de voz, audio, documento, sticker, ubicación, contacto, encuesta (de varias respuestas y una simple).
- [ ] Una **imagen de más de 10 MB** se envía como documento; un archivo grande (cientos de MB) sube.
- [ ] **Responder** (cita) y **reenviar** a un chat de la misma cuenta.
- [ ] Un texto de más de 4.096 caracteres da aviso antes de enviar.

### C.4 Reaccionar, editar, borrar, escribiendo, leído

- [ ] **Reaccionar** y quitar la reacción. Reacción de otro a tu mensaje: aparece. **Si el otro la quita, dConnector no se entera** (limitación conocida): anota qué pasa.
- [ ] **Editar** tu mensaje (hasta 48 horas; solo texto): en Telegram sale «editado».
- [ ] **Eliminar para todos** y «para mí».
- [ ] **Escribiendo** en los dos sentidos (en Telegram dura unos 7 segundos tras la última señal).
- [ ] **Leído:** abrir el chat marca leído; el otro ve sus mensajes leídos.
- [ ] **Archivar, fijar y silenciar** un chat (clic derecho) se refleja en Telegram.
- [ ] **[RIESGO] Floodwait:** envía **muchos mensajes seguidos** (por ejemplo 20 mensajes cortos en un chat tuyo propio) y mira si Telegram te frena. Esperado: dConnector espera hasta 20 s y reintenta 3 veces; si Telegram pide más, sale el aviso «Telegram pidió esperar N s…». Anota cuántos mensajes aguantó y el texto del aviso. (No insistas si te frena.)

### C.5 Medios, historial, estados

- [ ] Fotos, audios, notas de voz y stickers se bajan solos; vídeo y documento al abrir.
- [ ] Descarga de un archivo grande o de otro centro de datos de Telegram funciona. **[RIESGO]**
- [ ] **Historial:** sube hasta el final de un chat y trae mensajes antiguos.
- [ ] **Historias (estados) de otros:** salen en Estados (se piden al conectar y cada 15 minutos). Se abren, marcan visto y se puede **reaccionar**. **[RIESGO]**
- [ ] **Publicar una historia con foto o vídeo:** si tu cuenta puede, sale con elección de **audiencia** («Mis contactos», «Solo compartir con…», «Mis contactos excepto…», con listas y contactos). Si no puede (sin Premium, límite…), «Nuevo estado» da un aviso con el motivo.
- [ ] **Historia de solo texto:** debe dar el aviso «Telegram no admite historias solo de texto».
- [ ] **Vistas** de tu historia, **borrar** tu historia.
- [ ] **Grupos y canales:** enviar a un grupo; canal sin permiso de publicar muestra «Solo los administradores pueden enviar mensajes».
- [ ] **Persistencia:** al reabrir, siguen los chats y mensajes.

---

## D. Messenger

Cuenta: ______________  — **recuerda: vía no oficial; usa una cuenta prescindible.**

### D.1 Vincular

- [ ] Añadir cuenta › Messenger: sale el aviso «Vía no oficial: Meta puede bloquear la cuenta» y **no puedes continuar sin marcar «Entiendo»**.
- [ ] **Cookies en formato `nombre=valor; …`** (`c_user`, `xs`, `datr`): «Vincular» lleva a «Conectada ✓» y aparecen chats.
- [ ] **Cookies en JSON** (exportadas con una extensión tipo Cookie-Editor): también vinculan.
- [ ] **Falta una cookie** (quita `xs` a propósito): el aviso dice **cuál** falta y no muestra los valores.
- [ ] **«Cómo copiar las cookies»** (la ayuda desplegable) se entiende y coincide con tu navegador.
- [ ] **[RIESGO] Pestaña «Usuario y contraseña»** (solo Messenger): nunca se ha probado. Si la pruebas, anota qué pasa (código, captcha, error). Si falla, la pestaña debería ocultarse sola y volver a «Pegar cookies».
- [ ] **Persistencia:** «Salir» y reabrir: reconecta sin volver a pegar cookies.
- [ ] Aparecen tus chats recientes con fotos de perfil.
- [ ] **«Cerrar sesión»** en dConnector: borra la sesión local (en Meta, la sesión sigue en «Seguridad»; es lo esperado).
- [ ] La tarjeta **no** muestra «Los chats cifrados de extremo a extremo aún no se muestran» si E2EE funciona; si lo muestra, E2EE no está activo.

### D.2 Chats cifrados de extremo a extremo (E2EE)

> **[RIESGO] Lo más frágil de Messenger.** La mayoría de los chats personales de Messenger están cifrados de extremo a extremo desde 2024. dConnector registra un dispositivo cifrado y lo conecta, pero **ni el registro ni la conexión cifrada se han podido probar**. Activa «Registro detallado» antes de esta sección.

- [ ] Con la cuenta conectada, mira el registro: debe haber (o no) un aviso «los chats cifrados no están disponibles». Anota qué dice.
- [ ] Un **chat cifrado** (uno que ya tengas en Messenger con el candado) **aparece** en la lista.
- [ ] **Recibir** en un chat cifrado: texto, foto, vídeo, audio o nota de voz, documento, sticker, ubicación (solo recibir).
- [ ] **Enviar** en un chat cifrado: texto, foto, nota de voz, documento.
- [ ] **Reacción**, **edición** y **eliminar para todos** en un chat cifrado.
- [ ] «Escribiendo…» y marcas de leído en un chat cifrado.
- [ ] **Historial antiguo** de un chat cifrado: **no se trae** (esperado: «no admitido»).
- [ ] Si todo lo cifrado falla: anota que **falla E2EE** y guarda el registro. El agente que arregle esto tiene un interruptor de una línea (que la función de E2EE de `messenger/e2ee.go` devuelva `nil`), con lo que los chats cifrados se ocultan y la tarjeta muestra el aviso fijo; **no hace falta que lo toques tú**.

### D.3 Recibir de cada tipo (en chats normales, no cifrados)

- [ ] Texto, foto, vídeo, nota de voz, audio, documento, sticker.
- [ ] Mensaje con **cita**, mensaje **reenviado**.
- [ ] Mensaje de **grupo**.
- [ ] **Ubicación**, **contacto** o **encuesta** que te manden: salen como enlace o «Mensaje no compatible», sin romper el chat (esperado).
- [ ] **Varios adjuntos en un mensaje:** el primero va en el mensaje y los demás aparecen como mensajes aparte.
- [ ] Los **medios** se descargan; si el enlace caducó, el archivo dice «El archivo ya no está disponible».

### D.4 Enviar

- [ ] Texto; foto; vídeo; audio; nota de voz; documento.
- [ ] **Responder** (cita).
- [ ] Sticker, ubicación, contacto y encuesta **no** aparecen en «Adjuntar» (esperado).
- [ ] Un texto muy largo (más de 20.000 caracteres) da aviso.

### D.5 Reaccionar, editar, borrar, escribiendo, leído, historial

- [ ] **Reaccionar** (y quitar) a un mensaje.
- [ ] **Editar** un mensaje tuyo de texto (15 minutos). Si el texto vuelve igual al original, dConnector lo toma por rechazado: anota qué pasa.
- [ ] **Eliminar para todos** («deshacer envío»).
- [ ] **Escribiendo** y **leído** en los dos sentidos.
- [ ] **Historial:** sube en un chat normal y trae mensajes antiguos.
- [ ] **Grupos:** recibir y enviar.
- [ ] Reenviar, votar encuestas, archivar, fijar y silenciar **no están admitidos**: dan aviso de «no admitido» (esperado).
- [ ] **Estados / historias:** no hay (esperado: la pantalla Estados no lista cuentas de Messenger).
- [ ] **Campañas:** la cuenta de Messenger **no** aparece al crear una campaña (esperado).

---

## E. Instagram

Cuenta: ______________  — **vía no oficial; cuenta prescindible.**

> **[RIESGO] general de Instagram.** Usa una librería distinta a la de Messenger (`instameow`, otro protocolo interno: bandeja «mailbox» más avisos «slide»). **Nunca se ha probado con una cuenta real.** Es probable que algo falle; lo importante es anotar qué.

### E.1 Vincular

- [ ] Aviso «Vía no oficial: Meta puede bloquear la cuenta» con casilla «Entiendo» obligatoria.
- [ ] **Cookies** de instagram.com (`sessionid`, `ds_user_id`, `csrftoken`) en formato `nombre=valor; …` y en JSON: vinculan y llegas a «Conectada ✓».
- [ ] **Falta una cookie:** el aviso dice cuál.
- [ ] No hay pestaña «Usuario y contraseña» (solo cookies): esperado.
- [ ] **Persistencia:** reabrir y reconectar sin volver a pegar cookies.
- [ ] Aparecen tus chats (la bandeja trae los más recientes) y las fotos de perfil.
- [ ] **[RIESGO]** Tras reconectar con la app cerrada un rato, dConnector retoma desde su cursor y recupera lo ocurrido con la cuenta apagada (y si Instagram pide empezar de cero, lo hace). Anota si faltan mensajes.
- [ ] «Cerrar sesión» borra solo la sesión local.

### E.2 Recibir y enviar

- [ ] Recibir: texto, foto, vídeo, nota de voz, audio, cita, reacción, mensaje de grupo.
- [ ] Lo que no sepa mostrar sale como «Mensaje no compatible».
- [ ] Enviar: texto (máx. 1.000 caracteres), foto, vídeo, audio, nota de voz.
- [ ] **No se pueden enviar documentos** (la opción no aparece): esperado.
- [ ] **Responder** (cita).
- [ ] **Reaccionar** y **quitar** la reacción (quitar necesita saber el emoji; anota si falla).
- [ ] **Editar** (15 minutos). **[RIESGO]** (puede no estar disponible en Instagram aunque dConnector lo ofrezca).
- [ ] **Eliminar para todos** («deshacer envío»). **«Eliminar para mí»** da un aviso de «no admitido» (esperado).
- [ ] **Escribiendo** y **leído** en los dos sentidos.
- [ ] **Historial** al subir en un chat.
- [ ] **Medios** descargados; los enlaces caducados dicen «El archivo ya no está disponible».
- [ ] Reenviar, votar encuestas, archivar, fijar, silenciar, estados y campañas **no están admitidos** (esperado).

---

## F. Campaña de prueba a 3 contactos PROPIOS

> **Regla:** todos los mensajes de esta campaña empiezan por **`[PRUEBA]`** y van **solo** a **tres contactos propios**. Nunca mandes una campaña de prueba a clientes. Usa WhatsApp (o Telegram): Messenger e Instagram no hacen campañas.

Cuenta de la campaña: ______________

### F.1 Crear la lista de 3 contactos propios

Un contacto necesita estar **asociado a la cuenta** (tener «identidad») para recibir la campaña. Hay dos caminos; usa el que prefieras.

**Camino A, desde los chats (el más directo):**

1. [ ] Desde la cuenta, **escribe «hola» a cada uno de tus 3 contactos propios** (y haz que cada uno responda «hola» desde su teléfono, para que sean «Conocidos»).
2. [ ] En **Chats**, abre el chat de cada uno › «Información» › en «Contacto» pulsa **«Crear contacto»**.
3. [ ] Ve a **Contactos** › en la barra «Listas» pulsa **«Nueva lista»** › nombre `PRUEBA`, un color › guardar.
4. [ ] En la ficha de cada uno (Contactos › clic en el contacto) o en el panel «Información» del chat, pulsa el chip **PRUEBA** en **«Listas»** (se marca). Si usas la ficha de Contactos, pulsa **«Guardar»**.
5. [ ] Contactos › lista `PRUEBA` debe mostrar **3** contactos.

**Camino B, desde Contactos:**

1. [ ] «Nuevo contacto» ×3 (nombre y teléfono con `+593…`).
2. [ ] Menú ⋮ › **«Importar de una cuenta»** › elige la cuenta › «Importar»: al coincidir el teléfono, **añade la identidad** a tus contactos (resumen: actualizados/creados). (Requiere que esos números estén en la agenda de la cuenta.)
3. [ ] Crea la lista `PRUEBA` y mete a los 3 (ficha › Listas › «Guardar»).

### F.2 Crear la campaña

1. [ ] **Ajustes › Campañas:** el «Texto del pie» dice «Responde BAJA para no recibir más mensajes.» (o lo que hayas puesto).
2. [ ] **Campañas** › **«Nueva campaña»**.
3. [ ] **Paso 1 · Nombre y cuenta:** Nombre `[PRUEBA] 3 propios`; elige la cuenta (solo aparecen WhatsApp y Telegram).
4. [ ] **Paso 2 · Audiencia:** marca la lista `PRUEBA`. **Esperado:** «Destinatarios» = **3**; riesgo según el caso (si ellos te respondieron: «Conocidos» 3); «Omitidos» = 0. *Si sale «Sin identidad» > 0, ese contacto no está asociado a la cuenta (vuelve a F.1).*
5. [ ] **Paso 3 · Mensaje:**
   - Mensaje A: `[PRUEBA] Hola {nombre}, esto es una prueba de dConnector. {Saludos|Un abrazo}`
   - «+» para una **variante B**: `[PRUEBA] {nombre}: mensaje de prueba, no hace falta contestar.`
   - Deja activado **«Pie de baja»**; la vista previa muestra el pie debajo.
6. [ ] **Panel «Revisión» (semáforo):** no debe estar en rojo. (Verde «Todo en orden» o amarillo «Revisa los avisos» están bien; lee los avisos.) Con menos de 30 destinatarios las variantes no son obligatorias.
7. [ ] **Paso 4 · Envío:**
   - Inicio: «Ahora».
   - **Ventana horaria:** pon **Desde 00:00 Hasta 23:59** y marca **todos los días** (Lun…Dom). *(Si dejas 09:00–18:00 de lunes a viernes y estás fuera de horario, quedará en «Fuera de horario».)*
   - «Prueba A/B»: apagada.
   - **«Ritmo de la cuenta»:** pulsa **«Ajustar»**, mira los seis grupos (Esperas, Límites, Tandas, Calentamiento, Freno, Frecuencia) y los valores: 20–45 s, 60–120 s (fríos), 60/hora, 200/día, 20 fríos/día, tandas de 25 con 10 min, calentamiento 20 envíos el primer día (14 días), freno 30 % / 95 % / 3 bajas, 3 campañas por contacto en 7 días. Pulsa **«Restablecer valores seguros»** y **«Guardar»** solo si cambiaste algo.
8. [ ] **Paso 5 · Resumen:** Nombre, Cuenta, Destinatarios, Mensaje (+ 1 variante), Inicio, Ventana, Prueba A/B «No», Ritmo, **Duración** estimada (de unos minutos).
9. [ ] Pulsa **«Guardar borrador»**: vuelve a la lista y la campaña aparece como **«Borrador»**. Ábrela (clic) › «Editar» › vuelves al asistente con todo lo escrito. (Prueba también a cerrar el asistente a medias con la «X» y volver a abrir «Nueva campaña»: lo escrito se recupera como borrador, con el botón «Descartar borrador» en el paso 1.)

### F.3 Revisar el panel y lanzar

1. [ ] En el asistente, paso 5, **«Lanzar»** está activo (no gris ni rojo). Pulsa **«Lanzar»**.
2. [ ] Aparece el aviso «Campaña lanzada» y se abre el **detalle**: estado **«En curso»**, y debajo «Enviando…» o «Próximo envío HH:mm».
3. [ ] Contadores: Pendientes 3 al principio; sube **Enviados** conforme sale cada uno.
4. [ ] En el **otro teléfono** de cada contacto: llega el mensaje, **antes se ve «escribiendo…»**, **empieza por `[PRUEBA]`**, `{nombre}` se sustituye por el nombre, `{Saludos|Un abrazo}` elige una opción, y al final lleva el **pie de baja**.
5. [ ] Las pausas entre envíos son de decenas de segundos (20 a 45 s con conocidos; 60 a 120 s con fríos): **no** salen los 3 de golpe.
6. [ ] **Si ves «Fuera de horario»** es que la ventana no cubre la hora actual: edítala (Pausar › Editar).

### F.4 Pausar, reanudar, cola

1. [ ] Con la campaña «En curso» y aún con pendientes, pulsa **«Pausar»**: pasa a **«En pausa»** y **deja de enviar** (espera más que la pausa normal: no sale ninguno más).
2. [ ] Pulsa **«Reanudar»**: vuelve a **«En curso»** y sigue enviando.
3. [ ] Mira la **cola** del detalle, pestañas **Pendientes**, **Enviados**, **Fallidos**, **Omitidos**: los enviados salen con su número y punto de riesgo.
4. [ ] Al terminar, el estado pasa a **«Terminada»**, `3/3` enviados.
5. [ ] En **Chats**, los 3 mensajes aparecen también en el chat de cada uno.
6. [ ] **Baja:** desde el teléfono de **uno** de los contactos responde **exactamente** `BAJA`. En el detalle, **Bajas** sube a 1; en **Contactos**, ese contacto muestra el símbolo de campana tachada («No recibe campañas»).
7. [ ] **Segunda campaña** (mismo texto, `[PRUEBA] segunda`) a la misma lista: «Omitidos» muestra **1 en «Baja»**. Cancélala sin lanzar (o bórrala).
8. [ ] Quita la marca «No recibe campañas» de ese contacto (ficha › interruptor › «Guardar»).
9. [ ] Prueba de **cuenta desconectada:** lanza otra campaña de prueba, «Desconectar» la cuenta a mitad: la línea dice «Cuenta desconectada» (no «Fallido»); «Conectar» y sigue.
10. [ ] **«Cancelar»** una campaña en curso (con confirmación) y **«Eliminar»** la de prueba cuando no esté corriendo.
11. [ ] **[RIESGO] Lo que NO se puede probar con 3 contactos** (se anota «sin probar»): el **freno automático** (hace falta ≥10 intentos), el **calentamiento** de varios días, la **prueba A/B** (espera 4 h) y los **límites por hora y por día**. Si te animas con **A/B**, activa «Prueba A/B» con 2 mensajes y observa que parte del reparto queda «retenido».
12. [ ] Repite lo esencial con una cuenta de **Telegram** si la tienes.

---

## G. Mensaje programado

- [ ] **Programados › «Nuevo»**: cuenta, **Chat** (con buscador «Buscar chat»), Mensaje `[PRUEBA] programado`, **Fecha y hora** a **3 minutos** de ahora › **«Programar»**. En la tabla sale como **Pendiente**.
- [ ] A su hora (con un margen de hasta 15 s) el estado pasa a **«Enviado»** y el mensaje llega al otro teléfono.
- [ ] Crea otro y pulsa **«Enviar ahora»**: sale ya.
- [ ] Crea otro y **«Editar»** (cambia la hora o el texto) y luego **«Cancelar»**: pasa a **Cancelado**.
- [ ] **«Borrar»** uno enviado y uno cancelado.
- [ ] Filtros **Todos / Pendientes / Enviados / Fallidos / Cancelados** con su recuento.
- [ ] **Con adjunto:** programa una foto con pie; llega a su hora aunque borres el original antes.
- [ ] **Cuenta desconectada:** programa uno para dentro de 2 minutos, «Desconecta» la cuenta, espera a que pase la hora y «Conecta»: se envía al conectar (si no pasaron 6 horas).
- [ ] **[RIESGO]** Programar una cuenta desconectada **más de 6 horas** debería dejarlo «Fallido» con «La cuenta no estuvo conectada» (puedes dejarlo sin probar).
- [ ] **Estado programado** (desde Estados › «Programar»): sale en la tabla con tipo «Estado».

---

## H. Contactos: importar de una cuenta y desde CSV

- [ ] **«Importar de una cuenta»** (menú ⋮): solo las cuentas **conectadas** se pueden elegir. Se importan tus contactos; el resumen dice cuántos se crearon.
- [ ] **Importar otra vez**: no duplica (todo «omitido» o «actualizado»).
- [ ] Una cuenta de **Messenger/Instagram** avisa de que no está admitido.
- [ ] **CSV.** Crea un archivo `contactos-prueba.csv` (en el Bloc de notas, guardado como UTF-8) con este contenido, cambiando los teléfonos por números tuyos:

  ```
  nombre;telefono;etiquetas
  Prueba Uno;0991234567;prueba,csv
  Prueba Dos;+593987654321;prueba
  Prueba Tres;0981112223;prueba|csv
  ```

  Contactos › menú ⋮ › **«Importar CSV»** › «Elegir archivo» › marca **«Tiene encabezado»** › **Nombre = 1, Teléfono = 2, Etiquetas = 3**, Notas = «No importar» › **«Añadir a la lista»** `PRUEBA` › **«Importar»**.
- [ ] El resumen dice 3 creados. Los contactos aparecen con sus etiquetas y en la lista `PRUEBA`; los teléfonos se normalizan a `+593…`.
- [ ] **Reimportar el mismo CSV:** todo sale «omitido».
- [ ] Nota: **no hay vista previa de las filas** (esperado en esta versión). Anota si la selección de columnas por número es difícil de usar.
- [ ] Un CSV con **acentos hecho en Excel** (guardado como «CSV (delimitado por comas)») se lee bien (los nombres con tilde salen bien).
- [ ] **Sugerencias de unión:** si tienes dos contactos con el mismo teléfono, aparece el par y puedes «Unir» o «Ignorar».
- [ ] **Unir manualmente** dos contactos (ficha › «Unir con otro»): se conservan identidades, etiquetas y listas.
- [ ] **«Separar»** una identidad de un contacto con varias.
- [ ] **Etiquetas y notas:** añadir, guardar y filtrar por etiqueta.
- [ ] **«Abrir chat»** desde la ficha lleva al chat.
- [ ] **Importante para campañas:** un contacto del CSV **sin identidad** sale como «Sin identidad» en una campaña; tras «Importar de una cuenta» ya tiene identidad.

---

## I. Estadísticas con datos reales

Con unos días de uso real (o tras las pruebas de arriba):

- [ ] **Conversaciones:** «Enviados» y «Recibidos» coinciden aproximadamente con lo que sabes que hiciste hoy (compara con un chat concreto contando a mano).
- [ ] El periodo **«7 días»**, **«30 días»**, **«90 días»** y **«Personalizado»** (con «Desde» y «Hasta») cambian los números; un periodo futuro o al revés avisa.
- [ ] La **comparación con el periodo anterior** (flechitas y porcentajes) tiene sentido o está vacía si no había datos.
- [ ] **Mensajes por día**, **Horas de más actividad** (mapa de calor) y **Tiempos de respuesta** muestran datos creíbles.
- [ ] **Quién inicia**: «Conversaciones que inicias» / «que inician» cuadran con tu uso.
- [ ] **Chats más activos / frecuentes:** clic abre el chat.
- [ ] **Selector de cuentas:** al elegir una cuenta, solo cuenta esa.
- [ ] **Grupos:** «Grupos más activos» y «Personas más activas» (sin contarte a ti).
- [ ] **Estados › Estadísticas:** «Publicados», «Vistas medias», «Alcance» tras haber publicado y recibido vistas.
- [ ] Sin datos, salen los mensajes «Sin mensajes en este periodo» / «Sin actividad en grupos», no errores.

---

## J. Destino con el servidor de ejemplo (echo-server)

Probamos el **protocolo**: dConnector manda los eventos a un servidor tuyo y ese servidor responde «Recibido ✅» por el canal correcto.

### J.1 Arrancar el echo-server

El programa se llama `echo-server-<versión>-windows-x64.exe` (viene en las descargas; también se compila con `task echo-server`).

1. [ ] Abre **PowerShell** (o el Símbolo del sistema) en la carpeta donde está el archivo: en el Explorador, clic derecho en la carpeta › «Abrir en Terminal».
2. [ ] Escribe y pulsa Enter:

   ```
   .\echo-server-<versión>-windows-x64.exe -addr 127.0.0.1:8787 -token secreto
   ```

   (Estos son además sus valores por defecto: puedes ejecutarlo sin opciones, dando doble clic.)
3. [ ] **Esperado:** imprime

   ```
   servidor de ejemplo escuchando en ws://127.0.0.1:8787/ (token: "secreto")
   ```

   Deja esa ventana **abierta** (se detiene con `Ctrl+C`). Si Windows pregunta por el cortafuegos, no hace falta permitir nada: solo escucha en tu propio equipo (`127.0.0.1`). Si el puerto 8787 está ocupado, usa otro (por ejemplo `-addr 127.0.0.1:9000`) y cambia el número también en la dirección del destino.

### J.2 Crear el destino

1. [ ] dConnector › **Destinos** › **«Nuevo destino»**.
2. [ ] **Nombre:** `Eco`. **Dirección:** `ws://127.0.0.1:8787/`. **Token:** escribe **`secreto`** (si lo dejas vacío, dConnector genera uno aleatorio y el echo-server lo **rechazará**, porque espera `secreto`).
3. [ ] Cuentas: «Todas». Canales: «Todos». Eventos: todos marcados. «Activo»: sí. **«Incluir los míos»**: no. **«Incluir grupos»**: no. **«Permitir comandos»**: **no** (de momento).
4. [ ] Pulsa **«Probar conexión»**: aviso verde **«Conexión correcta»**. En la ventana del echo-server aparecen `dConnector conectado: instancia …, protocolo 1, dConnector/<versión>`, una línea `HELLO  instancia=… versión=<versión> protocolo=1 comandos=false cuentas=N` (una línea `cuenta wa-… (whatsapp, «Etiqueta») connected` por cuenta) y luego `dConnector desconectado` (la prueba abre y cierra).
5. [ ] **«Guardar»**. En la lista, el destino pasa a **«En línea»** con punto verde. La ventana del eco vuelve a mostrar `conectado` y `HELLO`.

### J.3 Eventos

Desde el **otro teléfono**, escribe a la cuenta un mensaje de texto `hola desde el eco` (chat individual).

- [ ] En la ventana del echo-server aparece una línea como:

  `EVENT  message.received cuenta=wa-… chat=593…@s.whatsapp.net de=593…@s.whatsapp.net fromMe=false «hola desde el eco»  id=01…`
- [ ] Con «Permitir comandos» **apagado**, además aparece: `RESULT echo-1 message.reply error=forbidden «…»` (el servidor intentó contestar y dConnector lo rechazó: esperado), y **no** llega «Recibido ✅».
- [ ] También llegan otros eventos (`chat.updated`, `account.state`…): el eco imprime solo su nombre.
- [ ] Un mensaje de **foto** muestra `«(image)»` (o el pie). Un **mensaje de grupo** **no** aparece (porque «Incluir grupos» está apagado).
- [ ] **«Incluir grupos» encendido:** edita el destino, enciende el interruptor, guarda; un mensaje de grupo **sí** se imprime, pero el eco **no contesta** (solo contesta a chats individuales).

### J.4 «Recibido ✅» y un comando

1. [ ] Edita el destino: enciende **«Permitir comandos»** (aparece el aviso **«El servidor podrá enviar mensajes en tu nombre»**) y **«Guardar»**. El echo-server muestra `HELLO … comandos=true`.
2. [ ] Desde el otro teléfono escribe `segundo mensaje`.
3. [ ] **Esperado, en el echo-server:** `EVENT  message.received …` y luego `RESULT echo-1 message.reply ok` (el número `echo-N` crece).
4. [ ] **Esperado, en el otro teléfono:** llega **«Recibido ✅»**, **citando** tu mensaje, enviado desde la cuenta. **Esperado, en dConnector:** el mensaje «Recibido ✅» aparece también en el chat como enviado por ti.
5. [ ] **«Recibido ✅» llega una sola vez** por mensaje (no se repite en cada reconexión).
6. [ ] **«Incluir los míos»:** enciéndelo, guarda, y escribe otro mensaje: ahora el eco imprime también `EVENT  message.sent … fromMe=true «Recibido ✅»` además del recibido. Apágalo si no lo quieres.
7. [ ] **Un mensaje tuyo escrito desde el teléfono principal** (o desde otro dispositivo de la cuenta) con «Incluir los míos» apagado: el eco **no** lo muestra; con «Incluir los míos» encendido, sí (`fromMe=true`).

### J.5 Pendientes, token y errores

- [ ] **Pendientes (outbox):** cierra el echo-server con `Ctrl+C`. En dConnector, el destino pasa a **«Conectando…»** o **«Sin conexión»**. Con el eco apagado, escribe **3 mensajes** al otro teléfono → cuenta. La tarjeta muestra **pendientes** (por ejemplo «3 pendientes»). **Vuelve a arrancar** el echo-server: aparece `HELLO` y llegan los 3 eventos **en orden**, y los pendientes bajan a 0. Ningún evento se pierde ni sale repetido (si se repite uno ya confirmado, el eco dice «repetido, se ignora»).
- [ ] **Token equivocado:** edita el destino, pon token `malo`, guarda. El eco imprime `conexión rechazada: token no válido`; el destino dice **«Error»** con «Token rechazado» y **no** vuelve a intentarlo solo (el eco no imprime más intentos). Corrige a `secreto`, guarda: vuelve a «En línea».
- [ ] **Dirección no válida:** una dirección `ws://ejemplo.com` o `http://…` no se acepta al guardar («Debe empezar por wss:// (ws:// solo con localhost o 127.0.0.1)»).
- [ ] **«Ver el protocolo»** abre una pestaña con el **resumen** del protocolo (es un resumen, no el documento completo). **[RIESGO]** el enlace usa una dirección `blob:`; anota si **no** abre nada.
- [ ] **Desactivar** el destino con su interruptor: pasa a «Desactivado» y el eco ya no recibe; reactivar vuelve a conectar.
- [ ] **Eliminar** el destino: pide confirmación.
- [ ] **Seguridad:** en los eventos que imprime el echo-server **nunca** aparece el QR ni el código de 8 caracteres de una cuenta que estés vinculando (solo `account.state`, sin código).
- [ ] **Comandos extra (opcional):** un destino con «Permitir comandos» podría recibir `message.send`, `message.react`, etc.; solo el eco de ejemplo está disponible, y solo usa `message.reply`.

*(El echo-server imprime el **texto** de los mensajes porque es un ejemplo para desarrolladores. dConnector, en cambio, nunca escribe el texto de los mensajes en su registro.)*

---

## K. Comprobaciones del sistema

### K.1 Bandeja y cierre

- [ ] Al arrancar aparece el **icono en la bandeja** (junto al reloj; quizá en «Mostrar iconos ocultos»).
- [ ] Pasar el ratón muestra «dConnector» o **«dConnector · N sin leer»** (con mensajes sin leer, el número cambia al leerlos).
- [ ] **Clic izquierdo** (o doble clic) abre la ventana; **clic derecho** muestra «Abrir dConnector», una línea por cada cuenta con su estado (por ejemplo `● Ventas (WhatsApp) — Conectada`) y «Salir».
- [ ] El estado de las líneas de cuenta cambia al desconectar o conectar una cuenta (con un pequeño retraso).
- [ ] **Cerrar con la X** (con «Cerrar a la bandeja» activo): la ventana se oculta y aparece (la primera vez) la notificación **«dConnector sigue abierto en la bandeja»**. Las cuentas siguen conectadas (mira desde el otro teléfono: el chat sigue entregando).
- [ ] **Notificación con la ventana oculta:** con la ventana oculta, escribe desde el otro teléfono: aparece una **notificación de Windows** con el título «chat · etiqueta de cuenta» y el principio del mensaje. Al pulsarla se abre dConnector **en ese chat**.
- [ ] **No** sale notificación cuando miras la ventana, ni de un chat **silenciado**, ni de tus propios mensajes.
- [ ] Apaga **Ajustes › General › «Notificaciones»**: ya no salen.
- [ ] **«Cerrar a la bandeja» apagado:** la X cierra el programa de verdad.
- [ ] **«Salir»** (menú de la bandeja), con «Seguir en segundo plano» apagado: terminan los dos procesos (compruébalo en el Administrador de tareas: ya no hay `dConnector.exe` ni `dConnector-server.exe`), y al reabrir las cuentas reconectan.
- [ ] **Instancia única:** con dConnector abierto (también oculto en la bandeja), vuelve a ejecutar `dConnector.exe`: **no** se abre una segunda copia; se muestra la ventana existente.
- [ ] **[RIESGO]** Si tienes instalada la versión MSI **y** abres también la portable, las dos comparten el mismo «identificador de instancia única»: la segunda solo mostrará la primera. Anota qué pasa.

### K.2 Arranque con Windows

- [ ] **Ajustes › General › «Iniciar con Windows»** activado y guardado.
- [ ] En el Editor del Registro, `HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Run` tiene un valor `dConnector` con `"<ruta del exe>" --minimized`.
- [ ] **Reinicia Windows** (o cierra sesión y vuelve a entrar): dConnector arranca **solo, oculto en la bandeja**, y las cuentas se conectan sin que abras nada.
- [ ] Desactivar el ajuste **quita** ese valor del registro.
- [ ] «Iniciar minimizado» (ajuste): activa «Iniciar con Windows» e «Iniciar minimizado», guarda, cierra sesión de Windows o reinicia y comprueba que dConnector arranca oculto en la bandeja. Si lo abres a mano, la ventana SIEMPRE se abre. Anota qué ves.
- [ ] Con el **portable**, el valor del registro apunta a la ruta actual del `.exe` (si mueves la carpeta hay que volver a guardar el ajuste).

### K.3 Datos y registro

- [ ] **Ajustes › Datos › «Abrir carpeta»** abre la carpeta de datos (MSI: `%APPDATA%\dConnector`; portable: `data\`). Contiene `app.db`, `accounts\`, `logs\`.
- [ ] **«Abrir registros»** abre `logs\` con el archivo del día `dconnector-AAAA-MM-DD.log`.
- [ ] Con «Registro detallado» el archivo crece más. Ábrelo con el Bloc de notas: **no hay texto de mensajes ni nombres de chats ni números completos**.
- [ ] Cierra con «Salir» y copia la carpeta de datos a otro lado; abre dConnector con esos datos restaurados (cierra antes, copia de vuelta): todo sigue igual.

### K.4 Rendimiento y estabilidad

- [ ] Con 2 o 3 cuentas conectadas durante una hora, la ventana va fluida y no se come la memoria (mira el Administrador de tareas).
- [ ] Cortar el wifi unos minutos y volver: las cuentas pasan a «Error»/«Conectando…» y **reconectan solas**.
- [ ] Una lista de chats larga (cientos) se desplaza con fluidez.
- [ ] Anota el tamaño de `dConnector.exe` y de `dConnector-server.exe` y la memoria que usa cada proceso en el Administrador de tareas.

---

## L. MSI y portable

### L.1 Instalador `.msi`

- [ ] Doble clic en `dConnector-<versión>-x64.msi` (o el `arm64`). SmartScreen: «Más información» › «Ejecutar de todas formas». **No pide permisos de administrador.**
- [ ] El asistente de instalación se ve bien. **Anota si el texto sale en español o en inglés** (si el idioma español de la herramienta no estaba disponible, sale en inglés).
- [ ] Termina y crea los accesos directos **«dConnector»** en el **menú Inicio** y en el **Escritorio**.
- [ ] Instala en `%LOCALAPPDATA%\Programs\dConnector` (comprueba que existe `dConnector.exe`).
- [ ] Aparece en «Configuración › Aplicaciones › Aplicaciones instaladas» con su icono, empresa «Darkmelon» y la versión que instalaste.
- [ ] Abre y funciona; los datos se crean en `%APPDATA%\dConnector`. Añade una cuenta para tener algo que conservar.
- [ ] **Actualizar encima:** hace falta un `.msi` de una versión **mayor** que la instalada (por ejemplo, instala primero el de la 0.2.x y luego el de la 0.3.0; o cambia el archivo `VERSION` y ejecuta `task msi`). Instálalo **encima** sin desinstalar: se instala en la misma carpeta, **la cuenta y los datos siguen ahí**, y «Aplicaciones instaladas» muestra la versión nueva (no dos entradas). Instalar uno **más antiguo** encima debe dar el aviso «Ya está instalada una versión más nueva de dConnector.»
- [ ] **Desinstalar** desde «Aplicaciones instaladas»: desaparecen el programa y los accesos directos, **pero `%APPDATA%\dConnector` sigue intacta**.
- [ ] **Reinstalar** el `.msi`: al abrir, las cuentas y chats **siguen** como estaban.
- [ ] El proceso `dConnector.exe` **no** debe estar abierto al desinstalar o actualizar (si lo está, usa «Salir» antes y anota si el instalador se queja).

### L.2 Carpeta portable `.zip`

- [ ] Descomprime `dConnector-<versión>-portable-x64.zip` en una carpeta nueva (por ejemplo `D:\prueba`). Dentro hay una carpeta `dConnector-<versión>-portable-x64` con `dConnector.exe`, `dConnector-server.exe`, `portable.txt`, `LEEME.txt` y `LICENCIAS.txt`.
- [ ] Abre `dConnector.exe` (SmartScreen la primera vez). Aparece la carpeta **`data\`** junto al `.exe` con `app.db`; **no** se crea `%APPDATA%\dConnector` nuevo.
- [ ] Añade una cuenta y escribe algo; cierra con «Salir».
- [ ] **Mueve toda la carpeta** a otro sitio y ábrela: la cuenta y los chats siguen.
- [ ] **Borra `portable.txt`** (a propósito, en una copia): ahora usaría `%APPDATA%\dConnector` en lugar de `data\` (vuelve a ponerlo).
- [ ] `LEEME.txt` y `LICENCIAS.txt` se leen bien con el Bloc de notas (tildes correctas).
- [ ] **Ajustes › Acerca de**: «Versión» (la que instalaste), «Protocolo v1» y la lista de licencias (whatsmeow, gotd, messagix, Wails, modernc sqlite).

---

## M. Conexión local y remota, y el servidor (0.3.0)

Lo nuevo de la 0.3.0: la ventana es un cliente de un servidor. Esta sección se prueba con una cuenta de prueba ya vinculada (o con el adaptador falso del `--selftest`, que no necesita cuentas). **Linux y arm64 no se han ejecutado todavía en vivo: aquí se comprueba por primera vez.**

### M.1 Servidor local (el de dentro de la aplicación)

- [ ] Con el MSI o el portable, abre dConnector: en el Administrador de tareas aparece **`dConnector-server.exe`** junto a `dConnector.exe`, sin ventana propia.
- [ ] **Ajustes › Conexión:** estado **«Conectado»**, modo **Local**, y la **versión del servidor** y de su API coincide con la de la aplicación.
- [ ] El servidor local **solo escucha en loopback**: en una consola, `netstat -ano | findstr LISTEN` muestra su puerto en `127.0.0.1`, **no** en `0.0.0.0` ni en la IP de la red.
- [ ] **Datos de la 0.2.x:** abre la versión 0.3.0 sobre una carpeta de datos de la 0.2.x (haz **una copia antes**): las cuentas, chats y contactos **siguen ahí** sin pasos manuales.
- [ ] **Reinicio del servidor local:** termina `dConnector-server.exe` desde el Administrador de tareas. La ventana pasa a **«Reconectando»**, el servidor **vuelve a arrancar solo** (esperas de 1, 2, 4… s) y la ventana vuelve a **«Conectado»** sin perder la pantalla en la que estabas. Los chats se actualizan (recarga automática).
- [ ] Repite la caída varias veces seguidas: la espera crece (hasta 30 s) y, al volver, todo funciona.
- [ ] Cierra `dConnector-server.exe` y **borra o renombra** el archivo (en una copia): al abrir dConnector aparece un aviso claro de que **no encuentra `dConnector-server`** (no una ventana en blanco). Restáuralo.

### M.2 «Seguir en segundo plano»

- [ ] **Apagado (por defecto):** «Salir» desde la bandeja: desaparecen `dConnector.exe` **y** `dConnector-server.exe` en menos de unos 10 s.
- [ ] Activa **Ajustes › Conexión › «Seguir en segundo plano»** y sal con «Salir»: `dConnector-server.exe` **sigue vivo**. Escribe desde el otro teléfono a la cuenta de prueba: **la cuenta sigue conectada** (el mensaje llega al abrir dConnector).
- [ ] Vuelve a abrir dConnector: se **reconecta** al mismo servidor (no arranca uno segundo): en el Administrador de tareas hay **un solo** `dConnector-server.exe`.
- [ ] Desactiva la opción y sal otra vez: el servidor se apaga.
- [ ] Con la opción encendida, cierra la sesión de Windows o reinicia el equipo: el servidor **no** debe quedar colgado ni duplicado al volver.

### M.3 Servidor remoto en otra máquina

Instala el servidor siguiendo el [README](../README.md#instalar-el-servidor) (systemd en Linux, o servicio de Windows, o Docker). Anota cuál usas: ______.

- [ ] El servidor **arranca** y se ve la **clave del administrador** una sola vez (o usas `DCONNECTOR_ADMIN_PASSWORD`). Reiniciar el servidor **no** vuelve a mostrar ninguna clave.
- [ ] `https://<servidor>:8443/api/v1/health` (con `curl -k` o el navegador) responde `{"status":"ok",…,"tls":true}`.
- [ ] **Sin TLS fuera de loopback:** arranca el servidor con `--listen 0.0.0.0:8443 --no-tls` (sin `--insecure`): **se niega** a arrancar con un mensaje en español. Con `--insecure` arranca y escribe un **aviso cada minuto** en el registro.
- [ ] `dconnector-server user list` muestra el administrador; `user passwd admin` cambia la clave y **cierra las sesiones abiertas**.
- [ ] En el escritorio: **Ajustes › Conexión › Remoto**, dirección `https://<servidor>:8443`, usuario y clave, **«Guardar y conectar»**.
- [ ] **Huella TLS:** aparece **«Certificado sin confirmar»** con una huella **idéntica** a la que imprimió el servidor (compárala carácter a carácter). **«Confiar en este certificado»** conecta y la huella queda visible en Ajustes › Conexión.
- [ ] Reinicia el servidor: **misma huella**, la ventana se reconecta sola, sin pedir confianza otra vez.
- [ ] **Huella cambiada:** borra `<datos>/tls/` del servidor y reinícialo (certificado nuevo): la ventana **se niega a conectar** y avisa **«El certificado del servidor cambió»** con la huella nueva; solo con «Confiar en este certificado» vuelve a conectar.
- [ ] **Usuario o clave incorrectos:** mensaje claro. **Seis intentos fallidos seguidos** desde la misma máquina: el servidor responde «Demasiados intentos fallidos…» y bloquea un rato (el acierto posterior debe esperar).
- [ ] Las cuentas, chats y campañas que ves son **los del servidor**, no los del equipo local. «Usar este equipo» vuelve a los locales sin borrar nada.
- [ ] **Adjuntar archivos con servidor remoto:** envía una foto, un documento de varios MB, un archivo cercano a 100 MiB y uno **mayor** (debe dar un error claro, no colgarse). Funciona también en una campaña, un programado y un estado, y al importar un CSV de contactos.
- [ ] «Guardar como» de un medio recibido guarda el archivo correcto.
- [ ] **Corte del servidor:** apaga el servidor con la ventana abierta: aparece **«Sin conexión»** (o la banda de aviso) con **«Reintentar»**, **no** una ventana en blanco; al encenderlo, la ventana se reconecta sola y **recarga** chats, programados, destinos y contactos.
- [ ] **Corte de red:** desconecta el wifi del cliente un minuto y vuelve: se reconecta sola.
- [ ] **Sesión caducada o cerrada:** cambia la clave con `user passwd` mientras la ventana está abierta: la ventana pide **«Iniciar sesión»**; con la clave nueva vuelve a entrar.

### M.4 Proxy inverso (Caddy o nginx)

Con la configuración de ejemplo del README (servidor con `--listen 127.0.0.1:8443 --no-tls`).

- [ ] La ventana conecta a `https://<dominio>` **sin pedir confiar** en ninguna huella (certificado de una autoridad).
- [ ] Los **eventos en vivo** funcionan (llega un mensaje y aparece sin recargar): el WebSocket `/api/v1/events` atraviesa el proxy. Déjalo 10 minutos sin actividad: no se corta.
- [ ] Un archivo de unos **100 MiB** se sube bien por `/api/v1/files` (si el proxy lo corta, el límite de cuerpo está mal).
- [ ] Anota el comportamiento del límite de intentos detrás del proxy: todos los fallos cuentan para la misma IP (el proxy).

### M.5 Dos clientes a la vez

Con el servidor remoto, abre dConnector en **dos equipos** (o un equipo con el servidor local y un navegador/otro cliente si tienes una forma de probarlo).

- [ ] Los dos ven los mismos chats y se actualizan **al mismo tiempo**: un mensaje enviado desde uno aparece en el otro.
- [ ] Marcar un chat como leído en uno **baja el contador** del otro.
- [ ] Silenciar o fijar un chat en uno se refleja en el otro.
- [ ] Un mensaje de entrada con **ninguno** mirando ese chat: **avisan los dos** (aviso y sonido en cada equipo).
- [ ] Con el chat **abierto y enfocado en un equipo**: **no avisa ninguno** de los dos.
- [ ] Cerrar sesión o cambiar la clave en uno no rompe al otro de forma rara (el otro pide iniciar sesión solo si su sesión se cerró).

### M.6 Notificaciones con cliente remoto

- [ ] **«Notificaciones de escritorio»**, **«Sonido»** y **«Mostrar el texto del mensaje»** (Ajustes › Notificaciones) se guardan **en el servidor**: cambia uno en un equipo y comprueba que el otro lo ve.
- [ ] Con la ventana oculta o sin foco, llega un mensaje: **aviso de Windows** con el título del chat y la cuenta, sonido propio, y el **contador rojo** de la barra de tareas sube. Al pulsar el aviso se abre **ese chat**.
- [ ] Con «Mostrar el texto del mensaje» apagado, el aviso dice **«Mensaje nuevo»**.
- [ ] Chat **silenciado**, mensaje **propio** o chat **mirándose con la ventana enfocada**: no avisa. **Ajustes › Notificaciones › «Última decisión»** explica por qué.
- [ ] **No molestar de Windows** activo: no se ve el aviso pero **sí suena** el sonido propio, y Ajustes lo dice.
- [ ] **«Probar notificación»** funciona y es honesta (dice si no se pudo mostrar).
- [ ] Al volver a conectar tras un corte, **no** salen avisos viejos repetidos.

### M.7 Servidor como servicio y en otras plataformas

- [ ] **Linux (x64 y, si puedes, arm64):** el servidor arranca con systemd con el usuario `dconnector`; `systemctl reload dconnector` recarga un certificado externo sin cortar; `journalctl -u dconnector` muestra el registro **sin claves ni tokens**; `systemctl stop` apaga en menos de unos 10 s.
- [ ] **Windows:** `--service install`, `start`, `stop` y `uninstall` funcionan (como administrador), el servicio arranca con Windows y se reinicia solo si se le mata el proceso, y desinstalar **no** borra los datos.
- [ ] **Docker:** la imagen construida con `deploy/Dockerfile` arranca, `docker ps` marca el contenedor como `healthy`, los datos persisten en el volumen `/data` al recrear el contenedor, y `docker exec -it dconnector /dconnector-server user list` funciona.
- [ ] **Registro:** busca en `logs/` y en la salida del servicio la clave del administrador, tokens y direcciones con `?token=`: **no debe haber ninguno**.
- [ ] **Copia de seguridad:** con el servidor parado, copia la carpeta de datos, restáurala en otra máquina y comprueba que las cuentas abren (con la cuenta de prueba).

---

## Resultado

Rellena cada celda con **OK**, **falla** o **n/a** (no aplica o no admitido por ese canal). Una celda vacía = sin probar. Donde ponga «falla», explica en **Notas**.

### Por canal y función

| Función | WhatsApp | Telegram | Messenger | Instagram |
| --- | --- | --- | --- | --- |
| Vincular: QR | | n/a | n/a | n/a |
| Vincular: código de 8 caracteres | | n/a | n/a | n/a |
| Vincular: teléfono, código y contraseña | n/a | | n/a | n/a |
| Vincular: cookies | n/a | n/a | | |
| Vincular: usuario y contraseña | n/a | n/a | | n/a |
| Sesión persiste al reabrir | | | | |
| Recibir texto | | | | |
| Recibir foto | | | | |
| Recibir vídeo | | | | |
| Recibir nota de voz | | | | |
| Recibir audio | | | | |
| Recibir documento | | | | |
| Recibir sticker | | | | |
| Recibir ubicación | | | | |
| Recibir contacto | | | | |
| Recibir encuesta | | | | |
| Recibir mensaje con cita | | | | |
| Recibir mensaje reenviado | | | | |
| Grupos (recibir y enviar) | | | | |
| Enviar texto | | | | |
| Enviar foto | | | | |
| Enviar vídeo | | | | |
| Enviar nota de voz | | | | |
| Enviar audio | | | | |
| Enviar documento | | | | n/a |
| Enviar sticker | | | n/a | n/a |
| Enviar ubicación | | | n/a | n/a |
| Enviar contacto | | | n/a | n/a |
| Enviar encuesta | | | n/a | n/a |
| Responder (cita) | | | | |
| Reenviar (misma cuenta) | | | n/a | n/a |
| Reaccionar | | | | |
| Editar | | | | |
| Eliminar para todos | | | | |
| Eliminar para mí | | | | n/a |
| Escribiendo (ver y mandar) | | | | |
| Leído (ver y mandar) | | | | |
| Medios se descargan y se ven | | | | |
| Historial al subir en un chat | | | | |
| Estados: publicar texto | | n/a | n/a | n/a |
| Estados: publicar foto/vídeo | | | n/a | n/a |
| Estados: ver los de otros | | | n/a | n/a |
| Estados: vistas de los míos | | | n/a | n/a |
| Chats cifrados (E2EE) | n/a | n/a | | n/a |
| Campaña de prueba (3 propios) | | | n/a | n/a |
| Mensaje programado | | | | |
| Importar contactos de la cuenta | | | n/a | n/a |
| Silenciar / archivar / fijar | | | n/a | n/a |
| Notificación con ventana oculta | | | | |

### Funciones generales (sin canal)

| Función | Resultado |
| --- | --- |
| Interfaz en tema claro (todas las pantallas) | |
| Interfaz en tema oscuro (todas las pantallas) | |
| `datetime-local`, `time` y `date` en WebView2 | |
| Diálogos de archivos y «Guardar como» | |
| Arrastrar y soltar / pegar imagen | |
| Nota de voz con micrófono | |
| Importar CSV | |
| Estadísticas con datos reales | |
| Destino con echo-server («Recibido ✅») | |
| Comandos con «Permitir comandos» | |
| Pendientes (outbox) y reconexión del destino | |
| Bandeja del sistema y menú | |
| Cerrar a la bandeja y «Salir» | |
| Instancia única | |
| Arranque con Windows | |
| Registro (`logs`) sin textos de mensajes | |
| MSI: instalar | |
| MSI: actualizar encima conservando datos | |
| MSI: desinstalar conservando datos | |
| Portable | |
| Servidor local: arranque, loopback y datos de la 0.2.x | |
| Servidor local: reinicio automático tras caída | |
| «Seguir en segundo plano» | |
| Conexión remota con TLS propio y huella (confiar, cambiar) | |
| Inicio de sesión remoto: error, bloqueo por intentos, sesión cerrada | |
| Subida de archivos con servidor remoto (hasta 100 MiB) | |
| «Sin conexión» y reconexión con recarga | |
| Proxy inverso (WebSocket y subidas) | |
| Dos clientes a la vez | |
| Notificaciones con cliente remoto (aviso, sonido, contador, «Última decisión») | |
| Servidor en Linux (systemd) x64 / arm64 | |
| Servidor como servicio de Windows | |
| Servidor en Docker | |
| Copia de seguridad y restauración del servidor | |

### Notas

Anota aquí cada fallo: canal, hora, qué hiciste, qué viste y qué registro guardaste.

| N.º | Canal / pantalla | Hora | Qué hice | Qué vi | Archivo de registro |
| --- | --- | --- | --- | --- | --- |
| 1 | | | | | |
| 2 | | | | | |
| 3 | | | | | |
| 4 | | | | | |
| 5 | | | | | |
| 6 | | | | | |
| 7 | | | | | |
| 8 | | | | | |

Notas libres:

&nbsp;

&nbsp;

&nbsp;

### Resumen de lo marcado como [RIESGO]

Para orientarte: los puntos más probables de fallo según las notas de construcción.

| Qué | Dónde está arriba |
| --- | --- |
| Chats cifrados de extremo a extremo de Messenger (registro del dispositivo, conexión, envío y recepción) y su interruptor | D.2 |
| Instagram con `instameow` (login con cookies, bandeja, avisos, cursor al reconectar) | E |
| Inicio con usuario y contraseña de Messenger | D.1 |
| Floodwait de Telegram al mandar mucho seguido | C.4 |
| Sesión de Telegram entre reinicios, descargas de otro centro de datos, historias | C.1, C.5 |
| `@lid` de WhatsApp (una persona = un solo chat) | B (cabecera) |
| «Escribiendo» y marcas de entregado de WhatsApp, historial inicial | B.4, B.1 |
| Citar y reenviar en WhatsApp con mensajes antiguos (caché de 3.000) | B.3 |
| Responder a un estado de WhatsApp como cita de estado | B.6 |
| Nota de voz (Opus de WebView2 + conversión), arrastrar y soltar, vídeo con audio, `blob:` de «Ver el protocolo» | A.3, J.5 |
| Freno, calentamiento, A/B y límites de campañas (no probables con 3 contactos) | F.4 |
| Interruptor «Iniciar minimizado» (con arranque con Windows) y «instancia única» entre MSI y portable | K.1, K.2 |
| Servidor en Linux y arm64 (solo compilado hasta ahora), servicio de Windows y Docker | M.7 |
| TLS con huella fijada, proxy inverso (WebSocket y límite de cuerpo) y límite de intentos tras un proxy | M.3, M.4 |
| «Seguir en segundo plano» y el reinicio automático del servidor local | M.1, M.2 |
| Notificaciones decididas por el servidor con dos clientes | M.5, M.6 |
