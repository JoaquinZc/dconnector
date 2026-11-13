// Revisión R3 (seguridad y robustez): reintentos, URLs de medios, subidas y cambio de servidor.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@wailsio/runtime", () => ({ Call: { ByName: vi.fn() }, Events: { On: vi.fn(() => () => undefined), Emit: vi.fn() } }));

import { ApiError, httpConfig, isReadOnlyRpc, rpcCall } from "./http";
import { mediaUrl, setMediaToken, stopMediaToken } from "./media";
import { configureSession, resetSession } from "./session";
import { MAX_UPLOAD_BYTES, safeFileName, uploadFile } from "./webfiles";

const fetchMock = vi.fn();
const res = (status: number, body: unknown = {}, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json", ...headers } });

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
  httpConfig.retryDelays = [1, 1];
  resetSession();
  configureSession({ baseUrl: "https://srv", token: "T" });
});
afterEach(() => {
  vi.unstubAllGlobals();
  stopMediaToken();
});

describe("503 y reintentos", () => {
  it("un envío (Messaging.Send) NO se repite tras un 503", async () => {
    fetchMock.mockResolvedValue(res(503, { error: { code: "unavailable", message: "x" } }));
    await expect(rpcCall("Messaging", "Send", ["k", {}])).rejects.toBeInstanceOf(ApiError);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("Forward, Save, SendNow tampoco; una lectura (ListChats) sí", async () => {
    for (const m of ["Forward", "Save", "SendNow", "Launch", "Edit", "Delete"]) expect(isReadOnlyRpc(m)).toBe(false);
    fetchMock.mockResolvedValueOnce(res(503)).mockResolvedValueOnce(res(200, []));
    await expect(rpcCall("Inbox", "ListChats", [{}])).resolves.toEqual([]);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("un corte de red en un envío no se repite", async () => {
    fetchMock.mockRejectedValue(new TypeError("fail"));
    await expect(rpcCall("Messaging", "Send", [])).rejects.toMatchObject({ code: "network" });
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("429 conserva Retry-After y lo dice en el mensaje", async () => {
    httpConfig.rateRetries = 0; // aquí solo se prueba el mensaje, no el reintento
    fetchMock.mockResolvedValue(res(429, {}, { "Retry-After": "12" }));
    const e = (await rpcCall("Inbox", "Search", []).catch((x) => x)) as ApiError;
    expect(e.status).toBe(429);
    expect(e.retryAfter).toBe(12);
    expect(e.message).toContain("12 s");
    httpConfig.rateRetries = 2;
  });
});

describe("mediaUrl", () => {
  it("descarta javascript: y data:text/html; deja http(s), blob: y data:image", () => {
    expect(mediaUrl("javascript:alert(1)")).toBe("");
    expect(mediaUrl("data:text/html,<script>1</script>")).toBe("");
    expect(mediaUrl("vbscript:x")).toBe("");
    expect(mediaUrl("https://x/y.png")).toBe("https://x/y.png");
    expect(mediaUrl("blob:https://a/1")).toBe("blob:https://a/1");
    expect(mediaUrl("data:image/png;base64,AAA")).toBe("data:image/png;base64,AAA");
  });

  it("el media-token de un servidor no se manda a otro", () => {
    setMediaToken("TOK-A");
    expect(mediaUrl("/media/a/b")).toBe("https://srv/api/v1/media/a/b?token=TOK-A");
    configureSession({ baseUrl: "https://otro" });
    expect(mediaUrl("/media/a/b")).toBe("https://otro/api/v1/media/a/b");
  });
});

describe("subidas", () => {
  it("rechaza más de 100 MiB sin tocar la red", async () => {
    const big = { size: MAX_UPLOAD_BYTES + 1, type: "" } as unknown as Blob;
    await expect(uploadFile(big, "a.bin")).rejects.toMatchObject({ code: "too_large", status: 413 });
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("limpia nombres con rutas, control e inversión de texto", () => {
    expect(safeFileName("../../etc/passwd")).toBe("passwd");
    expect(safeFileName("C:\\x\\foo.png")).toBe("foo.png");
    expect(safeFileName("a\u202egpj.exe")).toBe("agpj.exe");
    expect(safeFileName("..")).toBe("archivo");
    expect(safeFileName("")).toBe("archivo");
    expect(safeFileName("fiesta 🎉 ñandú.jpg")).toBe("fiesta 🎉 ñandú.jpg");
  });

  it("acepta una cancelación (signal)", async () => {
    const ac = new AbortController();
    fetchMock.mockImplementation((_u: string, init: RequestInit) => {
      expect(init.signal).toBe(ac.signal);
      return Promise.resolve(res(200, { id: "1", name: "a", mime: "x", size: 1 }));
    });
    await uploadFile(new Blob(["a"]), "a.txt", ac.signal);
  });
});
