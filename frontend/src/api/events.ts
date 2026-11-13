// Eventos en vivo (docs/14 §5): WebSocket `/api/v1/events` con ticket, `hello`, `seq` y reconexión con espera
// exponencial. Mismos nombres y cargas que antes (`EV`, `onEvent`, `onEvents`). Además hay un evento INTERNO
// `connection` (estado del enlace) para que los stores recarguen tras reconectar.
import { Events } from "@wailsio/runtime";
import { ApiError, apiUrl, requestJSON } from "./http";
import { session, wsBase } from "./session";
import type { ConnectionEvent, EventName, EventPayloads, HelloEvent } from "./types";

/** Nombres de los eventos, tal como los emite el servidor. */
export const EV = {
  accountStatus: "account:status",
  accountCaps: "account:caps",
  chatUpsert: "chat:upsert",
  chatRemoved: "chat:removed",
  messageUpsert: "message:upsert",
  messageUpdate: "message:update",
  receipt: "receipt",
  typing: "typing",
  personUpsert: "person:upsert",
  statusUpsert: "status:upsert",
  statusView: "status:view",
  historyProgress: "history:progress",
  historySync: "history:sync",
  campaignProgress: "campaign:progress",
  destinationState: "destination:state",
  unreadTotal: "unread:total",
  openChat: "ui:open-chat",
  hello: "hello",
  notify: "notify",
  notifyDecision: "notify:decision",
  connection: "connection",
} as const satisfies Record<string, EventName>;

export const EVENT_NAMES: EventName[] = Object.values(EV);

// ------------------------------------------------------------------ bus interno

type Handler = (data: unknown) => void;
const bus = new Map<string, Set<Handler>>();

/** Emite un evento a los suscritos (lo usa el WebSocket; las pruebas lo usan para simular el servidor). */
export function emitEvent(name: string, data: unknown): void {
  const set = bus.get(name);
  if (!set) return;
  for (const h of [...set]) {
    try {
      h(data);
    } catch (e) {
      console.error(`evento ${name}:`, e);
    }
  }
}

/** Eventos que genera el escritorio (no el servidor): llegan por Wails. */
const DESKTOP_EVENTS = new Set<string>(["ui:open-chat"]);

/**
 * Se suscribe a un evento. Devuelve la función que cancela la suscripción.
 * Si llega `undefined` el callback no se llama.
 */
export function onEvent<K extends EventName>(
  name: K,
  cb: (data: EventPayloads[K]) => void,
): () => void;
export function onEvent<T = unknown>(name: string, cb: (data: T) => void): () => void;
export function onEvent(name: string, cb: (data: never) => void): () => void {
  const fire = (data: unknown) => {
    if (data === undefined) return;
    (cb as (d: unknown) => void)(data);
  };
  if (DESKTOP_EVENTS.has(name)) {
    const off = Events.On(name, (event) => fire((event as { data?: unknown }).data));
    return () => {
      try {
        off();
      } catch {
        /* ya cancelada */
      }
    };
  }
  let set = bus.get(name);
  if (!set) bus.set(name, (set = new Set()));
  set.add(fire);
  return () => {
    bus.get(name)?.delete(fire);
  };
}

/** Atajo: se suscribe a varios eventos y devuelve una sola función de cancelación. */
export function onEvents(subs: Array<[name: string, cb: (data: never) => void]>): () => void {
  const offs = subs.map(([n, cb]) => onEvent(n, cb as (d: unknown) => void));
  return () => offs.forEach((o) => o());
}

// ------------------------------------------------------------------ WebSocket

/** Esperas de reconexión (ms); la última se repite. ±20 % de azar. */
export const BACKOFF_MS = [1000, 2000, 4000, 8000, 16000, 30000];
/** Tras tantos intentos fallidos seguidos el estado pasa de «Reconectando» a «Sin conexión». */
export const OFFLINE_AFTER_ATTEMPTS = 4;

export function backoffDelay(attempt: number, rnd: () => number = Math.random): number {
  const base = BACKOFF_MS[Math.min(attempt, BACKOFF_MS.length - 1)];
  return Math.round(base * (0.8 + rnd() * 0.4));
}

/** Gancho: ante un 401 al pedir el ticket, el store de conexión intenta renovar la sesión. */
export interface EventsHooks {
  onUnauthorized?: () => Promise<boolean>;
}
let evHooks: EventsHooks = {};
export function setEventsHooks(h: EventsHooks): void {
  evHooks = h;
}

let running = false;
let ws: WebSocket | null = null;
let retryTimer: ReturnType<typeof setTimeout> | null = null;
let attempt = 0;
let everConnected = false;
let lastSeq = 0;
let epoch = 0;
/** Identifica la llamada a `startEvents` vigente (el `epoch` cambia también al re-conectar). */
let startId = 0;
let connState: ConnectionEvent["state"] | null = null;

function setState(state: ConnectionEvent["state"], reconnected: boolean): void {
  if (connState === state && !reconnected) return;
  connState = state;
  emitEvent(EV.connection, { state, reconnected } satisfies ConnectionEvent);
}

