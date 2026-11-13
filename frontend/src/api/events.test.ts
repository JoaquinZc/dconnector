import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const wails = vi.hoisted(() => ({ Emit: vi.fn(), On: vi.fn(() => () => undefined) }));
vi.mock("@wailsio/runtime", () => ({ Call: { ByName: vi.fn() }, Events: { On: wails.On, Emit: wails.Emit } }));

import {
  BACKOFF_MS,
  OFFLINE_AFTER_ATTEMPTS,
  backoffDelay,
  emitEvent,
  onEvent,
  onEvents,
  reconnectNow,
  setEventsHooks,
  rebindEvents,
  startEvents,
  stopEvents,
} from "./events";
import type { ConnectionEvent } from "./types";
import { httpConfig, setHttpHooks } from "./http";
import { configureSession, resetSession, session } from "./session";

/** Servidor simulado según docs/14 §5: ticket por POST, hello, seq por conexión. */
class FakeSocket {
  static all: FakeSocket[] = [];
  onmessage: ((m: { data: string }) => void) | null = null;
  onclose: ((e: { code: number }) => void) | null = null;
  onerror: (() => void) | null = null;
  closed = false;
  seq = 0;
  constructor(public url: string) {
    FakeSocket.all.push(this);
  }
  close() {
    this.closed = true;
  }
  /** Mensaje del servidor. */
  send(event: string, data: unknown, seq = event === "hello" ? 0 : ++this.seq) {
    this.onmessage?.({ data: JSON.stringify({ event, seq, data }) });
  }
  hello(connId = "c1") {
    this.send("hello", { version: "0.3.0", apiVersion: 1, protocol: 1, serverTime: 1, user: "admin", connId });
  }
  drop(code = 1006) {
    this.onclose?.({ code });
  }
}

const fetchMock = vi.fn();
let tickets = 0;

function installFetch(failTickets = 0) {
  let failed = 0;
  fetchMock.mockImplementation(async (url: string) => {
    if (String(url).endsWith("/auth/ws-ticket")) {
      if (failed < failTickets) {
        failed++;
        throw new TypeError("Failed to fetch");
      }
      tickets++;
      return new Response(JSON.stringify({ ticket: `t${tickets}`, expiresAt: Date.now() + 30000 }), { status: 200 });
    }
    return new Response("{}", { status: 200 });
  });
}

const flush = () => vi.advanceTimersByTimeAsync(0);

let states: ConnectionEvent[] = [];
let offState: () => void;

beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal("WebSocket", FakeSocket);
  vi.stubGlobal("fetch", fetchMock);
  vi.spyOn(Math, "random").mockReturnValue(0.5); // ±20 %: 0,5 → exactamente la base
  FakeSocket.all = [];
  tickets = 0;
  fetchMock.mockReset();
  installFetch();
  httpConfig.retryDelays = [];
  setHttpHooks({});
  setEventsHooks({});
  resetSession();
  configureSession({ baseUrl: "https://srv.local:8443", token: "tok" });
  states = [];
  offState = onEvent("connection", (e) => states.push(e));
  wails.Emit.mockClear();
});

afterEach(() => {
  offState();
  stopEvents();
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  const w = window as unknown as { chrome?: unknown };
  delete w.chrome;
});

describe("backoffDelay", () => {
  it("1, 2, 4, 8, 16 y 30 s con tope y ±20 %", () => {
    expect(BACKOFF_MS).toEqual([1000, 2000, 4000, 8000, 16000, 30000]);
    expect([0, 1, 2, 3, 4, 5, 9].map((a) => backoffDelay(a, () => 0.5))).toEqual([1000, 2000, 4000, 8000, 16000, 30000, 30000]);
    expect(backoffDelay(2, () => 0)).toBe(3200);
    expect(backoffDelay(2, () => 1)).toBe(4800);
  });
});

