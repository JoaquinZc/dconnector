// Lógica pura de Programados: filtros, estados, acciones por fila, formulario y conversión de fecha local.
import { ContentKind } from "@/api/types";
import type { CapSet, Outgoing, Scheduled } from "@/api/types";
import { canSendKind } from "@/lib/caps";
import { formatFileSize, truncate } from "@/lib/format";
import { defaultScheduleMs, parseLocalInput, scheduleError, toLocalInput } from "@/components/status/statusForm";
import {
  buildRecurrence,
  emptyRecurrenceForm,
  recurrenceFormFromRule,
  validateRecurrenceForm,
  type RecurrenceForm,
} from "@/lib/recurrence";

// Fecha y hora local ↔ milisegundos (un `<input type="datetime-local">` no lleva zona).
export { defaultScheduleMs, parseLocalInput, scheduleError, toLocalInput };

export type Tone = "ok" | "warn" | "danger" | "muted" | "info";

// ---------------------------------------------------------------- estados y filtros

export type ScheduledFilter = "all" | "pending" | "sent" | "failed" | "canceled";

export const FILTERS: Array<{ id: ScheduledFilter; label: string }> = [
  { id: "all", label: "Todos" },
  { id: "pending", label: "Pendientes" },
  { id: "sent", label: "Enviados" },
  { id: "failed", label: "Fallidos" },
  { id: "canceled", label: "Cancelados" },
];

const STATES: Record<string, { label: string; tone: Tone }> = {
  pending: { label: "Pendiente", tone: "info" },
  sent: { label: "Enviado", tone: "ok" },
  failed: { label: "Fallido", tone: "danger" },
  canceled: { label: "Cancelado", tone: "muted" },
};

export function scheduledState(state: string | null | undefined): { label: string; tone: Tone } {
  return STATES[String(state)] ?? { label: "Desconocido", tone: "muted" };
}

export function filterScheduled(list: Scheduled[], filter: ScheduledFilter): Scheduled[] {
  return filter === "all" ? list : list.filter((s) => s.state === filter);
}

/** Cuántos hay por filtro (`all` = todos). */
export function countByFilter(list: Scheduled[]): Record<ScheduledFilter, number> {
  const out: Record<ScheduledFilter, number> = { all: list.length, pending: 0, sent: 0, failed: 0, canceled: 0 };
  for (const s of list) if (s.state in out) out[s.state as ScheduledFilter]++;
  return out;
}

/** Pendientes primero (la más próxima arriba); el resto, lo más reciente arriba. */
export function sortScheduled(list: Scheduled[]): Scheduled[] {
  return list.slice().sort((a, b) => {
    const pa = a.state === "pending" ? 0 : 1;
    const pb = b.state === "pending" ? 0 : 1;
    if (pa !== pb) return pa - pb;
    return pa === 0 ? a.sendAt - b.sendAt : b.sendAt - a.sendAt;
  });
}

/** Qué botones valen en cada fila. Los de tipo estado no se editan (se crean desde Estados). */
export function rowActions(s: Pick<Scheduled, "state" | "kind"> & Partial<Pick<Scheduled, "recurrence" | "seriesId">>): {
  edit: boolean;
  sendNow: boolean;
  cancel: boolean;
  /** Cancelar toda la serie: solo en la ocurrencia pendiente de una serie. */
  cancelSeries: boolean;
  remove: boolean;
} {
  const open = s.state === "pending" || s.state === "failed";
  return {
    edit: open && s.kind === "message",
    sendNow: open,
    cancel: s.state === "pending",
    cancelSeries: s.state === "pending" && !!s.recurrence && !!s.seriesId,
    remove: true,
  };
}

// ---------------------------------------------------------------- columnas

export function kindLabel(s: Pick<Scheduled, "kind">): string {
  return s.kind === "status" ? "Estado" : "Mensaje";
}

const MEDIA_LABEL: Record<string, string> = {
  image: "Foto",
  video: "Vídeo",
  audio: "Audio",
  voice: "Nota de voz",
  document: "Documento",
  sticker: "Sticker",
  location: "Ubicación",
  contact: "Contacto",
  poll: "Encuesta",
};

/** Texto de la columna «Contenido»: el mensaje, o «Foto · pie». */
export function contentSummary(s: Scheduled): string {
  const body = s.kind === "status" ? s.status : s.message;
  if (!body) return "";
  const kind = String(body.kind);
  const text = (body.text ?? "").trim();
  const media = MEDIA_LABEL[kind];
  if (!media) return text;
  return text ? `${media} · ${text}` : media;
}

/** Texto de la columna «Destino». */
export function destinationLabel(s: Scheduled): string {
  if (s.kind === "status") return "Estados";
  return s.chatTitle || s.chatNativeId || "—";
}

// ---------------------------------------------------------------- formulario

export interface ScheduledForm {
  /** Vacío = nuevo. */
  id: string;
  accountId: string;
  chatNativeId: string;
  chatTitle: string;
  text: string;
  filePath: string;
  fileName: string;
  fileMime: string;
  fileSize: number;
  /** Valor de `datetime-local`. */
  when: string;
  recurrence: RecurrenceForm;
}

