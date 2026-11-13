import { describe, expect, it } from "vitest";
import type { AccountStatus, AccountView, CapSet } from "@/api/types";
import { applyCaps, applyStatus, applyUnread, healthOf, indexViews, moveBefore, worstOf } from "./accounts.logic";

const view = (id: string, state: string, over: { enabled?: boolean; unread?: number } = {}): AccountView =>
  ({
    account: { id, channel: "whatsapp", label: id, color: "#25D366", enabled: over.enabled ?? true, config: null },
    status: { accountId: id, state, need: "", since: 1 },
    capabilities: { sendText: true } as CapSet,
    unread: over.unread ?? 0,
  }) as unknown as AccountView;

const status = (id: string, state: string, extra: Partial<AccountStatus> = {}) =>
  ({ accountId: id, state, need: "", since: 2, ...extra }) as unknown as AccountStatus;

describe("indexViews", () => {
  it("indexa por id y conserva el orden", () => {
    const { views, order } = indexViews([view("b", "connected"), view("a", "connected")]);
    expect(order).toEqual(["b", "a"]);
    expect(Object.keys(views).sort()).toEqual(["a", "b"]);
  });
});

describe("applyStatus", () => {
  it("cambia solo el estado de esa cuenta", () => {
    const { views } = indexViews([view("a", "connecting"), view("b", "connected")]);
    const next = applyStatus(views, status("a", "connected", { detail: "ok" }))!;
    expect(next.a.status.state).toBe("connected");
    expect(next.a.status.detail).toBe("ok");
    expect(next.a.account).toBe(views.a.account);
    expect(next.b).toBe(views.b);
  });
  it("no muta el original", () => {
    const { views } = indexViews([view("a", "connecting")]);
    applyStatus(views, status("a", "connected"));
    expect(views.a.status.state).toBe("connecting");
  });
  it("cuenta desconocida: null (hay que recargar)", () => {
    expect(applyStatus({}, status("zz", "connected"))).toBeNull();
  });
});

describe("applyCaps", () => {
  it("sustituye las capacidades", () => {
    const { views } = indexViews([view("a", "connected")]);
    const next = applyCaps(views, "a", { sendText: false, reply: true } as CapSet)!;
    expect(next.a.capabilities.reply).toBe(true);
    expect(next.a.capabilities.sendText).toBe(false);
    expect(applyCaps(views, "x", {} as CapSet)).toBeNull();
  });
});

describe("applyUnread", () => {
  it("pone los no leídos por cuenta y deja en 0 las que no vienen", () => {
    const { views } = indexViews([view("a", "connected", { unread: 5 }), view("b", "connected")]);
    const next = applyUnread(views, { b: 3 });
    expect(next.a.unread).toBe(0);
    expect(next.b.unread).toBe(3);
  });
  it("si nada cambia devuelve el mismo objeto", () => {
    const { views } = indexViews([view("a", "connected")]);
    expect(applyUnread(views, {})).toBe(views);
  });
});

describe("healthOf y worstOf", () => {
  it("conectada = verde; error y sesión cerrada = rojo; el resto ámbar", () => {
    expect(healthOf(view("a", "connected"))).toBe("green");
    expect(healthOf(view("a", "error"))).toBe("red");
    expect(healthOf(view("a", "logged_out"))).toBe("red");
    expect(healthOf(view("a", "connecting"))).toBe("amber");
    expect(healthOf(view("a", "pairing"))).toBe("amber");
    expect(healthOf(view("a", "disconnected"))).toBe("amber");
  });
  it("las desactivadas no cuentan", () => {
    expect(healthOf(view("a", "error", { enabled: false }))).toBe("none");
  });
  it("el peor estado manda", () => {
    expect(worstOf({})).toBe("none");
    expect(worstOf(indexViews([view("a", "connected")]).views)).toBe("green");
    expect(worstOf(indexViews([view("a", "connected"), view("b", "connecting")]).views)).toBe("amber");
    expect(worstOf(indexViews([view("a", "connecting"), view("b", "error"), view("c", "connected")]).views)).toBe("red");
    expect(worstOf(indexViews([view("a", "error", { enabled: false }), view("b", "connected")]).views)).toBe("green");
  });
});

describe("moveBefore (arrastrar para ordenar)", () => {
  it("mueve hacia delante y hacia atrás", () => {
    expect(moveBefore(["a", "b", "c", "d"], "d", "b")).toEqual(["a", "d", "b", "c"]);
    expect(moveBefore(["a", "b", "c", "d"], "a", "c")).toEqual(["b", "c", "a", "d"]);
  });
  it("casos sin cambio", () => {
    const o = ["a", "b"];
    expect(moveBefore(o, "a", "a")).toBe(o);
    expect(moveBefore(o, "x", "a")).toBe(o);
  });
});