describe("WebSocket de eventos", () => {
  it("pide un ticket, abre wss con ?ticket= y, con hello, queda conectado y guarda connId", async () => {
    startEvents();
    await flush();
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("https://srv.local:8443/api/v1/auth/ws-ticket");
    expect((init as RequestInit).method).toBe("POST");
    expect(FakeSocket.all).toHaveLength(1);
    expect(FakeSocket.all[0].url).toBe("wss://srv.local:8443/api/v1/events?ticket=t1");

    FakeSocket.all[0].hello("conn-9");
    expect(session.connId).toBe("conn-9");
    expect(states).toEqual([{ state: "connected", reconnected: false }]);
  });

  it("http → ws", async () => {
    configureSession({ baseUrl: "http://127.0.0.1:5000" });
    startEvents();
    await flush();
    expect(FakeSocket.all[0].url).toBe("ws://127.0.0.1:5000/api/v1/events?ticket=t1");
  });

  it("entrega los eventos con su carga, igual que antes", async () => {
    const chats: unknown[] = [];
    const both: string[] = [];
    const off1 = onEvent("chat:upsert", (d) => chats.push(d));
    const off2 = onEvents([
      ["unread:total", (d: unknown) => both.push(`u:${JSON.stringify(d)}`)],
      ["typing", (d: unknown) => both.push(`t:${JSON.stringify(d)}`)],
    ]);
    startEvents();
    await flush();
    const ws = FakeSocket.all[0];
    ws.hello();
    ws.send("chat:upsert", [{ key: "wa-1|a" }]);
    ws.send("unread:total", { total: 3, byAccount: { "wa-1": 3 } });
    ws.send("typing", { chatKey: "k", peerId: "p", typing: true, recording: false });
    expect(chats).toEqual([[{ key: "wa-1|a" }]]);
    expect(both).toEqual(['u:{"total":3,"byAccount":{"wa-1":3}}', 't:{"chatKey":"k","peerId":"p","typing":true,"recording":false}']);
    off1();
    off2();
    ws.send("chat:upsert", [{ key: "otro" }]);
    expect(chats).toHaveLength(1);
  });

  it("un evento sin datos no llama al callback; un marco roto se ignora", async () => {
    const cb = vi.fn();
    const off = onEvent("account:status", cb);
    startEvents();
    await flush();
    const ws = FakeSocket.all[0];
    ws.hello();
    ws.send("account:status", undefined);
    ws.onmessage?.({ data: "{no es json" });
    expect(cb).not.toHaveBeenCalled();
    off();
  });

  it("un salto en seq pide recargar el estado (el servidor no reenvía)", async () => {
    startEvents();
    await flush();
    const ws = FakeSocket.all[0];
    ws.hello();
    ws.send("receipt", { a: 1 }); // seq 1
    ws.send("receipt", { a: 2 }); // seq 2
    expect(states).toEqual([{ state: "connected", reconnected: false }]);
    ws.send("receipt", { a: 4 }, 4); // se perdió el 3
    expect(states.at(-1)).toEqual({ state: "connected", reconnected: true });
  });

  it("reconexión: Reconectando, espera 1 s, ticket NUEVO, hello y aviso de recarga", async () => {
    startEvents();
    await flush();
    FakeSocket.all[0].hello();
    states.length = 0;

    FakeSocket.all[0].drop(1006);
    expect(states).toEqual([{ state: "reconnecting", reconnected: false }]);
    expect(session.connId).toBe("");

    await vi.advanceTimersByTimeAsync(999);
    expect(FakeSocket.all).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(fetchMock).toHaveBeenCalledTimes(2); // otro ticket
    expect(FakeSocket.all).toHaveLength(2);
    expect(FakeSocket.all[1].url).toContain("ticket=t2");
    expect(FakeSocket.all[1].url).not.toBe(FakeSocket.all[0].url);

    FakeSocket.all[1].hello("c2");
    expect(states.at(-1)).toEqual({ state: "connected", reconnected: true });
    expect(session.connId).toBe("c2");
  });

  it("las esperas crecen 1, 2, 4 y 8 s y, tras varios fallos, pasa a «Sin conexión»", async () => {
    installFetch(99); // el servidor no responde
    startEvents();
    await flush();
    expect(states.at(-1)).toEqual({ state: "reconnecting", reconnected: false });
    const waits = [1000, 2000, 4000, 8000];
    for (let i = 0; i < waits.length; i++) {
      const before = fetchMock.mock.calls.length;
      await vi.advanceTimersByTimeAsync(waits[i] - 1);
      expect(fetchMock.mock.calls.length).toBe(before);
      await vi.advanceTimersByTimeAsync(1);
      expect(fetchMock.mock.calls.length).toBe(before + 1);
    }
    expect(OFFLINE_AFTER_ATTEMPTS).toBe(4);
    expect(states.at(-1)).toEqual({ state: "offline", reconnected: false });
    // cuando el servidor vuelve, conecta y pide recargar solo si ya había conectado antes
    installFetch();
    await vi.advanceTimersByTimeAsync(16000);
    FakeSocket.all[0].hello();
    expect(states.at(-1)).toEqual({ state: "connected", reconnected: false });
  });

  it("reconnectNow reintenta sin esperar", async () => {
    installFetch(1);
    startEvents();
    await flush();
    expect(FakeSocket.all).toHaveLength(0);
    reconnectNow();
    await flush();
    expect(FakeSocket.all).toHaveLength(1);
  });

  it("cierre 4401 (token revocado): renueva la sesión y reconecta con ticket nuevo", async () => {
    const renew = vi.fn(async () => true);
    setEventsHooks({ onUnauthorized: renew });
    startEvents();
    await flush();
    FakeSocket.all[0].hello();
    FakeSocket.all[0].drop(4401);
    await flush();
    expect(renew).toHaveBeenCalledTimes(1);
    expect(FakeSocket.all).toHaveLength(2);
    expect(FakeSocket.all[1].url).toContain("ticket=t2");
  });

  it("si el ticket da 401 y no hay sesión, no martillea: espera a reconnectNow()", async () => {
    fetchMock.mockImplementation(
      async () => new Response(JSON.stringify({ error: { code: "unauthorized", message: "Hay que iniciar sesión." } }), { status: 401 }),
    );
    startEvents();
    await flush();
    await vi.advanceTimersByTimeAsync(60000);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(FakeSocket.all).toHaveLength(0);
  });

  it("stopEvents cierra el socket y no reconecta", async () => {
    startEvents();
    await flush();
    const ws = FakeSocket.all[0];
    ws.hello();
    stopEvents();
    expect(ws.closed).toBe(true);
    await vi.advanceTimersByTimeAsync(60000);
    expect(FakeSocket.all).toHaveLength(1);
  });
});

