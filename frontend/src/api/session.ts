// Sesión HTTP en memoria: dirección del servidor, token de sesión y el id de esta conexión (`hello.connId`).
// Es un módulo plano (sin React) para que http.ts, events.ts y media.ts lo lean sin ciclos.

export interface Session {
  /** Origen del servidor sin barra final (p. ej. `http://127.0.0.1:51234`). Vacío = sin configurar. */
  baseUrl: string;
  /** Token de sesión (Bearer). En modo local es el token propio que entrega el escritorio. */
  token: string;
  /** `connId` del WebSocket actual; se manda en `X-DConnector-Conn`. */
  connId: string;
}

export const session: Session = { baseUrl: "", token: "", connId: "" };

export function configureSession(p: Partial<Session>): void {
  if (p.baseUrl !== undefined) session.baseUrl = normalizeBase(p.baseUrl);
  if (p.token !== undefined) session.token = p.token;
  if (p.connId !== undefined) session.connId = p.connId;
}

export function resetSession(): void {
  session.baseUrl = "";
  session.token = "";
  session.connId = "";
}

/** Quita espacios, la barra final y un `/api/v1` sobrante. */
export function normalizeBase(url: string): string {
  let u = url.trim();
  u = u.replace(/\/+$/, "");
  u = u.replace(/\/api\/v1$/i, "");
  return u.replace(/\/+$/, "");
}

/** Convierte lo que escribe la persona en un origen válido o `null` (se asume https si falta el esquema). */
export function parseServerUrl(input: string): string | null {
  const raw = input.trim();
  if (!raw) return null;
  const withScheme = /^[a-z][a-z0-9+.-]*:\/\//i.test(raw) ? raw : `https://${raw}`;
  try {
    const u = new URL(withScheme);
    if (u.protocol !== "http:" && u.protocol !== "https:") return null;
    if (!u.hostname) return null;
    return normalizeBase(u.origin + (u.pathname === "/" ? "" : u.pathname));
  } catch {
    return null;
  }
}

/** `http://x` → `ws://x`, `https://x` → `wss://x`. */
export function wsBase(baseUrl: string): string {
  return baseUrl.replace(/^http/i, (m) => (m.toLowerCase() === "https" ? "wss" : "ws"));
}

const TOKEN_KEY = "dc:session";

/** Token de la pestaña (solo sessionStorage): sobrevive a una recarga, no al cierre de la ventana. */
export function loadStoredToken(baseUrl: string): string {
  try {
    const raw = sessionStorage.getItem(TOKEN_KEY);
    if (!raw) return "";
    const j = JSON.parse(raw) as { baseUrl?: string; token?: string };
    return j.baseUrl === normalizeBase(baseUrl) && typeof j.token === "string" ? j.token : "";
  } catch {
    return "";
  }
}

export function storeToken(baseUrl: string, token: string): void {
  try {
    if (!token) sessionStorage.removeItem(TOKEN_KEY);
    else sessionStorage.setItem(TOKEN_KEY, JSON.stringify({ baseUrl: normalizeBase(baseUrl), token }));
  } catch {
    /* sin almacenamiento: no se recuerda */
  }
}

/** ¿La dirección es `http:` y NO es de esta misma máquina? Ahí la clave y el token viajan sin cifrar. */
export function isInsecureRemote(baseUrl: string): boolean {
  try {
    const u = new URL(baseUrl);
    if (u.protocol !== "http:") return false;
    const h = u.hostname.replace(/^\[|\]$/g, "").toLowerCase();
    return !(h === "localhost" || h.endsWith(".localhost") || h === "::1" || /^127\.\d+\.\d+\.\d+$/.test(h));
  } catch {
    return false;
  }
}

export const INSECURE_HTTP_WARNING =
  "Esta dirección usa http: la clave y los mensajes viajan sin cifrar. Usa https o una red de confianza.";
