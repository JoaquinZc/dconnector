// Cuentas con su estado en vivo (11 §7).
import { create } from "zustand";
import { api } from "@/api";
import { onEvents } from "@/api/events";
import type { AccountStatus, AccountCapsEvent, AccountView, UnreadTotalEvent } from "@/api/types";
import {
  applyCaps,
  applyStatus,
  applyUnread,
  indexViews,
  moveBefore,
  worstOf,
  type Health,
} from "./accounts.logic";

export * from "./accounts.logic";

interface AccountsState {
  views: Record<string, AccountView>;
  order: string[];
  loaded: boolean;

  load: () => Promise<void>;
  /** Cuentas en orden. */
  list: () => AccountView[];
  /** Peor estado de las cuentas activas, para el punto del rail. */
  worstState: () => Health;
  /** Reordena (arrastrar): actualiza ya y avisa al backend; si falla, recarga. */
  reorder: (ids: string[]) => Promise<void>;
  moveBefore: (id: string, overId: string) => string[];
  /** Suscripción a `account:status`, `account:caps` y `unread:total`. Devuelve la cancelación. */
  init: () => () => void;
}

export const useAccounts = create<AccountsState>((set, get) => ({
  views: {},
  order: [],
  loaded: false,

  load: async () => {
    try {
      const list = await api.accounts.List();
      const { views, order } = indexViews(list);
      set({ views, order, loaded: true });
    } catch {
      set({ loaded: true });
    }
  },

  list: () => {
    const { views, order } = get();
    return order.map((id) => views[id]).filter(Boolean);
  },

  worstState: () => worstOf(get().views),

  moveBefore: (id, overId) => moveBefore(get().order, id, overId),

  reorder: async (ids) => {
    set({ order: ids });
    try {
      await api.accounts.Reorder(ids);
    } catch {
      await get().load();
    }
  },

  init: () => {
    void get().load();
    return onEvents([
      [
        "account:status",
        (status: AccountStatus) => {
          const next = applyStatus(get().views, status);
          if (next) set({ views: next });
          else void get().load(); // cuenta nueva
        },
      ],
      [
        "account:caps",
        (ev: AccountCapsEvent) => {
          const next = applyCaps(get().views, ev.accountId, ev.capabilities);
          if (next) set({ views: next });
        },
      ],
      [
        "unread:total",
        (ev: UnreadTotalEvent) => set({ views: applyUnread(get().views, ev.byAccount ?? {}) }),
      ],
    ]);
  },
}));
