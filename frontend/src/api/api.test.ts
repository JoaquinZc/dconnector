import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const wails = vi.hoisted(() => ({
  ByName: vi.fn(),
  Emit: vi.fn(),
}));
vi.mock("@wailsio/runtime", () => ({
  Call: { ByName: wails.ByName },
  Events: { On: vi.fn(() => () => undefined), Emit: wails.Emit },
}));

import { api, errorMessage } from "./index";
import { useConnection } from "./connection";
import { EV, EVENT_NAMES } from "./events";
import { ApiError, httpConfig } from "./http";
import { configureSession, resetSession } from "./session";
import { useUI } from "@/store/ui";

const fetchMock = vi.fn();

function json(body: unknown, status = 200, headers: Record<string, string> = {}): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json", ...headers },
  });
}
const apiError = (status: number, code: string, message: string, headers: Record<string, string> = {}) =>
  json({ error: { code, message } }, status, headers);

function lastCall() {
  const [url, init] = fetchMock.mock.calls.at(-1) as [string, RequestInit];
  return { url, init, headers: init.headers as Record<string, string>, body: init.body as string };
}

function setWails(on: boolean) {
  const w = window as unknown as { chrome?: unknown };
  if (on) w.chrome = { webview: { postMessage: () => undefined } };
  else delete w.chrome;
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.stubGlobal("fetch", fetchMock);
  fetchMock.mockReset();
  httpConfig.retryDelays = [0, 0];
  resetSession();
  configureSession({ baseUrl: "http://127.0.0.1:5000", token: "tok", connId: "" });
  useUI.setState({ toasts: [] });
  useConnection.setState({ mode: "local", link: "connected", needLogin: false });
  setWails(false);
});

afterEach(() => {
  vi.unstubAllGlobals();
  setWails(false);
});

