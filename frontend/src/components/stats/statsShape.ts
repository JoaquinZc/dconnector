// Lectura tolerante de las estadísticas de conversaciones. La forma real la fija C2 en
// `internal/core/stats/conversation.go` (docs/bitacora/3-C2.md): campos camelCase, instantes en ms,
// duraciones de respuesta en SEGUNDOS (`...Sec`), listas siempre `[]` y `null` donde no hay dato.
// Mientras los enlaces TypeScript sigan con structs vacíos se lee desde `unknown` con valores por
// defecto, para que la pantalla no falle si llega menos. Tras regenerar los enlaces se pueden tipar las
// funciones `read*` con `ConversationOverview` y `GroupsOverview` sin tocar las pantallas.

type Obj = Record<string, unknown>;

const isObj = (v: unknown): v is Obj => !!v && typeof v === "object" && !Array.isArray(v);

/** Número finito o `fallback`. Acepta cadenas numéricas. */
export function toNum(v: unknown, fallback = 0): number {
  const n = typeof v === "string" && v.trim() !== "" ? Number(v) : v;
  return typeof n === "number" && Number.isFinite(n) ? n : fallback;
}

/** Número finito o `null` (los valores que pueden no existir). */
export function numOrNull(v: unknown): number | null {
  if (v === null || v === undefined || v === "") return null;
  const n = toNum(v, NaN);
  return Number.isNaN(n) ? null : n;
}

const str = (v: unknown): string => (typeof v === "string" ? v : v === null || v === undefined ? "" : String(v));
const list = (v: unknown): unknown[] => (Array.isArray(v) ? v : []);
const obj = (v: unknown): Obj => (isObj(v) ? v : {});

// ---------------------------------------------------------------- tipos de lectura

export type Granularity = "day" | "week" | "month";

export interface PeriodView {
  fromMs: number;
  toMs: number;
  prevFromMs: number;
  prevToMs: number;
  days: number;
  granularity: Granularity;
}

export interface MixView {
  text: number;
  image: number;
  video: number;
  voice: number;
  document: number;
  sticker: number;
  other: number;
}

export interface ResponseView {
  mineMedianSec: number | null;
  mineCount: number;
  /** 0..1 */
  mineUnderHour: number | null;
  theirsMedianSec: number | null;
  theirsCount: number;
}

export interface SummaryView {
  sent: number;
  received: number;
  chats: number;
  startedByMe: number;
  startedByThem: number;
  response: ResponseView;
  mix: MixView;
}

/** Variación frente al periodo anterior como fracción (0,25 = +25 %); `null` = sin base. */
export type ChangeKey =
  | "sent"
  | "received"
  | "chats"
  | "startedByMe"
  | "startedByThem"
  | "mineMedianSec"
  | "theirsMedianSec"
  | "mineUnderHour";
export type ChangesView = Record<ChangeKey, number | null>;

export interface DayPoint {
  day: string;
  sent: number;
  received: number;
}

export interface ChatRankView {
  chatKey: string;
  accountId: string;
  channel: string;
  nativeId: string;
  kind: string;
  title: string;
  avatarUrl: string;
  total: number;
  sent: number;
  received: number;
  activeDays: number;
  lastAt: number;
}

export interface ConversationView {
  period: PeriodView;
  current: SummaryView;
  previous: SummaryView;
  changes: ChangesView;
  groupSent: number;
  groupReceived: number;
  perDay: DayPoint[];
  /** Siempre 7 × 24 (lunes…domingo × horas locales). */
  heatmap: number[][];
  topChats: ChatRankView[];
  frequent: ChatRankView[];
  /** Hay algún mensaje en el periodo o en el anterior. */
  hasData: boolean;
}

export interface GroupRowView {
  chatKey: string;
  accountId: string;
  channel: string;
  nativeId: string;
  title: string;
  avatarUrl: string;
  total: number;
  mine: number;
  authors: number;
  activeDays: number;
  lastAt: number;
}

