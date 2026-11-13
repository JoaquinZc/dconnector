// Lectura tolerante de las estadísticas de un chat (`StatsService.Chat`). El tipo de Go
// (`ContactConversationStats`) lo rellena otro agente; aquí solo se leen los campos conocidos.
export interface ChatStatsView {
  sent: number;
  received: number;
  total: number;
  first: number | null; // ms
  last: number | null; // ms
  activeDays: number | null;
}

const num = (v: unknown): number | null => (typeof v === "number" && Number.isFinite(v) ? v : null);

/** Acepta segundos o milisegundos y devuelve milisegundos. */
function toMs(v: unknown): number | null {
  const n = num(v);
  if (n === null || n <= 0) return null;
  return n < 1e11 ? n * 1000 : n;
}

export function extractChatStats(raw: unknown): ChatStatsView | null {
  if (!raw || typeof raw !== "object") return null;
  const r = raw as Record<string, unknown>;
  const summary = (r.summary ?? r) as Record<string, unknown>;
  const sent = num(summary?.sent);
  const received = num(summary?.received);
  if (sent === null && received === null) return null;
  const s = sent ?? 0;
  const rc = received ?? 0;
  return {
    sent: s,
    received: rc,
    total: s + rc,
    first: toMs(r.first),
    last: toMs(r.last),
    activeDays: num(r.activeDays),
  };
}
