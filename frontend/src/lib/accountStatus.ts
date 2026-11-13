// Textos y tonos del estado de una cuenta.
import type { AccountView } from "@/api/types";

export type Tone = "ok" | "warn" | "danger" | "muted";

const LABELS: Record<string, string> = {
  connected: "Conectada",
  connecting: "Conectando…",
  pairing: "Esperando vinculación",
  disconnected: "Desconectada",
  logged_out: "Sesión cerrada",
  error: "Error",
};

const TONES: Record<string, Tone> = {
  connected: "ok",
  connecting: "warn",
  pairing: "warn",
  disconnected: "muted",
  logged_out: "danger",
  error: "danger",
};

export function stateLabel(state: string | null | undefined): string {
  return LABELS[String(state)] ?? "Desconocido";
}

export function stateTone(state: string | null | undefined): Tone {
  return TONES[String(state)] ?? "muted";
}

export const TONE_COLOR: Record<Tone, string> = {
  ok: "var(--ok)",
  warn: "var(--warn)",
  danger: "var(--danger)",
  muted: "var(--muted)",
};

/** Estado de una cuenta para la tarjeta; si está desactivada lo dice. */
export function accountStateText(view: AccountView): { label: string; tone: Tone; detail: string } {
  if (!view.account.enabled) return { label: "Desactivada", tone: "muted", detail: "" };
  const s = view.status;
  return { label: stateLabel(s?.state), tone: stateTone(s?.state), detail: s?.detail ?? "" };
}
