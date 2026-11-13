import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const wails = vi.hoisted(() => ({ On: vi.fn((_name: string, _cb: (e: { data: unknown }) => void) => () => undefined) }));
vi.mock("@wailsio/runtime", () => ({ Call: { ByName: vi.fn() }, Events: { On: wails.On, Emit: vi.fn() } }));

import {
  API_VERSION_WARNING,
  clearCredentials,
  desktopPhaseText,
  ensureDesktopListener,
  env,
  renewSession,
  stopDesktopListener,
  useConnection,
} from "./connection";
import { desktop, type ConnectionInfo } from "./desktop";
import { emitEvent, stopEvents } from "./events";
import { httpConfig, rpcCall } from "./http";
import { stopMediaToken } from "./media";
import { configureSession, resetSession, session } from "./session";

const fetchMock = vi.fn();
type Route = (init: RequestInit, url: string) => Response | Promise<Response>;
let routes: Record<string, Route> = {};

const ok = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status });
const err = (status: number, code: string, message: string) => ok({ error: { code, message } }, status);
const HEALTH = { status: "ok", version: "0.3.0", apiVersion: 1, protocol: 1, tls: false };
const LOGIN = { token: "T-login", expiresAt: Date.now() + 1e9, user: { id: "1", username: "admin", role: "admin" } };

function route(path: string, r: Route) {
  routes[path] = r;
}
function calls(path: string) {
  return fetchMock.mock.calls.filter(([u]) => String(u).endsWith(path)) as Array<[string, RequestInit]>;
}

function defaults() {
  routes = {};
  route("/health", () => ok(HEALTH));
  route("/auth/me", () => ok({ id: "1", username: "admin", role: "admin" }));
  route("/auth/media-token", () => ok({ token: "MT", expiresAt: Date.now() + 900_000 }));
  route("/auth/login", () => ok(LOGIN));
  route("/auth/logout", () => ok(null));
}

const local = (extra: Partial<ConnectionInfo> = {}): ConnectionInfo => ({
  mode: "local",
  baseUrl: "http://127.0.0.1:5000",
  token: "LOCAL",
  ...extra,
});

const initial = useConnection.getState();

beforeEach(() => {
  vi.stubGlobal("fetch", fetchMock);
  fetchMock.mockReset();
  fetchMock.mockImplementation(async (url: string, init: RequestInit) => {
    const key = Object.keys(routes).find((k) => String(url).endsWith(k));
    if (!key) throw new Error(`ruta sin simular: ${url}`);
    return routes[key](init, String(url));
  });
  defaults();
  httpConfig.retryDelays = [];
  resetSession();
  clearCredentials();
  stopEvents();
  stopMediaToken();
  sessionStorage.clear();
  localStorage.clear();
  useConnection.setState({ ...initial, link: "connecting", ready: false, needLogin: false, error: "", booting: false, mode: "local" });
  vi.spyOn(desktop, "connectionInfo").mockResolvedValue(local());
  env.reload = vi.fn();
});

