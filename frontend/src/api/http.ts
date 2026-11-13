// Cliente HTTP del servidor (docs/14): RPC, JSON suelto y descargas. Sin React ni stores.
import { session } from "./session";

export const API_PREFIX = "/api/v1";
export const CLIENT_NAME = "dconnector-ui/0.3.0";

/** Error de la API: `code` es el de `{error:{code}}` o `network` si no hubo respuesta. */
export class ApiError extends Error {
  readonly code: string;
  readonly status: number;
  /** Segundos de espera que pidió el servidor (429). */
  readonly retryAfter: number;
  constructor(code: string, message: string, status = 0, retryAfter = 0) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
    this.retryAfter = retryAfter;
  }
}

export const NETWORK_MESSAGE = "No se pudo conectar con el servidor.";

/** Ajustes del cliente (las pruebas los acortan). */
export const httpConfig = {
  /** Esperas entre reintentos de un 503 (servidor arrancando); la longitud es el número de reintentos. */
  retryDelays: [400, 1200] as number[],
  /**
   * Un 429 (demasiadas peticiones) lo da el servidor ANTES de ejecutar la llamada, así que repetirla es siempre
   * seguro, también un envío. Se espera lo que pida `Retry-After` si es corto; si es más largo, se avisa.
   */
  rateRetries: 2,
  /** Espera máxima (segundos) que se acepta para repetir un 429 en silencio. */
  rateMaxWaitSeconds: 15,
  /** Milisegundos por cada segundo de `Retry-After` (las pruebas lo reducen). */
  rateMsPerSecond: 1000,
};

/** Ganchos que registra el store de conexión (así este módulo no lo importa). */
export interface HttpHooks {
  /** Llegó un 401: devuelve `true` si se renovó la sesión y conviene reintentar. */
  onUnauthorized?: () => Promise<boolean>;
  /** No hubo respuesta (servidor caído, red cortada). */
  onNetworkError?: () => void;
  /** Hubo respuesta del servidor (aunque sea un error de negocio). */
  onResponse?: () => void;
}

let hooks: HttpHooks = {};
export function setHttpHooks(h: HttpHooks): void {
  hooks = h;
}

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

export function apiUrl(path: string, base = session.baseUrl): string {
  return `${base}${API_PREFIX}${path}`;
}

const STATUS_TEXT: Record<number, string> = {
  400: "La petición no es válida.",
  401: "Hay que iniciar sesión.",
  403: "No tienes permiso para hacer eso.",
  404: "No se encontró lo que se pidió.",
  409: "La cuenta no está conectada.",
  413: "El archivo es demasiado grande.",
  422: "Este canal no admite esa operación.",
  429: "Demasiados intentos. Espera un momento y vuelve a probar.",
  500: "El servidor tuvo un error inesperado.",
  503: "El servidor no está listo todavía.",
};

async function toApiError(res: Response): Promise<ApiError> {
  let code = "";
  let message = "";
  try {
    const j = (await res.json()) as { error?: { code?: string; message?: string } };
    code = j.error?.code ?? "";
    message = j.error?.message ?? "";
  } catch {
    /* sin cuerpo JSON */
  }
  if (!code) code = res.status === 401 ? "unauthorized" : res.status === 503 ? "unavailable" : "internal";
  const raw = Number(res.headers?.get?.("Retry-After") ?? 0);
  const ra = Number.isFinite(raw) && raw > 0 ? Math.min(Math.ceil(raw), 3600) : 0;
  if (!message) message = STATUS_TEXT[res.status] ?? "El servidor respondió con un error.";
  if (res.status === 429 && ra > 0 && !/\d/.test(message)) message = `${message.replace(/[.\s]+$/, "")} (espera ${ra} s).`;
  return new ApiError(code, message, res.status, ra);
}

export interface RequestOptions {
  method?: string;
  body?: BodyInit | null;
  headers?: Record<string, string>;
  /** `false` para rutas sin sesión (`/health`, `/auth/login`). */
  auth?: boolean;
  /** Origen alternativo (probar un servidor que aún no es el guardado). */
  base?: string;
  signal?: AbortSignal;
  /**
   * ¿Se puede repetir tras un 503? Por defecto solo GET/HEAD. Un envío (POST) NO se repite a ciegas: si el 503
   * llegó después de procesarlo, se duplicaría el mensaje.
   */
  retryOn503?: boolean;
}

