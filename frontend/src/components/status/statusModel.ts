// Lógica pura de Estados: colores y letras, duración en el visor, anillos segmentados, tiempos.
import type { Status, StatusAuthor } from "@/api/types";
import { CHANNEL_ORDER } from "@/lib/channels";

/** 12 colores de fondo de un estado de texto. */
export const STATUS_COLORS = [
  "#128C7E",
  "#075E54",
  "#25D366",
  "#34B7F1",
  "#0084FF",
  "#8B5CF6",
  "#EC4899",
  "#EF4444",
  "#F59E0B",
  "#84CC16",
  "#6B7280",
  "#111B21",
] as const;

export const DEFAULT_STATUS_COLOR = STATUS_COLORS[0];

export interface StatusFont {
  value: number;
  label: string;
  css: string;
}

/** 4 letras. El número viaja en `Status.font`/`OutgoingStatus.font`. */
export const STATUS_FONTS: StatusFont[] = [
  { value: 0, label: "Normal", css: '"Segoe UI", system-ui, sans-serif' },
  { value: 1, label: "Serif", css: 'Georgia, "Times New Roman", serif' },
  { value: 2, label: "Mono", css: 'Consolas, "Courier New", monospace' },
  { value: 3, label: "Cursiva", css: '"Segoe Script", "Brush Script MT", cursive' },
];

export function fontCss(font: number | null | undefined): string {
  return (STATUS_FONTS.find((f) => f.value === font) ?? STATUS_FONTS[0]).css;
}

/** Color de fondo válido (#RRGGBB) o el de por defecto. */
export function bgOf(color: string | null | undefined): string {
  return color && /^#[0-9a-f]{6}$/i.test(color) ? color : DEFAULT_STATUS_COLOR;
}

// ---------------------------------------------------------------- visor

/** Duración de un estado de foto o de texto en el visor. */
export const STORY_IMAGE_MS = 5000;
/** Duración que se asume de un vídeo mientras no se conoce la real. */
export const STORY_VIDEO_FALLBACK_MS = 30_000;

/** Lo que dura un estado en el visor: 5 s foto o texto; el vídeo, lo que dura. */
export function storyDurationMs(
  s: Pick<Status, "kind" | "media">,
  videoSeconds?: number,
): number {
  if (String(s.kind) !== "video") return STORY_IMAGE_MS;
  const sec = videoSeconds && videoSeconds > 0 ? videoSeconds : s.media?.duration;
  return sec && sec > 0 ? Math.round(sec * 1000) : STORY_VIDEO_FALLBACK_MS;
}

export interface Cursor {
  group: number;
  item: number;
}

/** Tamaño de cada grupo (autor) del visor; `undefined` mientras se carga. */
export type GroupSizes = Array<number | undefined>;

/** Siguiente posición, o `null` si se acabó todo. */
export function nextCursor(c: Cursor, sizes: GroupSizes): Cursor | null {
  const size = sizes[c.group];
  if (size !== undefined && c.item < size - 1) return { group: c.group, item: c.item + 1 };
  if (c.group < sizes.length - 1) return { group: c.group + 1, item: 0 };
  return null;
}

/** Posición anterior (la primera del autor anterior); en la primera no se mueve. */
export function prevCursor(c: Cursor): Cursor {
  if (c.item > 0) return { group: c.group, item: c.item - 1 };
  if (c.group > 0) return { group: c.group - 1, item: 0 };
  return c;
}

/** Posición por la que se empieza un grupo: el primer estado no visto, o el primero. */
export function startItem(list: Array<Pick<Status, "viewed" | "fromMe">>): number {
  const i = list.findIndex((s) => !s.viewed && !s.fromMe);
  return i >= 0 ? i : 0;
}

export interface Progress {
  /** 0..1 */
  value: number;
  done: boolean;
}

