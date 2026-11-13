// Estado del compositor (texto + adjuntos pendientes) como reductor puro, y utilidades de envío.
import type { MessageRef, Message, Outgoing } from "../../api/core/model/models.js";

export interface Attachment {
  id: string;
  path: string; // ruta local (dentro de <data>/tmp si viene de SaveTemp)
  name: string;
  mime: string;
  size: number;
  kind: "image" | "video" | "audio" | "document" | "sticker";
  caption: string;
}

export interface ComposerState {
  text: string;
  attachments: Attachment[];
  /** adjunto que se está viendo en la vista previa */
  active: number;
  sending: boolean;
}

export type ComposerAction =
  | { type: "text"; text: string }
  | { type: "insert"; at: number; value: string }
  | { type: "addFiles"; files: Attachment[] }
  | { type: "removeFile"; id: string }
  | { type: "caption"; id: string; caption: string }
  | { type: "select"; index: number }
  | { type: "clearFiles" }
  | { type: "sending"; value: boolean }
  | { type: "reset" }
  | { type: "load"; text: string };

export const initialComposer: ComposerState = { text: "", attachments: [], active: 0, sending: false };

export function composerReducer(s: ComposerState, a: ComposerAction): ComposerState {
  switch (a.type) {
    case "text":
      return { ...s, text: a.text };
    case "load":
      return { ...s, text: a.text };
    case "insert": {
      const at = Math.max(0, Math.min(a.at, s.text.length));
      return { ...s, text: s.text.slice(0, at) + a.value + s.text.slice(at) };
    }
    case "addFiles": {
      const known = new Set(s.attachments.map((x) => x.path));
      const fresh = a.files.filter((f) => !known.has(f.path));
      if (fresh.length === 0) return s;
      return { ...s, attachments: [...s.attachments, ...fresh], active: s.attachments.length === 0 ? 0 : s.active };
    }
    case "removeFile": {
      const attachments = s.attachments.filter((x) => x.id !== a.id);
      return { ...s, attachments, active: Math.min(s.active, Math.max(0, attachments.length - 1)) };
    }
    case "caption":
      return { ...s, attachments: s.attachments.map((x) => (x.id === a.id ? { ...x, caption: a.caption } : x)) };
    case "select":
      return { ...s, active: Math.max(0, Math.min(a.index, s.attachments.length - 1)) };
    case "clearFiles":
      return { ...s, attachments: [], active: 0 };
    case "sending":
      return { ...s, sending: a.value };
    case "reset":
      return { ...initialComposer };
  }
}

/** Tipo de contenido según el tipo MIME; `forced` fija «sticker» cuando se eligió desde ese menú. */
export function kindForMime(mime: string, forced?: "sticker"): Attachment["kind"] {
  if (forced) return forced;
  const m = (mime ?? "").toLowerCase();
  if (m.startsWith("image/")) return "image";
  if (m.startsWith("video/")) return "video";
  if (m.startsWith("audio/")) return "audio";
  return "document";
}

export function makeAttachment(
  f: { path: string; name: string; mime: string; size: number },
  forced?: "sticker",
): Attachment {
  return {
    id: `${f.path}:${f.size}`,
    path: f.path,
    name: f.name,
    mime: f.mime,
    size: f.size,
    kind: kindForMime(f.mime, forced),
    caption: "",
  };
}

/** Intervalo mínimo entre avisos de «escribiendo…». */
export const TYPING_INTERVAL_MS = 4000;

export function shouldPingTyping(lastSentAt: number, now: number): boolean {
  return now - lastSentAt >= TYPING_INTERVAL_MS;
}

export function refOf(m: Message): MessageRef {
  return {
    chatNativeId: m.chatNativeId,
    nativeId: m.nativeId,
    senderNativeId: m.senderNativeId,
    fromMe: m.fromMe,
    timestamp: m.timestamp,
  };
}

/** Salida de un adjunto. El pie solo acompaña a fotos, vídeos y documentos. */
export function outgoingForAttachment(a: Attachment, replyTo: Message | null): Outgoing {
  const withCaption = a.kind === "image" || a.kind === "video" || a.kind === "document";
  return {
    kind: a.kind as Outgoing["kind"],
    text: withCaption ? a.caption.trim() : "",
    filePath: a.path,
    fileName: a.name,
    mime: a.mime,
    replyTo: replyTo ? refOf(replyTo) : null,
  };
}

export function outgoingText(text: string, replyTo: Message | null): Outgoing {
  return { kind: "text" as Outgoing["kind"], text: text.trim(), replyTo: replyTo ? refOf(replyTo) : null };
}

/** Supera el tamaño máximo admitido para ese tipo de archivo (0 o ausente = sin dato). */
export function exceedsLimit(
  a: Pick<Attachment, "kind" | "size">,
  caps: { maxFileSize?: number; maxImageSize?: number; maxVideoSize?: number } | null | undefined,
): boolean {
  if (!caps) return false;
  const max = a.kind === "image" ? caps.maxImageSize : a.kind === "video" ? caps.maxVideoSize : caps.maxFileSize;
  return !!max && max > 0 && a.size > max;
}
