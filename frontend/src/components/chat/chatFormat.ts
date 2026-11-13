// Formatos de la pantalla de chats. Funciones puras, locales a esta carpeta.
import type { Chat, Message } from "../../api/core/model/models.js";

const DAYS = ["domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"];
const MONTHS = [
  "enero", "febrero", "marzo", "abril", "mayo", "junio",
  "julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre",
];

const pad = (n: number) => String(n).padStart(2, "0");

export function startOfDay(ts: number): number {
  const d = new Date(ts);
  d.setHours(0, 0, 0, 0);
  return d.getTime();
}

/** Diferencia en días de calendario entre `now` y `ts` (0 = hoy, 1 = ayer…). */
export function daysAgo(ts: number, now: number): number {
  return Math.round((startOfDay(now) - startOfDay(ts)) / 86_400_000);
}

export function formatClock(ts: number): string {
  const d = new Date(ts);
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** Hora de la lista: hoy `HH:mm`, ayer «Ayer», esta semana el día, si no `dd/MM/yy`. */
export function formatListTime(ts: number, now = Date.now()): string {
  if (!ts) return "";
  const diff = daysAgo(ts, now);
  if (diff <= 0) return formatClock(ts);
  if (diff === 1) return "Ayer";
  if (diff < 7) return DAYS[new Date(ts).getDay()];
  const d = new Date(ts);
  return `${pad(d.getDate())}/${pad(d.getMonth() + 1)}/${pad(d.getFullYear() % 100)}`;
}

/** Texto del separador de día de la conversación. */
export function dayLabel(ts: number, now = Date.now()): string {
  const diff = daysAgo(ts, now);
  if (diff <= 0) return "Hoy";
  if (diff === 1) return "Ayer";
  const d = new Date(ts);
  if (diff < 7) return DAYS[d.getDay()].replace(/^./, (c) => c.toUpperCase());
  return `${d.getDate()} de ${MONTHS[d.getMonth()]} de ${d.getFullYear()}`;
}

export function dayKey(ts: number): string {
  const d = new Date(ts);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

export function formatFullDate(ts: number): string {
  const d = new Date(ts);
  return `${d.getDate()} de ${MONTHS[d.getMonth()]} de ${d.getFullYear()}, ${formatClock(ts)}`;
}

export function formatBytes(n: number): string {
  if (!n || n < 0) return "";
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(n < 10 * 1024 ? 1 : 0).replace(".", ",")} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1).replace(".", ",")} MB`;
  return `${(n / 1024 / 1024 / 1024).toFixed(1).replace(".", ",")} GB`;
}

/** Duración en segundos → `m:ss` (o `h:mm:ss`). */
export function formatDuration(seconds: number): string {
  const s = Math.max(0, Math.round(seconds || 0));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const r = s % 60;
  return h > 0 ? `${h}:${pad(m)}:${pad(r)}` : `${m}:${pad(r)}`;
}

export function truncate(s: string, max: number): string {
  const t = (s ?? "").trim();
  return t.length > max ? t.slice(0, Math.max(0, max - 1)).trimEnd() + "…" : t;
}

export function initials(name: string): string {
  const parts = (name ?? "").trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return "?";
  const first = Array.from(parts[0])[0] ?? "?";
  const second = parts.length > 1 ? (Array.from(parts[1])[0] ?? "") : "";
  return (first + second).toUpperCase();
}

const SENDER_COLORS = [
  "#1fa855", "#d4380d", "#0f7ac2", "#c2185b", "#7b3fc4",
  "#b26a00", "#00838f", "#5d4037", "#c0392b", "#3949ab",
];

/** Color estable por id (nombre del emisor en grupos). */
export function senderColor(id: string): string {
  let h = 0;
  for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) >>> 0;
  return SENDER_COLORS[h % SENDER_COLORS.length];
}

/** ¿Está silenciado en este instante? `mutedUntil`: 0 no, -1 siempre, si no ms de epoch. */
export function isMuted(mutedUntil: number, now = Date.now()): boolean {
  return mutedUntil === -1 || mutedUntil > now;
}

export type Tick = "pending" | "sent" | "delivered" | "read" | "failed";

/** Estado de entrega de un mensaje propio (null si no es mío o no aplica). */
export function deliveryTick(m: Pick<Message, "fromMe" | "delivery" | "kind">): Tick | null {
  if (!m.fromMe || (m.kind as string) === "system") return null;
  switch (m.delivery as string) {
    case "pending":
      return "pending";
    case "failed":
      return "failed";
    case "delivered":
      return "delivered";
    case "read":
    case "played":
      return "read";
    case "sent":
      return "sent";
    default:
      return "sent";
  }
}

export const TICK_LABEL: Record<Tick, string> = {
  pending: "Pendiente",
  sent: "Enviado",
  delivered: "Entregado",
  read: "Leído",
  failed: "No se pudo enviar",
};

/** Vista previa de la lista: «Tú: » si es mío. Sin los ✓ (los pinta el componente). */
export function previewParts(chat: Pick<Chat, "lastPreview" | "lastFromMe" | "kind">): {
  prefix: string;
  text: string;
} {
  const text = (chat.lastPreview ?? "").replace(/\s+/g, " ").trim();
  return { prefix: chat.lastFromMe && text ? "Tú: " : "", text };
}

/** Texto genérico de un mensaje que el canal no sabe traducir. */
export const UNSUPPORTED_LABEL = "Mensaje no compatible";

/**
 * Lo que se dice de un mensaje «no compatible»: el texto que trae (p. ej. «Mensaje de una sola vez:
 * ábrelo en tu teléfono») si no está vacío; si no, el aviso genérico.
 */
export function unsupportedLabel(m: { text?: string | null }): string {
  const t = (m.text ?? "").replace(/\s+/g, " ").trim();
  return t || UNSUPPORTED_LABEL;
}

/** Texto de la lista cuando el chat no tiene vista previa. */
export const NO_PREVIEW_LABEL = "Sin mensajes cargados";

/**
 * Qué mostrar en la fila de un chat sin vista previa: el teléfono (si es un directo y el título no lo
 * repite) o «Sin mensajes cargados». `placeholder` avisa de que no es un mensaje real.
 */
export function emptyPreview(chat: Pick<Chat, "kind" | "title" | "peerNativeId" | "nativeId">): {
  text: string;
  placeholder: boolean;
} {
  if ((chat.kind as string) === "direct") {
    const phone = phoneFromNative(chat.peerNativeId || chat.nativeId);
    if (phone && digitsOf(chat.title) !== digitsOf(phone)) return { text: phone, placeholder: false };
  }
  return { text: NO_PREVIEW_LABEL, placeholder: true };
}

/** Teléfono legible a partir del id nativo del par (p. ej. `593999…@s.whatsapp.net`). */
export function phoneFromNative(nativeId: string): string {
  const base = (nativeId ?? "").split("@")[0].split(":")[0];
  return /^\d{7,15}$/.test(base) ? `+${base}` : "";
}

/** Solo los dígitos de un teléfono. */
export function digitsOf(s: string): string {
  return (s ?? "").replace(/\D/g, "");
}

export function osmLink(lat: number, lng: number): string {
  return `https://www.openstreetmap.org/?mlat=${lat}&mlon=${lng}#map=16/${lat}/${lng}`;
}

