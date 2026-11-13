// Lógica pura de la repetición de programados: formulario ↔ regla del backend, validación,
// resumen legible, atajos de fecha y conversión datetime-local ↔ milisegundos.
// Las fechas son siempre de la hora local de la máquina (igual que el backend).
import { format } from "date-fns";
import { es } from "date-fns/locale";
import type { Recurrence } from "../api/core/model/models.js";

export type RepeatKind = "none" | "daily" | "weekly" | "monthly" | "yearly" | "custom";
export type CustomUnit = "minutes" | "hours" | "days" | "weeks" | "months";
export type EndMode = "never" | "date" | "count";

export interface RecurrenceForm {
  repeat: RepeatKind;
  /** Solo «Cada semana»: cada cuántas semanas. */
  weeklyEvery: number;
  /** Solo «Cada semana»: 0 = domingo … 6 = sábado. */
  weekdays: number[];
  /** Solo «Personalizado». */
  customEvery: number;
  customUnit: CustomUnit;
  endMode: EndMode;
  /** `yyyy-MM-dd` (valor de `<input type="date">`). */
  endDate: string;
  maxCount: number;
}

/** Los mismos topes que el backend. */
export const MAX_EVERY = 365;
export const MIN_MINUTES = 5;
export const MAX_COUNT = 1000;
/** Tolerancia hacia atrás del primer envío (la misma que el backend). */
export const PAST_GRACE_MS = 60 * 60_000;

export const REPEAT_OPTIONS: Array<{ id: RepeatKind; label: string }> = [
  { id: "none", label: "No se repite" },
  { id: "daily", label: "Cada día" },
  { id: "weekly", label: "Cada semana" },
  { id: "monthly", label: "Cada mes" },
  { id: "yearly", label: "Cada año" },
  { id: "custom", label: "Personalizado" },
];

export const UNIT_OPTIONS: Array<{ id: CustomUnit; label: string }> = [
  { id: "minutes", label: "minutos" },
  { id: "hours", label: "horas" },
  { id: "days", label: "días" },
  { id: "weeks", label: "semanas" },
  { id: "months", label: "meses" },
];

export const DAY_NAMES = ["domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"];
/** Orden de la fila de días: de lunes a domingo. */
export const WEEK_ORDER = [1, 2, 3, 4, 5, 6, 0];
export const DAY_LETTERS: Record<number, string> = { 1: "L", 2: "M", 3: "X", 4: "J", 5: "V", 6: "S", 0: "D" };

const MIN = 60_000;
const HOUR = 60 * MIN;

// ---------------------------------------------------------------- fecha local ↔ ms

const pad = (n: number) => String(n).padStart(2, "0");

/** Valor de `<input type="datetime-local">` para unos milisegundos. */
export function toLocalInput(ms: number): string {
  const d = new Date(ms);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** Milisegundos de un valor `datetime-local`, o `null` si no es válido. */
export function parseLocalInput(value: string): number | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/.exec(value ?? "");
  if (!m) return null;
  const [y, mo, d, h, mi] = m.slice(1).map(Number);
  const date = new Date(y, mo - 1, d, h, mi, 0, 0);
  return Number.isNaN(date.getTime()) ? null : date.getTime();
}

/** Valor de `<input type="date">` para unos milisegundos. */
export function toDateInput(ms: number): string {
  const d = new Date(ms);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/** Último instante (23:59:59.999 locales) del día de un valor `yyyy-MM-dd`, o `null`. */
export function parseDateInputEnd(value: string): number | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value ?? "");
  if (!m) return null;
  const [y, mo, d] = m.slice(1).map(Number);
  const date = new Date(y, mo - 1, d, 23, 59, 59, 999);
  return Number.isNaN(date.getTime()) ? null : date.getTime();
}

/** La próxima hora en punto (estrictamente posterior a `now`). */
export function defaultFirstMs(now: number = Date.now()): number {
  const d = new Date(now);
  d.setMinutes(0, 0, 0);
  return d.getTime() + HOUR;
}

