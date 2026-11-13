// Formato de fechas, tamaños y duraciones (todo en español).
import { differenceInCalendarDays, format } from "date-fns";
import { es } from "date-fns/locale";

/** HH:mm */
export function formatClock(ts: number): string {
  if (!ts) return "";
  return format(new Date(ts), "HH:mm", { locale: es });
}

/** dd/MM/yyyy HH:mm */
export function formatDateTime(ts: number): string {
  if (!ts) return "";
  return format(new Date(ts), "dd/MM/yyyy HH:mm", { locale: es });
}

/** dd/MM/yyyy */
export function formatDate(ts: number): string {
  if (!ts) return "";
  return format(new Date(ts), "dd/MM/yyyy", { locale: es });
}

/**
 * Hora en la lista de chats: hoy `HH:mm`, ayer «Ayer», dentro de la última semana el día
 * («lunes»), si no `dd/MM/yy`.
 */
export function formatListTime(ts: number, now: number = Date.now()): string {
  if (!ts) return "";
  const days = differenceInCalendarDays(new Date(now), new Date(ts));
  if (days <= 0) return format(new Date(ts), "HH:mm", { locale: es });
  if (days === 1) return "Ayer";
  if (days < 7) return format(new Date(ts), "EEEE", { locale: es });
  return format(new Date(ts), "dd/MM/yy", { locale: es });
}

/** Separador de día dentro de una conversación: «Hoy», «Ayer», «lunes», «3 de mayo de 2026». */
export function formatDayLabel(ts: number, now: number = Date.now()): string {
  if (!ts) return "";
  const days = differenceInCalendarDays(new Date(now), new Date(ts));
  if (days <= 0) return "Hoy";
  if (days === 1) return "Ayer";
  if (days < 7) return format(new Date(ts), "EEEE", { locale: es });
  return format(new Date(ts), "d 'de' MMMM 'de' yyyy", { locale: es });
}

/** Clave estable del día local (para agrupar mensajes por separador). */
export function dayKey(ts: number): string {
  return format(new Date(ts), "yyyy-MM-dd");
}

/** Tamaño de archivo: «512 B», «1,5 KB», «20 MB». */
export function formatFileSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return "";
  if (bytes < 1024) return `${Math.round(bytes)} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let v = bytes / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  const text = v >= 100 ? String(Math.round(v)) : v.toFixed(1).replace(/\.0$/, "").replace(".", ",");
  return `${text} ${units[i]}`;
}

/** Duración en segundos como `mm:ss` (`h:mm:ss` desde una hora). */
export function formatDuration(seconds: number): string {
  const total = Math.max(0, Math.floor(Number.isFinite(seconds) ? seconds : 0));
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  const ss = String(s).padStart(2, "0");
  if (h > 0) return `${h}:${String(m).padStart(2, "0")}:${ss}`;
  return `${String(m).padStart(2, "0")}:${ss}`;
}

/** Recorta con «…» (el texto completo debe ir en `title`). */
export function truncate(text: string, max: number): string {
  const chars = Array.from(text ?? "");
  if (chars.length <= max) return text ?? "";
  return chars.slice(0, Math.max(0, max - 1)).join("").trimEnd() + "…";
}

/** Entero con punto de miles (`12.345`), sin depender de la configuración regional del sistema. */
export function formatCount(n: number): string {
  const v = Math.max(0, Math.round(Number.isFinite(n) ? n : 0));
  return String(v).replace(/\B(?=(\d{3})+(?!\d))/g, ".");
}