export interface PersonRowView {
  accountId: string;
  channel: string;
  senderId: string;
  name: string;
  avatarUrl: string;
  total: number;
  groups: number;
  lastAt: number;
}

export interface GroupsView {
  period: PeriodView;
  groupSent: number;
  groupReceived: number;
  groups: GroupRowView[];
  people: PersonRowView[];
  hasData: boolean;
}

// ---------------------------------------------------------------- comparación

/**
 * Variación de `current` frente a `previous` como fracción. Sin base (antes 0 y ahora algo) = `null`;
 * 0 contra 0 = 0.
 */
export function pctChange(current: number, previous: number): number | null {
  if (!Number.isFinite(current) || !Number.isFinite(previous)) return null;
  if (previous === 0) return current === 0 ? 0 : null;
  return (current - previous) / Math.abs(previous);
}

/** Como `pctChange`, pero con valores que pueden no existir (medianas): sin uno de los dos, `null`. */
export function pctChangeNullable(current: number | null, previous: number | null): number | null {
  if (current === null || previous === null) return null;
  return pctChange(current, previous);
}

/** «+25 %», «−10 %», «0 %»; vacío si no hay base para comparar. */
export function changeText(change: number | null | undefined): string {
  if (change === null || change === undefined || !Number.isFinite(change)) return "";
  const pct = Math.round(change * 100);
  if (pct === 0) return "0 %";
  return `${pct > 0 ? "+" : "−"}${Math.abs(pct).toLocaleString("es-EC")} %`;
}

/** Tono de una variación: bajar es bueno cuando `lowerIsBetter` (tiempos de respuesta). */
export function changeTone(change: number | null | undefined, lowerIsBetter = false): "good" | "bad" | "flat" {
  if (change === null || change === undefined || Math.abs(change) < 0.005) return "flat";
  const up = change > 0;
  return up === !lowerIsBetter ? "good" : "bad";
}

// ---------------------------------------------------------------- mapa de calor

export const HEAT_DAYS = ["Lun", "Mar", "Mié", "Jue", "Vie", "Sáb", "Dom"] as const;
export const HEAT_HOURS = 24;

/** Cualquier cosa → matriz 7 × 24 de enteros ≥ 0. Filas o columnas que falten quedan en 0; las que sobren se ignoran. */
export function normalizeHeatmap(raw: unknown): number[][] {
  const rows = list(raw);
  return Array.from({ length: 7 }, (_, d) => {
    const row = list(rows[d]);
    return Array.from({ length: HEAT_HOURS }, (_, h) => Math.max(0, Math.round(toNum(row[h]))));
  });
}

export function heatmapTotal(grid: number[][]): number {
  return grid.reduce((a, row) => a + row.reduce((b, v) => b + v, 0), 0);
}

export interface HeatPeak {
  day: number;
  hour: number;
  value: number;
}

/** La celda con más mensajes (la primera si hay empate); `null` si todo está en 0. */
export function heatmapPeak(grid: number[][]): HeatPeak | null {
  let best: HeatPeak | null = null;
  for (let day = 0; day < grid.length; day++) {
    for (let hour = 0; hour < grid[day].length; hour++) {
      const value = grid[day][hour];
      if (value > 0 && (best === null || value > best.value)) best = { day, hour, value };
    }
  }
  return best;
}

/** Intensidad 0..1 de una celda respecto al máximo de la matriz (0 si no hay mensajes). */
export function heatLevel(value: number, max: number): number {
  if (value <= 0 || max <= 0) return 0;
  return Math.min(1, 0.12 + 0.88 * (value / max));
}

// ---------------------------------------------------------------- formato