export interface QuickShortcut {
  id: "in1h" | "tomorrow" | "monday";
  label: string;
  ms: number;
}

/** «En 1 hora», «Mañana 9:00», «Lunes 9:00» (el próximo lunes, nunca hoy). */
export function quickShortcuts(now: number = Date.now()): QuickShortcut[] {
  const d = new Date(now);
  const inOneHour = Math.ceil((now + HOUR) / MIN) * MIN;
  const tomorrow = new Date(d.getFullYear(), d.getMonth(), d.getDate() + 1, 9, 0, 0, 0).getTime();
  const ahead = (1 - d.getDay() + 7) % 7 || 7;
  const monday = new Date(d.getFullYear(), d.getMonth(), d.getDate() + ahead, 9, 0, 0, 0).getTime();
  return [
    { id: "in1h", label: "En 1 hora", ms: inOneHour },
    { id: "tomorrow", label: "Mañana 9:00", ms: tomorrow },
    { id: "monday", label: "Lunes 9:00", ms: monday },
  ];
}

/** Mensaje si la fecha no sirve para programar; `null` si vale (se tolera 1 h de atraso, como el backend). */
export function whenError(ms: number | null, now: number = Date.now()): string | null {
  if (ms === null) return "Elige fecha y hora";
  if (ms < now - PAST_GRACE_MS) return "La fecha ya pasó";
  return null;
}

// ---------------------------------------------------------------- formulario ↔ regla

export function emptyRecurrenceForm(): RecurrenceForm {
  return {
    repeat: "none",
    weeklyEvery: 1,
    weekdays: [],
    customEvery: 1,
    customUnit: "days",
    endMode: "never",
    endDate: "",
    maxCount: 10,
  };
}

/** Mezcla un borrador (posiblemente viejo o incompleto) con los valores por defecto. */
export function normalizeRecurrenceForm(d: Partial<RecurrenceForm> | null | undefined): RecurrenceForm {
  const base = emptyRecurrenceForm();
  if (!d || typeof d !== "object") return base;
  const num = (v: unknown, def: number) => (typeof v === "number" && Number.isFinite(v) ? v : def);
  return {
    repeat: REPEAT_OPTIONS.some((o) => o.id === d.repeat) ? (d.repeat as RepeatKind) : base.repeat,
    weeklyEvery: num(d.weeklyEvery, base.weeklyEvery),
    weekdays: Array.isArray(d.weekdays) ? d.weekdays.filter((x) => Number.isInteger(x) && x >= 0 && x <= 6) : [],
    customEvery: num(d.customEvery, base.customEvery),
    customUnit: UNIT_OPTIONS.some((o) => o.id === d.customUnit) ? (d.customUnit as CustomUnit) : base.customUnit,
    endMode: d.endMode === "date" || d.endMode === "count" ? d.endMode : "never",
    endDate: typeof d.endDate === "string" ? d.endDate : "",
    maxCount: num(d.maxCount, base.maxCount),
  };
}

/** El formulario que corresponde a una regla guardada (`firstMs` da el día por defecto de «Cada semana»). */
export function recurrenceFormFromRule(r: Recurrence | null | undefined, firstMs?: number): RecurrenceForm {
  const f = emptyRecurrenceForm();
  if (!r) return f;
  if (r.endAt > 0) {
    f.endMode = "date";
    f.endDate = toDateInput(r.endAt);
  } else if (r.maxCount > 0) {
    f.endMode = "count";
    f.maxCount = r.maxCount;
  }
  const every = r.every >= 1 ? r.every : 1;
  switch (r.frequency) {
    case "daily":
      if (every === 1) f.repeat = "daily";
      else Object.assign(f, { repeat: "custom", customEvery: every, customUnit: "days" });
      break;
    case "weekly": {
      f.repeat = "weekly";
      f.weeklyEvery = every;
      const days = (r.weekdays ?? []).filter((d) => d >= 0 && d <= 6);
      f.weekdays = days.length ? days : firstMs !== undefined ? [new Date(firstMs).getDay()] : [];
      break;
    }
    case "monthly":
      if (every === 1) f.repeat = "monthly";
      else Object.assign(f, { repeat: "custom", customEvery: every, customUnit: "months" });
      break;
    case "yearly":
      if (every === 1) f.repeat = "yearly";
      else Object.assign(f, { repeat: "custom", customEvery: Math.min(every * 12, MAX_EVERY), customUnit: "months" });
      break;
    case "custom": {
      f.repeat = "custom";
      f.customEvery = every;
      f.customUnit = UNIT_OPTIONS.some((o) => o.id === r.unit) ? (r.unit as CustomUnit) : "days";
      break;
    }
  }
  return f;
}

