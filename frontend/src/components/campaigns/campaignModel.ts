// Lógica pura de Campañas: estados, esperas, ventana horaria, formulario del asistente y qué impide lanzar.
// No importa los enlaces de Wails (solo tipos), para probarse sin simular nada.
import { CampaignState } from "@/api/types";
import type {
  AccountView,
  Campaign,
  CampaignReview,
  CampaignTotals,
  Estimate,
  PacingSettings,
  SendWindow,
} from "@/api/types";
import { parseLocalInput, toLocalInput } from "@/components/status/statusForm";
import { format } from "date-fns";

// ---------------------------------------------------------------- cuentas

/** Canales que hacen campañas en v1 (Messenger e Instagram no). */
export const CAMPAIGN_CHANNELS = ["whatsapp", "telegram"] as const;

/** Cuentas activas de WhatsApp o Telegram, en el orden dado. */
export function campaignAccounts(views: AccountView[]): AccountView[] {
  return views.filter(
    (v) => v?.account?.enabled && (CAMPAIGN_CHANNELS as readonly string[]).includes(String(v.account.channel)),
  );
}

// ---------------------------------------------------------------- estados y esperas

export type Tone = "ok" | "warn" | "danger" | "muted" | "info";

const STATE_INFO: Record<string, { label: string; tone: Tone }> = {
  draft: { label: "Borrador", tone: "muted" },
  ready: { label: "Programada", tone: "info" },
  running: { label: "En curso", tone: "ok" },
  paused: { label: "En pausa", tone: "warn" },
  braked: { label: "Frenada", tone: "danger" },
  done: { label: "Terminada", tone: "muted" },
  canceled: { label: "Cancelada", tone: "muted" },
};

/** Texto y tono de la píldora de estado. */
export function stateInfo(state: string | null | undefined): { label: string; tone: Tone } {
  return STATE_INFO[String(state)] ?? { label: "Desconocido", tone: "muted" };
}

const WAITING: Record<string, string> = {
  window: "Fuera de horario",
  hourly: "Límite por hora",
  daily: "Límite del día",
  batch_rest: "Descanso",
  disconnected: "Cuenta desconectada",
};

/** «Fuera de horario», «Límite por hora»… ; `""` si no hay espera (o no se conoce el motivo). */
export function waitingLabel(waiting: string | null | undefined): string {
  return WAITING[String(waiting)] ?? "";
}

/** Lo último que avisó el motor (`campaign:progress`) y que la lista no trae. */
export interface CampaignLive {
  nextAt: number;
  waiting: string;
}

/** Dato del evento `campaign:progress` (Go: service.CampaignProgress). */
export interface CampaignProgressData {
  campaignId?: string;
  state?: string;
  totals?: CampaignTotals;
  nextAt?: number;
  waiting?: string;
  brakeReason?: string;
}

const EMPTY_TOTALS: CampaignTotals = { queued: 0, sent: 0, failed: 0, skipped: 0, replied: 0, optOut: 0 };

/**
 * Aplica un aviso de progreso a la lista. Devuelve `null` si la campaña no está en la lista
 * (la pantalla recarga) y, si no, la lista nueva y el dato en vivo de esa campaña.
 */
export function applyProgress(
  list: Campaign[],
  p: CampaignProgressData,
): { list: Campaign[]; live: CampaignLive } | null {
  if (!p?.campaignId) return null;
  const i = list.findIndex((c) => c.id === p.campaignId);
  if (i < 0) return null;
  const prev = list[i];
  const next: Campaign = {
    ...prev,
    state: (p.state as CampaignState | undefined) ?? prev.state,
    totals: p.totals ? { ...EMPTY_TOTALS, ...p.totals } : prev.totals,
    brakeReason: p.brakeReason ?? prev.brakeReason,
  };
  const copy = list.slice();
  copy[i] = next;
  return { list: copy, live: { nextAt: Number(p.nextAt) || 0, waiting: p.waiting ?? "" } };
}

/** `HH:mm` si es hoy; si no `dd/MM HH:mm`. */
export function formatWhen(ts: number, now: number = Date.now()): string {
  if (!ts) return "";
  const d = new Date(ts);
  const n = new Date(now);
  const sameDay = d.getFullYear() === n.getFullYear() && d.getMonth() === n.getMonth() && d.getDate() === n.getDate();
  return format(d, sameDay ? "HH:mm" : "dd/MM HH:mm");
}

