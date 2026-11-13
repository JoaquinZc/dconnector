// Lectura tolerante de `stats.StatusStats`. En los enlaces de la oleada 2 es un struct vacío; el
// real (B3, internal/core/stats/status.go) es:
//   { period, current: {posts, views, avgViews, reach, reactions, replies, …}, previous, changes,
//     perDay: [{day, posts, views, …}], perStatus, curve: [{hour, views}], loyalty }
// Aquí se leen esos nombres (y algunos alias) con valores por defecto, para que la pantalla no falle
// si el servicio devuelve menos. Cuando los enlaces se regeneren con los campos reales, se puede
// tipar `readStatusStats(raw: StatusStats)` sin tocar la pantalla.

export interface CurvePoint {
  label: string;
  views: number;
}

/** Variación frente al periodo anterior, en %; `null` = sin base para comparar. */
export interface StatChanges {
  published: number | null;
  views: number | null;
  avgViews: number | null;
  reach: number | null;
  reactions: number | null;
}

export interface StatusStatsView {
  /** Estados publicados en el periodo. */
  published: number;
  /** Vistas medias por estado. */
  avgViews: number;
  /** Alcance: personas distintas que vieron al menos un estado. */
  reach: number;
  totalViews: number;
  reactions: number;
  replies: number;
  /** Vistas por día de publicación. */
  curve: CurvePoint[];
  /** Vistas acumuladas por hora desde la publicación (0..24 h), sumadas. */
  hourly: CurvePoint[];
  changes: StatChanges;
  /** Hay algo que mostrar. */
  hasData: boolean;
}

const NO_CHANGES: StatChanges = { published: null, views: null, avgViews: null, reach: null, reactions: null };

export const EMPTY_STATS: StatusStatsView = {
  published: 0,
  avgViews: 0,
  reach: 0,
  totalViews: 0,
  reactions: 0,
  replies: 0,
  curve: [],
  hourly: [],
  changes: NO_CHANGES,
  hasData: false,
};

type Obj = Record<string, unknown>;

const isObj = (v: unknown): v is Obj => !!v && typeof v === "object" && !Array.isArray(v);

/** Número finito o `fallback`. Acepta cadenas numéricas. */
export function toNum(v: unknown, fallback = 0): number {
  const n = typeof v === "string" && v.trim() !== "" ? Number(v) : v;
  return typeof n === "number" && Number.isFinite(n) ? n : fallback;
}

/** Objetos donde buscar las cifras: `current` (B3) y los sub-objetos habituales; al final la raíz. */
function sources(raw: unknown): Obj[] {
  if (!isObj(raw)) return [];
  const out: Obj[] = [];
  for (const k of ["current", "summary", "kpis", "kpi", "totals", "overview", "stats"]) {
    if (isObj(raw[k])) out.push(raw[k] as Obj);
  }
  out.push(raw);
  return out;
}

function pick(srcs: Obj[], keys: string[]): unknown {
  for (const s of srcs) {
    for (const k of keys) {
      if (k in s && s[k] !== null && s[k] !== undefined) return s[k];
    }
  }
  return undefined;
}

const pickNum = (srcs: Obj[], keys: string[]): number => toNum(pick(srcs, keys));

/** «2026-11-03» → «03/11»; «2026-11» → «11/2026»; otro texto, tal cual. */
export function curveLabel(v: unknown, index: number): string {
  if (typeof v === "number" && Number.isFinite(v)) return String(v);
  if (typeof v !== "string" || !v) return String(index + 1);
  const d = /^(\d{4})-(\d{2})-(\d{2})/.exec(v);
  if (d) return `${d[3]}/${d[2]}`;
  const m = /^(\d{4})-(\d{2})$/.exec(v);
  return m ? `${m[2]}/${m[1]}` : v;
}

const DAY_KEYS = ["perDay", "viewsByDay", "byDay", "daily", "dayCurve", "series"];
const HOUR_KEYS = ["curve", "hourly", "viewsCurve", "byHour"];
const LABEL_KEYS = ["day", "label", "date", "bucket", "key", "x"];
const VIEW_KEYS = ["views", "count", "value", "total", "v", "y"];

function readSeries(srcs: Obj[], keys: string[], label: (p: Obj, i: number) => string): CurvePoint[] {
  for (const s of srcs) {
    for (const k of keys) {
      const v = s[k];
      if (!Array.isArray(v)) continue;
      return v.map((p, i): CurvePoint => {
        if (isObj(p)) return { label: label(p, i), views: toNum(pick([p], VIEW_KEYS)) };
        if (Array.isArray(p)) return { label: curveLabel(p[0], i), views: toNum(p[1]) };
        return { label: String(i + 1), views: toNum(p) };
      });
    }
  }
  return [];
}

const dayLabel = (p: Obj, i: number) => curveLabel(pick([p], LABEL_KEYS), i);
const hourLabel = (p: Obj, i: number) => {
  const h = pick([p], ["hour", "h"]);
  return h !== undefined ? `${toNum(h)} h` : curveLabel(pick([p], LABEL_KEYS), i);
};

/** `changes` llega como fracción (0,25 = +25 %) o `null`. */
function readChanges(raw: Obj): StatChanges {
  const c = isObj(raw.changes) ? raw.changes : {};
  const pct = (...keys: string[]): number | null => {
    const v = pick([c], keys);
    if (v === undefined) return null;
    const n = toNum(v, NaN);
    return Number.isNaN(n) ? null : Math.round(n * 1000) / 10;
  };
  return {
    published: pct("posts", "published"),
    views: pct("views"),
    avgViews: pct("avgViews"),
    reach: pct("reach"),
    reactions: pct("reactions"),
  };
}

/** Datos de `StatusesService.Stats` listos para pintar. Nunca lanza. */
export function readStatusStats(raw: unknown): StatusStatsView {
  if (!isObj(raw)) return { ...EMPTY_STATS, changes: { ...NO_CHANGES } };
  const srcs = sources(raw);

  const published = Math.round(pickNum(srcs, ["posts", "published", "posted", "statusCount"]));
  const totalViews = Math.round(pickNum(srcs, ["views", "totalViews", "viewCount", "viewsTotal"]));
  const reactions = Math.round(pickNum(srcs, ["reactions", "reactionCount", "totalReactions"]));
  const replies = Math.round(pickNum(srcs, ["replies", "replyCount"]));
  const reach = Math.round(pickNum(srcs, ["reach", "people", "reachPeople"]));

  let avgViews = pickNum(srcs, ["avgViews", "averageViews", "viewsAvg", "meanViews"]);
  if (!avgViews && published > 0 && totalViews > 0) avgViews = totalViews / published;

  const curve = readSeries(srcs, DAY_KEYS, dayLabel);
  const hourly = readSeries(srcs, HOUR_KEYS, hourLabel);

  const hasData =
    published > 0 || totalViews > 0 || reactions > 0 || reach > 0 || curve.some((p) => p.views > 0) || hourly.some((p) => p.views > 0);
  return {
    published,
    avgViews: Math.round(avgViews * 10) / 10,
    reach,
    totalViews,
    reactions,
    replies,
    curve,
    hourly,
    changes: readChanges(raw),
    hasData,
  };
}
