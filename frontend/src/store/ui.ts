// Estado de la interfaz: página, chat que se pide abrir, avisos (toasts), confirmaciones y tema.
import { create } from "zustand";

export type Page =
  | "chats"
  | "statuses"
  | "campaigns"
  | "scheduled"
  | "contacts"
  | "stats"
  | "accounts"
  | "destinations"
  | "settings";

/** Orden de las páginas = atajos Ctrl+1…9. */
export const PAGE_ORDER: Page[] = [
  "chats",
  "statuses",
  "campaigns",
  "scheduled",
  "contacts",
  "stats",
  "accounts",
  "destinations",
  "settings",
];

export type ToastKind = "error" | "info" | "success";
export interface Toast {
  id: number;
  kind: ToastKind;
  message: string;
}

export type Theme = "system" | "light" | "dark";

export interface ConfirmOptions {
  title: string;
  message?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  danger?: boolean;
  /** Si se indica, el botón solo se habilita al escribir exactamente este texto. */
  requireText?: string;
}

interface ConfirmRequest {
  options: ConfirmOptions;
  resolve: (ok: boolean) => void;
}

const MAX_TOASTS = 4;
const THEME_KEY = "dc:theme";
const toastTimers = new Map<number, ReturnType<typeof setTimeout>>();
let toastSeq = 0;

function readTheme(): Theme {
  try {
    const v = localStorage.getItem(THEME_KEY);
    if (v === "light" || v === "dark" || v === "system") return v;
  } catch {
    /* sin almacenamiento */
  }
  return "system";
}

let mediaQuery: MediaQueryList | null = null;
let mediaListener: (() => void) | null = null;

/** Pone o quita la clase `dark` de <html>. Con «system» sigue al sistema operativo. */
export function applyTheme(theme: Theme): void {
  if (typeof document === "undefined") return;
  const root = document.documentElement;
  if (mediaQuery && mediaListener) {
    mediaQuery.removeEventListener("change", mediaListener);
    mediaListener = null;
  }
  const set = (dark: boolean) => {
    root.classList.toggle("dark", dark);
    root.style.colorScheme = dark ? "dark" : "light";
  };
  if (theme === "system") {
    mediaQuery = typeof window.matchMedia === "function" ? window.matchMedia("(prefers-color-scheme: dark)") : null;
    set(mediaQuery?.matches ?? false);
    if (mediaQuery) {
      const q = mediaQuery;
      mediaListener = () => set(q.matches);
      q.addEventListener("change", mediaListener);
    }
  } else {
    set(theme === "dark");
  }
}

interface UIState {
  page: Page;
  setPage: (p: Page) => void;

  /** Clave del chat que se pidió abrir (A5 la consume y la limpia con `clearOpenChat`). */
  openChatKey: string | null;
  openChat: (key: string) => void;
  clearOpenChat: () => void;

  toasts: Toast[];
  toast: (kind: ToastKind, message: string) => void;
  dismissToast: (id: number) => void;

  theme: Theme;
  setTheme: (t: Theme) => void;

  confirmReq: ConfirmRequest | null;
  /** Abre el diálogo de confirmación global y resuelve con la decisión. */
  confirm: (options: ConfirmOptions) => Promise<boolean>;
  resolveConfirm: (ok: boolean) => void;
}

export const useUI = create<UIState>((set, get) => ({
  page: "chats",
  setPage: (page) => set({ page }),

  openChatKey: null,
  openChat: (key) => set({ page: "chats", openChatKey: key }),
  clearOpenChat: () => set({ openChatKey: null }),

  toasts: [],
  toast: (kind, message) => {
    const text = String(message ?? "").trim();
    if (!text) return;
    const existing = get().toasts.find((t) => t.kind === kind && t.message === text);
    const id = existing ? existing.id : ++toastSeq;
    if (!existing) {
      set((s) => ({ toasts: [...s.toasts, { id, kind, message: text }].slice(-MAX_TOASTS) }));
    }
    const prev = toastTimers.get(id);
    if (prev) clearTimeout(prev);
    toastTimers.set(
      id,
      setTimeout(() => get().dismissToast(id), kind === "error" ? 8000 : 5000),
    );
  },
  dismissToast: (id) => {
    const t = toastTimers.get(id);
    if (t) clearTimeout(t);
    toastTimers.delete(id);
    set((s) => ({ toasts: s.toasts.filter((x) => x.id !== id) }));
  },

  theme: readTheme(),
  setTheme: (theme) => {
    try {
      localStorage.setItem(THEME_KEY, theme);
    } catch {
      /* sin almacenamiento */
    }
    applyTheme(theme);
    set({ theme });
  },

  confirmReq: null,
  confirm: (options) =>
    new Promise<boolean>((resolve) => {
      // Si ya había una pendiente, se cancela.
      get().confirmReq?.resolve(false);
      set({ confirmReq: { options, resolve } });
    }),
  resolveConfirm: (ok) => {
    const req = get().confirmReq;
    set({ confirmReq: null });
    req?.resolve(ok);
  },
}));
