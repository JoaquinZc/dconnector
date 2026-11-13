// Formulario de «Publicar estado»: estado, validación y conversión a `OutgoingStatus`.
import { ContentKind } from "@/api/types";
import type { CapSet, OutgoingStatus, PickedFile } from "@/api/types";
import { formatFileSize } from "@/lib/format";
import { DEFAULT_STATUS_COLOR, STATUS_COLORS, STATUS_FONTS } from "./statusModel";

export type PostMode = "text" | "media";
export type AudienceMode = "contacts" | "allow" | "deny";

/** Tope de texto cuando la cuenta no indica otro. */
export const DEFAULT_MAX_TEXT = 700;

export interface StatusForm {
  mode: PostMode;
  /** Texto del estado de texto, o pie de la foto/vídeo. */
  text: string;
  color: string;
  font: number;
  file: PickedFile | null;
  audienceMode: AudienceMode;
  listIds: string[];
  peerIds: string[];
}

export function emptyForm(): StatusForm {
  return {
    mode: "text",
    text: "",
    color: DEFAULT_STATUS_COLOR,
    font: 0,
    file: null,
    audienceMode: "contacts",
    listIds: [],
    peerIds: [],
  };
}

/** Lo que se guarda como borrador: sin el archivo (la ruta puede dejar de existir). */
export type StatusDraft = Omit<StatusForm, "file">;

export function draftOf(f: StatusForm): StatusDraft {
  const { file: _file, ...rest } = f;
  void _file;
  return rest;
}

/** Restaura un borrador, ignorando lo que no sea válido. */
export function formFromDraft(d: Partial<StatusDraft> | null | undefined): StatusForm {
  const base = emptyForm();
  if (!d || typeof d !== "object") return base;
  return {
    ...base,
    mode: d.mode === "media" ? "media" : "text",
    text: typeof d.text === "string" ? d.text : "",
    color: STATUS_COLORS.includes(d.color as (typeof STATUS_COLORS)[number]) ? (d.color as string) : base.color,
    font: STATUS_FONTS.some((f) => f.value === d.font) ? (d.font as number) : 0,
    audienceMode: d.audienceMode === "allow" || d.audienceMode === "deny" ? d.audienceMode : "contacts",
    listIds: Array.isArray(d.listIds) ? d.listIds.filter((x) => typeof x === "string") : [],
    peerIds: Array.isArray(d.peerIds) ? d.peerIds.filter((x) => typeof x === "string") : [],
  };
}

/** ¿Hay algo escrito o elegido? (para decidir si guardar el borrador). */
export function isFormBlank(f: StatusForm): boolean {
  return !f.text.trim() && !f.file && f.listIds.length === 0 && f.peerIds.length === 0;
}

/** Foto o vídeo según el tipo MIME. */
export function mediaKindOf(mime: string | null | undefined, name = ""): "image" | "video" {
  const m = (mime ?? "").toLowerCase();
  if (m.startsWith("video/")) return "video";
  if (m.startsWith("image/")) return "image";
  return /\.(mp4|mov|m4v|webm|avi|mkv|3gp)$/i.test(name) ? "video" : "image";
}

export interface FormOptions {
  /** `statusAudience`: se puede elegir audiencia. */
  audienceChoice: boolean;
  caps?: Pick<CapSet, "maxImageSize" | "maxVideoSize" | "maxTextLen"> | null;
}

/** Mensaje de lo que falta o sobra; `null` si se puede publicar. */
export function validateForm(f: StatusForm, opts: FormOptions): string | null {
  const maxText = opts.caps?.maxTextLen && opts.caps.maxTextLen > 0 ? opts.caps.maxTextLen : DEFAULT_MAX_TEXT;
  if (f.mode === "text") {
    if (!f.text.trim()) return "Escribe el texto del estado";
  } else {
    if (!f.file) return "Elige una foto o un vídeo";
    const kind = mediaKindOf(f.file.mime, f.file.name);
    const limit = kind === "video" ? opts.caps?.maxVideoSize : opts.caps?.maxImageSize;
    if (limit && limit > 0 && f.file.size > limit) {
      return `El archivo pesa ${formatFileSize(f.file.size)} y el máximo es ${formatFileSize(limit)}`;
    }
  }
  if (f.text.length > maxText) return `El texto pasa de ${maxText} caracteres`;
  if (opts.audienceChoice && f.audienceMode !== "contacts" && f.listIds.length + f.peerIds.length === 0) {
    return f.audienceMode === "allow" ? "Elige con quién compartirlo" : "Elige a quién excluir";
  }
  return null;
}

/** El `OutgoingStatus` del formulario. Sin elección de audiencia: «Mis contactos» (el teléfono manda). */
export function buildOutgoing(f: StatusForm, opts: FormOptions): OutgoingStatus {
  const audience = opts.audienceChoice
    ? {
        mode: f.audienceMode,
        peerIds: f.audienceMode === "contacts" ? [] : f.peerIds,
        listIds: f.audienceMode === "contacts" ? [] : f.listIds,
      }
    : { mode: "contacts", peerIds: [], listIds: [] };

  if (f.mode === "text") {
    return {
      kind: ContentKind.KindText,
      text: f.text.trim(),
      bgColor: f.color,
      font: f.font,
      audience,
    };
  }
  const kind = mediaKindOf(f.file?.mime, f.file?.name);
  return {
    kind: kind === "video" ? ContentKind.KindVideo : ContentKind.KindImage,
    text: f.text.trim(),
    bgColor: "",
    font: 0,
    filePath: f.file?.path ?? "",
    mime: f.file?.mime ?? "",
    audience,
  };
}

// ---------------------------------------------------------------- programar

const pad = (n: number) => String(n).padStart(2, "0");

/** Valor para `<input type="datetime-local">` (hora local, sin segundos). */
export function toLocalInput(ms: number): string {
  const d = new Date(ms);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** Milisegundos de un valor `datetime-local`, o `null` si no es válido. */
export function parseLocalInput(value: string): number | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/.exec(value ?? "");
  if (!m) return null;
  const [y, mo, d, h, mi] = m.slice(1).map(Number);
  const date = new Date(y, mo - 1, d, h, mi, 0, 0);
  return Number.isNaN(date.getTime()) ? null : date.getTime();
}

/** Mensaje si la fecha no sirve para programar; `null` si vale. */
export function scheduleError(value: string, now: number = Date.now()): string | null {
  const ms = parseLocalInput(value);
  if (ms === null) return "Elige fecha y hora";
  if (ms <= now) return "La fecha ya pasó";
  return null;
}

/** Una hora a partir de `now`, redondeada a los 5 minutos siguientes. */
export function defaultScheduleMs(now: number = Date.now()): number {
  const step = 5 * 60_000;
  return Math.ceil((now + 60 * 60_000) / step) * step;
}