/** Avanza la barra: sin avance si está en pausa; `done` al llegar a 1. */
export function advanceProgress(value: number, elapsedMs: number, durationMs: number, paused: boolean): Progress {
  if (paused || durationMs <= 0 || elapsedMs <= 0) return { value: clamp01(value), done: false };
  const next = clamp01(value + elapsedMs / durationMs);
  return { value: next, done: next >= 1 };
}

export function clamp01(n: number): number {
  return n < 0 ? 0 : n > 1 ? 1 : n;
}

/** Ancho (0..1) de la barra `i` cuando se muestra el estado `current` con avance `progress`. */
export function barFill(i: number, current: number, progress: number): number {
  if (i < current) return 1;
  if (i > current) return 0;
  return clamp01(progress);
}

// ---------------------------------------------------------------- anillos

export interface RingSegment {
  /** Inicio y fin, como fracción de la vuelta (0..1). */
  start: number;
  end: number;
  viewed: boolean;
}

/**
 * Segmentos del anillo de un autor: uno por estado; los vistos (los más antiguos) primero y los no
 * vistos después. Con un solo estado es la vuelta entera. Máximo 30 segmentos.
 */
export function ringSegments(count: number, unviewed: number, gap = 0.025): RingSegment[] {
  const n = Math.max(0, Math.min(30, Math.floor(count)));
  if (n === 0) return [];
  const pending = Math.max(0, Math.min(n, Math.floor(unviewed)));
  if (n === 1) return [{ start: 0, end: 1, viewed: pending === 0 }];
  const slot = 1 / n;
  const g = Math.min(gap, slot * 0.4);
  return Array.from({ length: n }, (_, i) => ({
    start: i * slot + g / 2,
    end: (i + 1) * slot - g / 2,
    viewed: i < n - pending,
  }));
}

// ---------------------------------------------------------------- tiempos

/** «hace 5 min», «hace 3 h». */
export function formatAgo(ts: number, now: number = Date.now()): string {
  const min = Math.max(0, Math.round((now - ts) / 60_000));
  if (min < 1) return "ahora";
  if (min < 60) return `hace ${min} min`;
  const h = Math.round(min / 60);
  if (h < 24) return `hace ${h} h`;
  return `hace ${Math.round(h / 24)} d`;
}

/** «Caduca en 5 h», «Caduca en 20 min», «Caducado». */
export function formatExpiry(expiresAt: number, now: number = Date.now()): string {
  if (!expiresAt) return "";
  const min = Math.round((expiresAt - now) / 60_000);
  if (min <= 0) return "Caducado";
  if (min < 60) return `Caduca en ${min} min`;
  return `Caduca en ${Math.round(min / 60)} h`;
}

/** Un estado que todavía se ve: no borrado ni caducado. */
export function isLive(s: Pick<Status, "deletedAt" | "expiresAt">, now: number = Date.now()): boolean {
  if (s.deletedAt) return false;
  return !s.expiresAt || s.expiresAt > now;
}

/** Estados vigentes, del más antiguo al más reciente. */
export function liveSorted<T extends Pick<Status, "deletedAt" | "expiresAt" | "postedAt">>(
  list: T[],
  now: number = Date.now(),
): T[] {
  return list.filter((s) => isLive(s, now)).sort((a, b) => a.postedAt - b.postedAt);
}

// ---------------------------------------------------------------- lista de autores

export interface SplitAuthors {
  /** Con estados sin ver. */
  recent: StatusAuthor[];
  /** Todo visto. */
  seen: StatusAuthor[];
  /** Orden del visor: primero los recientes. */
  ordered: StatusAuthor[];
}

/** Separa los autores en recientes (no vistos) y vistos, sin mí, del más nuevo al más antiguo. */
export function splitAuthors(authors: StatusAuthor[], selfId?: string): SplitAuthors {
  const others = authors.filter((a) => a.count > 0 && (!selfId || a.authorId !== selfId));
  const byRecent = (a: StatusAuthor, b: StatusAuthor) => b.lastAt - a.lastAt;
  const recent = others.filter((a) => a.unviewed > 0).sort(byRecent);
  const seen = others.filter((a) => a.unviewed <= 0).sort(byRecent);
  return { recent, seen, ordered: [...recent, ...seen] };
}

