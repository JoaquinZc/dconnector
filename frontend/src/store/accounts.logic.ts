// Funciones puras del estado de cuentas (sin enlaces ni eventos): se prueban con datos de ejemplo.
import type { AccountStatus, AccountView, CapSet } from "@/api/types";

export type Health = "green" | "amber" | "red" | "none";

export type Views = Record<string, AccountView>;

/** Salud de una cuenta: conectada = verde; error o sesión cerrada = rojo; el resto = ámbar. */
export function healthOf(view: AccountView): Health {
  if (!view.account.enabled) return "none";
  switch (String(view.status?.state)) {
    case "connected":
      return "green";
    case "error":
    case "logged_out":
      return "red";
    default:
      return "amber";
  }
}

const RANK: Record<Health, number> = { none: 0, green: 1, amber: 2, red: 3 };

/** El peor estado entre las cuentas activas (`none` si no hay ninguna). */
export function worstOf(views: Views): Health {
  let worst: Health = "none";
  for (const v of Object.values(views)) {
    const h = healthOf(v);
    if (RANK[h] > RANK[worst]) worst = h;
  }
  return worst;
}

/** Lista del servicio → mapa por id y orden. */
export function indexViews(list: AccountView[]): { views: Views; order: string[] } {
  const views: Views = {};
  const order: string[] = [];
  for (const v of list) {
    views[v.account.id] = v;
    order.push(v.account.id);
  }
  return { views, order };
}

/** Aplica un `account:status`. Devuelve `null` si la cuenta aún no se conoce (hay que recargar). */
export function applyStatus(views: Views, status: AccountStatus): Views | null {
  const cur = views[status.accountId];
  if (!cur) return null;
  return { ...views, [status.accountId]: { ...cur, status } };
}

/** Aplica un `account:caps`. Devuelve `null` si la cuenta no se conoce. */
export function applyCaps(views: Views, accountId: string, capabilities: CapSet): Views | null {
  const cur = views[accountId];
  if (!cur) return null;
  return { ...views, [accountId]: { ...cur, capabilities } };
}

/** Pone los no leídos por cuenta (viene de `unread:total`). */
export function applyUnread(views: Views, byAccount: Record<string, number>): Views {
  let changed = false;
  const next: Views = {};
  for (const [id, v] of Object.entries(views)) {
    const n = byAccount[id] ?? 0;
    if (n !== v.unread) {
      changed = true;
      next[id] = { ...v, unread: n };
    } else next[id] = v;
  }
  return changed ? next : views;
}

/** Mueve `id` a la posición de `overId` (arrastrar para ordenar). */
export function moveBefore(order: string[], id: string, overId: string): string[] {
  if (id === overId) return order;
  const from = order.indexOf(id);
  const to = order.indexOf(overId);
  if (from < 0 || to < 0) return order;
  const next = order.slice();
  next.splice(from, 1);
  next.splice(to, 0, id);
  return next;
}
