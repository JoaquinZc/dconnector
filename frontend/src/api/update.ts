// Actualizaciones de la aplicación (escritorio). Se llama por nombre a `UpdateService` de Wails; fuera de Wails
// (navegador, pruebas) todo responde «no disponible». El estado vive en un store pequeño que escuchan el aviso
// y la sección de Ajustes: el escritorio emite `update:state` (estado completo) y `update:progress`.
import { create } from "zustand";
import { callWails, hasWails, onDesktopEvent } from "./desktop";

export type UpdateKind = "msi" | "portable" | "other";
export type UpdateState =
  | "idle"
  | "checking"
  | "uptodate"
  | "available"
  | "downloading"
  | "verifying"
  | "ready"
  | "installing"
  | "error";

export interface UpdateRelease {
  version: string;
  notes: string;
  url: string;
  assetName: string;
  sizeBytes: number;
}

export interface UpdateProgress {
  bytes: number;
  total: number;
  phase: "checking" | "downloading" | "verifying" | "ready" | "installing" | "error" | "";
}

export interface UpdateStatus {
  currentVersion: string;
  installKind: UpdateKind;
  canInstall: boolean;
  /** Milisegundos Unix; 0 = nunca. */
  lastCheck: number;
  autoCheck: boolean;
  latest?: UpdateRelease | null;
  state: UpdateState;
  progress: UpdateProgress;
  error?: string;
  folder?: string;
}

export interface UpdateApi {
  status(): Promise<UpdateStatus>;
  check(): Promise<UpdateStatus>;
  apply(): Promise<void>;
  setAutoCheck(on: boolean): Promise<void>;
  openReleasePage(): Promise<void>;
  openUpdatesFolder(): Promise<void>;
}

export const UNAVAILABLE = "Las actualizaciones solo están disponibles en la aplicación de escritorio.";

const unavailable = () => Promise.reject(new Error(UNAVAILABLE));

/** API tipada sobre `UpdateService`; fuera de Wails rechaza con «no disponible». */
export const updateApi: UpdateApi = {
  status: () => (hasWails() ? callWails<UpdateStatus>("Update", "Status") : unavailable()),
  check: () => (hasWails() ? callWails<UpdateStatus>("Update", "Check") : unavailable()),
  apply: () => (hasWails() ? callWails<void>("Update", "Apply") : unavailable()),
  setAutoCheck: (on) => (hasWails() ? callWails<void>("Update", "SetAutoCheck", [on]) : unavailable()),
  openReleasePage: () => (hasWails() ? callWails<void>("Update", "OpenReleasePage") : unavailable()),
  openUpdatesFolder: () => (hasWails() ? callWails<void>("Update", "OpenUpdatesFolder") : unavailable()),
};

/** Texto legible de un error (de Wails llega a veces como JSON). */
export function updateErrorText(e: unknown): string {
  let msg = "";
  if (typeof e === "string") msg = e;
  else if (e instanceof Error) msg = e.message;
  else if (e && typeof e === "object" && "message" in e) msg = String((e as { message: unknown }).message);
  msg = msg.trim();
  if (msg.startsWith("{")) {
    try {
      const j = JSON.parse(msg) as { message?: string; cause?: { message?: string } };
      msg = (j.cause?.message || j.message || msg).trim();
    } catch {
      /* no era JSON */
    }
  }
  return msg || "Ocurrió un error inesperado";
}

interface UpdateStore {
  /** ¿Hay escritorio? Falso en el navegador. */
  available: boolean;
  loaded: boolean;
  status: UpdateStatus | null;
  /** Versiones cuyo aviso ya se cerró en esta sesión. */
  dismissed: string[];
  /** Error de una llamada propia (p. ej. «Buscar» sin red) cuando el estado no lo trae. */
  error: string;
  init: () => void;
  check: () => Promise<void>;
  apply: () => Promise<void>;
  setAutoCheck: (on: boolean) => Promise<void>;
  dismiss: (version: string) => void;
}

let started = false;
let offState: (() => void) | null = null;
let offProgress: (() => void) | null = null;

export const useUpdate = create<UpdateStore>((set, get) => ({
  available: hasWails(),
  loaded: false,
  status: null,
  dismissed: [],
  error: "",
  init: () => {
    if (started) return;
    started = true;
    if (!hasWails()) {
      set({ available: false, loaded: true });
      return;
    }
    set({ available: true });
    offState = onDesktopEvent<UpdateStatus>("update:state", (s) => set({ status: s, error: "" }));
    offProgress = onDesktopEvent<UpdateProgress>("update:progress", (p) =>
      set((st) => (st.status ? { status: { ...st.status, state: "downloading", progress: p } } : st)),
    );
    updateApi
      .status()
      .then((s) => set({ status: s, loaded: true }))
      .catch((e) => set({ loaded: true, error: updateErrorText(e) }));
  },
  check: async () => {
    set({ error: "" });
    try {
      set({ status: await updateApi.check() });
    } catch (e) {
      // El estado «error» ya llega por evento; si no, se guarda aquí.
      const s = get().status;
      set({ error: s?.state === "error" ? "" : updateErrorText(e) });
      try {
        set({ status: await updateApi.status() });
      } catch {
        /* sin escritorio */
      }
    }
  },
  apply: async () => {
    set({ error: "" });
    try {
      await updateApi.apply();
    } catch (e) {
      set({ error: updateErrorText(e) });
    }
  },
  setAutoCheck: async (on) => {
    const prev = get().status;
    if (prev) set({ status: { ...prev, autoCheck: on } });
    try {
      await updateApi.setAutoCheck(on);
    } catch (e) {
      if (prev) set({ status: prev });
      set({ error: updateErrorText(e) });
    }
  },
  dismiss: (version) => set((s) => (s.dismissed.includes(version) ? s : { dismissed: [...s.dismissed, version] })),
}));

/** Solo para pruebas: deja el store como nuevo. */
export function resetUpdateStore(): void {
  offState?.();
  offProgress?.();
  offState = offProgress = null;
  started = false;
  useUpdate.setState({ available: hasWails(), loaded: false, status: null, dismissed: [], error: "" });
}

/** «85 MB» (con coma decimal si hace falta; 0 → «—»). */
export function formatMB(bytes: number): string {
  if (!bytes || bytes < 0) return "—";
  const mb = bytes / (1024 * 1024);
  return mb >= 10 ? `${Math.round(mb)} MB` : `${mb.toFixed(1).replace(".", ",")} MB`;
}

/** Fase en español para la interfaz. */
export function phaseText(s: UpdateStatus): string {
  const p = s.progress;
  switch (s.state) {
    case "checking":
      return "Buscando…";
    case "downloading":
      return p.total > 0 ? `Descargando… ${formatMB(p.bytes)} de ${formatMB(p.total)}` : "Descargando…";
    case "verifying":
      return "Verificando la descarga…";
    case "ready":
      return "Listo para instalar";
    case "installing":
      return "Instalando… dConnector se cerrará y volverá a abrirse";
    default:
      return "";
  }
}

/** ¿Hay una actualización en marcha (no se puede buscar ni volver a pulsar)? */
export function isBusy(state: UpdateState | undefined): boolean {
  return state === "checking" || state === "downloading" || state === "verifying" || state === "ready" || state === "installing";
}
