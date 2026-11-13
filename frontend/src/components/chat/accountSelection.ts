// Selección del filtro de cuentas (chips): lógica pura y persistencia en localStorage.
export interface AccountLite {
  id: string;
  label: string;
  color: string;
  channel: string;
}

export interface AccountSelection {
  channels: string[];
  accounts: string[];
}

export const EMPTY_SELECTION: AccountSelection = { channels: [], accounts: [] };
export const FILTER_KEY = "dc:chats:accountFilter";

export function loadSelection(): AccountSelection {
  try {
    const raw = localStorage.getItem(FILTER_KEY);
    if (!raw) return EMPTY_SELECTION;
    const v = JSON.parse(raw) as Partial<AccountSelection>;
    return {
      channels: Array.isArray(v.channels) ? v.channels.filter((x) => typeof x === "string") : [],
      accounts: Array.isArray(v.accounts) ? v.accounts.filter((x) => typeof x === "string") : [],
    };
  } catch {
    return EMPTY_SELECTION;
  }
}

export function saveSelection(s: AccountSelection): void {
  try {
    localStorage.setItem(FILTER_KEY, JSON.stringify(s));
  } catch {
    /* sin almacenamiento */
  }
}

/** Canales con al menos una cuenta, en orden de aparición. */
export function channelsPresent(accounts: AccountLite[]): string[] {
  const seen: string[] = [];
  for (const a of accounts) if (!seen.includes(a.channel)) seen.push(a.channel);
  return seen;
}

/** Quita de la selección lo que ya no existe (cuentas borradas, canales sin cuentas). */
export function pruneSelection(s: AccountSelection, accounts: AccountLite[]): AccountSelection {
  const ids = new Set(accounts.map((a) => a.id));
  const chans = new Set(channelsPresent(accounts));
  const channels = s.channels.filter((c) => chans.has(c));
  const accs = s.accounts.filter((a) => ids.has(a));
  return channels.length === s.channels.length && accs.length === s.accounts.length ? s : { channels, accounts: accs };
}

export function isEmptySelection(s: AccountSelection): boolean {
  return s.channels.length === 0 && s.accounts.length === 0;
}

/** Clic en «Todas», en un canal o en una cuenta. */
export function toggleSelection(
  s: AccountSelection,
  what: { kind: "all" } | { kind: "channel"; id: string } | { kind: "account"; id: string },
): AccountSelection {
  if (what.kind === "all") return EMPTY_SELECTION;
  const key = what.kind === "channel" ? "channels" : "accounts";
  const list = s[key];
  const next = list.includes(what.id) ? list.filter((x) => x !== what.id) : [...list, what.id];
  return { ...s, [key]: next };
}

/**
 * Cuentas que cubre la selección (unión de las cuentas elegidas y las de los canales elegidos).
 * Lista vacía = todas (sin filtro).
 */
export function resolveAccountIds(s: AccountSelection, accounts: AccountLite[]): string[] {
  if (isEmptySelection(s)) return [];
  const out = new Set<string>(s.accounts);
  for (const a of accounts) if (s.channels.includes(a.channel)) out.add(a.id);
  // conserva el orden de las cuentas
  return accounts.filter((a) => out.has(a.id)).map((a) => a.id);
}

/**
 * Selección canónica para un conjunto de cuentas elegido a mano (menú «Cuentas»): los canales que
 * quedan completos pasan a `channels` (así su chip se ve activo) y el resto va como cuentas sueltas.
 * Vacío o todas las cuentas = sin filtro.
 */
export function selectionFromIds(ids: readonly string[], accounts: AccountLite[]): AccountSelection {
  const set = new Set(ids);
  const chosen = accounts.filter((a) => set.has(a.id));
  if (chosen.length === 0 || chosen.length === accounts.length) return EMPTY_SELECTION;
  const channels = channelsPresent(accounts).filter((ch) => accounts.filter((a) => a.channel === ch).every((a) => set.has(a.id)));
  return { channels, accounts: chosen.filter((a) => !channels.includes(a.channel)).map((a) => a.id) };
}

/** Cuentas que se ven con la selección actual (sin filtro = todas). */
export function visibleAccountIds(s: AccountSelection, accounts: AccountLite[]): string[] {
  return isEmptySelection(s) ? accounts.map((a) => a.id) : resolveAccountIds(s, accounts);
}

/**
 * Marca o desmarca una cuenta en el menú. No deja la lista vacía (vacío significaría «todas»):
 * la última cuenta marcada no se puede quitar.
 */
export function toggleAccountChecked(s: AccountSelection, accounts: AccountLite[], id: string): AccountSelection {
  const cur = visibleAccountIds(s, accounts);
  if (cur.length === 1 && cur[0] === id) return s;
  const next = cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id];
  return selectionFromIds(next, accounts);
}

export function sameIds(a: readonly string[] | null | undefined, b: readonly string[] | null | undefined): boolean {
  const x = a ?? [];
  const y = b ?? [];
  return x.length === y.length && x.every((v, i) => v === y[i]);
}
