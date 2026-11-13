// Qué se puede hacer según las capacidades del chat (11 §6.2 · MessageMenu y Composer).
import type { CapSet, Message } from "@/api/types";

type MaybeCaps = CapSet | null | undefined;

/** ¿Se puede enviar este tipo de contenido? */
export function canSendKind(caps: MaybeCaps, kind: string): boolean {
  if (!caps) return false;
  switch (kind) {
    case "text":
      return !!caps.sendText;
    case "image":
      return !!caps.sendImage;
    case "video":
      return !!caps.sendVideo;
    case "audio":
      return !!caps.sendAudio;
    case "voice":
      return !!caps.sendVoice;
    case "document":
      return !!caps.sendDocument;
    case "sticker":
      return !!caps.sendSticker;
    case "location":
      return !!caps.sendLocation;
    case "contact":
      return !!caps.sendContact;
    case "poll":
      return !!caps.sendPoll;
    default:
      return false;
  }
}

export function canReply(caps: MaybeCaps): boolean {
  return !!caps?.reply;
}

export function canReact(caps: MaybeCaps): boolean {
  return !!caps?.react;
}

export function canForward(caps: MaybeCaps): boolean {
  return !!caps?.forward;
}

/** Dentro de la ventana (0 = sin límite) contada desde `timestamp`. */
function withinWindow(timestamp: number, windowMs: number, now: number): boolean {
  if (!windowMs || windowMs <= 0) return true;
  return now - timestamp <= windowMs;
}

/** Editar: capacidad, mensaje mío, no borrado y dentro de `editWindowMs`. */
export function canEdit(msg: Pick<Message, "fromMe" | "deletedAt" | "timestamp">, caps: MaybeCaps, now: number = Date.now()): boolean {
  if (!caps?.edit || !msg.fromMe || msg.deletedAt) return false;
  return withinWindow(msg.timestamp, caps.editWindowMs, now);
}

/** Eliminar para todos: capacidad, mensaje mío, no borrado y dentro de `deleteWindowMs`. */
export function canDeleteForEveryone(
  msg: Pick<Message, "fromMe" | "deletedAt" | "timestamp">,
  caps: MaybeCaps,
  now: number = Date.now(),
): boolean {
  if (!caps?.deleteForEveryone || !msg.fromMe || msg.deletedAt) return false;
  return withinWindow(msg.timestamp, caps.deleteWindowMs, now);
}
