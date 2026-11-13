import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MEDIA_RENEW_MARGIN_MS, currentMediaToken, mediaUrl, refreshMediaToken, stopMediaToken } from "./media";
import { configureSession, resetSession } from "./session";

const fetchMock = vi.fn();

beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal("fetch", fetchMock);
  fetchMock.mockReset();
  stopMediaToken();
  resetSession();
});

afterEach(() => {
  stopMediaToken();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("mediaUrl", () => {
  it("sin servidor configurado devuelve la ruta tal cual (pruebas)", () => {
    expect(mediaUrl("/media/wa-1/m1")).toBe("/media/wa-1/m1");
    expect(mediaUrl("")).toBe("");
    expect(mediaUrl(undefined)).toBe("");
    expect(mediaUrl(null)).toBe("");
  });

  it("convierte /media y /avatar en {base}/api/v1/…?token=<media-token>", async () => {
    configureSession({ baseUrl: "http://127.0.0.1:5000/", token: "sesion" });
    fetchMock.mockResolvedValue(new Response(JSON.stringify({ token: "m+t/1", expiresAt: Date.now() + 900_000 })));
    await refreshMediaToken();
    expect(mediaUrl("/media/wa-1/m1")).toBe("http://127.0.0.1:5000/api/v1/media/wa-1/m1?token=m%2Bt%2F1");
    expect(mediaUrl("/avatar/wa-1/ana.jpg")).toBe("http://127.0.0.1:5000/api/v1/avatar/wa-1/ana.jpg?token=m%2Bt%2F1");
    // nunca el token de sesión en la URL
    expect(mediaUrl("/media/wa-1/m1")).not.toContain("sesion");
  });

  it("respeta una consulta previa, las URL absolutas, data:, blob: y /local", async () => {
    configureSession({ baseUrl: "https://srv:8443", token: "s" });
    fetchMock.mockResolvedValue(new Response(JSON.stringify({ token: "T", expiresAt: Date.now() + 900_000 })));
    await refreshMediaToken();
    expect(mediaUrl("/media/a/b?r=1")).toBe("https://srv:8443/api/v1/media/a/b?r=1&token=T");
    expect(mediaUrl("https://cdn/x.png")).toBe("https://cdn/x.png");
    expect(mediaUrl("data:image/png;base64,AA")).toBe("data:image/png;base64,AA");
    expect(mediaUrl("blob:abc")).toBe("blob:abc");
    expect(mediaUrl("/local?path=C%3A%5Ca.png")).toBe("/local?path=C%3A%5Ca.png");
  });

  it("upload://<id> apunta a /files/<id>", async () => {
    configureSession({ baseUrl: "https://srv:8443", token: "s" });
    fetchMock.mockResolvedValue(new Response(JSON.stringify({ token: "T", expiresAt: Date.now() + 900_000 })));
    await refreshMediaToken();
    expect(mediaUrl("upload://01JXYZ")).toBe("https://srv:8443/api/v1/files/01JXYZ?token=T");
  });
});

describe("renovación del media-token", () => {
  it("pide uno nuevo antes de que caduque y usa el más reciente", async () => {
    configureSession({ baseUrl: "http://h", token: "s" });
    let n = 0;
    fetchMock.mockImplementation(
      async () => new Response(JSON.stringify({ token: `T${++n}`, expiresAt: Date.now() + 15 * 60_000 })),
    );
    await refreshMediaToken();
    expect(currentMediaToken()).toBe("T1");
    expect(fetchMock).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(15 * 60_000 - MEDIA_RENEW_MARGIN_MS - 1);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(mediaUrl("/media/a/b")).toBe("http://h/api/v1/media/a/b?token=T2");
  });

  it("si falla, reintenta al minuto sin romper nada", async () => {
    configureSession({ baseUrl: "http://h", token: "s" });
    fetchMock.mockRejectedValueOnce(new TypeError("x")).mockResolvedValue(new Response(JSON.stringify({ token: "OK", expiresAt: Date.now() + 900_000 })));
    await refreshMediaToken();
    expect(currentMediaToken()).toBe("");
    await vi.advanceTimersByTimeAsync(60_000);
    expect(currentMediaToken()).toBe("OK");
  });
});
