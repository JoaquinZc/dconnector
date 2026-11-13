// Un 429 («demasiadas peticiones») lo da el servidor antes de ejecutar la llamada: repetirlo es seguro y no debe
// molestar al usuario con avisos repetidos.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@wailsio/runtime", () => ({ Call: { ByName: vi.fn() }, Events: { On: vi.fn(() => () => undefined), Emit: vi.fn() } }));

import { api, resetToastDedupe } from "./index";
import { ApiError, httpConfig, rpcCall } from "./http";
import { configureSession, resetSession } from "./session";
import { useUI } from "@/store/ui";

const fetchMock = vi.fn();
const limited = (wait: number) =>
  new Response(JSON.stringify({ error: { code: "rate_limited", message: "Demasiadas peticiones. Espere un momento e inténtelo de nuevo." } }), {
    status: 429,
    headers: { "Content-Type": "application/json", "Retry-After": String(wait) },
  });
const ok = (body: unknown = null) =>
  new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
  httpConfig.rateMsPerSecond = 1; // 1 ms por «segundo» de Retry-After
  httpConfig.retryDelays = [1, 1];
  resetSession();
  configureSession({ baseUrl: "https://srv", token: "T" });
  resetToastDedupe();
  useUI.setState({ toasts: [] });
});
afterEach(() => {
  vi.unstubAllGlobals();
  httpConfig.rateMsPerSecond = 1000;
});

describe("429: demasiadas peticiones", () => {
  it("un envío rechazado con 429 corto se repite solo y sale bien, sin aviso", async () => {
    fetchMock.mockResolvedValueOnce(limited(3)).mockResolvedValueOnce(ok({ id: "m1" }));
    await expect(rpcCall("Messaging", "Send", ["k", {}])).resolves.toEqual({ id: "m1" });
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(useUI.getState().toasts).toHaveLength(0);
  });

  it("se rinde tras dos reintentos y devuelve el error con la espera", async () => {
    fetchMock.mockImplementation(async () => limited(2));
    await expect(rpcCall("Inbox", "ListChats", [{}])).rejects.toMatchObject({ status: 429, retryAfter: 2 });
    expect(fetchMock).toHaveBeenCalledTimes(1 + httpConfig.rateRetries);
  });

  it("una espera larga (más de 15 s) no se espera: se avisa de inmediato", async () => {
    fetchMock.mockResolvedValue(limited(40));
    await expect(rpcCall("Messaging", "Send", ["k", {}])).rejects.toBeInstanceOf(ApiError);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("el inicio de sesión nunca se repite solo (su bloqueo es de seguridad)", async () => {
    fetchMock.mockResolvedValue(limited(2));
    const { request } = await import("./http");
    await expect(request("/auth/login", { method: "POST", auth: false, body: "{}" })).rejects.toBeInstanceOf(ApiError);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});

describe("avisos de error repetidos", () => {
  it("el mismo error varias veces seguidas sale una sola vez", async () => {
    fetchMock.mockImplementation(async () => limited(40));
    await Promise.allSettled([api.inbox.ListChats({} as never), api.inbox.UnreadTotals(), api.contacts.Lists(), api.accounts.List()]);
    const errores = useUI.getState().toasts.filter((t) => t.kind === "error");
    expect(errores).toHaveLength(1);
    expect(errores[0].message).toContain("Demasiadas peticiones");
  });
});