function sortedDays(days: number[]): number[] {
  return Array.from(new Set(days)).sort((a, b) => WEEK_ORDER.indexOf(a) - WEEK_ORDER.indexOf(b));
}

/**
 * La regla que se manda al backend, o `null` si no se repite. `base` es la regla ya guardada de una
 * serie: de ella se conservan `count` y `firstAt`, que lleva el servicio.
 */
export function buildRecurrence(f: RecurrenceForm, base?: Recurrence | null): Recurrence | null {
  if (f.repeat === "none") return null;
  const r: Recurrence = {
    frequency: f.repeat,
    every: 1,
    endAt: f.endMode === "date" ? (parseDateInputEnd(f.endDate) ?? 0) : 0,
    maxCount: f.endMode === "count" ? Math.trunc(f.maxCount) : 0,
    count: base?.count ?? 0,
    firstAt: base?.firstAt ?? 0,
  };
  if (f.repeat === "weekly") {
    r.every = Math.trunc(f.weeklyEvery);
    r.weekdays = sortedDays(f.weekdays);
  } else if (f.repeat === "custom") {
    r.every = Math.trunc(f.customEvery);
    r.unit = f.customUnit;
  }
  return r;
}

const intIn = (v: number, min: number, max: number) => Number.isInteger(v) && v >= min && v <= max;

/** Qué falta o sobra en la repetición (`null` = vale). `firstMs` es la fecha del primer envío. */
export function validateRecurrenceForm(f: RecurrenceForm, firstMs: number | null): string | null {
  if (f.repeat === "none") return null;
  if (f.repeat === "weekly") {
    if (!intIn(f.weeklyEvery, 1, MAX_EVERY)) return `Cada cuántas semanas: de 1 a ${MAX_EVERY}`;
    if (f.weekdays.length === 0) return "Elige al menos un día de la semana";
  }
  if (f.repeat === "custom") {
    if (!intIn(f.customEvery, 1, MAX_EVERY)) return `La repetición debe ser cada 1 a ${MAX_EVERY} veces la unidad`;
    if (f.customUnit === "minutes" && f.customEvery < MIN_MINUTES) return `La repetición mínima es cada ${MIN_MINUTES} minutos`;
  }
  if (f.endMode === "date") {
    const end = parseDateInputEnd(f.endDate);
    if (end === null) return "Elige el día en que termina";
    if (firstMs !== null && end <= firstMs) return "El día final debe ser posterior al primer envío";
  }
  if (f.endMode === "count" && !intIn(f.maxCount, 1, MAX_COUNT)) return `El número de veces va de 1 a ${MAX_COUNT}`;
  return null;
}

// ---------------------------------------------------------------- resumen legible

function joinList(items: string[]): string {
  if (items.length <= 1) return items.join("");
  return `${items.slice(0, -1).join(", ")} y ${items[items.length - 1]}`;
}

function atTime(ms: number): string {
  const d = new Date(ms);
  return `a ${d.getHours() === 1 ? "la" : "las"} ${format(d, "HH:mm")}`;
}

