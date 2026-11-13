// Lógica pura del diálogo «Programar» del chat: qué mensajes se programan, validación y el `Scheduled` final.
import type { Outgoing, Scheduled } from "../../api/core/model/models.js";
import {
  buildRecurrence,
  parseLocalInput,
  validateRecurrenceForm,
  whenError,
  type RecurrenceForm,
} from "@/lib/recurrence";
import { outgoingForAttachment, outgoingText, type Attachment } from "./composerReducer";

export interface ScheduleTarget {
  accountId: string;
  chatNativeId: string;
  chatTitle: string;
}

/**
 * Los mensajes que se programan desde el compositor: el texto (si hay) y cada adjunto, uno por uno
 * (un programado lleva un solo mensaje). Salen en este orden.
 */
export function outgoingsFor(text: string, files: Attachment[]): Outgoing[] {
  const out: Outgoing[] = [];
  if (text.trim()) out.push(outgoingText(text, null));
  for (const f of files) out.push(outgoingForAttachment(f, null));
  return out;
}

export interface ScheduleDraft {
  text: string;
  /** Cuántos adjuntos nuevos hay (al editar, `keepsFile` indica que el programado ya tiene uno). */
  fileCount: number;
  keepsFile?: boolean;
  /** Valor de `datetime-local`. */
  when: string;
  recurrence: RecurrenceForm;
  maxTextLen?: number;
}

/** Qué falta o sobra (`null` = se puede programar). */
export function validateScheduleDraft(d: ScheduleDraft, now: number = Date.now()): string | null {
  if (!d.text.trim() && d.fileCount === 0 && !d.keepsFile) return "Escribe un mensaje o adjunta un archivo";
  if (d.maxTextLen && d.maxTextLen > 0 && d.text.length > d.maxTextLen) return "El texto es demasiado largo";
  const first = parseLocalInput(d.when);
  const w = whenError(first, now);
  if (w) return w;
  return validateRecurrenceForm(d.recurrence, first);
}

/** El `Scheduled` que se guarda (nuevo, o el editado conservando id, serie y creación). */
export function buildChatScheduled(
  target: ScheduleTarget,
  message: Outgoing,
  when: string,
  recurrence: RecurrenceForm,
  base?: Scheduled | null,
): Scheduled {
  return {
    id: base?.id ?? "",
    accountId: target.accountId,
    kind: "message",
    chatNativeId: target.chatNativeId,
    chatTitle: target.chatTitle,
    message,
    sendAt: parseLocalInput(when) ?? 0,
    state: "pending",
    sentAt: 0,
    error: "",
    createdAt: base?.createdAt ?? 0,
    recurrence: buildRecurrence(recurrence, base?.recurrence),
    seriesId: base?.seriesId ?? "",
  };
}

/** Aviso al terminar: «Mensaje programado», «3 mensajes programados» o «Cambios guardados». */
export function savedToast(count: number, editing: boolean): string {
  if (editing) return "Cambios guardados";
  return count === 1 ? "Mensaje programado" : `${count} mensajes programados`;
}

/** Pendientes (y fallidos) de un chat, para la insignia junto al botón. */
export function scheduleCounts(items: Array<Pick<Scheduled, "state">>): { pending: number; failed: number } {
  let pending = 0;
  let failed = 0;
  for (const s of items) {
    if (s.state === "pending") pending++;
    else if (s.state === "failed") failed++;
  }
  return { pending, failed };
}
