// Conexión con el servidor: modo Local (por defecto) o Remoto, sesión, estado visible y «Probar».
// El escritorio entrega `baseUrl` y, en modo local, un token propio que se usa como Bearer sin pedir login.
import { create } from "zustand";
import { desktop, hasWails, onDesktopEvent, type ConnectionMode } from "./desktop";
import { rebindEvents, reconnectNow, setEventsHooks, startEvents, stopEvents, onEvent } from "./events";
import { ApiError, NETWORK_MESSAGE, requestJSON, setHttpHooks } from "./http";
import { refreshMediaToken, stopMediaToken } from "./media";
import {
  configureSession,
  loadStoredToken,
  normalizeBase,
  parseServerUrl,
  session,
  storeToken,
} from "./session";

/** Versión de la API que habla esta interfaz (docs/14: `apiVersion`). */
export const EXPECTED_API_VERSION = 1;
export const API_VERSION_WARNING = "Este servidor usa otra versión de la API.";

export type Link = "connecting" | "connected" | "reconnecting" | "offline";

export interface Health {
  status: string;
  version: string;
  apiVersion: number;
  protocol: number;
  tls: boolean;
}

interface LoginResponse {
  token: string;
  expiresAt: number;
  user: { id: string; username: string; role: string };
}

export interface ProbeResult {
  base: string;
  version: string;
  apiVersion: number;
  tls: boolean;
  user: string;
  token: string;
  /** Huella TLS si el escritorio la informa para esta dirección. */
  tlsFingerprint: string;
  warning: string;
}

/** Costuras del entorno (las pruebas las sustituyen). */
export const env = {
  reload: () => window.location.reload(),
};

/** Fases del servidor según el escritorio (evento `desktop:connection`). */
export type DesktopPhase = "starting" | "ready" | "restarting" | "stopped" | "disconnected" | "unavailable";

/** Carga del evento `desktop:connection`. */
export interface DesktopConnectionEvent {
  mode: ConnectionMode;
  state: DesktopPhase;
  error?: string;
  retryInMs?: number;
}

/** Último estado que informó el escritorio y cuándo llegó (para la cuenta atrás «Reintentando en N s»). */
export interface DesktopStatus extends DesktopConnectionEvent {
  at: number;
}

/** Texto del estado del servidor según el escritorio; "" cuando está listo. */
export function desktopPhaseText(d: Pick<DesktopConnectionEvent, "mode" | "state">): string {
  const who = d.mode === "remote" ? "servidor remoto" : "servidor local";
  switch (d.state) {
    case "starting":
      return `Iniciando el ${who}…`;
    case "restarting":
      return `Reiniciando el ${who}…`;
    case "stopped":
      return `El ${who} está detenido.`;
    case "disconnected":
      return `Se perdió la conexión con el ${who}.`;
    case "unavailable":
      return `El ${who} no está disponible.`;
    default:
      return "";
  }
}

interface ConnectionState {
  mode: ConnectionMode;
  baseUrl: string;
  user: string;
  serverVersion: string;
  apiVersion: number;
  tlsFingerprint: string;
  /** Aviso no bloqueante (API distinta). */
  warning: string;
  link: Link;
  /** Ya hubo una conexión buena: la interfaz puede mostrarse. */
  ready: boolean;
  /** El servidor pidió iniciar sesión (401 sin forma de renovarla). */
  needLogin: boolean;
  /** Último motivo de fallo, en español. */
  error: string;
  booting: boolean;
  /** Estado del servidor que informa el escritorio (solo en la app de escritorio). */
  desktop: DesktopStatus | null;

