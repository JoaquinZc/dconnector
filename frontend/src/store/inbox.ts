// Bandeja unificada: chats de todas las cuentas, filtro, paginación y no leídos (11 §7).
import { create } from "zustand";
import { api } from "@/api";
import { onEvents } from "@/api/events";
import type { Chat, ChatFilter, ChatRemovedEvent, UnreadTotalEvent } from "@/api/types";
import {
  DEFAULT_FILTER,
  PAGE_SIZE,
  appendChats,
  pagingCursor,
  removeChat,
  replaceChats,
  sortKeys,
  unreadFromMap,
  upsertChats,
  type UnreadState,
} from "./inbox.logic";

export * from "./inbox.logic";

interface InboxState {
  chats: Record<string, Chat>;
  /** Claves ordenadas `pinned DESC, lastMessageAt DESC`. */
  order: string[];
  filter: ChatFilter;
  loading: boolean;
  hasMore: boolean;
  unread: UnreadState;

  /** Cambia el filtro (parcial) y recarga la primera página. */
  setFilter: (patch: Partial<ChatFilter>) => void;
  loadFirst: () => Promise<void>;
  /** Mezcla la primera página con lo que ya hay (sin vaciar la lista): se usa al llegar historial. */
  refreshFirst: () => Promise<void>;
  loadMore: () => Promise<void>;
  loadUnread: () => Promise<void>;
  /** Suscripción a `chat:upsert`, `chat:removed` y `unread:total`. Devuelve la cancelación. */
  init: () => () => void;
}

// Descarta respuestas de una carga anterior si mientras tanto cambió el filtro.
let seq = 0;

// El historial y la sincronización de chats llegan sin datos (history:progress): la lista se
// vuelve a pedir, agrupando los avisos seguidos.
const HISTORY_RELOAD_MS = 1500;
let historyTimer: ReturnType<typeof setTimeout> | null = null;

function toBinding(filter: ChatFilter, before: number): Parameters<typeof api.inbox.ListChats>[0] {
  return { ...filter, before, limit: filter.limit || PAGE_SIZE } as unknown as Parameters<
    typeof api.inbox.ListChats
  >[0];
}

export const useInbox = create<InboxState>((set, get) => ({
  chats: {},
  order: [],
  filter: DEFAULT_FILTER,
  loading: false,
  hasMore: true,
  unread: { total: 0, byAccount: {} },

  setFilter: (patch) => {
    set((s) => ({ filter: { ...s.filter, ...patch, before: 0 } }));
    void get().loadFirst();
  },

  loadFirst: async () => {
    const my = ++seq;
    set({ loading: true });
    try {
      const page = await api.inbox.ListChats(toBinding(get().filter, 0));
      if (my !== seq) return;
      const chats = replaceChats(page);
      set({
        chats,
        order: sortKeys(chats),
        hasMore: page.length >= (get().filter.limit || PAGE_SIZE),
        loading: false,
      });
    } catch {
      if (my === seq) set({ loading: false });
    }
  },

  refreshFirst: async () => {
    const my = seq;
    try {
      const page = await api.inbox.ListChats(toBinding(get().filter, 0));
      if (my !== seq) return;
      const chats = appendChats(get().chats, page);
      set({ chats, order: sortKeys(chats) });
    } catch {
      /* se reintenta con el siguiente aviso */
    }
  },

  loadMore: async () => {
    const s = get();
    if (s.loading || !s.hasMore) return;
    const before = pagingCursor(s.chats, s.order);
    if (!before) return;
    const my = seq;
    set({ loading: true });
    try {
      const page = await api.inbox.ListChats(toBinding(s.filter, before));
      if (my !== seq) return;
      const chats = appendChats(get().chats, page);
      set({
        chats,
        order: sortKeys(chats),
        hasMore: page.length >= (s.filter.limit || PAGE_SIZE),
        loading: false,
      });
    } catch {
      if (my === seq) set({ loading: false });
    }
  },

  loadUnread: async () => {
    try {
      set({ unread: unreadFromMap(await api.inbox.UnreadTotals()) });
    } catch {
      /* el aviso ya salió */
    }
  },

  init: () => {
    void get().loadFirst();
    void get().loadUnread();
    const off = onEvents([
      [
        "chat:upsert",
        (incoming: Chat[] | Chat) => {
          const list = Array.isArray(incoming) ? incoming : [incoming];
          const chats = upsertChats(get().chats, list, get().filter);
          if (chats !== get().chats) set({ chats, order: sortKeys(chats) });
        },
      ],
      [
        "chat:removed",
        (ev: ChatRemovedEvent) => {
          const chats = removeChat(get().chats, ev.chatKey);
          if (chats !== get().chats) set({ chats, order: sortKeys(chats) });
        },
      ],
      [
        "unread:total",
        (ev: UnreadTotalEvent) => set({ unread: { total: ev.total ?? 0, byAccount: ev.byAccount ?? {} } }),
      ],
      [
        "history:progress",
        () => {
          if (historyTimer) clearTimeout(historyTimer);
          historyTimer = setTimeout(() => {
            historyTimer = null;
            void get().refreshFirst();
            void get().loadUnread();
          }, HISTORY_RELOAD_MS);
        },
      ],
    ]);
    return () => {
      if (historyTimer) {
        clearTimeout(historyTimer);
        historyTimer = null;
      }
      off();
    };
  },
}));