/**
 * Línea de «qué pasa ahora» de una campaña: la espera, el próximo envío o el motivo del freno.
 * `text` vacío = nada que decir.
 */
export function statusLine(c: Campaign, live?: CampaignLive, now: number = Date.now()): { text: string; tone: Tone } {
  switch (c.state as string) {
    case "braked":
      return { text: `Frenada: ${c.brakeReason || "sin motivo"}`, tone: "danger" };
    case "running": {
      const w = waitingLabel(live?.waiting);
      if (w) return { text: w, tone: live?.waiting === "disconnected" ? "danger" : "warn" };
      if (live && live.nextAt > now) return { text: `Próximo envío ${formatWhen(live.nextAt, now)}`, tone: "muted" };
      return { text: "Enviando…", tone: "ok" };
    }
    case "ready":
      return c.startAt ? { text: `Inicia ${formatWhen(c.startAt, now)}`, tone: "info" } : { text: "", tone: "muted" };
    case "paused":
      return { text: "En pausa", tone: "warn" };
    default:
      return { text: "", tone: "muted" };
  }
}

/** Enviados / total (lo que falta + lo que ya salió, falló o se omitió) y su porcentaje. */
export function progressOf(t: CampaignTotals | null | undefined): { sent: number; total: number; pct: number } {
  const x = t ?? EMPTY_TOTALS;
  const total = x.queued + x.sent + x.failed + x.skipped;
  return { sent: x.sent, total, pct: total > 0 ? Math.round((x.sent / total) * 100) : 0 };
}

const ORDER: Record<string, number> = { running: 0, braked: 1, ready: 2, paused: 3, draft: 4, done: 5, canceled: 6 };

/** Activas primero; dentro de cada estado, la más reciente arriba. */
export function sortCampaigns(list: Campaign[]): Campaign[] {
  return list
    .slice()
    .sort(
      (a, b) =>
        (ORDER[String(a.state)] ?? 9) - (ORDER[String(b.state)] ?? 9) ||
        (b.updatedAt || b.createdAt) - (a.updatedAt || a.createdAt),
    );
}

/** Qué botones del detalle valen en cada estado. */
export function campaignActions(state: string): { pause: boolean; resume: boolean; cancel: boolean; edit: boolean; remove: boolean } {
  return {
    pause: state === "running" || state === "ready",
    resume: state === "paused" || state === "braked",
    cancel: state === "running" || state === "ready" || state === "paused" || state === "braked",
    edit: state === "draft" || state === "ready" || state === "paused",
    remove: state !== "running",
  };
}

// ---------------------------------------------------------------- cola

export type QueueTab = "pending" | "sent" | "failed" | "skipped";

export const QUEUE_TABS: Array<{ id: QueueTab; label: string; states: string[] }> = [
  { id: "pending", label: "Pendientes", states: ["pending", "held"] },
  { id: "sent", label: "Enviados", states: ["sent"] },
  { id: "failed", label: "Fallidos", states: ["failed"] },
  { id: "skipped", label: "Omitidos", states: ["skipped"] },
];

/** Cuántos hay en cada pestaña, según los totales de la campaña. */
export function queueCount(tab: QueueTab, t: CampaignTotals | null | undefined): number {
  const x = t ?? EMPTY_TOTALS;
  return tab === "pending" ? x.queued : tab === "sent" ? x.sent : tab === "failed" ? x.failed : x.skipped;
}

export const RISKS: Array<{ id: "warm" | "lukewarm" | "cold"; label: string; color: string }> = [
  { id: "warm", label: "Conocidos", color: "var(--ok)" },
  { id: "lukewarm", label: "Sin respuesta", color: "var(--warn)" },
  { id: "cold", label: "Fríos", color: "var(--danger)" },
];

export function riskInfo(risk: string): { label: string; color: string } {
  return RISKS.find((r) => r.id === risk) ?? { label: "—", color: "var(--muted)" };
}

/** Recuento por riesgo (siempre los tres). */
export function riskCounts(r: CampaignReview | null | undefined): Array<{ id: string; label: string; color: string; count: number }> {
  return RISKS.map((x) => ({ ...x, count: r?.risks?.[x.id] ?? 0 }));
}