  boot: () => Promise<void>;
  /** «Reintentar»: vuelve a arrancar si nunca conectó; si no, reconecta el WebSocket ya. */
  retry: () => Promise<void>;
  login: (username: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  probe: (url: string, user: string, password: string) => Promise<ProbeResult>;
  saveRemote: (url: string, user: string, password: string, token?: string) => Promise<void>;
  useLocal: () => Promise<void>;
}

/** Credenciales de esta sesión (solo memoria) para renovar el token sin molestar. */
let creds: { user: string; password: string } | null = null;
let renewing: Promise<boolean> | null = null;

/** Olvida las credenciales de esta sesión (cerrar sesión, cambiar de servidor y pruebas). */
export function clearCredentials(): void {
  creds = null;
}

function errText(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  if (e instanceof Error && e.message) return e.message;
  return NETWORK_MESSAGE;
}

function loginRequest(base: string, username: string, password: string): Promise<LoginResponse> {
  return requestJSON<LoginResponse>("/auth/login", { method: "POST", json: { username, password }, auth: false, base });
}

function warningFor(h: Health): string {
  return h.apiVersion !== EXPECTED_API_VERSION ? API_VERSION_WARNING : "";
}

export const useConnection = create<ConnectionState>((set, get) => ({
  mode: "local",
  baseUrl: "",
  user: "",
  serverVersion: "",
  apiVersion: 0,
  tlsFingerprint: "",
  warning: "",
  link: "connecting",
  ready: false,
  needLogin: false,
  error: "",
  booting: false,
  desktop: null,

  boot: async () => {
    ensureDesktopListener();
    if (get().booting) return;
    set({ booting: true, link: "connecting", error: "", needLogin: false });
    try {
      let info;
      try {
        info = await desktop.connectionInfo();
      } catch (e) {
        set({ link: "offline", error: errText(e) || "No se pudo leer la configuración de conexión." });
        return;
      }
      const base = normalizeBase(info.baseUrl || "");
      configureSession({ baseUrl: base, token: info.token || loadStoredToken(base) });
      set({
        mode: info.mode,
        baseUrl: base,
        user: info.user ?? "",
        tlsFingerprint: info.tlsFingerprint ?? "",
        serverVersion: info.serverVersion ?? "",
      });

      let health: Health;
      try {
        health = await requestJSON<Health>("/health", { auth: false });
      } catch (e) {
        set({ link: "offline", error: errText(e) });
        return;
      }
      set({ serverVersion: health.version, apiVersion: health.apiVersion, warning: warningFor(health) });

      if (!session.token) {
        set({ needLogin: true });
        return;
      }
      try {
        const me = await requestJSON<{ username: string }>("/auth/me");
        set({ user: me.username || get().user });
      } catch (e) {
        if (e instanceof ApiError && e.code === "unauthorized") return; // el gancho ya pidió el login
        set({ link: "offline", error: errText(e) });
        return;
      }
      await refreshMediaToken();
      set({ ready: true, link: "connected", error: "" });
    } finally {
      set({ booting: false });
    }
  },

  retry: async () => {
    if (!get().ready) {
      await get().boot();
      return;
    }
    set({ error: "" });
    reconnectNow();
  },

  login: async (username, password) => {
    const base = session.baseUrl;
    const r = await loginRequest(base, username.trim(), password);
    configureSession({ token: r.token });
    storeToken(base, r.token);
    creds = { user: username.trim(), password };
    set({ needLogin: false, user: r.user?.username || username.trim(), error: "" });
    if (get().ready) {
      await refreshMediaToken();
      reconnectNow();
    } else {
      await get().boot();
    }
  },

  logout: async () => {
    try {
      await requestJSON("/auth/logout", { method: "POST" });
    } catch {
      /* sin red o ya caducó: igual se cierra aquí */
    }
    creds = null;
    storeToken(session.baseUrl, "");
    configureSession({ token: "" });
    stopEvents();
    stopMediaToken();
    set({ needLogin: true, ready: false, link: "connecting" });
  },

  probe: async (url, user, password) => {
    const base = parseServerUrl(url);
    if (!base) throw new ApiError("invalid", "La dirección no es válida.");
    const health = await requestJSON<Health>("/health", { auth: false, base });
    const login = await loginRequest(base, user.trim(), password);
    let fp = "";
    try {
      const info = await desktop.connectionInfo();
      if (info.mode === "remote" && normalizeBase(info.baseUrl) === base) fp = info.tlsFingerprint ?? "";
    } catch {
      /* sin escritorio */
    }
    return {
      base,
      version: health.version,
      apiVersion: health.apiVersion,
      tls: !!health.tls,
      user: login.user?.username || user.trim(),
      token: login.token,
      tlsFingerprint: fp,
      warning: warningFor(health),
    };
  },

  saveRemote: async (url, user, password, token) => {
    const base = parseServerUrl(url);
    if (!base) throw new ApiError("invalid", "La dirección no es válida.");
    await desktop.saveRemote({ url: base, user: user.trim(), password });
    if (token) storeToken(base, token);
    creds = { user: user.trim(), password };
    env.reload();
  },

  useLocal: async () => {
    await desktop.useLocal();
    creds = null;
    storeToken(session.baseUrl, "");
    env.reload();
  },
}));

/**
 * Llegó un 401. Orden: (1) el escritorio puede tener un token nuevo (modo local), (2) credenciales de esta
 * sesión, (3) pedir iniciar sesión. Las llamadas simultáneas comparten el mismo intento.
 */
export function renewSession(): Promise<boolean> {
  if (renewing) return renewing;
  renewing = (async () => {
    try {
      const info = await desktop.connectionInfo();
      if (info.token && info.token !== session.token && normalizeBase(info.baseUrl) === session.baseUrl) {
        configureSession({ token: info.token });
        return true;
      }
    } catch {
      /* sin escritorio */
    }
    if (creds) {
      try {
        const r = await loginRequest(session.baseUrl, creds.user, creds.password);
        configureSession({ token: r.token });
        storeToken(session.baseUrl, r.token);
        return true;
      } catch {
        /* la clave cambió o el servidor no responde */
      }
    }
    storeToken(session.baseUrl, "");
    configureSession({ token: "" });
    stopMediaToken();
    useConnection.setState({ needLogin: true });
    return false;
  })().finally(() => {
    renewing = null;
  });
  return renewing;
}

setHttpHooks({ onUnauthorized: renewSession });
setEventsHooks({ onUnauthorized: renewSession });

// El WebSocket manda el estado del enlace; el store lo refleja y, al volver, renueva el media-token.
onEvent("connection", (ev) => {
  const was = useConnection.getState().link;
  useConnection.setState({ link: ev.state });
  if (ev.state === "connected" && was !== "connected") void refreshMediaToken();
});

// ------------------------------------------------------------------ escritorio

/** Evita pedir `ConnectionInfo` dos veces a la vez cuando llegan varios eventos seguidos. */
let rebinding: Promise<void> | null = null;

/**
 * El escritorio dice que el servidor está listo (arrancó, se reinició o cambió de modo): vuelve a pedir
 * `ConnectionInfo` (el puerto del servidor local cambia al reiniciarlo) y re-apunta el cliente HTTP y el WebSocket
 * sin recargar la ventana. Solo recarga si cambió el modo (local ↔ remoto) o si algo falla de forma inesperada.
 */
export function rebindToDesktop(): Promise<void> {
  if (rebinding) return rebinding;
  rebinding = (async () => {
    const st = useConnection.getState();
    if (!st.ready) {
      if (!st.booting) await st.boot();
      return;
    }
    try {
      const info = await desktop.connectionInfo();
      const base = normalizeBase(info.baseUrl || "");
      const serverChanged = base !== session.baseUrl;
      // Otro servidor: jamás se lleva el token (ni la clave, ni el media-token) del anterior.
      const token = info.token || (serverChanged ? loadStoredToken(base) : session.token);
      const changed = serverChanged || token !== session.token;
      if (serverChanged) {
        clearCredentials();
        stopMediaToken();
      }
      const modeChanged = info.mode !== useConnection.getState().mode;
      configureSession({ baseUrl: base, token });
      if (serverChanged && !token) useConnection.setState({ needLogin: true });
      useConnection.setState({
        mode: info.mode,
        baseUrl: base,
        user: info.user ?? useConnection.getState().user,
        tlsFingerprint: info.tlsFingerprint ?? "",
        serverVersion: info.serverVersion ?? useConnection.getState().serverVersion,
        error: "",
      });
      if (modeChanged) {
        env.reload();
        return;
      }
      await refreshMediaToken();
      if (changed) rebindEvents();
      else reconnectNow();
    } catch (e) {
      // Sin dirección nueva no hay nada que re-apuntar: se muestra el motivo y la banda deja reintentar.
      useConnection.setState({ error: errText(e) });
    }
  })().finally(() => {
    rebinding = null;
  });
  return rebinding;
}

/** Procesa `desktop:connection`. Exportado para las pruebas. */
export function handleDesktopEvent(ev: DesktopConnectionEvent): void {
  if (!ev || typeof ev.state !== "string") return;
  useConnection.setState({ desktop: { ...ev, at: Date.now() } });
  const st = useConnection.getState();
  if (ev.state === "ready") {
    void rebindToDesktop();
    return;
  }
  if (st.ready || st.booting) return; // con la interfaz abierta basta la banda; mientras arranca, `boot` manda
  if (ev.state === "starting" || ev.state === "restarting") {
    useConnection.setState({ link: "connecting", error: "" });
  } else {
    useConnection.setState({ link: "offline", error: ev.error || desktopPhaseText(ev) });
  }
}

let offDesktop: (() => void) | null = null;

/** Se suscribe (una vez) al evento del escritorio; fuera de Wails no hace nada. */
export function ensureDesktopListener(): void {
  if (offDesktop || !hasWails()) return;
  offDesktop = onDesktopEvent<DesktopConnectionEvent>("desktop:connection", handleDesktopEvent);
}

/** Cancela la suscripción (pruebas). */
export function stopDesktopListener(): void {
  offDesktop?.();
  offDesktop = null;
}

/** Abre el flujo en vivo (una vez que la interfaz ya está lista). */
export function startLive(): () => void {
  return startEvents();
}

/** ¿Estamos hablando con un servidor remoto? (`false` también cuando aún no se sabe.) */
export function isRemote(): boolean {
  return useConnection.getState().mode === "remote";
}

export { hasWails };
