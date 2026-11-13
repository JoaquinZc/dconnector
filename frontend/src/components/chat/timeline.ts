// Agrupación de mensajes por día y por emisor para la conversación. Lógica pura.
import type { Message } from "../../api/core/model/models.js";
import { dayKey, dayLabel } from "./chatFormat";

export type TimelineItem =
  | { type: "day"; key: string; label: string; ts: number }
  | {
      type: "msg";
      key: string;
      message: Message;
      /** muestra el nombre del emisor (grupos, primer mensaje de la racha de esa persona) */
      showSender: boolean;
      /** primer mensaje de una racha del mismo emisor (más aire arriba) */
      first: boolean;
    };

/**
 * `messages` puede venir sin ordenar ni sin duplicados: se ordena por fecha (y clave) y se
 * deduplica por clave. Devuelve separadores de día y mensajes de más antiguo a más reciente.
 */
export function buildTimeline(messages: Message[], isGroup: boolean, now = Date.now()): TimelineItem[] {
  const seen = new Set<string>();
  const sorted = messages
    .filter((m) => {
      if (seen.has(m.key)) return false;
      seen.add(m.key);
      return true;
    })
    .sort((a, b) => a.timestamp - b.timestamp || (a.key < b.key ? -1 : 1));

  const items: TimelineItem[] = [];
  let prev: Message | null = null;
  let prevDay = "";
  for (const m of sorted) {
    const dk = dayKey(m.timestamp);
    const newDay = dk !== prevDay;
    if (newDay) {
      items.push({ type: "day", key: `day:${dk}`, label: dayLabel(m.timestamp, now), ts: m.timestamp });
      prevDay = dk;
    }
    const system = (m.kind as string) === "system";
    const sameRun =
      !newDay &&
      prev !== null &&
      (prev.kind as string) !== "system" &&
      !system &&
      prev.fromMe === m.fromMe &&
      prev.senderNativeId === m.senderNativeId;
    items.push({
      type: "msg",
      key: m.key,
      message: m,
      showSender: isGroup && !m.fromMe && !system && !sameRun,
      first: !sameRun,
    });
    prev = m;
  }
  return items;
}

/** Aplica o reemplaza un mensaje en una lista (por clave), conservando el orden por fecha. */
export function upsertMessage(list: Message[], m: Message): Message[] {
  const i = list.findIndex((x) => x.key === m.key);
  if (i >= 0) {
    const copy = list.slice();
    copy[i] = m;
    return copy;
  }
  return [...list, m].sort((a, b) => a.timestamp - b.timestamp);
}

/** Índice (en la lista de ítems) del mensaje con ese id nativo, o -1. */
export function indexOfNative(items: TimelineItem[], nativeId: string): number {
  return items.findIndex((it) => it.type === "msg" && it.message.nativeId === nativeId);
}

/** Índices de ítems cuyos mensajes contienen el texto buscado (sin distinguir mayúsculas ni tildes). */
export function searchIndexes(items: TimelineItem[], query: string): number[] {
  const q = fold(query);
  if (!q) return [];
  const out: number[] = [];
  items.forEach((it, i) => {
    if (it.type !== "msg") return;
    const m = it.message;
    const hay = fold(`${m.text ?? ""} ${m.media?.fileName ?? ""} ${m.poll?.question ?? ""}`);
    if (hay.includes(q)) out.push(i);
  });
  return out;
}

export function fold(s: string): string {
  return (s ?? "").normalize("NFD").replace(/[̀-ͯ]/g, "").toLowerCase();
}
