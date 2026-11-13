// Lógica pura de Destinos: validación del formulario, conversión a `Destination` y estado en vivo.
import type { Channel, ContactList, Destination, DestinationState } from "@/api/types";

/** Alcance de contactos de un destino (`filter.contactScope`). */
export type ContactScope = Destination["filter"]["contactScope"];
export type ContactMode = "all" | "lists";

/** Tope de listas por lado (el servicio Go usa el mismo). */
export const MAX_SCOPE_LISTS = 100;

export interface ProtocolEvent {
  /** Nombre en el protocolo (10 §4). */
  name: string;
  label: string;
}

/** Eventos que el protocolo envía (10 §4), en ese orden. */
export const PROTOCOL_EVENTS: ProtocolEvent[] = [
  { name: "account.state", label: "Estado de la cuenta" },
  { name: "account.capabilities", label: "Capacidades de la cuenta" },
  { name: "chat.updated", label: "Chat actualizado" },
  { name: "message.received", label: "Mensaje recibido" },
  { name: "message.sent", label: "Mensaje enviado" },
  { name: "message.updated", label: "Mensaje modificado" },
  { name: "reaction", label: "Reacción" },
  { name: "receipt", label: "Acuse de entrega" },
  { name: "person.updated", label: "Persona actualizada" },
  { name: "status.posted", label: "Estado publicado" },
  { name: "status.viewed", label: "Estado visto" },
];

export const ALL_EVENT_NAMES = PROTOCOL_EVENTS.map((e) => e.name);

export interface DestForm {
  name: string;
  url: string;
  token: string;
  enabled: boolean;
  allowCommands: boolean;
  /** Vacío = todas las cuentas. */
  accountIds: string[];
  /** Vacío = todos los canales. */
  channels: string[];
  /** Los marcados; todos marcados = todos (se guarda vacío). */
  events: string[];
  includeFromMe: boolean;
  includeGroups: boolean;
  /** «Todos los contactos» o «Solo estas listas». */
  contactMode: ContactMode;
  /** Listas incluidas; solo cuentan con `contactMode = "lists"`. */
  includeListIds: string[];
  /** Listas excluidas (siempre se restan). */
  excludeListIds: string[];
}

export function emptyDestForm(): DestForm {
  return {
    name: "",
    url: "",
    token: "",
    enabled: true,
    allowCommands: false,
    accountIds: [],
    channels: [],
    events: [...ALL_EVENT_NAMES],
    includeFromMe: false,
    includeGroups: false,
    contactMode: "all",
    includeListIds: [],
    excludeListIds: [],
  };
}

// ---------------------------------------------------------------- validación

/**
 * Misma regla que `service.ValidateDestinationURL`: `wss://` obligatorio, salvo `ws://localhost`,
 * `ws://127.0.0.1` y `ws://[::1]`. Devuelve el mensaje o `null` si vale.
 */
export function validateDestinationUrl(raw: string): string | null {
  const text = (raw ?? "").trim();
  if (!text) return "Falta la dirección";
  let u: URL;
  try {
    u = new URL(text);
  } catch {
    return "La dirección no es válida";
  }
  if (!u.host) return "La dirección no es válida";
  const scheme = u.protocol.replace(/:$/, "").toLowerCase();
  if (scheme === "wss") return null;
  if (scheme === "ws") {
    const host = u.hostname.toLowerCase();
    if (host === "localhost" || host === "127.0.0.1" || host === "[::1]") return null;
    return "Debe empezar por wss:// (ws:// solo con localhost, 127.0.0.1 o [::1])";
  }
  return "Debe empezar por wss://";
}

export interface DestErrors {
  name?: string;
  url?: string;
  events?: string;
  contacts?: string;
}