const SKIPPED: Array<{ id: string; label: string }> = [
  { id: "optedOut", label: "Baja" },
  { id: "noIdentity", label: "Sin identidad" },
  { id: "frequency", label: "Frecuencia" },
];

/** Omitidos por motivo (siempre los tres). */
export function skippedCounts(r: CampaignReview | null | undefined): Array<{ id: string; label: string; count: number }> {
  return SKIPPED.map((x) => ({ ...x, count: r?.skipped?.[x.id] ?? 0 }));
}

export function skippedTotal(r: CampaignReview | null | undefined): number {
  return skippedCounts(r).reduce((a, x) => a + x.count, 0);
}

export function levelTone(level: string | null | undefined): Tone {
  return level === "red" ? "danger" : level === "yellow" ? "warn" : level === "green" ? "ok" : "muted";
}

export const LEVEL_LABEL: Record<string, string> = {
  green: "Todo en orden",
  yellow: "Revisa los avisos",
  red: "Corrige antes de lanzar",
};

/** «≈ 2 h 5 min», «≈ 3 días», «menos de 1 min». */
export function formatEstimate(e: Estimate | null | undefined): string {
  if (!e) return "";
  if (e.days >= 2) return `≈ ${e.days} días`;
  const s = Math.max(0, e.seconds);
  if (s < 60) return "menos de 1 min";
  const totalMin = Math.round(s / 60);
  if (totalMin < 60) return `≈ ${totalMin} min`;
  const h = Math.floor(totalMin / 60);
  const m = totalMin % 60;
  return m === 0 ? `≈ ${h} h` : `≈ ${h} h ${m} min`;
}

// ---------------------------------------------------------------- ventana horaria y días

export function minutesToHHmm(min: number): string {
  const m = Math.min(1439, Math.max(0, Math.round(Number(min) || 0)));
  return `${String(Math.floor(m / 60)).padStart(2, "0")}:${String(m % 60).padStart(2, "0")}`;
}

/** Minutos desde las 00:00 de un `HH:mm`, o `null` si no vale. */
export function hhmmToMinutes(value: string): number | null {
  const m = /^(\d{1,2}):(\d{2})$/.exec((value ?? "").trim());
  if (!m) return null;
  const h = Number(m[1]);
  const mi = Number(m[2]);
  if (h > 23 || mi > 59) return null;
  return h * 60 + mi;
}

/** Días de la semana en el orden en que se muestran (lunes primero). `id`: 0 = domingo … 6 = sábado. */
export const WEEKDAYS: Array<{ id: number; short: string; label: string }> = [
  { id: 1, short: "L", label: "Lun" },
  { id: 2, short: "M", label: "Mar" },
  { id: 3, short: "X", label: "Mié" },
  { id: 4, short: "J", label: "Jue" },
  { id: 5, short: "V", label: "Vie" },
  { id: 6, short: "S", label: "Sáb" },
  { id: 0, short: "D", label: "Dom" },
];

const DAY_POS = (id: number) => WEEKDAYS.findIndex((d) => d.id === id);

/** Pone o quita un día y deja la lista en el orden de la semana (lunes primero). */
export function toggleWeekday(days: number[], id: number): number[] {
  const set = new Set(days);
  if (set.has(id)) set.delete(id);
  else set.add(id);
  return Array.from(set).sort((a, b) => DAY_POS(a) - DAY_POS(b));
}

/** «Todos los días», «Lun a Vie», «Lun, Mié, Sáb». */
export function weekdaysLabel(days: number[] | null | undefined): string {
  const list = Array.from(new Set((days ?? []).filter((d) => DAY_POS(d) >= 0))).sort((a, b) => DAY_POS(a) - DAY_POS(b));
  if (list.length === 0) return "Ningún día";
  if (list.length === 7) return "Todos los días";
  const pos = list.map(DAY_POS);
  const consecutive = pos.every((p, i) => i === 0 || p === pos[i - 1] + 1);
  const name = (id: number) => WEEKDAYS[DAY_POS(id)].label;
  if (consecutive && list.length >= 3) return `${name(list[0])} a ${name(list[list.length - 1])}`;
  return list.map(name).join(", ");
}

/** «09:00–18:00 · Lun a Vie». */
export function windowSummary(w: SendWindow | null | undefined): string {
  if (!w) return "";
  return `${minutesToHHmm(w.fromMinute)}–${minutesToHHmm(w.toMinute)} · ${weekdaysLabel(w.weekdays)}`;
}