/** Duración legible: «45 s», «12 min», «3 h 20 min», «2 d». `null` = «—». */
export function formatSeconds(seconds: number | null | undefined): string {
  if (seconds === null || seconds === undefined || !Number.isFinite(seconds)) return "—";
  if (seconds < 60) return `${Math.round(seconds)} s`;
  const min = Math.round(seconds / 60);
  if (min < 60) return `${min} min`;
  const h = Math.floor(min / 60);
  if (h < 48) return min % 60 ? `${h} h ${min % 60} min` : `${h} h`;
  return `${Math.round(h / 24)} d`;
}

/** Fracción 0..1 → «45 %»; `null` = «—». */
export function formatFraction(v: number | null | undefined): string {
  if (v === null || v === undefined || !Number.isFinite(v)) return "—";
  return `${Math.round(v * 100)} %`;
}

const MONTHS = ["ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sep", "oct", "nov", "dic"];

/** «2026-10-05» → «5 oct»; «2026-10» (mes) → «oct 2026»; otro texto, tal cual. */
export function formatBucket(day: string): string {
  const d = /^(\d{4})-(\d{2})-(\d{2})$/.exec(day);
  if (d) return `${Number(d[3])} ${MONTHS[Number(d[2]) - 1] ?? d[2]}`;
  const m = /^(\d{4})-(\d{2})$/.exec(day);
  if (m) return `${MONTHS[Number(m[2]) - 1] ?? m[2]} ${m[1]}`;
  return day;
}

// ---------------------------------------------------------------- lectura

const GRANULARITIES: Granularity[] = ["day", "week", "month"];

export function readPeriod(raw: unknown): PeriodView {
  const p = obj(raw);
  const g = str(p.granularity) as Granularity;
  return {
    fromMs: toNum(p.fromMs),
    toMs: toNum(p.toMs),
    prevFromMs: toNum(p.prevFromMs),
    prevToMs: toNum(p.prevToMs),
    days: toNum(p.days),
    granularity: GRANULARITIES.includes(g) ? g : "day",
  };
}

function readMix(raw: unknown): MixView {
  const m = obj(raw);
  return {
    text: toNum(m.text),
    image: toNum(m.image),
    video: toNum(m.video),
    voice: toNum(m.voice),
    document: toNum(m.document),
    sticker: toNum(m.sticker),
    other: toNum(m.other),
  };
}

function readResponse(raw: unknown): ResponseView {
  const r = obj(raw);
  return {
    mineMedianSec: numOrNull(r.mineMedianSec),
    mineCount: toNum(r.mineCount),
    mineUnderHour: numOrNull(r.mineUnderHour),
    theirsMedianSec: numOrNull(r.theirsMedianSec),
    theirsCount: toNum(r.theirsCount),
  };
}

export function readSummary(raw: unknown): SummaryView {
  const s = obj(raw);
  return {
    sent: toNum(s.sent),
    received: toNum(s.received),
    chats: toNum(s.chats),
    startedByMe: toNum(s.startedByMe),
    startedByThem: toNum(s.startedByThem),
    response: readResponse(s.response),
    mix: readMix(s.mix),
  };
}

const CHANGE_KEYS: ChangeKey[] = ["sent", "received", "chats", "startedByMe", "startedByThem", "mineMedianSec", "theirsMedianSec"];

/**
 * `changes` del servicio (fracción, `null` sin base). Si el servicio no trae la clave se calcula con
 * los dos resúmenes. «Respondes en menos de 1 h» no viene del servicio: siempre se calcula aquí.
 */
export function readChanges(raw: unknown, current: SummaryView, previous: SummaryView): ChangesView {
  const c = obj(raw);
  const computed: ChangesView = {
    sent: pctChange(current.sent, previous.sent),
    received: pctChange(current.received, previous.received),
    chats: pctChange(current.chats, previous.chats),
    startedByMe: pctChange(current.startedByMe, previous.startedByMe),
    startedByThem: pctChange(current.startedByThem, previous.startedByThem),
    mineMedianSec: pctChangeNullable(current.response.mineMedianSec, previous.response.mineMedianSec),
    theirsMedianSec: pctChangeNullable(current.response.theirsMedianSec, previous.response.theirsMedianSec),
    mineUnderHour: pctChangeNullable(current.response.mineUnderHour, previous.response.mineUnderHour),
  };
  const out = { ...computed };
  for (const k of CHANGE_KEYS) if (k in c) out[k] = numOrNull(c[k]);
  return out;
}