/** Error del bloque «Contactos» (el servicio Go valida lo mismo); `null` si está bien. */
export function validateContactScope(f: Pick<DestForm, "contactMode" | "includeListIds" | "excludeListIds">): string | null {
  if (f.contactMode === "lists") {
    if (f.includeListIds.length === 0) return "Elige al menos una lista, o recibe de todos los contactos";
    if (f.includeListIds.some((id) => f.excludeListIds.includes(id))) return "Una lista no puede estar a la vez en «incluir» y en «excluir»";
  }
  if (f.includeListIds.length > MAX_SCOPE_LISTS || f.excludeListIds.length > MAX_SCOPE_LISTS) return `Como máximo ${MAX_SCOPE_LISTS} listas por lado`;
  return null;
}

export function validateDestForm(f: DestForm): DestErrors {
  const e: DestErrors = {};
  if (!f.name.trim()) e.name = "Falta el nombre";
  const u = validateDestinationUrl(f.url);
  if (u) e.url = u;
  if (f.events.length === 0) e.events = "Elige al menos un evento";
  const c = validateContactScope(f);
  if (c) e.contacts = c;
  return e;
}

export function hasErrors(e: DestErrors): boolean {
  return Object.keys(e).length > 0;
}

// ---------------------------------------------------------------- conversión

/** Destino guardado → formulario. Sin eventos guardados = todos marcados. */
export function toForm(d: Destination): DestForm {
  const events = d.filter?.events ?? [];
  return {
    name: d.name,
    url: d.url,
    token: d.token,
    enabled: d.enabled,
    allowCommands: d.allowCommands,
    accountIds: [...(d.filter?.accountIds ?? [])],
    channels: [...(d.filter?.channels ?? [])].map(String),
    events: events.length > 0 ? events.filter((x) => ALL_EVENT_NAMES.includes(x)) : [...ALL_EVENT_NAMES],
    includeFromMe: !!d.filter?.includeFromMe,
    includeGroups: !!d.filter?.includeGroups,
    contactMode: d.filter?.contactScope?.mode === "lists" ? "lists" : "all",
    includeListIds: [...(d.filter?.contactScope?.includeListIds ?? [])],
    excludeListIds: [...(d.filter?.contactScope?.excludeListIds ?? [])],
  };
}

/** Formulario → `Destination`. Todos los eventos marcados se guardan como «todos» (lista vacía). */
export function toDestination(f: DestForm, existing?: Pick<Destination, "id" | "createdAt"> | null): Destination {
  const allEvents = ALL_EVENT_NAMES.every((n) => f.events.includes(n));
  return {
    id: existing?.id ?? "",
    name: f.name.trim(),
    url: f.url.trim(),
    token: f.token.trim(),
    enabled: f.enabled,
    allowCommands: f.allowCommands,
    filter: {
      accountIds: f.accountIds,
      channels: f.channels as unknown as Channel[],
      events: allEvents ? [] : ALL_EVENT_NAMES.filter((n) => f.events.includes(n)),
      includeFromMe: f.includeFromMe,
      includeGroups: f.includeGroups,
      contactScope: {
        mode: f.contactMode,
        // Con «todos» las incluidas no cuentan: no se guardan.
        includeListIds: f.contactMode === "lists" ? f.includeListIds : [],
        excludeListIds: f.excludeListIds,
      },
    },
    createdAt: existing?.createdAt ?? 0,
  };
}

/** Borrador: todo menos el token (es un secreto). */
export type DestDraft = Omit<DestForm, "token">;

export function draftOfDest(f: DestForm): DestDraft {
  const { token: _t, ...rest } = f;
  void _t;
  return rest;
}

