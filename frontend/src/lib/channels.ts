// Canales, colores de insignia, paleta de cuentas y colores estables por id.
import type { ChannelId } from "@/api/types";

export interface ChannelInfo {
  id: ChannelId;
  name: string;
  /** Color de la insignia (11 §5). */
  color: string;
  /** Letra de la insignia. */
  letter: string;
}

export const CHANNELS: Record<ChannelId, ChannelInfo> = {
  whatsapp: { id: "whatsapp", name: "WhatsApp", color: "#25D366", letter: "W" },
  telegram: { id: "telegram", name: "Telegram", color: "#229ED9", letter: "T" },
  messenger: { id: "messenger", name: "Messenger", color: "#0084FF", letter: "M" },
  instagram: { id: "instagram", name: "Instagram", color: "#E1306C", letter: "I" },
};

export const CHANNEL_ORDER: ChannelId[] = ["whatsapp", "telegram", "messenger", "instagram"];

const UNKNOWN: ChannelInfo = { id: "whatsapp", name: "Canal", color: "#8696a0", letter: "?" };

/** Datos de un canal; acepta el enum de los enlaces o una cadena cualquiera. */
export function channelInfo(channel: string | null | undefined): ChannelInfo {
  return CHANNELS[String(channel) as ChannelId] ?? UNKNOWN;
}

export function channelName(channel: string | null | undefined): string {
  return channelInfo(channel).name;
}

/** Paleta de cuentas, en el orden por defecto (11 §5). */
export const ACCOUNT_PALETTE = [
  "#25D366",
  "#229ED9",
  "#8B5CF6",
  "#F59E0B",
  "#EF4444",
  "#0EA5E9",
  "#10B981",
  "#EC4899",
  "#6366F1",
  "#84CC16",
] as const;

/** El siguiente color de la paleta que no usa ninguna de las cuentas dadas. */
export function nextAccountColor(used: string[]): string {
  const taken = new Set(used.map((c) => c.toLowerCase()));
  return ACCOUNT_PALETTE.find((c) => !taken.has(c.toLowerCase())) ?? ACCOUNT_PALETTE[used.length % ACCOUNT_PALETTE.length];
}

/** Colores legibles en claro y oscuro para nombres de emisor y avatares. */
const NAME_COLORS = [
  "#1fa855",
  "#d4571e",
  "#8b5cf6",
  "#0e87c9",
  "#c2185b",
  "#b8860b",
  "#3949ab",
  "#00897b",
  "#e53935",
  "#6d4c41",
  "#7cb342",
  "#546e7a",
] as const;

function hash(text: string): number {
  let h = 5381;
  for (let i = 0; i < text.length; i++) h = ((h << 5) + h + text.charCodeAt(i)) | 0;
  return Math.abs(h);
}

/** Color estable para un id (nombre del emisor en grupos, avatar sin foto). */
export function colorForId(id: string): string {
  return NAME_COLORS[hash(id || "?") % NAME_COLORS.length];
}

/** Iniciales (máx. 2) de un nombre para el avatar. */
export function initialsOf(name: string): string {
  const words = Array.from(name ?? "")
    .join("")
    .replace(/[^\p{L}\p{N}\s]/gu, "")
    .split(/\s+/)
    .filter(Boolean);
  if (words.length === 0) return "?";
  const first = Array.from(words[0])[0] ?? "?";
  const second = words.length > 1 ? (Array.from(words[1])[0] ?? "") : "";
  return (first + second).toUpperCase();
}
