// Sincronización de historial por cuenta: estado de `HistoryService.StatusAll` al arrancar y
// actualizado con el evento `history:sync`; más la petición de «cargar mensajes anteriores» de un chat.
import { create } from "zustand";
import { api } from "@/api";
import { onEvents } from "@/api/events";
import type { HistoryStatus } from "@/api/types";
import {
  applySync,
  emptyStatus,
  indexStatuses,
  settlePending,
  type HistoryMap,
  type PendingMap,
} from "./history.logic";

export * from "./history.logic";

interface HistoryState {
  byAccount: HistoryMap;
  pending: PendingMap;
  loaded: boolean;

  load: () => Promise<void>;
  /** Estado de una cuenta (en reposo si aún no hay dato). */
  statusOf: (accountId: string) => HistoryStatus;
  start: (accountId: string) => Promise<void>;
  stop: (accountId: string) => Promise<void>;
  reset: (accountId: string) => Promise<void>;
  /** «Cargar mensajes anteriores» del chat abierto. */
  loadMore: (accountId: string, chatNativeId: string, title: string) => Promise<void>;
  /** Suscripción a `history:sync`. Devuelve la cancelación. */
  init: () => () => void;
}

const recheckTimers = new Map<string, ReturnType<typeof setTimeout>>();

export const useHistory = create<HistoryState>((set, get) => {
  /** Vuelve a evaluar la petición pendiente de una cuenta con su último estado. */
  const settle = (accountId: string) => {
    const p = get().pending[accountId];
    if (!p) return;
    const next = settlePending(p, get().statusOf(accountId), Date.now());
    if (next === p) return;
    const rest = { ...get().pending };
    if (next) rest[accountId] = next;
    else delete rest[accountId];
    set({ pending: rest });
  };

  return {
    byAccount: {},
    pending: {},
    loaded: false,

    load: async () => {
      try {
        const list = await api.history.StatusAll();
        // Un aviso que llegó mientras tanto es más nuevo que la lectura: se conserva.
        set((s) => ({ byAccount: { ...indexStatuses(list), ...s.byAccount }, loaded: true }));
      } catch {
        set({ loaded: true });
      }
    },

    statusOf: (accountId) => get().byAccount[accountId] ?? emptyStatus(accountId),

    start: async (accountId) => {
      try {
        await api.history.Start(accountId);
        const st = await api.history.Status(accountId);
        if (st) set((s) => ({ byAccount: applySync(s.byAccount, st) }));
      } catch {
        /* el aviso ya salió */
      }
    },

    stop: async (accountId) => {
      try {
        await api.history.Stop(accountId);
        const st = await api.history.Status(accountId);
        if (st) set((s) => ({ byAccount: applySync(s.byAccount, st) }));
      } catch {
        /* el aviso ya salió */
      }
    },

    reset: async (accountId) => {
      try {
        await api.history.Reset(accountId);
        const st = await api.history.Status(accountId);
        if (st) set((s) => ({ byAccount: applySync(s.byAccount, st) }));
      } catch {
        /* el aviso ya salió */
      }
    },

    loadMore: async (accountId, chatNativeId, title) => {
      const pending = { chatNativeId, title, at: Date.now(), seen: false };
      set((s) => ({ pending: { ...s.pending, [accountId]: pending } }));
      try {
        await api.history.LoadMore(accountId, chatNativeId);
        const prev = recheckTimers.get(accountId);
        if (prev) clearTimeout(prev);
        // Si la tanda acaba antes de que llegue un aviso, nadie volvería a evaluar la petición.
        recheckTimers.set(
          accountId,
          setTimeout(() => {
            recheckTimers.delete(accountId);
            settle(accountId);
          }, 1_700),
        );
      } catch (e) {
        const rest = { ...get().pending };
        delete rest[accountId];
        set({ pending: rest });
        throw e;
      }
    },

    init: () => {
      void get().load();
      return onEvents([
        [
          "history:sync",
          (st: HistoryStatus) => {
            set((s) => ({ byAccount: applySync(s.byAccount, st) }));
            if (st?.accountId) settle(String(st.accountId));
          },
        ],
      ]);
    },
  };
});