describe("rebindEvents", () => {
  it("cierra el socket y abre otro contra la dirección nueva; el hello siguiente cuenta como reconexión", async () => {
    startEvents();
    await flush();
    FakeSocket.all[0].hello();
    states = [];
    configureSession({ baseUrl: "http://127.0.0.1:5999", token: "nuevo" });
    rebindEvents();
    await flush();
    expect(FakeSocket.all[0].closed).toBe(true);
    expect(FakeSocket.all).toHaveLength(2);
    expect(FakeSocket.all[1].url).toBe("ws://127.0.0.1:5999/api/v1/events?ticket=t2");
    FakeSocket.all[1].hello("c2");
    expect(states).toEqual([{ state: "connected", reconnected: true }]);
    expect(session.connId).toBe("c2");
  });

  it("el cierre del socket viejo ya no programa reintentos", async () => {
    startEvents();
    await flush();
    FakeSocket.all[0].hello();
    const old = FakeSocket.all[0];
    rebindEvents();
    await flush();
    old.drop();
    await vi.advanceTimersByTimeAsync(60_000);
    expect(FakeSocket.all).toHaveLength(2);
  });

  it("sin eventos en marcha no hace nada", async () => {
    rebindEvents();
    await flush();
    expect(FakeSocket.all).toHaveLength(0);
  });
});

describe("notify", () => {
  const payload = { chatKey: "k", accountId: "wa-1", channel: "whatsapp", title: "Ana", body: "Hola", preview: "Hola", silent: false, sound: true, reason: "ok" };

  it("llega a los suscritos y no se reenvía al escritorio (este lee el WebSocket por su cuenta)", async () => {
    (window as unknown as { chrome: unknown }).chrome = { webview: { postMessage: () => undefined } };
    const got: unknown[] = [];
    const off = onEvent("notify", (d) => got.push(d));
    startEvents();
    await flush();
    FakeSocket.all[0].hello();
    FakeSocket.all[0].send("notify", payload);
    expect(got).toEqual([payload]);
    expect(wails.Emit).not.toHaveBeenCalled();
    off();
  });
});

describe("bus", () => {
  it("emitEvent aísla los fallos de un suscrito", () => {
    const err = vi.spyOn(console, "error").mockImplementation(() => undefined);
    const ok = vi.fn();
    const a = onEvent("receipt", () => {
      throw new Error("x");
    });
    const b = onEvent("receipt", ok);
    emitEvent("receipt", { a: 1 });
    expect(ok).toHaveBeenCalledWith({ a: 1 });
    a();
    b();
    err.mockRestore();
  });
});

describe("carreras de reconexión (R3)", () => {
  it("reconnectNow mientras otro connect espera el ticket no abre dos sockets", async () => {
    startEvents();
    reconnectNow(); // el primer connect aún espera su ticket
    await flush();
    await flush();
    expect(FakeSocket.all.length).toBe(1);
  });

  it("rebindEvents con un connect en vuelo descarta el viejo: un solo socket, contra la sesión vigente", async () => {
    startEvents();
    rebindEvents();
    await flush();
    await flush();
    expect(FakeSocket.all.length).toBe(1);
  });

  it("la función de cierre de startEvents sigue valiendo tras un reconnectNow", async () => {
    const stop = startEvents();
    reconnectNow();
    await flush();
    await flush();
    stop();
    expect(FakeSocket.all[0].closed).toBe(true);
  });
});
