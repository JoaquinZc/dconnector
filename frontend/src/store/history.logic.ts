// Lógica pura del historial completo (HistoryStatus, evento `history:sync`). Sin React ni enlaces.
import type { HistoryStatus } from "@/api/types";

export { formatCount } from "@/lib/format";

export type HistoryMap = Record<string, HistoryStatus>;

/** Estado de una cuenta sin dato: en reposo y sin nada guardado. */
export function emptyStatus(accountId: string): HistoryStatus {
  return {
    accountId,
    running: false,
    phase: "idle",
    chatsTotal: 0,
    chatsDone: 0,
    groupsTotal: 0,
    groupsDone: 0,
    visited: 0,
    added: 0,
    current: "",
    stoppedReason: "",
    imported: 0,
    messages: 0,
    oldestAt: 0,
    auto: false,
    nextAutoAt: 0,
  } as HistoryStatus;
}

/** Lista del backend (puede llegar `null`) → un estado por cuenta. */
export function indexStatuses(list: HistoryStatus[] | null | undefined): HistoryMap {
  const out: HistoryMap = {};
  for (const s of list ?? []) if (s?.accountId) out[String(s.accountId)] = s;
  return out;
}

/** Aplica un aviso `history:sync`; un aviso sin cuenta se ignora. */
export function applySync(map: HistoryMap, st: HistoryStatus | null | undefined): HistoryMap {
  if (!st?.accountId) return map;
  return { ...map, [String(st.accountId)]: st };
}

/** Avance de la tanda, 0…100 (chats completos o sin posibilidad de pedir más sobre el total). */
export function progressPercent(st: Pick<HistoryStatus, "chatsTotal" | "chatsDone">): number {
  if (!st.chatsTotal || st.chatsTotal <= 0) return 0;
  return Math.max(0, Math.min(100, Math.floor((st.chatsDone / st.chatsTotal) * 100)));
}

/** Frase del paso en curso. */
export function phaseText(st: Pick<HistoryStatus, "running" | "phase">): string {
  if (!st.running) return "Detenido";
  switch (st.phase) {
    case "queued":
      return "En cola: espera a que acabe otra cuenta del mismo canal";
    case "waiting":
      return "Esperando para no saturar el canal";
    case "requesting":
      return "Pidiendo mensajes anteriores";
    default:
      return "En marcha";
  }
}

/** ¿Hay una tanda activa (incluida la que espera turno)? */
export function isActive(st: Pick<HistoryStatus, "running"> | undefined): boolean {
  return !!st?.running;
}

/** ¿Se recorrieron todos los chats que se podían? */
export function isComplete(st: Pick<HistoryStatus, "chatsTotal" | "chatsDone" | "running">): boolean {
  return !st.running && st.chatsTotal > 0 && st.chatsDone >= st.chatsTotal;
}

/** ¿Hay avance guardado que se pueda reiniciar? */
export function hasProgress(st: Pick<HistoryStatus, "chatsDone" | "visited" | "imported">): boolean {
  return st.chatsDone > 0 || st.visited > 0 || st.imported > 0;
}

// ---------------------------------------------------------------- «Cargar mensajes anteriores»

/** Petición de «cargar más» de un chat, pendiente de que el servicio termine su tanda. */
export interface PendingLoad {
  chatNativeId: string;
  /** Título del chat: `HistoryStatus.current` lo trae mientras se le piden mensajes. */
  title: string;
  /** ms en que se hizo la petición. */
  at: number;
  /** ¿Ya se vio la tanda trabajando en este chat? */
  seen: boolean;
}

export type PendingMap = Record<string, PendingLoad>;

/** Tiempo máximo que el botón dice «Cargando…» sin noticias del servicio. */
export const PENDING_TTL_MS = 120_000;
/** Un aviso «en reposo» más viejo que esto, anterior a la petición, no cuenta. */
const IDLE_GRACE_MS = 1_500;

/**
 * Cómo queda la petición pendiente de una cuenta tras un aviso: `undefined` = terminó.
 * Termina cuando la tanda queda en reposo (tras haberla visto trabajar, o pasado el margen), o cuando
 * ya la vimos pedir ese chat y pasó a otro.
 */
export function settlePending(p: PendingLoad, st: HistoryStatus, now: number): PendingLoad | undefined {
  if (now - p.at > PENDING_TTL_MS) return undefined;
  const onIt = st.running && !!p.title && st.current === p.title;
  const seen = p.seen || onIt;
  if (!st.running) return seen || now - p.at >= IDLE_GRACE_MS ? undefined : p;
  if (seen && !onIt) return undefined; // ya pasó a otro chat
  return seen === p.seen ? p : { ...p, seen };
}

/** ¿Se está cargando este chat? */
export function isChatLoading(pending: PendingMap, accountId: string, chatNativeId: string): boolean {
  return pending[accountId]?.chatNativeId === chatNativeId;
}