export function formFromDraft(d: Partial<DestDraft> | null | undefined): DestForm {
  const base = emptyDestForm();
  if (!d || typeof d !== "object") return base;
  const strings = (v: unknown) => (Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : []);
  return {
    ...base,
    name: typeof d.name === "string" ? d.name : "",
    url: typeof d.url === "string" ? d.url : "",
    enabled: d.enabled !== false,
    allowCommands: !!d.allowCommands,
    accountIds: strings(d.accountIds),
    channels: strings(d.channels),
    events: Array.isArray(d.events) ? strings(d.events).filter((x) => ALL_EVENT_NAMES.includes(x)) : base.events,
    includeFromMe: !!d.includeFromMe,
    includeGroups: !!d.includeGroups,
    contactMode: d.contactMode === "lists" ? "lists" : "all",
    includeListIds: strings(d.includeListIds),
    excludeListIds: strings(d.excludeListIds),
  };
}

// ---------------------------------------------------------------- alcance de contactos

/** Nombres de las listas por id (`null` mientras no se han cargado). */
export type ListNames = Record<string, string> | null;

export function indexListNames(lists: ContactList[]): Record<string, string> {
  const m: Record<string, string> = {};
  for (const l of lists) m[l.id] = l.name;
  return m;
}

/** «A», «A y B», «A, B y C». */
export function joinNames(names: string[]): string {
  if (names.length <= 1) return names[0] ?? "";
  return `${names.slice(0, -1).join(", ")} y ${names[names.length - 1]}`;
}

const listsPhrase = (names: string[]) => (names.length === 1 ? `la lista ${names[0]}` : `las listas ${joinNames(names)}`);

/**
 * Frase del alcance de contactos en lenguaje natural, p. ej. «Recibe: todos los contactos, excepto
 * las listas Familia y Amigos». Las listas que ya no existen se ignoran (como hace el programa).
 * Sin nombres cargados (`null`) no se pueden nombrar las listas y la frase es genérica.
 */
export function contactScopeSummary(
  scope: Pick<DestForm, "contactMode" | "includeListIds" | "excludeListIds">,
  names: ListNames,
): string {
  const lists = scope.contactMode === "lists";
  const restricted = lists || scope.excludeListIds.length > 0;
  if (!restricted) return "Recibe: todos los contactos";
  if (names === null) return "Recibe: según las listas de contactos elegidas";
  const known = (ids: string[]) => ids.map((id) => names[id]).filter((n): n is string => !!n);
  const exclude = known(scope.excludeListIds);
  const except = exclude.length > 0 ? `, excepto ${listsPhrase(exclude)}` : "";
  if (!lists) return `Recibe: todos los contactos${except}`;
  const include = known(scope.includeListIds);
  if (include.length === 0) return "No recibirá nada: elige al menos una lista";
  return `Recibe: solo los contactos de ${listsPhrase(include)}${except}`;
}

// ---------------------------------------------------------------- estado en vivo

export type StateTone = "ok" | "warn" | "danger" | "muted";

export interface StateView {
  tone: StateTone;
  label: string;
  pending: number;
  error: string;
}

/** Texto y tono del estado de un destino (`off | connecting | online | error`). */
export function stateView(enabled: boolean, s: DestinationState | undefined): StateView {
  const pending = s?.pending ?? 0;
  const error = s?.lastError ?? "";
  if (!enabled) return { tone: "muted", label: "Desactivado", pending, error: "" };
  switch (s?.state) {
    case "online":
      return { tone: "ok", label: "En línea", pending, error: "" };
    case "connecting":
      return { tone: "warn", label: "Conectando…", pending, error };
    case "error":
      return { tone: "danger", label: "Error", pending, error };
    default:
      return { tone: "muted", label: "Sin conexión", pending, error };
  }
}

export type StateMap = Record<string, DestinationState>;

export function indexStates(list: DestinationState[]): StateMap {
  const m: StateMap = {};
  for (const s of list) m[s.destinationId] = s;
  return m;
}

/** Aplica un `destination:state`. */
export function applyState(map: StateMap, s: DestinationState): StateMap {
  return { ...map, [s.destinationId]: s };
}

export function pendingLabel(n: number): string {
  return n === 1 ? "1 pendiente" : `${n} pendientes`;
}
