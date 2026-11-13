// Funciones puras de la conversación abierta: mensajes de antiguo a reciente, recibos y escritura.
import type { Message, ReceiptEvent, TypingEvent } from "@/api/types";

export function compareMessages(a: Message, b: Message): number {
  if (a.timestamp !== b.timestamp) return a.timestamp - b.timestamp;
  return a.key < b.key ? -1 : a.key > b.key ? 1 : 0;
}

const DELIVERY_RANK: Record<string, number> = {
  "": 0,
  failed: 0,
  pending: 1,
  sent: 2,
  delivered: 3,
  read: 4,
  played: 5,
};

/** El estado de entrega solo avanza (un recibo atrasado no baja ✓✓ azul a ✓). */
export function betterDelivery(current: string, incoming: string): string {
  return (DELIVERY_RANK[incoming] ?? 0) > (DELIVERY_RANK[current] ?? 0) ? incoming : current;
}

/** Inserta o reemplaza (por clave) y deja la lista ordenada. Solo los de `chatKey`. */
export function upsertMessages(messages: Message[], incoming: Message[], chatKey: string): Message[] {
  const mine = incoming.filter((m) => m.chatKey === chatKey);
  if (!mine.length) return messages;
  const byKey = new Map<string, Message>();
  for (const m of messages) byKey.set(m.key, m);
  for (const m of mine) {
    const prev = byKey.get(m.key);
    // Un upsert tardío no debe bajar la entrega que ya tenemos.
    byKey.set(m.key, prev ? { ...m, delivery: betterDelivery(prev.delivery, m.delivery) as Message["delivery"] } : m);
  }
  return Array.from(byKey.values()).sort(compareMessages);
}

/** `message:update`: reemplaza el mensaje completo si está en la lista (si no, se ignora). */
export function updateMessage(messages: Message[], updated: Message, chatKey: string): Message[] {
  if (updated.chatKey !== chatKey) return messages;
  const i = messages.findIndex((m) => m.key === updated.key);
  if (i < 0) return messages;
  const next = messages.slice();
  next[i] = updated;
  return next;
}

/** `receipt`: sube la entrega de los mensajes indicados. */
export function applyReceipt(messages: Message[], ev: ReceiptEvent, chatKey: string): Message[] {
  if (ev.chatKey !== chatKey || !ev.messageKeys?.length) return messages;
  const keys = new Set(ev.messageKeys);
  let changed = false;
  const next = messages.map((m) => {
    if (!keys.has(m.key)) return m;
    const d = betterDelivery(m.delivery, String(ev.delivery));
    if (d === m.delivery) return m;
    changed = true;
    return { ...m, delivery: d as Message["delivery"] };
  });
  return changed ? next : messages;
}

export type TypingMap = Record<string, { recording: boolean }>;

/** `typing`: quién está escribiendo o grabando (por persona). */
export function applyTyping(map: TypingMap, ev: TypingEvent): TypingMap {
  if (!ev.typing && !ev.recording) {
    if (!(ev.peerId in map)) return map;
    const next = { ...map };
    delete next[ev.peerId];
    return next;
  }
  const cur = map[ev.peerId];
  if (cur && cur.recording === ev.recording) return map;
  return { ...map, [ev.peerId]: { recording: ev.recording } };
}

/** Texto del «escribiendo…» de la cabecera ("" si nadie). */
export function typingLabel(map: TypingMap, isGroup: boolean, nameOf?: (peerId: string) => string): string {
  const ids = Object.keys(map);
  if (!ids.length) return "";
  const recording = ids.every((id) => map[id].recording);
  const verb = recording ? "grabando audio…" : "escribiendo…";
  if (!isGroup) return verb;
  const first = nameOf ? nameOf(ids[0]) : "";
  if (ids.length === 1) return first ? `${first} está ${recording ? "grabando audio…" : "escribiendo…"}` : verb;
  return `${ids.length} personas ${recording ? "grabando…" : "escribiendo…"}`;
}
