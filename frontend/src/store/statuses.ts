// Estados (historias): autores con estados vigentes y los estados cargados. Básico: B4 amplía.
import { create } from "zustand";
import { api } from "@/api";
import { onEvents } from "@/api/events";
import type { Status, StatusAuthor, StatusView } from "@/api/types";

export type StatusMap = Record<string, Status>;

/** Un estado nuevo o cambiado; los borrados o vencidos salen del mapa. */
export function upsertStatus(map: StatusMap, s: Status, now: number = Date.now()): StatusMap {
  if (s.deletedAt || (s.expiresAt && s.expiresAt <= now)) {
    if (!(s.key in map)) return map;
    const next = { ...map };
    delete next[s.key];
    return next;
  }
  return { ...map, [s.key]: s };
}

/** `status:view`: suma una vista (o su reacción) al contador del estado. */
export function applyStatusView(map: StatusMap, v: StatusView): StatusMap {
  const cur = map[v.statusKey];
  if (!cur) return map;
  return {
    ...map,
    [v.statusKey]: {
      ...cur,
      viewCount: cur.viewCount + 1,
      reactionCount: v.reaction ? cur.reactionCount + 1 : cur.reactionCount,
    },
  };
}

interface StatusesState {
  authors: StatusAuthor[];
  /** Estados vigentes cargados, por clave. */
  statuses: StatusMap;

  loadAuthors: (accountIds?: string[]) => Promise<void>;
  /** Vuelve a pedir los autores de las últimas cuentas consultadas (tras reconectar). */
  refresh: () => Promise<void>;
  loadAuthor: (accountId: string, authorId: string) => Promise<Status[]>;
  /** Suscripción a `status:upsert` y `status:view`. */
  init: () => () => void;
}

let authorsAccounts: string[] | undefined;

export const useStatuses = create<StatusesState>((set, get) => ({
  authors: [],
  statuses: {},

  loadAuthors: async (accountIds) => {
    authorsAccounts = accountIds;
    try {
      set({ authors: await api.statuses.Authors(accountIds ?? []) });
    } catch {
      /* el aviso ya salió */
    }
  },

  refresh: async () => {
    if (authorsAccounts) await get().loadAuthors(authorsAccounts);
  },

  loadAuthor: async (accountId, authorId) => {
    const list = await api.statuses.ByAuthor(accountId, authorId);
    let map = get().statuses;
    for (const s of list) map = upsertStatus(map, s);
    set({ statuses: map });
    return list;
  },

  init: () =>
    onEvents([
      [
        "status:upsert",
        (s: Status) => {
          set({ statuses: upsertStatus(get().statuses, s) });
          void get().loadAuthors(authorsAccounts);
        },
      ],
      ["status:view", (v: StatusView) => set({ statuses: applyStatusView(get().statuses, v) })],
    ]),
}));
