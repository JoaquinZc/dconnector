// Conversación abierta (11 §7): mensajes, paginación hacia atrás, respuesta/edición y «escribiendo…».
// Solo aplica los eventos del chat abierto.
import { create } from "zustand";
import { api } from "@/api";
import { onEvents } from "@/api/events";
import type { HistoryProgressEvent, Message, ReceiptEvent, TypingEvent } from "@/api/types";
import {
  applyReceipt,
  applyTyping,
  updateMessage,
  upsertMessages,
  type TypingMap,
} from "./chat.logic";

export * from "./chat.logic";

const PAGE = 50;
const TYPING_TTL_MS = 8000;
const typingTimers = new Map<string, ReturnType<typeof setTimeout>>();
// El historial llega al almacén sin datos hacia la ventana (history:progress): la conversación
// abierta vuelve a pedir su página más antigua, agrupando los avisos seguidos.
const HISTORY_RELOAD_MS = 600;
let historyTimer: ReturnType<typeof setTimeout> | null = null;

function clearTypingTimers() {
  typingTimers.forEach((t) => clearTimeout(t));
  typingTimers.clear();
}

interface ChatState {
  chatKey: string | null;
  /** De más antiguo a más reciente. */
  messages: Message[];
  /** Hay mensajes más antiguos guardados en local. */
  hasMore: boolean;
  loading: boolean;
  replyTo: Message | null;
  editing: Message | null;
  /** Personas que escriben o graban ahora, por id nativo. */
  typing: TypingMap;

  open: (key: string | null) => Promise<void>;
  loadOlder: () => Promise<void>;
  /** Vuelve a pedir la página más reciente del chat abierto sin vaciarlo (tras reconectar). */
  reload: () => Promise<void>;
  setReplyTo: (m: Message | null) => void;
  setEditing: (m: Message | null) => void;
  /** Para que el compositor pueda añadir el mensaje devuelto por Send sin esperar al evento. */
  addMessages: (ms: Message[]) => void;
  /** Suscripción a `message:upsert`, `message:update`, `receipt` y `typing`. */
  init: () => () => void;
}

export const useChat = create<ChatState>((set, get) => ({
  chatKey: null,
  messages: [],
  hasMore: false,
  loading: false,
  replyTo: null,
  editing: null,
  typing: {},

  open: async (key) => {
    clearTypingTimers();
    set({ chatKey: key, messages: [], hasMore: false, replyTo: null, editing: null, typing: {}, loading: !!key });
    if (!key) return;
    try {
      const page = await api.inbox.Messages(key, 0, PAGE);
      if (get().chatKey !== key) return; // se abrió otro mientras tanto
      // Lo que llegó por eventos durante la carga se conserva.
      const merged = upsertMessages(page.messages ?? [], get().messages, key);
      set({ messages: merged, hasMore: !!page.hasMore, loading: false });
    } catch {
      if (get().chatKey === key) set({ loading: false });
    }
  },

  loadOlder: async () => {
    const { chatKey, messages, hasMore, loading } = get();
    if (!chatKey || loading) return;
    if (!hasMore) {
      // Lo guardado se acabó: se pide más historial al canal (llega por `history:progress`, que recarga la página más antigua).
      void api.inbox.LoadOlder(chatKey).catch(() => undefined);
      return;
    }
    const before = messages[0]?.timestamp ?? 0;
    set({ loading: true });
    try {
      const page = await api.inbox.Messages(chatKey, before, PAGE);
      if (get().chatKey !== chatKey) return;
      set({
        messages: upsertMessages(get().messages, page.messages ?? [], chatKey),
        hasMore: !!page.hasMore,
        loading: false,
      });
    } catch {
      if (get().chatKey === chatKey) set({ loading: false });
    }
  },

  reload: async () => {
    const key = get().chatKey;
    if (!key) return;
    try {
      const page = await api.inbox.Messages(key, 0, PAGE);
      if (get().chatKey !== key) return;
      set({ messages: upsertMessages(get().messages, page.messages ?? [], key), hasMore: get().messages.length ? get().hasMore : !!page.hasMore });
    } catch {
      /* se reintenta con la siguiente reconexión */
    }
  },

  setReplyTo: (m) => set({ replyTo: m, editing: m ? null : get().editing }),
  setEditing: (m) => set({ editing: m, replyTo: m ? null : get().replyTo }),

  addMessages: (ms) => {
    const { chatKey, messages } = get();
    if (!chatKey) return;
    set({ messages: upsertMessages(messages, ms, chatKey) });
  },

  init: () => {
    const off = onEvents([
      [
        "message:upsert",
        (incoming: Message[] | Message) => {
          const { chatKey, messages } = get();
          if (!chatKey) return;
          const list = Array.isArray(incoming) ? incoming : [incoming];
          const next = upsertMessages(messages, list, chatKey);
          if (next !== messages) set({ messages: next });
        },
      ],
      [
        "message:update",
        (m: Message) => {
          const { chatKey, messages } = get();
          if (!chatKey) return;
          const next = updateMessage(messages, m, chatKey);
          if (next !== messages) set({ messages: next });
        },
      ],
      [
        "receipt",
        (ev: ReceiptEvent) => {
          const { chatKey, messages } = get();
          if (!chatKey) return;
          const next = applyReceipt(messages, ev, chatKey);
          if (next !== messages) set({ messages: next });
        },
      ],
      [
        "typing",
        (ev: TypingEvent) => {
          const { chatKey, typing } = get();
          if (!chatKey || ev.chatKey !== chatKey) return;
          const next = applyTyping(typing, ev);
          if (next !== typing) set({ typing: next });
          // Si el aviso de «dejó de escribir» se pierde, se apaga solo.
          const timerKey = ev.peerId;
          const prev = typingTimers.get(timerKey);
          if (prev) clearTimeout(prev);
          if (ev.typing || ev.recording) {
            typingTimers.set(
              timerKey,
              setTimeout(() => {
                typingTimers.delete(timerKey);
                const cur = get();
                if (cur.chatKey === ev.chatKey) {
                  set({ typing: applyTyping(cur.typing, { ...ev, typing: false, recording: false }) });
                }
              }, TYPING_TTL_MS),
            );
          } else typingTimers.delete(timerKey);
        },
      ],
      [
        "history:progress",
        (ev: HistoryProgressEvent) => {
          const { chatKey } = get();
          if (!chatKey || !chatKey.startsWith(ev.accountId + "|")) return;
          if (historyTimer) clearTimeout(historyTimer);
          historyTimer = setTimeout(() => {
            historyTimer = null;
            const { chatKey: key, messages } = get();
            if (!key || !key.startsWith(ev.accountId + "|")) return;
            const before = messages[0]?.timestamp ?? 0;
            api.inbox
              .Messages(key, before, PAGE)
              .then((page) => {
                if (get().chatKey !== key) return;
                const next = upsertMessages(get().messages, page.messages ?? [], key);
                if (next !== get().messages) set({ messages: next, hasMore: !!page.hasMore || get().hasMore });
              })
              .catch(() => undefined);
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