// ---------------------------------------------------------------- secciones por canal y cuenta

/** Clave única de un autor: la misma persona puede tener estados en varias cuentas. */
export function authorKey(a: Pick<StatusAuthor, "accountId" | "authorId">): string {
  return `${a.accountId}|${a.authorId}`;
}

/** Lo que la lista necesita saber de una cuenta con estados. */
export interface AccountMeta {
  id: string;
  channel: string;
  label: string;
  color: string;
  /** Id propio en el canal: sus estados no salen en la lista de los demás. */
  selfId?: string;
}

export interface AccountBlock {
  account: AccountMeta;
  /** Con estados sin ver. */
  recent: StatusAuthor[];
  /** Todo visto. */
  seen: StatusAuthor[];
}

export interface ChannelBlock {
  channel: string;
  accounts: AccountBlock[];
  /** Estados sin ver en todo el canal (antes de ocultar vistos). */
  newCount: number;
  /** Autores que se muestran. */
  total: number;
}

export interface SectionOptions {
  /** Solo este canal. */
  channel?: string | null;
  /** Oculta los autores con todo visto. */
  hideSeen?: boolean;
  /** Autor seleccionado: no se oculta aunque esté visto. */
  keepKey?: string | null;
}

/** Autores agrupados por canal y, dentro, por cuenta; cada cuenta con «recientes» y «vistos». */
export function buildSections(authors: StatusAuthor[], accounts: AccountMeta[], opts: SectionOptions = {}): ChannelBlock[] {
  const present = new Set(accounts.map((a) => a.channel));
  const channels = [...CHANNEL_ORDER.filter((c) => present.has(c)), ...[...present].filter((c) => !(CHANNEL_ORDER as string[]).includes(c))];
  const out: ChannelBlock[] = [];
  for (const channel of channels) {
    if (opts.channel && opts.channel !== channel) continue;
    const blocks: AccountBlock[] = [];
    let newCount = 0;
    let total = 0;
    for (const account of accounts.filter((a) => a.channel === channel)) {
      const { recent, seen } = splitAuthors(
        authors.filter((a) => String(a.accountId) === account.id),
        account.selfId,
      );
      newCount += recent.reduce((n, a) => n + Math.max(0, a.unviewed), 0);
      const shownSeen = opts.hideSeen ? seen.filter((a) => authorKey(a) === opts.keepKey) : seen;
      if (recent.length === 0 && shownSeen.length === 0) continue;
      blocks.push({ account, recent, seen: shownSeen });
      total += recent.length + shownSeen.length;
    }
    if (blocks.length > 0) out.push({ channel, accounts: blocks, newCount, total });
  }
  return out;
}

/** Los autores en el orden en que se ven en la lista. */
export function flattenSections(sections: ChannelBlock[]): StatusAuthor[] {
  return sections.flatMap((c) => c.accounts.flatMap((b) => [...b.recent, ...b.seen]));
}

/**
 * Orden de navegación del visor que no salta al recargar: conserva el orden anterior de los que siguen,
 * pone al final los nuevos y mantiene `keep` aunque ya no esté en la lista.
 */
export function stableOrder(prev: string[], fresh: string[], keep?: string | null): string[] {
  const freshSet = new Set(fresh);
  const kept = prev.filter((k) => freshSet.has(k) || k === keep);
  const known = new Set(kept);
  return [...kept, ...fresh.filter((k) => !known.has(k))];
}

/** «1 estado», «3 estados». */
export function countText(n: number): string {
  return n === 1 ? "1 estado" : `${n} estados`;
}

/** «1 nuevo», «2 nuevos». */
export function newText(n: number): string {
  return n === 1 ? "1 nuevo" : `${n} nuevos`;
}