export function emptyScheduledForm(now: number = Date.now()): ScheduledForm {
  return {
    id: "",
    accountId: "",
    chatNativeId: "",
    chatTitle: "",
    text: "",
    filePath: "",
    fileName: "",
    fileMime: "",
    fileSize: 0,
    when: toLocalInput(defaultScheduleMs(now)),
    recurrence: emptyRecurrenceForm(),
  };
}

export function formFromScheduled(s: Scheduled): ScheduledForm {
  const m = s.message;
  return {
    id: s.id,
    accountId: String(s.accountId),
    chatNativeId: s.chatNativeId,
    chatTitle: s.chatTitle,
    text: m?.text ?? "",
    filePath: m?.filePath ?? "",
    fileName: m?.fileName ?? "",
    fileMime: m?.mime ?? "",
    fileSize: 0,
    when: toLocalInput(s.sendAt),
    // Un fallido se corrige suelto: no hereda la repetición de su serie.
    recurrence: s.state === "failed" ? emptyRecurrenceForm() : recurrenceFormFromRule(s.recurrence, s.sendAt),
  };
}

const IMAGE_EXT = /\.(jpe?g|png|webp|gif|bmp)$/i;
const VIDEO_EXT = /\.(mp4|mov|mkv|avi|3gp)$/i;
const AUDIO_EXT = /\.(mp3|ogg|oga|opus|m4a|wav|aac)$/i;

/** Tipo de contenido que se envía para un archivo (por su mime o, si falta, por su extensión). */
export function kindForFile(file: { name?: string; mime?: string } | null | undefined): ContentKind {
  if (!file) return ContentKind.KindText;
  const mime = (file.mime ?? "").toLowerCase();
  const name = file.name ?? "";
  if (mime.startsWith("image/") || (!mime && IMAGE_EXT.test(name))) return ContentKind.KindImage;
  if (mime.startsWith("video/") || (!mime && VIDEO_EXT.test(name))) return ContentKind.KindVideo;
  if (mime.startsWith("audio/") || (!mime && AUDIO_EXT.test(name))) return ContentKind.KindAudio;
  return ContentKind.KindDocument;
}

/** El mensaje a enviar: texto, o archivo con el texto de pie. */
export function outgoingFromForm(f: ScheduledForm): Outgoing {
  if (!f.filePath) return { kind: ContentKind.KindText, text: f.text };
  return {
    kind: kindForFile({ name: f.fileName, mime: f.fileMime }),
    text: f.text,
    filePath: f.filePath,
    fileName: f.fileName,
    mime: f.fileMime,
  };
}

const KIND_NAME: Record<string, string> = {
  text: "texto",
  image: "fotos",
  video: "vídeos",
  audio: "audios",
  document: "documentos",
};

/** Qué falta o sobra (`null` = se puede guardar). `caps` son las de la cuenta; sin ellas no se comprueba. */
export function validateScheduledForm(f: ScheduledForm, caps: CapSet | null | undefined, now: number = Date.now()): string | null {
  if (!f.accountId) return "Elige una cuenta";
  if (!f.chatNativeId) return "Elige un chat";
  if (!f.text.trim() && !f.filePath) return "Escribe un mensaje o adjunta un archivo";
  const out = outgoingFromForm(f);
  if (caps) {
    if (!canSendKind(caps, String(out.kind))) return `Esta cuenta no envía ${KIND_NAME[String(out.kind)] ?? "este tipo de archivo"}`;
    if (caps.maxTextLen > 0 && f.text.length > caps.maxTextLen) return "El texto es demasiado largo";
    if (f.filePath && f.fileSize > 0) {
      const max =
        out.kind === ContentKind.KindImage ? caps.maxImageSize : out.kind === ContentKind.KindVideo ? caps.maxVideoSize : caps.maxFileSize;
      if (max > 0 && f.fileSize > max) return `El archivo pesa más de ${formatFileSize(max)}`;
    }
  }
  const when = scheduleError(f.when, now);
  if (when) return when;
  return validateRecurrenceForm(f.recurrence, parseLocalInput(f.when));
}

/** El `Scheduled` que se guarda. Al editar, conserva `createdAt`; siempre vuelve a `pending`. */
export function buildScheduled(f: ScheduledForm, base?: Scheduled | null): Scheduled {
  return {
    id: f.id,
    accountId: f.accountId,
    kind: "message",
    chatNativeId: f.chatNativeId,
    chatTitle: f.chatTitle,
    message: outgoingFromForm(f),
    sendAt: parseLocalInput(f.when) ?? 0,
    state: "pending",
    sentAt: 0,
    error: "",
    createdAt: base?.createdAt ?? 0,
    recurrence: buildRecurrence(f.recurrence, base?.recurrence),
    seriesId: base?.seriesId ?? "",
  };
}

/** Texto corto de un error o motivo para la tabla. */
export function shortError(s: Scheduled): string {
  return s.error ? truncate(s.error, 80) : "";
}