// ---------------------------------------------------------------- formulario del asistente

export interface CampaignForm {
  /** Vacío = campaña nueva. */
  id: string;
  name: string;
  accountId: string;
  listIds: string[];
  contactIds: string[];
  text: string;
  /** Variantes B…E (hasta 4). */
  variants: string[];
  filePath: string;
  fileName: string;
  optOutFooter: boolean;
  startMode: "now" | "later";
  /** Valor de `datetime-local`. */
  startAt: string;
  fromTime: string;
  toTime: string;
  weekdays: number[];
  abEnabled: boolean;
}

export const MAX_EXTRA_VARIANTS = 4;
export const VARIANT_LETTERS = ["A", "B", "C", "D", "E"] as const;
export const DEFAULT_WINDOW = { from: "09:00", to: "18:00", weekdays: [1, 2, 3, 4, 5] };

/** Una hora a partir de `now`, a los 5 minutos siguientes. */
export function defaultStart(now: number = Date.now()): string {
  const step = 5 * 60_000;
  return toLocalInput(Math.ceil((now + 60 * 60_000) / step) * step);
}

export function emptyForm(now: number = Date.now()): CampaignForm {
  return {
    id: "",
    name: "",
    accountId: "",
    listIds: [],
    contactIds: [],
    text: "",
    variants: [],
    filePath: "",
    fileName: "",
    optOutFooter: true,
    startMode: "now",
    startAt: defaultStart(now),
    fromTime: DEFAULT_WINDOW.from,
    toTime: DEFAULT_WINDOW.to,
    weekdays: [...DEFAULT_WINDOW.weekdays],
    abEnabled: false,
  };
}

export function baseName(path: string): string {
  const parts = (path ?? "").split(/[\\/]/);
  return parts[parts.length - 1] ?? "";
}

export function formFromCampaign(c: Campaign, now: number = Date.now()): CampaignForm {
  const w = c.window;
  return {
    id: c.id,
    name: c.name,
    accountId: String(c.accountId),
    listIds: [...(c.listIds ?? [])],
    contactIds: [...(c.contactIds ?? [])],
    text: c.text ?? "",
    variants: [...(c.variants ?? [])],
    filePath: c.filePath ?? "",
    fileName: baseName(c.filePath ?? ""),
    optOutFooter: !!c.optOutFooter,
    startMode: c.startAt > 0 ? "later" : "now",
    startAt: c.startAt > 0 ? toLocalInput(c.startAt) : defaultStart(now),
    fromTime: minutesToHHmm(w?.fromMinute ?? 540),
    toTime: minutesToHHmm(w?.toMinute ?? 1080),
    weekdays: w?.weekdays?.length ? toggleSorted(w.weekdays) : [...DEFAULT_WINDOW.weekdays],
    abEnabled: !!c.abEnabled,
  };
}

function toggleSorted(days: number[]): number[] {
  return Array.from(new Set(days)).sort((a, b) => DAY_POS(a) - DAY_POS(b));
}

/** Variantes con texto (en el orden escrito). */
export function filledVariants(f: Pick<CampaignForm, "variants">): string[] {
  return f.variants.map((v) => v.trim()).filter(Boolean);
}

/** Cuántos mensajes distintos hay escritos: A (si tiene texto) + las variantes con texto. */
export function filledTexts(f: Pick<CampaignForm, "text" | "variants">): number {
  return (f.text.trim() ? 1 : 0) + filledVariants(f).length;
}

/** Una `Campaign` lista para `Save`/`Review`. `base` conserva estado y marcas de una campaña existente. */
export function campaignFromForm(f: CampaignForm, pacing: PacingSettings, base?: Campaign | null): Campaign {
  const from = hhmmToMinutes(f.fromTime) ?? 540;
  const to = hhmmToMinutes(f.toTime) ?? 1080;
  const startAt = f.startMode === "later" ? (parseLocalInput(f.startAt) ?? 0) : 0;
  return {
    id: f.id,
    name: f.name.trim(),
    accountId: f.accountId,
    state: base?.state ?? CampaignState.CampaignDraft,
    text: f.text,
    variants: filledVariants(f),
    filePath: f.filePath,
    listIds: [...f.listIds],
    contactIds: [...f.contactIds],
    startAt,
    window: { fromMinute: from, toMinute: to, weekdays: toggleSorted(f.weekdays) },
    pacing,
    optOutFooter: f.optOutFooter,
    abEnabled: f.abEnabled,
    brakeReason: base?.brakeReason ?? "",
    createdAt: base?.createdAt ?? 0,
    updatedAt: base?.updatedAt ?? 0,
    totals: base?.totals ?? { ...EMPTY_TOTALS },
  };
}