afterEach(() => {
  stopDesktopListener();
  delete (window as unknown as { chrome?: unknown }).chrome;
  wails.On.mockClear();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("arranque en modo local", () => {
  it("usa el token del escritorio como Bearer, sin pedir login, y queda conectado", async () => {
    await useConnection.getState().boot();
    const s = useConnection.getState();
    expect(s).toMatchObject({ ready: true, link: "connected", needLogin: false, mode: "local", serverVersion: "0.3.0", user: "admin" });
    expect(session).toMatchObject({ baseUrl: "http://127.0.0.1:5000", token: "LOCAL" });
    const [, healthInit] = calls("/health")[0];
    expect((healthInit.headers as Record<string, string>).Authorization).toBeUndefined();
    const [, meInit] = calls("/auth/me")[0];
    expect((meInit.headers as Record<string, string>).Authorization).toBe("Bearer LOCAL");
    expect(calls("/auth/login")).toHaveLength(0);
    expect(calls("/auth/media-token")).toHaveLength(1);
  });

  it("un servidor con otra apiVersion avisa pero conecta", async () => {
    route("/health", () => ok({ ...HEALTH, apiVersion: 2 }));
    await useConnection.getState().boot();
    expect(useConnection.getState()).toMatchObject({ ready: true, warning: API_VERSION_WARNING, apiVersion: 2 });
  });

  it("servidor caído: Sin conexión con el motivo y nunca listo; Reintentar lo levanta", async () => {
    route("/health", () => {
      throw new TypeError("Failed to fetch");
    });
    await useConnection.getState().boot();
    expect(useConnection.getState()).toMatchObject({
      ready: false,
      link: "offline",
      error: "No se pudo conectar con el servidor.",
    });
    route("/health", () => ok(HEALTH));
    await useConnection.getState().retry();
    expect(useConnection.getState()).toMatchObject({ ready: true, link: "connected", error: "" });
  });

  it("servidor arrancando (503): se reintenta y acaba conectando", async () => {
    httpConfig.retryDelays = [0, 0];
    let n = 0;
    route("/health", () => (++n < 3 ? err(503, "unavailable", "Arrancando.") : ok(HEALTH)));
    await useConnection.getState().boot();
    expect(useConnection.getState().ready).toBe(true);
    expect(n).toBe(3);
  });

  it("si el escritorio no responde, también hay pantalla de «Sin conexión»", async () => {
    vi.spyOn(desktop, "connectionInfo").mockRejectedValue(new Error("fallo del puente"));
    await useConnection.getState().boot();
    expect(useConnection.getState()).toMatchObject({ ready: false, link: "offline", error: "fallo del puente" });
  });
});

describe("inicio de sesión", () => {
  const remote = (): ConnectionInfo => ({ mode: "remote", baseUrl: "https://srv:8443/", user: "ana" });

  it("modo remoto sin token: pide login; con credenciales buenas entra y recuerda el token en la pestaña", async () => {
    vi.spyOn(desktop, "connectionInfo").mockResolvedValue(remote());
    await useConnection.getState().boot();
    expect(useConnection.getState()).toMatchObject({ ready: false, needLogin: true, mode: "remote", baseUrl: "https://srv:8443", user: "ana" });
    expect(calls("/auth/me")).toHaveLength(0);

    await useConnection.getState().login(" ana ", "una-clave-larga");
    const [, init] = calls("/auth/login")[0];
    expect(JSON.parse(init.body as string)).toEqual({ username: "ana", password: "una-clave-larga" });
    expect(useConnection.getState()).toMatchObject({ ready: true, needLogin: false, link: "connected" });
    expect(session.token).toBe("T-login");
    expect(sessionStorage.getItem("dc:session")).toContain("T-login");

    // tras recargar la página el token de la pestaña evita volver a pedir la clave
    resetSession();
    useConnection.setState({ ready: false });
    await useConnection.getState().boot();
    expect(useConnection.getState()).toMatchObject({ ready: true, needLogin: false });
    expect(session.token).toBe("T-login");
  });

  it("clave errónea: el error llega tal cual (en español) y se sigue pidiendo login", async () => {
    vi.spyOn(desktop, "connectionInfo").mockResolvedValue(remote());
    await useConnection.getState().boot();
    route("/auth/login", () => err(401, "unauthorized", "Usuario o clave incorrectos."));
    await expect(useConnection.getState().login("ana", "mala")).rejects.toMatchObject({ message: "Usuario o clave incorrectos.", code: "unauthorized" });
    expect(useConnection.getState()).toMatchObject({ ready: false, needLogin: true });
  });

  it("límite de intentos: 429 con Retry-After", async () => {
    vi.spyOn(desktop, "connectionInfo").mockResolvedValue(remote());
    await useConnection.getState().boot();
    route("/auth/login", () => new Response(JSON.stringify({ error: { code: "rate_limited", message: "Demasiados intentos." } }), { status: 429, headers: { "Retry-After": "60" } }));
    await expect(useConnection.getState().login("ana", "x")).rejects.toMatchObject({ code: "rate_limited", retryAfter: 60 });
  });

  it("401 en /auth/me (token caducado): pide login en vez de quedarse en blanco", async () => {
    vi.spyOn(desktop, "connectionInfo").mockResolvedValue({ ...remote(), token: "viejo" });
    route("/auth/me", () => err(401, "unauthorized", "Hay que iniciar sesión."));
    await useConnection.getState().boot();
    expect(useConnection.getState()).toMatchObject({ ready: false, needLogin: true });
    expect(session.token).toBe("");
  });

  it("un 401 a mitad de sesión se renueva solo con las credenciales de esta sesión", async () => {
    vi.spyOn(desktop, "connectionInfo").mockResolvedValue(remote());
    await useConnection.getState().boot();
    await useConnection.getState().login("ana", "una-clave-larga");
    route("/auth/login", () => ok({ ...LOGIN, token: "T-2" }));
    let first = true;
    route("/rpc/Settings/Version", (init) => {
      const h = init.headers as Record<string, string>;
      if (first) {
        first = false;
        return err(401, "unauthorized", "Hay que iniciar sesión.");
      }
      return h.Authorization === "Bearer T-2" ? ok("0.3.0") : err(500, "internal", "token viejo");
    });
    await expect(rpcCall("Settings", "Version", [])).resolves.toBe("0.3.0");
    expect(useConnection.getState().needLogin).toBe(false);
  });

  it("modo local: un 401 relee el token del escritorio (reinicio del servidor local)", async () => {
    await useConnection.getState().boot();
    vi.spyOn(desktop, "connectionInfo").mockResolvedValue(local({ token: "LOCAL-2" }));
    expect(await renewSession()).toBe(true);
    expect(session.token).toBe("LOCAL-2");
  });

  it("sin forma de renovar: needLogin y token borrado", async () => {
    await useConnection.getState().boot();
    expect(await renewSession()).toBe(false);
    expect(useConnection.getState().needLogin).toBe(true);
    expect(session.token).toBe("");
  });

  it("cerrar sesión borra el token y vuelve a pedirlo", async () => {
    vi.spyOn(desktop, "connectionInfo").mockResolvedValue(remote());
    await useConnection.getState().boot();
    await useConnection.getState().login("ana", "una-clave-larga");
    await useConnection.getState().logout();
    expect(calls("/auth/logout")).toHaveLength(1);
    expect(useConnection.getState()).toMatchObject({ ready: false, needLogin: true });
    expect(session.token).toBe("");
    expect(sessionStorage.getItem("dc:session")).toBeNull();
  });
});

describe("estado del enlace", () => {
  it("sigue al evento interno `connection` y, al volver, renueva el media-token", async () => {
    await useConnection.getState().boot();
    expect(calls("/auth/media-token")).toHaveLength(1);
    emitEvent("connection", { state: "reconnecting", reconnected: false });
    expect(useConnection.getState().link).toBe("reconnecting");
    emitEvent("connection", { state: "offline", reconnected: false });
    expect(useConnection.getState().link).toBe("offline");
    emitEvent("connection", { state: "connected", reconnected: true });
    expect(useConnection.getState().link).toBe("connected");
    await vi.waitFor(() => expect(calls("/auth/media-token")).toHaveLength(2));
  });
});

describe("Probar", () => {
  it("health + login: versión, API, TLS y el token para guardar", async () => {
    route("/health", () => ok({ ...HEALTH, tls: true }));
    vi.spyOn(desktop, "connectionInfo").mockResolvedValue({
      mode: "remote",
      baseUrl: "https://srv:8443",
      tlsFingerprint: "AB:CD",
    });
    const r = await useConnection.getState().probe("srv:8443", "ana", "clave");
    expect(r).toMatchObject({ base: "https://srv:8443", version: "0.3.0", apiVersion: 1, tls: true, user: "admin", token: "T-login", tlsFingerprint: "AB:CD", warning: "" });
    expect(String(fetchMock.mock.calls[0][0])).toBe("https://srv:8443/api/v1/health");
    // sin sesión: ni Authorization en la prueba
    expect((fetchMock.mock.calls[0][1].headers as Record<string, string>).Authorization).toBeUndefined();
    // probar no cambia la sesión actual
    expect(session.baseUrl).toBe("");
  });

  it("dirección inválida y servidor sin respuesta", async () => {
    await expect(useConnection.getState().probe("ftp://x", "a", "b")).rejects.toMatchObject({ message: "La dirección no es válida." });
    route("/health", () => {
      throw new TypeError("x");
    });
    await expect(useConnection.getState().probe("https://caido", "a", "b")).rejects.toMatchObject({ code: "network" });
  });

  it("avisa si la API es otra", async () => {
    route("/health", () => ok({ ...HEALTH, apiVersion: 7 }));
    const r = await useConnection.getState().probe("https://x", "a", "b");
    expect(r.warning).toBe(API_VERSION_WARNING);
  });

  it("guardar manda la configuración al escritorio y recarga", async () => {
    const save = vi.spyOn(desktop, "saveRemote").mockResolvedValue();
    await useConnection.getState().saveRemote("srv:8443", " ana ", "clave", "TK");
    expect(save).toHaveBeenCalledWith({ url: "https://srv:8443", user: "ana", password: "clave" });
    expect(sessionStorage.getItem("dc:session")).toContain("TK");
    expect(env.reload).toHaveBeenCalled();
  });

  it("volver a Local", async () => {
    configureSession({ baseUrl: "https://srv:8443" });
    const use = vi.spyOn(desktop, "useLocal").mockResolvedValue();
    await useConnection.getState().useLocal();
    expect(use).toHaveBeenCalled();
    expect(env.reload).toHaveBeenCalled();
  });
});

describe("evento desktop:connection", () => {
  function inDesktop() {
    (window as unknown as { chrome: unknown }).chrome = { webview: { postMessage: () => undefined } };
    ensureDesktopListener();
    const call = wails.On.mock.calls.find((c) => c[0] === "desktop:connection");
    expect(call).toBeTruthy();
    return (data: unknown) => call![1]({ data });
  }

  it("solo se suscribe dentro de Wails y una sola vez", () => {
    ensureDesktopListener();
    expect(wails.On).not.toHaveBeenCalled();
    inDesktop();
    ensureDesktopListener();
    expect(wails.On.mock.calls.filter((c) => c[0] === "desktop:connection")).toHaveLength(1);
  });

  it("con la interfaz abierta: reiniciando solo guarda el estado; al quedar listo pide ConnectionInfo y re-apunta al puerto nuevo", async () => {
    await useConnection.getState().boot();
    expect(session.baseUrl).toBe("http://127.0.0.1:5000");
    const emit = inDesktop();
    const info = vi.mocked(desktop.connectionInfo);
    info.mockClear();

    emit({ mode: "local", state: "restarting", error: "", retryInMs: 3000 });
    expect(useConnection.getState().desktop).toMatchObject({ state: "restarting", retryInMs: 3000 });
    expect(info).not.toHaveBeenCalled();
    expect(useConnection.getState().ready).toBe(true);

    info.mockResolvedValue(local({ baseUrl: "http://127.0.0.1:5999", token: "NUEVO", serverVersion: "0.3.1" }));
    const mediaBefore = calls("/auth/media-token").length;
    emit({ mode: "local", state: "ready", error: "", retryInMs: 0 });
    await vi.waitFor(() => expect(session).toMatchObject({ baseUrl: "http://127.0.0.1:5999", token: "NUEVO" }));
    await vi.waitFor(() => expect(calls("/auth/media-token").length).toBeGreaterThan(mediaBefore));
    expect(info).toHaveBeenCalledTimes(1);
    expect(useConnection.getState()).toMatchObject({ baseUrl: "http://127.0.0.1:5999", serverVersion: "0.3.1", ready: true });
    expect(useConnection.getState().desktop?.state).toBe("ready");
    expect(env.reload).not.toHaveBeenCalled();
  });

  it("varios «ready» seguidos comparten una sola consulta", async () => {
    await useConnection.getState().boot();
    const emit = inDesktop();
    const info = vi.mocked(desktop.connectionInfo);
    info.mockClear();
    emit({ mode: "local", state: "ready" });
    emit({ mode: "local", state: "ready" });
    await vi.waitFor(() => expect(info).toHaveBeenCalled());
    expect(info).toHaveBeenCalledTimes(1);
  });

  it("si cambia el modo (local ↔ remoto) recarga la ventana", async () => {
    await useConnection.getState().boot();
    const emit = inDesktop();
    vi.mocked(desktop.connectionInfo).mockResolvedValue({ mode: "remote", baseUrl: "http://127.0.0.1:6000", token: "R", user: "ana" });
    emit({ mode: "remote", state: "ready" });
    await vi.waitFor(() => expect(env.reload).toHaveBeenCalled());
  });

  it("si ConnectionInfo falla, no tumba la interfaz: guarda el motivo", async () => {
    await useConnection.getState().boot();
    const emit = inDesktop();
    vi.mocked(desktop.connectionInfo).mockRejectedValue(new Error("El servidor local no arrancó."));
    emit({ mode: "local", state: "ready" });
    await vi.waitFor(() => expect(useConnection.getState().error).toBe("El servidor local no arrancó."));
    expect(useConnection.getState().ready).toBe(true);
  });

  it("antes de estar listo: «unavailable» muestra Sin conexión con el motivo; «starting» vuelve a Conectando", () => {
    const emit = inDesktop();
    emit({ mode: "local", state: "unavailable", error: "dConnector-server no se encontró.", retryInMs: 5000 });
    expect(useConnection.getState()).toMatchObject({ ready: false, link: "offline", error: "dConnector-server no se encontró." });
    emit({ mode: "local", state: "starting" });
    expect(useConnection.getState()).toMatchObject({ link: "connecting", error: "" });
  });

  it("antes de estar listo, «ready» arranca la conexión", async () => {
    const emit = inDesktop();
    emit({ mode: "local", state: "ready" });
    await vi.waitFor(() => expect(useConnection.getState().ready).toBe(true));
  });

  it("textos del estado", () => {
    expect(desktopPhaseText({ mode: "local", state: "restarting" })).toBe("Reiniciando el servidor local…");
    expect(desktopPhaseText({ mode: "remote", state: "disconnected" })).toBe("Se perdió la conexión con el servidor remoto.");
    expect(desktopPhaseText({ mode: "local", state: "ready" })).toBe("");
  });
});

describe("cambio de servidor (R3)", () => {
  it("al re-apuntar a otra dirección no se lleva el token del servidor anterior", async () => {
    await useConnection.getState().boot();
    expect(session.token).toBe("LOCAL");
    vi.spyOn(desktop, "connectionInfo").mockResolvedValue({ mode: "local", baseUrl: "http://127.0.0.1:6000" });
    const { rebindToDesktop } = await import("./connection");
    await rebindToDesktop();
    expect(session.baseUrl).toBe("http://127.0.0.1:6000");
    expect(session.token).toBe("");
    expect(useConnection.getState().needLogin).toBe(true);
  });
});