function readDays(raw: unknown): DayPoint[] {
  return list(raw)
    .filter(isObj)
    .map((d) => ({ day: str(d.day), sent: toNum(d.sent), received: toNum(d.received) }));
}

function readChatRank(raw: unknown): ChatRankView {
  const r = obj(raw);
  return {
    chatKey: str(r.chatKey),
    accountId: str(r.accountId),
    channel: str(r.channel),
    nativeId: str(r.nativeId),
    kind: str(r.kind),
    title: str(r.title),
    avatarUrl: str(r.avatarUrl),
    total: toNum(r.total),
    sent: toNum(r.sent),
    received: toNum(r.received),
    activeDays: toNum(r.activeDays),
    lastAt: toNum(r.lastAt),
  };
}

/** `StatsService.Conversations` listo para pintar. Nunca lanza. */
export function readConversationOverview(raw: unknown): ConversationView {
  const o = obj(raw);
  const current = readSummary(o.current);
  const previous = readSummary(o.previous);
  const perDay = readDays(o.perDay);
  const heatmap = normalizeHeatmap(o.heatmap);
  const total = (s: SummaryView) => s.sent + s.received;
  return {
    period: readPeriod(o.period),
    current,
    previous,
    changes: readChanges(o.changes, current, previous),
    groupSent: toNum(o.groupSent),
    groupReceived: toNum(o.groupReceived),
    perDay,
    heatmap,
    topChats: list(o.topChats).map(readChatRank),
    frequent: list(o.frequent).map(readChatRank),
    hasData:
      total(current) + total(previous) > 0 ||
      toNum(o.groupSent) + toNum(o.groupReceived) > 0 ||
      perDay.some((d) => d.sent + d.received > 0),
  };
}

/** `StatsService.Groups` listo para pintar. Nunca lanza. */
export function readGroupsOverview(raw: unknown): GroupsView {
  const o = obj(raw);
  const groups = list(o.groups)
    .filter(isObj)
    .map(
      (g): GroupRowView => ({
        chatKey: str(g.chatKey),
        accountId: str(g.accountId),
        channel: str(g.channel),
        nativeId: str(g.nativeId),
        title: str(g.title),
        avatarUrl: str(g.avatarUrl),
        total: toNum(g.total),
        mine: toNum(g.mine),
        authors: toNum(g.authors),
        activeDays: toNum(g.activeDays),
        lastAt: toNum(g.lastAt),
      }),
    );
  const people = list(o.people)
    .filter(isObj)
    .map(
      (p): PersonRowView => ({
        accountId: str(p.accountId),
        channel: str(p.channel),
        senderId: str(p.senderId),
        name: str(p.name),
        avatarUrl: str(p.avatarUrl),
        total: toNum(p.total),
        groups: toNum(p.groups),
        lastAt: toNum(p.lastAt),
      }),
    );
  return {
    period: readPeriod(o.period),
    groupSent: toNum(o.groupSent),
    groupReceived: toNum(o.groupReceived),
    groups,
    people,
    hasData: groups.length > 0 || people.length > 0 || toNum(o.groupSent) + toNum(o.groupReceived) > 0,
  };
}

/** Total de mensajes de un resumen. */
export const summaryTotal = (s: SummaryView): number => s.sent + s.received;

/** Parte (0..1) de `value` respecto a `max`, para las barras de los rankings. */
export function barShare(value: number, max: number): number {
  if (max <= 0 || value <= 0) return 0;
  return Math.min(1, value / max);
}

/** Máximo de una lista de valores (mínimo 1, para no dividir entre 0). */
export function maxOf(values: number[]): number {
  return Math.max(1, ...values);
}