describe("api (HTTP)", () => {
  it("llama a POST /api/v1/rpc/{Servicio}/{Método} con el array de argumentos y el Bearer", async () => {
    fetchMock.mockResolvedValue(json(null));
    await api.accounts.Delete("wa-1");
    const c = lastCall();
    expect(c.url).toBe("http://127.0.0.1:5000/api/v1/rpc/Accounts/Delete");
    expect(c.init.method).toBe("POST");
    expect(JSON.parse(c.body)).toEqual(["wa-1"]);
    expect(c.headers.Authorization).toBe("Bearer tok");
    expect(c.headers["Content-Type"]).toBe("application/json");
    expect(c.headers["X-DConnector-Client"]).toMatch(/^dconnector-ui\//);
  });

  it("sin argumentos manda []", async () => {
    fetchMock.mockResolvedValue(json([]));
    await api.accounts.List();
    expect(JSON.parse(lastCall().body)).toEqual([]);
  });

  it("devuelve lo que responde el servidor", async () => {
    fetchMock.mockResolvedValue(json([{ account: { id: "wa-1" } }]));
    await expect(api.accounts.List()).resolves.toEqual([{ account: { id: "wa-1" } }]);
  });

  it("las listas nulas llegan como []", async () => {
    fetchMock.mockImplementation(async () => json(null));
    await expect(api.accounts.List()).resolves.toEqual([]);
    await expect(api.inbox.ListChats({} as never)).resolves.toEqual([]);
  });

  it("manda X-DConnector-Conn cuando hay connId", async () => {
    configureSession({ connId: "c-77" });
    fetchMock.mockResolvedValue(json(null));
    await api.notifications.SetFocus("wa-1|123", true);
    const c = lastCall();
    expect(c.url).toContain("/rpc/Notifications/SetFocus");
    expect(JSON.parse(c.body)).toEqual(["wa-1|123", true]);
    expect(c.headers["X-DConnector-Conn"]).toBe("c-77");
  });

  it("ante un error con código: toast con el mensaje del servidor y se relanza", async () => {
    fetchMock.mockResolvedValue(apiError(409, "not_connected", "La cuenta no está conectada."));
    const p = api.accounts.Delete("wa-1");
    await expect(p).rejects.toMatchObject({ code: "not_connected", status: 409, message: "La cuenta no está conectada." });
    const toasts = useUI.getState().toasts;
    expect(toasts).toHaveLength(1);
    expect(toasts[0]).toMatchObject({ kind: "error", message: "La cuenta no está conectada." });
  });

  it("LoadOlder no molesta con avisos", async () => {
    fetchMock.mockResolvedValue(apiError(422, "unsupported", "no admitido por este canal"));
    await expect(api.inbox.LoadOlder("wa-1|x")).rejects.toBeInstanceOf(ApiError);
    expect(useUI.getState().toasts).toHaveLength(0);
  });

  it("un error sin cuerpo JSON usa un texto en español según el estado", async () => {
    fetchMock.mockResolvedValue(new Response("<html>", { status: 500 }));
    await expect(api.accounts.Delete("x")).rejects.toMatchObject({ code: "internal", message: "El servidor tuvo un error inesperado." });
  });

  it("429 conserva Retry-After", async () => {
    fetchMock.mockResolvedValue(apiError(429, "rate_limited", "Demasiados intentos.", { "Retry-After": "30" }));
    await expect(api.accounts.Delete("x")).rejects.toMatchObject({ code: "rate_limited", retryAfter: 30 });
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("401: pide renovar la sesión una vez, reintenta con el token nuevo y sin toast", async () => {
    const { setHttpHooks } = await import("./http");
    const renew = vi.fn(async () => {
      configureSession({ token: "nuevo" });
      return true;
    });
    setHttpHooks({ onUnauthorized: renew });
    fetchMock.mockResolvedValueOnce(apiError(401, "unauthorized", "Hay que iniciar sesión.")).mockResolvedValueOnce(json("ok"));
    await expect(api.settings.Version()).resolves.toBe("ok");
    expect(renew).toHaveBeenCalledTimes(1);
    expect(lastCall().headers.Authorization).toBe("Bearer nuevo");
    expect(useUI.getState().toasts).toHaveLength(0);
    setHttpHooks({});
  });

  it("401 sin forma de renovar: falla sin toast (lo muestra la pantalla de inicio de sesión)", async () => {
    const { setHttpHooks } = await import("./http");
    setHttpHooks({ onUnauthorized: async () => false });
    fetchMock.mockResolvedValue(apiError(401, "unauthorized", "Hay que iniciar sesión."));
    await expect(api.settings.Version()).rejects.toMatchObject({ code: "unauthorized" });
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(useUI.getState().toasts).toHaveLength(0);
    setHttpHooks({});
  });

  it("503 (servidor arrancando) se reintenta y acaba bien", async () => {
    fetchMock
      .mockResolvedValueOnce(apiError(503, "unavailable", "Arrancando."))
      .mockResolvedValueOnce(apiError(503, "unavailable", "Arrancando."))
      .mockResolvedValueOnce(json("0.3.0"));
    await expect(api.settings.Version()).resolves.toBe("0.3.0");
    expect(fetchMock).toHaveBeenCalledTimes(3);
  });

  it("503 persistente: tras los reintentos falla con el código", async () => {
    fetchMock.mockImplementation(async () => apiError(503, "unavailable", "Arrancando."));
    await expect(api.settings.Version()).rejects.toMatchObject({ code: "unavailable" });
    expect(fetchMock).toHaveBeenCalledTimes(3);
  });

  it("sin red: ApiError network; con el enlace caído no hay toast por cada llamada", async () => {
    fetchMock.mockRejectedValue(new TypeError("Failed to fetch"));
    await expect(api.settings.Version()).rejects.toMatchObject({ code: "network", message: "No se pudo conectar con el servidor." });
    expect(useUI.getState().toasts).toHaveLength(1); // enlace «conectado»: sí se avisa
    useUI.setState({ toasts: [] });
    useConnection.setState({ link: "offline" });
    await expect(api.settings.Version()).rejects.toMatchObject({ code: "network" });
    expect(useUI.getState().toasts).toHaveLength(0);
    expect(fetchMock).toHaveBeenCalledTimes(2); // sin reintentos de red
  });
});

describe("api: métodos de escritorio", () => {
  it("en modo local con escritorio van por los enlaces de Wails", async () => {
    setWails(true);
    wails.ByName.mockResolvedValue([{ path: "C:\\a.png", name: "a.png", mime: "image/png", size: 3 }]);
    await expect(api.files.PickFiles("media")).resolves.toHaveLength(1);
    expect(wails.ByName).toHaveBeenCalledWith("dconnector/internal/adapter/ui/wails.FilesService.PickFiles", "media");
    expect(fetchMock).not.toHaveBeenCalled();

    wails.ByName.mockResolvedValue(null);
    await expect(api.files.PickFiles("any")).resolves.toEqual([]);

    wails.ByName.mockResolvedValue("C:\\guardado.jpg");
    await expect(api.messaging.SaveMediaAs("wa-1", "m1", "foto.jpg")).resolves.toBe("C:\\guardado.jpg");
    expect(wails.ByName).toHaveBeenLastCalledWith("dconnector/internal/adapter/ui/wails.FilesService.SaveFileAs", "foto.jpg", "/media/wa-1/m1");
    await api.messaging.SaveMediaAs("wa-1", "m2");
    expect(wails.ByName).toHaveBeenLastCalledWith("dconnector/internal/adapter/ui/wails.FilesService.SaveFileAs", "m2", "/media/wa-1/m2");

    wails.ByName.mockResolvedValue(undefined);
    await api.settings.OpenDataDir();
    expect(wails.ByName).toHaveBeenLastCalledWith("dconnector/internal/adapter/ui/wails.SettingsService.OpenDataDir");
  });

  it("las notificaciones de Windows van por Wails; SetFocus por RPC", async () => {
    setWails(true);
    wails.ByName.mockResolvedValue({ state: "granted" });
    await expect(api.notifications.Status()).resolves.toMatchObject({ state: "granted" });
    expect(wails.ByName).toHaveBeenCalledWith("dconnector/internal/adapter/ui/wails.NotificationsService.Status");
    await api.notifications.OpenSystemSettings();
    await api.notifications.MarkAsked();
    await api.notifications.SendTest();
    await api.notifications.RequestPermission();
    expect(wails.ByName).toHaveBeenCalledTimes(5);
    fetchMock.mockResolvedValue(json(null));
    await api.notifications.SetFocus("", false);
    expect(lastCall().url).toContain("/rpc/Notifications/SetFocus");
  });

  it("Status falla en silencio (sin toast) si Wails falla", async () => {
    setWails(true);
    wails.ByName.mockRejectedValue(new Error("sin servicio"));
    await expect(api.notifications.Status()).rejects.toThrow("sin servicio");
    expect(useUI.getState().toasts).toHaveLength(0);
  });

  it("sin escritorio, Status dice «no disponible» y el resto no hace nada", async () => {
    await expect(api.notifications.Status()).resolves.toMatchObject({ state: "unsupported", asked: true });
    await expect(api.notifications.OpenSystemSettings()).resolves.toBeUndefined();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("en remoto abrir carpetas se rechaza (not_available)", async () => {
    setWails(true);
    useConnection.setState({ mode: "remote" });
    await expect(api.settings.OpenDataDir()).rejects.toMatchObject({ code: "not_available" });
    expect(wails.ByName).not.toHaveBeenCalled();
  });

  it("en remoto con escritorio, PickFiles abre el diálogo nativo y sube cada archivo (upload://<id>)", async () => {
    setWails(true);
    useConnection.setState({ mode: "remote" });
    wails.ByName.mockImplementation(async (name: string, ...args: unknown[]) => {
      if (name.endsWith("FilesService.PickFiles")) {
        return [
          { path: "C:/a.png", name: "a.png", mime: "image/png", size: 3 },
          { path: "C:/b.pdf", name: "b.pdf", mime: "application/pdf", size: 9 },
        ];
      }
      if (name.endsWith("FilesService.Upload")) return { id: args[0] === "C:/a.png" ? "U1" : "U2", name: "x", mime: "m/x", size: 1 };
      throw new Error(name);
    });
    const out = await api.files.PickFiles("any");
    expect(out.map((f) => f.path)).toEqual(["upload://U1", "upload://U2"]);
    expect(out[0]).toEqual({ path: "upload://U1", name: "x", mime: "m/x", size: 1 });
    expect(wails.ByName).toHaveBeenCalledWith("dconnector/internal/adapter/ui/wails.FilesService.Upload", "C:/a.png");
    expect(fetchMock).not.toHaveBeenCalled();
    wails.ByName.mockReset();
  });

  it("en remoto con escritorio, cancelar el diálogo no sube nada", async () => {
    setWails(true);
    useConnection.setState({ mode: "remote" });
    wails.ByName.mockResolvedValue(null);
    await expect(api.files.PickFiles("media")).resolves.toEqual([]);
    expect(wails.ByName).toHaveBeenCalledTimes(1);
  });

  it("en remoto con escritorio, «Guardar como» usa Files.SaveFileAs y no el navegador", async () => {
    setWails(true);
    useConnection.setState({ mode: "remote" });
    wails.ByName.mockResolvedValue("D:/x.jpg");
    await expect(api.messaging.SaveMediaAs("wa-1", "m9", "x.jpg")).resolves.toBe("D:/x.jpg");
    expect(wails.ByName).toHaveBeenLastCalledWith("dconnector/internal/adapter/ui/wails.FilesService.SaveFileAs", "x.jpg", "/media/wa-1/m9");
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("en remoto SaveTemp sube a /files y devuelve upload://<id>", async () => {
    useConnection.setState({ mode: "remote" });
    fetchMock.mockResolvedValue(json({ id: "01JABC", name: "nota.ogg", mime: "audio/ogg", size: 4 }));
    const f = await api.files.SaveTemp("nota.ogg", btoa("abcd"));
    expect(f).toEqual({ path: "upload://01JABC", name: "nota.ogg", mime: "audio/ogg", size: 4 });
    const c = lastCall();
    expect(c.url).toBe("http://127.0.0.1:5000/api/v1/files");
    expect(c.body).toBeInstanceOf(FormData);
    expect((c.body as unknown as FormData).get("file")).toBeInstanceOf(Blob);
    // multipart: el navegador pone su propio Content-Type
    expect(c.headers["Content-Type"]).toBeUndefined();
  });

  it("en remoto SaveMediaAs descarga /media/… con la sesión y guarda con el navegador", async () => {
    useConnection.setState({ mode: "remote" });
    URL.createObjectURL = vi.fn(() => "blob:x");
    URL.revokeObjectURL = vi.fn();
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => undefined);
    fetchMock.mockResolvedValue(
      new Response("datos", { headers: { "Content-Type": "image/jpeg", "Content-Disposition": 'attachment; filename="foto.jpg"' } }),
    );
    await expect(api.messaging.SaveMediaAs("wa-1", "m9")).resolves.toBe("foto.jpg");
    expect(lastCall().url).toBe("http://127.0.0.1:5000/api/v1/media/wa-1/m9");
    expect(lastCall().headers.Authorization).toBe("Bearer tok");
    expect(click).toHaveBeenCalled();
    click.mockRestore();
  });
});

describe("eventos", () => {
  it("history:sync y los eventos nuevos están registrados", () => {
    expect(EV.historySync).toBe("history:sync");
    expect(EVENT_NAMES).toContain("history:sync");
    expect(EVENT_NAMES).toEqual(expect.arrayContaining(["hello", "notify", "notify:decision", "connection"]));
  });
});

describe("errorMessage", () => {
  it("entiende cadenas, errores, objetos y JSON", () => {
    expect(errorMessage("falló")).toBe("falló");
    expect(errorMessage(new Error("boom"))).toBe("boom");
    expect(errorMessage({ message: "obj" })).toBe("obj");
    expect(errorMessage(new ApiError("invalid", "El número no es válido.", 400))).toBe("El número no es válido.");
    expect(errorMessage(new Error('{"message":"externo","cause":{"message":"causa real"}}'))).toBe("causa real");
    expect(errorMessage(new Error('{"message":"solo mensaje"}'))).toBe("solo mensaje");
    expect(errorMessage(undefined)).toBe("Ocurrió un error inesperado");
  });
});