/** «lunes y jueves», «de lunes a viernes» o «todos los días». */
export function weekdaysLabel(days: number[]): string {
  const sorted = sortedDays(days);
  if (sorted.length === 7) return "todos los días";
  if (sorted.length === 5 && sorted.join() === "1,2,3,4,5") return "de lunes a viernes";
  return joinList(sorted.map((d) => DAY_NAMES[d]));
}

type RuleLike = Pick<Recurrence, "frequency" | "every" | "endAt" | "maxCount"> &
  Partial<Pick<Recurrence, "unit" | "weekdays">>;

/**
 * Resumen de una regla: «Cada semana, lunes y jueves, a las 09:00». `firstMs` da la hora y el día
 * (del mes, de la semana o del año) cuando la regla no los lleva.
 */
export function summarizeRule(r: RuleLike, firstMs: number): string {
  const every = r.every >= 1 ? r.every : 1;
  const first = new Date(firstMs);
  const time = atTime(firstMs);
  const dayOfMonth = first.getDate();
  const monthDay = dayOfMonth >= 29 ? `el día ${dayOfMonth} o el último del mes` : `el día ${dayOfMonth}`;
  let text: string;
  switch (r.frequency) {
    case "daily":
      text = `${every === 1 ? "Cada día" : `Cada ${every} días`}, ${time}`;
      break;
    case "weekly": {
      const days = r.weekdays && r.weekdays.length ? r.weekdays : [first.getDay()];
      text = `${every === 1 ? "Cada semana" : `Cada ${every} semanas`}, ${weekdaysLabel(days)}, ${time}`;
      break;
    }
    case "monthly":
      text = `${every === 1 ? "Cada mes" : `Cada ${every} meses`}, ${monthDay}, ${time}`;
      break;
    case "yearly":
      text = `${every === 1 ? "Cada año" : `Cada ${every} años`}, el ${format(first, "d 'de' MMMM", { locale: es })}, ${time}`;
      break;
    case "custom":
      switch (r.unit) {
        case "minutes":
          text = every === 1 ? "Cada minuto" : `Cada ${every} minutos`;
          break;
        case "hours":
          text = every === 1 ? "Cada hora" : `Cada ${every} horas`;
          break;
        case "weeks":
          text = `${every === 1 ? "Cada semana" : `Cada ${every} semanas`}, el ${DAY_NAMES[first.getDay()]}, ${time}`;
          break;
        case "months":
          text = `${every === 1 ? "Cada mes" : `Cada ${every} meses`}, ${monthDay}, ${time}`;
          break;
        default:
          text = `${every === 1 ? "Cada día" : `Cada ${every} días`}, ${time}`;
      }
      break;
    default:
      return "";
  }
  if (r.endAt > 0) text += `, hasta el ${format(new Date(r.endAt), "dd/MM/yyyy")}`;
  else if (r.maxCount > 0) text += `, ${r.maxCount} ${r.maxCount === 1 ? "vez" : "veces"} en total`;
  return text;
}

/** Resumen del formulario («No se repite» si no hay repetición; vacío si la regla no vale). */
export function summarizeForm(f: RecurrenceForm, firstMs: number | null): string {
  if (f.repeat === "none") return "No se repite";
  if (firstMs === null || validateRecurrenceForm(f, firstMs)) return "";
  const r = buildRecurrence(f);
  return r ? summarizeRule(r, firstMs) : "";
}

/** «Se repite: cada semana, lunes y jueves, a las 09:00» (para el distintivo de la serie). */
export function repeatChipText(r: Recurrence | null | undefined, firstMs: number): string {
  if (!r) return "";
  const s = summarizeRule(r, r.firstAt > 0 ? r.firstAt : firstMs);
  return s ? `Se repite: ${s.charAt(0).toLowerCase()}${s.slice(1)}` : "";
}

/** Una fecha de la vista previa: «lun 16/11/2026 09:00». */
export function formatOccurrence(ms: number): string {
  return format(new Date(ms), "EEE dd/MM/yyyy HH:mm", { locale: es });
}
