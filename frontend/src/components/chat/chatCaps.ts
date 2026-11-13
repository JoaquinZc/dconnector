// Capacidades y opciones del menú de un mensaje. Lógica pura (sin React ni stores).
// `CapsLike` es el subconjunto de `capability.Set` (JSON de 03-DOMINIO) que usa esta pantalla;
// el `Set` completo de `@/api/types` es asignable a él.
import type { Message } from "../../api/core/model/models.js";

export interface CapsLike {
  sendText?: boolean;
  sendImage?: boolean;
  sendVideo?: boolean;
  sendAudio?: boolean;
  sendVoice?: boolean;
  sendDocument?: boolean;
  sendSticker?: boolean;
  sendLocation?: boolean;
  sendContact?: boolean;
  sendPoll?: boolean;
  votePoll?: boolean;
  reply?: boolean;
  forward?: boolean;
  react?: boolean;
  edit?: boolean;
  /** milisegundos; 0 = sin límite */
  editWindowMs?: number;
  deleteForEveryone?: boolean;
  /** milisegundos; 0 = sin límite */
  deleteWindowMs?: number;
  typing?: boolean;
  readReceipts?: boolean;
  maxFileSize?: number;
  maxImageSize?: number;
  maxVideoSize?: number;
}

export interface MenuOptions {
  reply: boolean;
  react: boolean;
  forward: boolean;
  copy: boolean;
  edit: boolean;
  deleteForMe: boolean;
  deleteForAll: boolean;
  info: boolean;
}

/** ¿Cabe todavía dentro de la ventana de tiempo? `windowMs` 0 o ausente = sin límite. */
export function withinWindow(timestamp: number, windowMs: number | undefined, now: number): boolean {
  if (!windowMs || windowMs <= 0) return true;
  return now - timestamp <= windowMs;
}

const NO_ACTION_KINDS = new Set(["system", "unsupported"]);

/** Un mensaje todavía no confirmado por el servidor no admite acciones remotas. */
function isSettled(m: Pick<Message, "delivery">): boolean {
  const d = m.delivery as string;
  return d !== "pending" && d !== "failed";
}

export function canEditMessage(m: Message, caps: CapsLike | null | undefined, now = Date.now()): boolean {
  if (!caps?.edit || !m.fromMe || m.deletedAt > 0 || !isSettled(m)) return false;
  if ((m.kind as string) !== "text") return false;
  return withinWindow(m.timestamp, caps.editWindowMs, now);
}

export function canDeleteMessageForAll(m: Message, caps: CapsLike | null | undefined, now = Date.now()): boolean {
  if (!caps?.deleteForEveryone || !m.fromMe || m.deletedAt > 0 || !isSettled(m)) return false;
  if (NO_ACTION_KINDS.has(m.kind as string)) return false;
  return withinWindow(m.timestamp, caps.deleteWindowMs, now);
}

/** Qué entradas muestra el menú de un mensaje según capacidades, tipo, estado y ventanas de tiempo. */
export function messageMenuOptions(m: Message, caps: CapsLike | null | undefined, now = Date.now()): MenuOptions {
  const kind = m.kind as string;
  const deleted = m.deletedAt > 0;
  const none = NO_ACTION_KINDS.has(kind);
  const settled = isSettled(m);
  const live = !deleted && !none && settled;
  return {
    reply: live && !!caps?.reply,
    react: live && !!caps?.react,
    forward: live && !!caps?.forward && kind !== "poll",
    copy: !deleted && !none && (m.text ?? "").trim() !== "",
    edit: canEditMessage(m, caps, now),
    deleteForMe: true,
    deleteForAll: canDeleteMessageForAll(m, caps, now),
    info: !none,
  };
}

/** ¿El menú tendría algo que mostrar además de «Eliminar para mí»? */
export function hasAnyOption(o: MenuOptions): boolean {
  return o.reply || o.react || o.forward || o.copy || o.edit || o.deleteForAll || o.info;
}

/** Qué tipos de archivo admite adjuntar el compositor. */
export interface AttachOptions {
  media: boolean;
  document: boolean;
  sticker: boolean;
  location: boolean;
  contact: boolean;
  poll: boolean;
}

export function attachOptions(caps: CapsLike | null | undefined): AttachOptions {
  return {
    media: !!(caps?.sendImage || caps?.sendVideo),
    document: !!caps?.sendDocument,
    sticker: !!caps?.sendSticker,
    location: !!caps?.sendLocation,
    contact: !!caps?.sendContact,
    poll: !!caps?.sendPoll,
  };
}

/** ¿Se puede enviar algún tipo de contenido? Si no, el chat se trata como de solo lectura. */
export function canSendAnything(caps: CapsLike | null | undefined): boolean {
  if (!caps) return true; // sin datos todavía: no bloquear
  return !!(
    caps.sendText || caps.sendImage || caps.sendVideo || caps.sendAudio || caps.sendVoice ||
    caps.sendDocument || caps.sendSticker || caps.sendLocation || caps.sendContact || caps.sendPoll
  );
}
