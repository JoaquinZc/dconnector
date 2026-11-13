import { describe, expect, it } from "vitest";
import {
  composerReducer,
  exceedsLimit,
  initialComposer,
  kindForMime,
  makeAttachment,
  outgoingForAttachment,
  outgoingText,
  shouldPingTyping,
  TYPING_INTERVAL_MS,
} from "./composerReducer";
import { msg } from "./testkit";

const file = (path: string, mime = "image/png", size = 100) => ({ path, name: path.split("/").pop()!, mime, size });

describe("composerReducer", () => {
  it("texto, inserción en el cursor y carga", () => {
    let s = composerReducer(initialComposer, { type: "text", text: "Hola mundo" });
    s = composerReducer(s, { type: "insert", at: 4, value: "😀" });
    expect(s.text).toBe("Hola😀 mundo");
    s = composerReducer(s, { type: "insert", at: 999, value: "!" });
    expect(s.text.endsWith("!")).toBe(true);
    expect(composerReducer(s, { type: "load", text: "otro" }).text).toBe("otro");
  });

  it("añade archivos sin repetir y marca el primero como activo", () => {
    let s = composerReducer(initialComposer, { type: "addFiles", files: [makeAttachment(file("/t/a.png")), makeAttachment(file("/t/b.mp4", "video/mp4"))] });
    expect(s.attachments.map((a) => a.kind)).toEqual(["image", "video"]);
    expect(s.active).toBe(0);
    const again = composerReducer(s, { type: "addFiles", files: [makeAttachment(file("/t/a.png"))] });
    expect(again).toBe(s);
    s = composerReducer(s, { type: "select", index: 1 });
    expect(s.active).toBe(1);
    s = composerReducer(s, { type: "removeFile", id: s.attachments[1].id });
    expect(s.attachments).toHaveLength(1);
    expect(s.active).toBe(0);
  });

  it("pie por archivo y limpieza", () => {
    let s = composerReducer(initialComposer, { type: "addFiles", files: [makeAttachment(file("/t/a.png"))] });
    s = composerReducer(s, { type: "caption", id: s.attachments[0].id, caption: "Mira" });
    expect(s.attachments[0].caption).toBe("Mira");
    expect(composerReducer(s, { type: "clearFiles" }).attachments).toEqual([]);
    expect(composerReducer(s, { type: "reset" })).toEqual(initialComposer);
    expect(composerReducer(s, { type: "sending", value: true }).sending).toBe(true);
  });
});

describe("tipos y salida", () => {
  it("kindForMime", () => {
    expect(kindForMime("image/jpeg")).toBe("image");
    expect(kindForMime("video/mp4")).toBe("video");
    expect(kindForMime("audio/mpeg")).toBe("audio");
    expect(kindForMime("application/pdf")).toBe("document");
    expect(kindForMime("image/webp", "sticker")).toBe("sticker");
  });

  it("outgoingForAttachment: el pie solo en foto, vídeo y documento; cita en la primera", () => {
    const reply = msg({ chatNativeId: "c1", nativeId: "q1", senderNativeId: "x", fromMe: false, timestamp: 5 });
    const img = { ...makeAttachment(file("/t/a.png")), caption: "  pie  " };
    const o = outgoingForAttachment(img, reply);
    expect(o).toMatchObject({ kind: "image", text: "pie", filePath: "/t/a.png", fileName: "a.png", mime: "image/png" });
    expect(o.replyTo).toEqual({ chatNativeId: "c1", nativeId: "q1", senderNativeId: "x", fromMe: false, timestamp: 5 });
    const sticker = { ...makeAttachment(file("/t/s.webp", "image/webp"), "sticker"), caption: "no" };
    expect(outgoingForAttachment(sticker, null)).toMatchObject({ kind: "sticker", text: "", replyTo: null });
  });

  it("outgoingText recorta", () => {
    expect(outgoingText("  hola ", null)).toMatchObject({ kind: "text", text: "hola", replyTo: null });
  });

  it("exceedsLimit según el tipo", () => {
    const caps = { maxFileSize: 100, maxImageSize: 10, maxVideoSize: 50 };
    expect(exceedsLimit({ kind: "image", size: 11 }, caps)).toBe(true);
    expect(exceedsLimit({ kind: "video", size: 11 }, caps)).toBe(false);
    expect(exceedsLimit({ kind: "document", size: 101 }, caps)).toBe(true);
    expect(exceedsLimit({ kind: "document", size: 5000 }, { maxFileSize: 0 })).toBe(false);
    expect(exceedsLimit({ kind: "document", size: 5000 }, null)).toBe(false);
  });
});

describe("aviso de escritura", () => {
  it("como mucho cada 4 s", () => {
    expect(shouldPingTyping(0, 1000)).toBe(false);
    expect(shouldPingTyping(1000, 1000 + TYPING_INTERVAL_MS - 1)).toBe(false);
    expect(shouldPingTyping(1000, 1000 + TYPING_INTERVAL_MS)).toBe(true);
    expect(shouldPingTyping(0, Date.now())).toBe(true);
  });
});
