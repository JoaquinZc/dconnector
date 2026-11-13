// Conversión ajustes ↔ formulario de Ajustes (06 §12, `AppSettings`).
import type { AppSettings } from "@/api/types";

export type ThemeId = "system" | "light" | "dark";
export const THEMES: Array<{ id: ThemeId; label: string }> = [
  { id: "system", label: "Del sistema" },
  { id: "light", label: "Claro" },
  { id: "dark", label: "Oscuro" },
];

export type AutoKind = "image" | "audio" | "voice" | "video" | "document" | "sticker";

export const AUTO_KINDS: Array<{ id: AutoKind; label: string }> = [
  { id: "image", label: "Fotos" },
  { id: "audio", label: "Audios" },
  { id: "voice", label: "Notas de voz" },
  { id: "video", label: "Vídeos" },
  { id: "document", label: "Documentos" },
  { id: "sticker", label: "Stickers" },
];

export interface AutoRule {
  enabled: boolean;
  maxMB: number;
}

export type AutoDownload = Record<AutoKind, AutoRule>;

/** Los mismos valores que `defaultsAutoDownload` del servicio. */
export const DEFAULT_AUTO_DOWNLOAD: AutoDownload = {
  image: { enabled: true, maxMB: 20 },
  audio: { enabled: true, maxMB: 20 },
  voice: { enabled: true, maxMB: 20 },
  video: { enabled: false, maxMB: 50 },
  document: { enabled: false, maxMB: 20 },
  sticker: { enabled: true, maxMB: 5 },
};

export const MAX_MB_LIMIT = 2048;

export interface SettingsForm {
  closeToTray: boolean;
  startWithWindows: boolean;
  startMinimized: boolean;
  notifications: boolean;
  /** El aviso de un mensaje nuevo suena (`AppSettings.notificationsSound`). */
  notificationsSound: boolean;
  /** La notificación muestra el texto del mensaje (`AppSettings.notificationsPreview`). */
  notificationsPreview: boolean;
  theme: ThemeId;
  verboseLog: boolean;
  /** Guardar el historial solo, despacio y en segundo plano (`AppSettings.autoHistory`). */
  autoHistory: boolean;
  autoDownload: AutoDownload;
  optOutKeywords: string;
  optOutFooterText: string;
}

export const DEFAULT_SETTINGS_FORM: SettingsForm = {
  closeToTray: true,
  startWithWindows: false,
  startMinimized: false,
  notifications: true,
  notificationsSound: true,
  notificationsPreview: true,
  theme: "system",
  verboseLog: false,
  autoHistory: true,
  autoDownload: DEFAULT_AUTO_DOWNLOAD,
  optOutKeywords: "",
  optOutFooterText: "",
};

const isObj = (v: unknown): v is Record<string, unknown> => !!v && typeof v === "object" && !Array.isArray(v);

export function asTheme(v: unknown): ThemeId {
  return v === "light" || v === "dark" ? v : "system";
}

function toRule(raw: unknown, fallback: AutoRule): AutoRule {
  if (!isObj(raw)) return { ...fallback };
  const mb = typeof raw.maxMB === "number" ? raw.maxMB : Number(raw.maxMB);
  return {
    enabled: typeof raw.enabled === "boolean" ? raw.enabled : fallback.enabled,
    maxMB: Number.isFinite(mb) && mb > 0 ? mb : fallback.maxMB,
  };
}

/** `autoDownload` del backend → un valor por cada tipo; lo que falte toma el valor por defecto. */
export function readAutoDownload(raw: unknown): AutoDownload {
  const src = isObj(raw) ? raw : {};
  const out = {} as AutoDownload;
  for (const { id } of AUTO_KINDS) out[id] = toRule(src[id], DEFAULT_AUTO_DOWNLOAD[id]);
  return out;
}

export function toSettingsForm(s: Partial<AppSettings> | null | undefined): SettingsForm {
  const b = DEFAULT_SETTINGS_FORM;
  return {
    closeToTray: s?.closeToTray ?? b.closeToTray,
    startWithWindows: s?.startWithWindows ?? b.startWithWindows,
    startMinimized: s?.startMinimized ?? b.startMinimized,
    notifications: s?.notifications ?? b.notifications,
    notificationsSound: s?.notificationsSound ?? b.notificationsSound,
    notificationsPreview: s?.notificationsPreview ?? b.notificationsPreview,
    theme: asTheme(s?.theme),
    verboseLog: s?.verboseLog ?? b.verboseLog,
    autoHistory: s?.autoHistory ?? b.autoHistory,
    autoDownload: readAutoDownload(s?.autoDownload),
    optOutKeywords: s?.optOutKeywords ?? "",
    optOutFooterText: s?.optOutFooterText ?? "",
  };
}

/** Palabras separadas por comas o saltos de línea, sin vacías ni repetidas (sin distinguir mayúsculas). */
export function normalizeKeywords(text: string): string {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const part of (text ?? "").split(/[,\n;]/)) {
    const w = part.trim();
    const k = w.toLowerCase();
    if (!w || seen.has(k)) continue;
    seen.add(k);
    out.push(w);
  }
  return out.join(",");
}

/**
 * Formulario → `AppSettings`. Si se pasa el `AppSettings` de partida se conservan los tipos de
 * descarga que la interfaz no conoce.
 */
export function toAppSettings(f: SettingsForm, base?: Partial<AppSettings> | null): AppSettings {
  const extra = isObj(base?.autoDownload) ? base.autoDownload : {};
  const auto: Record<string, unknown> = { ...extra };
  for (const { id } of AUTO_KINDS) {
    auto[id] = { enabled: !!f.autoDownload[id].enabled, maxMB: Math.round(f.autoDownload[id].maxMB) };
  }
  return {
    closeToTray: f.closeToTray,
    startWithWindows: f.startWithWindows,
    startMinimized: f.startMinimized,
    notifications: f.notifications,
    // Siempre se envían: un `Save` sin estos campos los dejaría apagados.
    notificationsSound: !!f.notificationsSound,
    notificationsPreview: !!f.notificationsPreview,
    theme: f.theme,
    verboseLog: f.verboseLog,
    // Siempre se envía: un `Save` sin este campo lo dejaría apagado.
    autoHistory: !!f.autoHistory,
    autoDownload: auto,
    optOutKeywords: normalizeKeywords(f.optOutKeywords),
    optOutFooterText: f.optOutFooterText.trim(),
  };
}

/** ¿Difieren (ya normalizados)? */
export function isDirty(a: SettingsForm, b: SettingsForm): boolean {
  return JSON.stringify(toAppSettings(a)) !== JSON.stringify(toAppSettings(b));
}

/** Mensaje de lo que impide guardar; `null` si todo vale. */
export function validateSettings(f: SettingsForm): string | null {
  for (const { id, label } of AUTO_KINDS) {
    const mb = f.autoDownload[id].maxMB;
    if (!Number.isFinite(mb) || mb < 1 || mb > MAX_MB_LIMIT) return `${label}: el tamaño máximo va de 1 a ${MAX_MB_LIMIT} MB`;
  }
  return null;
}

/** Borrador guardado en `localStorage` → formulario, ignorando lo que no sea válido. */
export function formFromDraft(raw: unknown): SettingsForm | null {
  if (!isObj(raw)) return null;
  const base = toSettingsForm(raw as Partial<AppSettings>);
  return { ...base, optOutKeywords: typeof raw.optOutKeywords === "string" ? raw.optOutKeywords : base.optOutKeywords };
}