export function formatCoords(lat: number, lng: number): string {
  return `${lat.toFixed(5)}, ${lng.toFixed(5)}`;
}

/** Quita el sufijo de dispositivo de un id nativo (`593…:12@s.whatsapp.net` → `593…@s.whatsapp.net`). */
export function normId(id: string): string {
  return (id ?? "").replace(/:\d+@/, "@");
}

export function sameId(a: string, b: string): boolean {
  return !!a && !!b && normId(a) === normId(b);
}

/** Texto corto de un mensaje para citas y barras de respuesta: el texto o una etiqueta por tipo. */
export function messagePreview(m: Pick<Message, "kind" | "text" | "media" | "poll" | "location" | "contacts" | "deletedAt">): string {
  if (m.deletedAt > 0) return "Mensaje eliminado";
  const text = (m.text ?? "").replace(/\s+/g, " ").trim();
  switch (m.kind as string) {
    case "image":
      return text ? `📷 ${text}` : "📷 Foto";
    case "video":
      return text ? `🎥 ${text}` : "🎥 Vídeo";
    case "voice":
      return "🎤 Nota de voz";
    case "audio":
      return "🎧 Audio";
    case "document":
      return `📄 ${m.media?.fileName || "Documento"}`;
    case "sticker":
      return "Sticker";
    case "location":
      return "📍 Ubicación";
    case "contact":
      return `👤 ${m.contacts?.[0]?.name || "Contacto"}`;
    case "poll":
      return `📊 ${m.poll?.question ?? "Encuesta"}`;
    case "unsupported":
      return unsupportedLabel(m);
    default:
      return text;
  }
}

const HOUR_MS = 3_600_000;

/** Fin del silencio: 0 = activar sonido, -1 = siempre, si no ms de epoch. */
export function muteUntil(kind: "8h" | "week" | "always" | "off", now = Date.now()): number {
  switch (kind) {
    case "8h":
      return now + 8 * HOUR_MS;
    case "week":
      return now + 7 * 24 * HOUR_MS;
    case "always":
      return -1;
    default:
      return 0;
  }
}
