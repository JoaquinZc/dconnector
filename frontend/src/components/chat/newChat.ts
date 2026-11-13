// Lógica de «Nuevo chat» y del botón «Escribir» de Contactos: con qué cuentas se puede escribir a una
// persona y por qué no. Sin pantalla, para probarla suelta.
import { api } from "@/api";
import type { AccountView, CapSet, Chat, Contact, Identity } from "@/api/types";
import { channelName } from "@/lib/channels";

/** Una forma de escribir a alguien: una cuenta y el id de la persona en ella. */
export interface ChatTarget {
  accountId: string;
  peerNativeId: string;
  channel: string;
  accountLabel: string;
  accountColor: string;
  /** Teléfono o nombre que se muestra junto a la cuenta. */
  detail: string;
  enabled: boolean;
  /** Por qué no se puede (solo si `enabled` es false). */
  reason: string;
}

type Views = Record<string, AccountView | undefined>;

/** ¿Se puede iniciar el chat con esta cuenta? Devuelve el motivo si no. */
export function accountBlock(view: AccountView | undefined, channel: string): string {
  if (!view) return "La cuenta ya no existe";
  if (!view.account.enabled) return "Cuenta deshabilitada";
  const caps = view.capabilities as Partial<CapSet> | undefined;
  if (caps && caps.startChat === false) return `${channelName(channel)} no permite iniciar chats nuevos`;
  if (String(view.status?.state ?? "") !== "connected") return "Cuenta desconectada";
  return "";
}

function target(accountId: string, peer: string, channel: string, detail: string, views: Views): ChatTarget {
  const v = views[accountId];
  const reason = accountBlock(v, channel);
  return {
    accountId,
    peerNativeId: peer,
    channel,
    accountLabel: v?.account.label ?? accountId,
    accountColor: v?.account.color ?? "#8696a0",
    detail,
    enabled: reason === "",
    reason,
  };
}

/** Un chat por cada identidad del contacto (una por cuenta). */
export function identityTarget(i: Pick<Identity, "accountId" | "channel" | "peerNativeId" | "phone" | "displayName">, views: Views): ChatTarget {
  return target(String(i.accountId), i.peerNativeId, String(i.channel), i.phone || i.displayName || "", views);
}

/** Las formas de escribir a un contacto: sus identidades; sin ellas, su teléfono por las cuentas de WhatsApp. */
export function contactTargets(c: Pick<Contact, "identities" | "phone">, views: Views): ChatTarget[] {
  const ids = c.identities ?? [];
  if (ids.length > 0) return ids.map((i) => identityTarget(i, views));
  return c.phone?.trim() ? numberTargets(c.phone.trim(), views) : [];
}

/** Lo escrito parece un número de teléfono (7 a 15 dígitos, con «+», espacios, guiones o paréntesis). */
export function phoneCandidate(text: string): string | null {
  const t = text.trim();
  if (!/^\+?[\d\s().-]+$/.test(t)) return null;
  const digits = t.replace(/\D/g, "");
  return digits.length >= 7 && digits.length <= 15 ? t : null;
}

/** Escribir a un número que no está en la libreta: solo WhatsApp sabe comprobar «¿está en WhatsApp?». */
export function numberTargets(number: string, views: Views): ChatTarget[] {
  return Object.values(views)
    .filter((v): v is AccountView => !!v && String(v.account.channel) === "whatsapp")
    .map((v) => target(String(v.account.id), number, "whatsapp", number, views));
}

/** `OpenChat` del servicio: el chat existente o uno vacío nuevo. `null` si falló (el aviso ya salió). */
export async function openChatWith(t: Pick<ChatTarget, "accountId" | "peerNativeId">): Promise<Chat | null> {
  try {
    return await api.inbox.OpenChat(t.accountId, t.peerNativeId);
  } catch {
    return null;
  }
}
