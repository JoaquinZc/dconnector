// Funciones puras de la bandeja: orden, filtro y actualización por eventos.
import type { Chat, ChatFilter } from "@/api/types";

export type Chats = Record<string, Chat>;

export const PAGE_SIZE = 60;

export const DEFAULT_FILTER: ChatFilter = {
  accountIds: [],
  channels: [],
  tab: "all",
  query: "",
  before: 0,
  limit: PAGE_SIZE,
  includeEmpty: false,
};

/** `pinned DESC, lastMessageAt DESC` (y la clave para desempatar de forma estable). */
export function compareChats(a: Chat, b: Chat): number {
  if (a.pinned !== b.pinned) return a.pinned ? -1 : 1;
  if (a.lastMessageAt !== b.lastMessageAt) return b.lastMessageAt - a.lastMessageAt;
  return a.key < b.key ? -1 : a.key > b.key ? 1 : 0;
}

export function sortKeys(chats: Chats): string[] {
  return Object.values(chats)
    .sort(compareChats)
    .map((c) => c.key);
}

/**
 * Chat directo (o «Tú») sin ningún mensaje y sin fijar: un contacto de la libreta que el canal lista como
 * conversación vacía. No sale en la lista de chats hasta que haya un mensaje (igual que el filtro de Go).
 */
export function isEmptyDirect(chat: Chat): boolean {
  const kind = String(chat.kind);
  return (kind === "direct" || kind === "self") && chat.lastMessageAt === 0 && !chat.pinned;
}

/** ¿El chat pertenece a la lista con este filtro? (las pestañas excluyen o incluyen archivados). */
export function matchesFilter(chat: Chat, f: ChatFilter): boolean {
  if (!f.includeEmpty && isEmptyDirect(chat)) return false;
  if (f.accountIds && f.accountIds.length > 0 && !f.accountIds.includes(chat.accountId)) return false;
  if (f.channels && f.channels.length > 0 && !f.channels.includes(String(chat.channel))) return false;
  switch (f.tab) {
    case "archived":
      if (!chat.archived) return false;
      break;
    case "unread":
      if (chat.archived || chat.unreadCount <= 0) return false;
      break;
    case "groups":
      if (chat.archived || String(chat.kind) !== "group") return false;
      break;
    default:
      if (chat.archived) return false;
  }
  const q = (f.query ?? "").trim().toLowerCase();
  if (q && !chat.title.toLowerCase().includes(q)) return false;
  return true;
}

/** Aplica chats nuevos o cambiados: los que ya no cumplen el filtro salen de la lista. */
export function upsertChats(chats: Chats, incoming: Chat[], filter: ChatFilter): Chats {
  if (!incoming.length) return chats;
  const next: Chats = { ...chats };
  for (const c of incoming) {
    if (matchesFilter(c, filter)) next[c.key] = c;
    else delete next[c.key];
  }
  return next;
}

export function removeChat(chats: Chats, key: string): Chats {
  if (!(key in chats)) return chats;
  const next = { ...chats };
  delete next[key];
  return next;
}

/** Lista nueva (primera página): sustituye todo. */
export function replaceChats(list: Chat[]): Chats {
  const out: Chats = {};
  for (const c of list) out[c.key] = c;
  return out;
}

/** Siguiente página: se añade a lo que ya hay. */
export function appendChats(chats: Chats, page: Chat[]): Chats {
  const next = { ...chats };
  for (const c of page) next[c.key] = c;
  return next;
}

/** Cursor de paginación: `lastMessageAt` del último chat de la lista (el más antiguo no fijado). */
export function pagingCursor(chats: Chats, order: string[]): number {
  for (let i = order.length - 1; i >= 0; i--) {
    const c = chats[order[i]];
    if (c && !c.pinned) return c.lastMessageAt;
  }
  return 0;
}

export interface UnreadState {
  total: number;
  byAccount: Record<string, number>;
}

/** `UnreadTotals` devuelve cuenta → cantidad (y puede traer la clave "total"). */
export function unreadFromMap(map: Record<string, number | undefined> | null | undefined): UnreadState {
  const byAccount: Record<string, number> = {};
  let sum = 0;
  let total: number | undefined;
  for (const [k, v] of Object.entries(map ?? {})) {
    const n = v ?? 0;
    if (k === "total") total = n;
    else {
      byAccount[k] = n;
      sum += n;
    }
  }
  return { total: total ?? sum, byAccount };
}