function clearRetry(): void {
  if (retryTimer) clearTimeout(retryTimer);
  retryTimer = null;
}

function scheduleRetry(my: number): void {
  if (!running || my !== epoch) return;
  setState(attempt >= OFFLINE_AFTER_ATTEMPTS ? "offline" : "reconnecting", false);
  clearRetry();
  const wait = backoffDelay(attempt);
  attempt++;
  retryTimer = setTimeout(() => void connect(my), wait);
}

async function connect(my: number): Promise<void> {
  if (!running || my !== epoch) return;
  retryTimer = null;
  let ticket = "";
  try {
    const t = await requestJSON<{ ticket: string }>("/auth/ws-ticket", { method: "POST" });
    ticket = t.ticket;
  } catch (e) {
    // Sin sesión: el store de conexión pide credenciales y, al volver, llama a `reconnectNow()`.
    if (e instanceof ApiError && e.code === "unauthorized") {
      if (my === epoch) setState("reconnecting", false);
      return;
    }
    scheduleRetry(my);
    return;
  }
  if (!running || my !== epoch) return;

  let sock: WebSocket;
  try {
    sock = new WebSocket(`${wsBase(apiUrl("/events"))}?ticket=${encodeURIComponent(ticket)}`);
  } catch {
    scheduleRetry(my);
    return;
  }
  ws = sock;
  lastSeq = 0;
  let gotHello = false;

  sock.onmessage = (m: MessageEvent) => {
    if (my !== epoch) return;
    let msg: { event?: string; seq?: number; data?: unknown };
    try {
      msg = JSON.parse(String(m.data));
    } catch {
      return;
    }
    if (!msg.event) return;
    if (msg.event === EV.hello) {
      gotHello = true;
      const hello = msg.data as HelloEvent;
      session.connId = hello?.connId ?? "";
      lastSeq = 0;
      const reconnected = everConnected;
      everConnected = true;
      attempt = 0;
      emitEvent(EV.hello, hello);
      setState("connected", reconnected);
      return;
    }
    // Un salto en `seq` significa eventos perdidos: se vuelve a cargar el estado (el servidor no reenvía).
    if (typeof msg.seq === "number") {
      const gap = msg.seq > lastSeq + 1;
      lastSeq = msg.seq;
      if (gap) setState("connected", true);
    }
    emitEvent(msg.event, msg.data);
  };

  sock.onclose = (ev: CloseEvent) => {
    if (ws === sock) ws = null;
    if (!running || my !== epoch) return;
    session.connId = "";
    if (ev?.code === 4401) {
      // Token revocado: se intenta renovar la sesión y volver a conectar.
      void (evHooks.onUnauthorized?.() ?? Promise.resolve(false)).then((ok) => {
        if (ok) void connect(my);
        else if (my === epoch) setState("reconnecting", false);
      });
      return;
    }
    if (!gotHello) {
      scheduleRetry(my);
      return;
    }
    attempt = 0;
    scheduleRetry(my);
  };
  sock.onerror = () => {
    /* el cierre que sigue decide */
  };
}

/** Abre el WebSocket y lo mantiene vivo. Devuelve la función que lo cierra. */
export function startEvents(): () => void {
  stopEvents();
  running = true;
  const my = ++epoch;
  const id = ++startId;
  attempt = 0;
  everConnected = false;
  connState = null;
  void connect(my);
  return () => {
    if (id === startId) stopEvents();
  };
}

export function stopEvents(): void {
  running = false;
  epoch++;
  clearRetry();
  const s = ws;
  ws = null;
  if (s) {
    s.onclose = null;
    s.onmessage = null;
    try {
      s.close(1000);
    } catch {
      /* ya cerrado */
    }
  }
  session.connId = "";
}

/**
 * El servidor local cambió de puerto o de token (el escritorio lo reinició): cierra el socket actual y vuelve a
 * abrirlo contra la dirección nueva de `session`. Conserva «ya hubo conexión», así que el `hello` siguiente sale
 * como reconexión y los estados recargan lo suyo.
 */
export function rebindEvents(): void {
  if (!running) return;
  clearRetry();
  const s = ws;
  ws = null;
  if (s) {
    s.onclose = null;
    s.onmessage = null;
    try {
      s.close(1000);
    } catch {
      /* ya cerrado */
    }
  }
  session.connId = "";
  attempt = 0;
  // Época nueva: cualquier `connect` que esté esperando su ticket (contra el servidor anterior) se descarta.
  void connect(++epoch);
}

/** Reintenta ya (botón «Reintentar» o tras iniciar sesión). */
export function reconnectNow(): void {
  if (!running) return;
  clearRetry();
  if (ws) return; // ya hay un socket abierto o abriéndose
  // Época nueva: si otro `connect` aún espera su ticket, queda descartado y no se abren dos sockets.
  void connect(++epoch);
}

/** ¿Hay alguien suscrito a este evento? (útil en pruebas para esperar a que un componente se monte). */
export function hasEventListeners(name: string): boolean {
  return (bus.get(name)?.size ?? 0) > 0;
}
