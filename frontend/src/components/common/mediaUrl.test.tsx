import { cleanup, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { setMediaToken, stopMediaToken } from "@/api/media";
import { configureSession, resetSession } from "@/api/session";
import { localUrl } from "@/components/chat/AttachmentPreview";
import { ChatAvatar } from "@/components/chat/parts";
import { retryUrl } from "@/components/chat/MediaState";
import Avatar from "./Avatar";

beforeEach(() => {
  resetSession();
  stopMediaToken();
});
afterEach(cleanup);

describe("los avatares y adjuntos usan mediaUrl", () => {
  it("sin servidor (pruebas) la ruta queda como viene", () => {
    const { container } = render(<Avatar src="/avatar/wa-1/ana.jpg" name="Ana" />);
    expect(container.querySelector("img")).toHaveAttribute("src", "/avatar/wa-1/ana.jpg");
  });

  it("con servidor: URL absoluta del servidor con el media-token", () => {
    configureSession({ baseUrl: "http://127.0.0.1:5000", token: "s" });
    setMediaToken("MT");
    const a = render(<Avatar src="/avatar/wa-1/ana.jpg" name="Ana" />);
    expect(a.container.querySelector("img")).toHaveAttribute("src", "http://127.0.0.1:5000/api/v1/avatar/wa-1/ana.jpg?token=MT");
    a.unmount();
    const c = render(<ChatAvatar title="Ana" avatarUrl="/avatar/wa-1/ana.jpg" />);
    expect(c.container.querySelector("img")).toHaveAttribute("src", "http://127.0.0.1:5000/api/v1/avatar/wa-1/ana.jpg?token=MT");
  });

  it("el reintento de un medio conserva el token y añade ?r=N", () => {
    configureSession({ baseUrl: "http://h", token: "s" });
    setMediaToken("MT");
    expect(retryUrl("http://h/api/v1/media/a/b?token=MT", 2)).toBe("http://h/api/v1/media/a/b?token=MT&r=2");
  });

  it("la vista previa de adjuntos: upload:// va al servidor y las rutas locales a /local", () => {
    configureSession({ baseUrl: "http://h", token: "s" });
    setMediaToken("MT");
    expect(localUrl("upload://01J1")).toBe("http://h/api/v1/files/01J1?token=MT");
    expect(localUrl("C:/fotos/a.png")).toBe("/local?path=C%3A%2Ffotos%2Fa.png");
  });
});