/**
 * Hace la petición y devuelve la `Response` si fue 2xx. Si no: `ApiError`. Un 401 intenta renovar la sesión
 * una vez; un 503 se reintenta con espera; sin respuesta de red avisa al store de conexión.
 */
export async function request(path: string, opts: RequestOptions = {}): Promise<Response> {
  const auth = opts.auth !== false;
  let renewed = false;
  let rateRetries = 0;
  for (let attempt = 0; ; attempt++) {
    const headers: Record<string, string> = { "X-DConnector-Client": CLIENT_NAME, ...opts.headers };
    if (auth && session.token) headers.Authorization = `Bearer ${session.token}`;
    if (auth && session.connId) headers["X-DConnector-Conn"] = session.connId;

    let res: Response;
    try {
      res = await fetch(apiUrl(path, opts.base ?? session.baseUrl), {
        method: opts.method ?? "GET",
        headers,
        body: opts.body ?? null,
        signal: opts.signal,
      });
    } catch (e) {
      if (opts.signal?.aborted) throw e;
      hooks.onNetworkError?.();
      throw new ApiError("network", NETWORK_MESSAGE);
    }
    hooks.onResponse?.();
    if (res.ok) return res;

    const err = await toApiError(res);
    if (res.status === 401 && auth && !renewed && hooks.onUnauthorized) {
      renewed = true;
      if (await hooks.onUnauthorized()) continue;
      throw err;
    }
    const method = (opts.method ?? "GET").toUpperCase();
    const mayRetry = opts.retryOn503 ?? (method === "GET" || method === "HEAD");
    if (res.status === 503 && mayRetry && attempt < httpConfig.retryDelays.length) {
      await sleep(httpConfig.retryDelays[attempt]);
      continue;
    }
    // 429: el servidor no llegó a ejecutar nada, repetir es seguro (salvo el inicio de sesión, cuyo bloqueo es de seguridad).
    if (
      res.status === 429 &&
      !path.includes("/auth/login") &&
      err.retryAfter > 0 &&
      err.retryAfter <= httpConfig.rateMaxWaitSeconds &&
      rateRetries < httpConfig.rateRetries &&
      !opts.signal?.aborted
    ) {
      rateRetries++;
      await sleep(err.retryAfter * httpConfig.rateMsPerSecond + 50);
      continue;
    }
    throw err;
  }
}

/** JSON de ida y vuelta. Una respuesta vacía es `null`. */
export async function requestJSON<T>(path: string, opts: RequestOptions & { json?: unknown } = {}): Promise<T> {
  const { json, ...rest } = opts;
  const headers = { ...rest.headers };
  let body = rest.body;
  if (json !== undefined) {
    headers["Content-Type"] = "application/json";
    body = JSON.stringify(json);
  }
  const res = await request(path, { ...rest, headers, body });
  const text = await res.text();
  return (text ? JSON.parse(text) : null) as T;
}

/** Métodos RPC de solo lectura: repetirlos tras un 503 no tiene efectos. El resto (Send, Forward, Save…) no se repite. */
const READ_ONLY_RPC =
  /^(List|Lists|ListChats|ListForChat|Get|GetChat|GetPacing|Messages|Search|States|Stats|Status|StatusAll|Version|DataDir|ProtocolDoc|Capabilities|Authors|ByAuthor|Mine|Viewers|Queue|Review|PreviewRecurrence|UnreadTotals|Chat|Conversations|Groups|SuggestMerges)$/;

export function isReadOnlyRpc(method: string): boolean {
  return READ_ONLY_RPC.test(method);
}

/** `POST /rpc/{servicio}/{método}` con el array de argumentos. */
export function rpcCall<T = unknown>(service: string, method: string, args: unknown[]): Promise<T> {
  return requestJSON<T>(`/rpc/${encodeURIComponent(service)}/${encodeURIComponent(method)}`, {
    method: "POST",
    json: args,
    retryOn503: isReadOnlyRpc(method),
  });
}