export type Step = 1 | 2 | 3 | 4 | 5;
export const STEPS: Array<{ id: Step; label: string }> = [
  { id: 1, label: "Nombre y cuenta" },
  { id: 2, label: "Audiencia" },
  { id: 3, label: "Mensaje" },
  { id: 4, label: "Envío" },
  { id: 5, label: "Resumen" },
];

/** Qué falta en un paso (`null` = completo). El paso 5 no tiene campos propios. */
export function validateStep(step: Step, f: CampaignForm, now: number = Date.now()): string | null {
  switch (step) {
    case 1:
      if (!f.name.trim()) return "Ponle un nombre";
      if (!f.accountId) return "Elige una cuenta";
      return null;
    case 2:
      if (f.listIds.length + f.contactIds.length === 0) return "Elige una lista o un contacto";
      return null;
    case 3:
      if (!f.text.trim() && !f.filePath) return "Escribe el mensaje o adjunta un archivo";
      return null;
    case 4: {
      const from = hhmmToMinutes(f.fromTime);
      const to = hhmmToMinutes(f.toTime);
      if (from === null || to === null) return "Revisa la ventana horaria";
      if (from >= to) return "La hora de inicio debe ser menor que la de fin";
      if (f.weekdays.length === 0) return "Elige al menos un día";
      if (f.startMode === "later") {
        const at = parseLocalInput(f.startAt);
        if (at === null) return "Elige fecha y hora de inicio";
        if (at <= now) return "El inicio ya pasó";
      }
      if (f.abEnabled && filledTexts(f) < 2) return "La prueba A/B necesita al menos dos mensajes";
      return null;
    }
    default:
      return null;
  }
}

/** Primer paso incompleto, o `null` si los cuatro están completos. */
export function firstInvalidStep(f: CampaignForm, now: number = Date.now()): Step | null {
  for (const s of [1, 2, 3, 4] as const) if (validateStep(s, f, now)) return s;
  return null;
}

/**
 * Todo lo que impide lanzar (vacío = se puede). Un nivel rojo de la revisión impide lanzar; sin
 * revisión (aún no llega o falló) también.
 */
export function launchBlockers(
  f: CampaignForm,
  review: CampaignReview | null | undefined,
  reviewing: boolean,
  now: number = Date.now(),
): string[] {
  const out: string[] = [];
  for (const s of [1, 2, 3, 4] as const) {
    const e = validateStep(s, f, now);
    if (e) out.push(e);
  }
  if (out.length > 0) return out;
  if (reviewing) return ["Revisando…"];
  if (!review) return ["Sin revisión"];
  if (review.level === "red") {
    const reds = (review.items ?? []).filter((i) => i.level === "red").map((i) => i.message);
    return reds.length > 0 ? reds : ["La revisión tiene errores"];
  }
  if (review.recipients <= 0) return ["Sin destinatarios"];
  return [];
}

/** Datos que cambian el resultado de `Review` (para no revisar al cambiar solo el nombre). */
export function reviewKey(f: CampaignForm, pacing: PacingSettings | null): string {
  return JSON.stringify([
    f.accountId,
    f.listIds,
    f.contactIds,
    f.text,
    f.variants,
    f.filePath,
    f.optOutFooter,
    f.abEnabled,
    f.startMode === "later" ? f.startAt : "",
    f.fromTime,
    f.toTime,
    f.weekdays,
    pacing,
  ]);
}

/** Vista previa del texto: `{nombre}` por un nombre de ejemplo y `{a|b}` por la primera opción. */
export function previewText(text: string, name = "Ana"): string {
  return (text ?? "")
    .replace(/\{nombre\}/gi, name)
    .replace(/\{([^{}|]+(?:\|[^{}|]*)+)\}/g, (_m, inner: string) => inner.split("|")[0].trim());
}
