// URLs de medios y avatares. Los modelos traen rutas relativas (`/media/<cuenta>/<id>`, `/avatar/<cuenta>/<archivo>`);
// `mediaUrl` las convierte en `{base}/api/v1/media/…?token=<media-token>`. El media-token vale 15 minutos y se
// renueva antes de que caduque. Es el ÚNICO sitio donde se arman esas URLs.
import { requestJSON } from "./http";
import { session } from "./session";

interface MediaToken {
  token: string;
  expiresAt: number;
}

let mediaToken = "";
/** Servidor al que pertenece `mediaToken` (un token de otro servidor no se manda a este). */
let mediaTokenBase = "";
let timer: ReturnType<typeof setTimeout> | null = null;
let generation = 0;

/** Margen antes de la caducidad para pedir uno nuevo. */
export const MEDIA_RENEW_MARGIN_MS = 2 * 60_000;
const MIN_RENEW_MS = 30_000;

export function currentMediaToken(): string {
  return mediaToken;
}

export function setMediaToken(token: string): void {
  mediaToken = token;
  mediaTokenBase = session.baseUrl;
}

export function stopMediaToken(): void {
  generation++;
  if (timer) clearTimeout(timer);
  timer = null;
  mediaToken = "";
  mediaTokenBase = "";
}

/** Pide un media-token y programa el siguiente. Un fallo no es grave: se reintenta en un minuto. */
export async function refreshMediaToken(): Promise<void> {
  const gen = ++generation;
  if (timer) clearTimeout(timer);
  timer = null;
  try {
    const t = await requestJSON<MediaToken>("/auth/media-token", { method: "POST" });
    if (gen !== generation) return;
    mediaToken = t.token;
    mediaTokenBase = session.baseUrl;
    const wait = Math.max(MIN_RENEW_MS, (t.expiresAt || 0) - Date.now() - MEDIA_RENEW_MARGIN_MS);
    timer = setTimeout(() => void refreshMediaToken(), wait);
  } catch {
    if (gen !== generation) return;
    timer = setTimeout(() => void refreshMediaToken(), 60_000);
  }
}

const ABSOLUTE = /^(?:[a-z][a-z0-9+.-]*:|\/\/)/i;
/** Lo único absoluto que se pinta: http(s), medios `data:` y `blob:`. Un `javascript:` o similar se descarta. */
const SAFE_ABSOLUTE = /^(?:https?:\/\/|\/\/|blob:|data:(?:image|audio|video)\/)/i;

function withToken(url: string): string {
  if (!mediaToken || mediaTokenBase !== session.baseUrl) return url;
  return `${url}${url.includes("?") ? "&" : "?"}token=${encodeURIComponent(mediaToken)}`;
}

/**
 * Ruta relativa de un modelo → URL absoluta del servidor con el media-token.
 * - vacío, `data:`, `blob:`, `http(s):` → tal cual;
 * - `/media/…` y `/avatar/…` → `{base}/api/v1/…?token=…`;
 * - `upload://<id>` (archivo subido) → `{base}/api/v1/files/<id>?token=…`;
 * - sin servidor configurado (p. ej. pruebas) → tal cual.
 */
export function mediaUrl(u: string | null | undefined): string {
  if (!u) return "";
  if (u.startsWith("upload://")) {
    if (!session.baseUrl) return u;
    return withToken(`${session.baseUrl}/api/v1/files/${encodeURIComponent(u.slice("upload://".length))}`);
  }
  if (ABSOLUTE.test(u)) return SAFE_ABSOLUTE.test(u) ? u : "";
  if (!session.baseUrl) return u;
  if (u.startsWith("/media/") || u.startsWith("/avatar/")) return withToken(`${session.baseUrl}/api/v1${u}`);
  return u;
}
