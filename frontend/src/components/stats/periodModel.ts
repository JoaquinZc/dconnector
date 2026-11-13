// Selector de periodo → `stats.Range`. Un periodo es «los últimos N días» (`days > 0`) o un rango de
// fechas LOCALES `AAAA-MM-DD`. En Go el rango de fechas es [from, to): `to` es EXCLUSIVO. La persona
// elige el último día incluido, así que aquí `to` se envía como ese día + 1.
import type { Range } from "@/api/types";

export const PRESET_DAYS = [7, 30, 90] as const;
export type PresetDays = (typeof PRESET_DAYS)[number];
export type PeriodPreset = PresetDays | "custom";

export interface PeriodState {
  preset: PeriodPreset;
  /** Primer día incluido (solo «custom»), `AAAA-MM-DD`. */
  from: string;
  /** Último día incluido (solo «custom»), `AAAA-MM-DD`. */
  to: string;
}

const DAY_RE = /^(\d{4})-(\d{2})-(\d{2})$/;

/** Fecha local de un `Date` como `AAAA-MM-DD`. */
export function localDay(d: Date): string {
  const y = String(d.getFullYear()).padStart(4, "0");
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${y}-${m}-${day}`;
}

/** `AAAA-MM-DD` → `Date` local a medianoche; `null` si no es una fecha real. */
export function parseDay(s: string): Date | null {
  const m = DAY_RE.exec(s);
  if (!m) return null;
  const d = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]));
  return localDay(d) === s ? d : null;
}

/** Suma `n` días (negativo = resta) a un `AAAA-MM-DD`. Una fecha mal formada se devuelve tal cual. */
export function addDays(day: string, n: number): string {
  const d = parseDay(day);
  if (!d) return day;
  d.setDate(d.getDate() + n);
  return localDay(d);
}

/** Días entre dos fechas (ambas incluidas): del 1 al 7 = 7. 0 si alguna no es válida. */
export function spanDays(from: string, to: string): number {
  const a = parseDay(from);
  const b = parseDay(to);
  if (!a || !b) return 0;
  return Math.round((Date.UTC(b.getFullYear(), b.getMonth(), b.getDate()) - Date.UTC(a.getFullYear(), a.getMonth(), a.getDate())) / 86_400_000) + 1;
}

/** Periodo inicial: 30 días. Para «personalizado» arranca con las dos últimas semanas. */
export function defaultPeriod(today: Date = new Date()): PeriodState {
  const to = localDay(today);
  return { preset: 30, from: addDays(to, -13), to };
}

/** Error del rango personalizado, o `null` si vale. */
export function validateCustom(from: string, to: string, today: Date = new Date()): string | null {
  if (!parseDay(from) || !parseDay(to)) return "Elige las dos fechas";
  if (from > to) return "La fecha inicial es posterior a la final";
  if (to > localDay(today)) return "La fecha final no puede ser futura";
  return null;
}

/** ¿El periodo se puede pedir? (los preajustes siempre; el personalizado, si vale). */
export function isValidPeriod(p: PeriodState, today: Date = new Date()): boolean {
  return p.preset !== "custom" || validateCustom(p.from, p.to, today) === null;
}

/** `stats.Range` del periodo. El personalizado envía `to` + 1 día (el rango de Go es [from, to)). */
export function toRange(p: PeriodState): Range {
  if (p.preset === "custom") return { days: 0, from: p.from, to: addDays(p.to, 1) };
  return { days: p.preset, from: "", to: "" };
}

/** Clave estable del rango (para dependencias de efectos). */
export function rangeKey(r: Range): string {
  return r.days > 0 ? `d${r.days}` : `${r.from}..${r.to}`;
}

/** Etiqueta del selector: «7 días», «Personalizado». */
export function presetLabel(p: PeriodPreset): string {
  return p === "custom" ? "Personalizado" : `${p} días`;
}
