// Puente con el escritorio (Wails). Solo tres servicios siguen por los enlaces de Wails: Files, Notifications y
// Desktop. Se llaman por nombre (`Call.ByName`) para no depender de `frontend/bindings` (que genera otro agente).
// Fuera de Wails (navegador, pruebas) `desktop` cae a `window.location.origin` y `localStorage`.
import { Call, Events } from "@wailsio/runtime";

const NS = "dconnector/internal/adapter/ui/wails";

export type ConnectionMode = "local" | "remote";

export interface ConnectionInfo {
  mode: ConnectionMode;
  baseUrl: string;
  token?: string;
  user?: string;
  tlsFingerprint?: string;
  serverVersion?: string;
}

export interface RemoteSettings {
  url: string;
  user: string;
  password: string;
}

export interface DesktopApi {
  connectionInfo(): Promise<ConnectionInfo>;
  saveRemote(r: RemoteSettings): Promise<void>;
  useLocal(): Promise<void>;
  keepRunningInBackground(): Promise<boolean>;
  setKeepRunningInBackground(on: boolean): Promise<void>;
  /** Fija la huella TLS del servidor remoto (confianza al primer uso). */
  tlsTrust(fingerprint: string): Promise<void>;
}

/** ¿Corre dentro de una ventana de Wails (WebView2/WKWebView)? */
export function hasWails(): boolean {
  if (typeof window === "undefined") return false;
  const w = window as unknown as {
    chrome?: { webview?: { postMessage?: unknown } };
    webkit?: { messageHandlers?: { external?: unknown } };
  };
  return !!(w.chrome?.webview?.postMessage || w.webkit?.messageHandlers?.external);
}

/** Llama a `dconnector/internal/adapter/ui/wails.<Servicio>Service.<Método>`. */
export function callWails<T = unknown>(service: string, method: string, args: unknown[] = []): Promise<T> {
  return Call.ByName(`${NS}.${service}Service.${method}`, ...args) as unknown as Promise<T>;
}

/**
 * Se suscribe a un evento que emite el escritorio por Wails (p. ej. `desktop:connection`). Fuera de Wails no hace
 * nada. Devuelve la función que cancela la suscripción.
 */
export function onDesktopEvent<T = unknown>(name: string, cb: (data: T) => void): () => void {
  if (!hasWails()) return () => undefined;
  try {
    const off = Events.On(name, (ev) => {
      const d = (ev as { data?: unknown }).data;
      if (d !== undefined) cb(d as T);
    });
    return () => {
      try {
        off();
      } catch {
        /* ya cancelada */
      }
    };
  } catch {
    return () => undefined;
  }
}

const LS_KEY = "dc:connection";
const LS_BG = "dc:keep-running";

interface StoredConn {
  mode?: ConnectionMode;
  url?: string;
  user?: string;
}

function readStored(): StoredConn {
  try {
    return JSON.parse(localStorage.getItem(LS_KEY) || "{}") as StoredConn;
  } catch {
    return {};
  }
}

function writeStored(v: StoredConn | null): void {
  try {
    if (v) localStorage.setItem(LS_KEY, JSON.stringify(v));
    else localStorage.removeItem(LS_KEY);
  } catch {
    /* sin almacenamiento */
  }
}

function origin(): string {
  return typeof window !== "undefined" && window.location?.origin && window.location.origin !== "null"
    ? window.location.origin
    : "";
}

/** Implementación para navegador puro (y pruebas): sin escritorio. */
export const browserDesktop: DesktopApi = {
  async connectionInfo() {
    const s = readStored();
    if (s.mode === "remote" && s.url) return { mode: "remote", baseUrl: s.url, user: s.user };
    return { mode: "local", baseUrl: origin() };
  },
  async saveRemote(r) {
    writeStored({ mode: "remote", url: r.url, user: r.user });
  },
  async useLocal() {
    writeStored(null);
  },
  async keepRunningInBackground() {
    try {
      return localStorage.getItem(LS_BG) === "1";
    } catch {
      return false;
    }
  },
  async setKeepRunningInBackground(on) {
    try {
      localStorage.setItem(LS_BG, on ? "1" : "0");
    } catch {
      /* sin almacenamiento */
    }
  },
  async tlsTrust() {
    /* el navegador gestiona sus propios certificados */
  },
};

/** Implementación sobre `DesktopService` (la escribe el agente E en el paquete Go `ui/wails`). */
export const wailsDesktop: DesktopApi = {
  connectionInfo: () => callWails<ConnectionInfo>("Desktop", "ConnectionInfo"),
  saveRemote: (r) => callWails<void>("Desktop", "SaveRemote", [r.url, r.user, r.password]),
  useLocal: () => callWails<void>("Desktop", "UseLocal"),
  keepRunningInBackground: () => callWails<boolean>("Desktop", "KeepRunningInBackground"),
  setKeepRunningInBackground: (on) => callWails<void>("Desktop", "SetKeepRunningInBackground", [on]),
  tlsTrust: (fp) => callWails<void>("Desktop", "TrustTLS", [fp]),
};

/** Elige en cada llamada: Wails si hay ventana de escritorio, si no el navegador. */
export const desktop: DesktopApi = {
  connectionInfo: () => (hasWails() ? wailsDesktop : browserDesktop).connectionInfo(),
  saveRemote: (r) => (hasWails() ? wailsDesktop : browserDesktop).saveRemote(r),
  useLocal: () => (hasWails() ? wailsDesktop : browserDesktop).useLocal(),
  keepRunningInBackground: () => (hasWails() ? wailsDesktop : browserDesktop).keepRunningInBackground(),
  setKeepRunningInBackground: (on) => (hasWails() ? wailsDesktop : browserDesktop).setKeepRunningInBackground(on),
  tlsTrust: (fp) => (hasWails() ? wailsDesktop : browserDesktop).tlsTrust(fp),
};
