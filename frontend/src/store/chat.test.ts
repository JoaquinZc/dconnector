import { describe, expect, it } from "vitest";
import type { Message, ReceiptEvent, TypingEvent } from "@/api/types";
import {
  applyReceipt,
  applyTyping,
  betterDelivery,
  typingLabel,
  updateMessage,
  upsertMessages,
  type TypingMap,
} from "./chat.logic";

const CHAT = "wa-1|chat1";
const msg = (n: number, over: Record<string, unknown> = {}): Message =>
  ({
    key: `wa-1|chat1|m${n}`,
    chatKey: CHAT,
    accountId: "wa-1",
    channel: "whatsapp",
    nativeId: `m${n}`,
    chatNativeId: "chat1",
    senderNativeId: "x",
    senderName: "X",
    fromMe: false,
    timestamp: n * 1000,
    kind: "text",
    text: `texto ${n}`,
    editedAt: 0,
    deletedAt: 0,
    delivery: "sent",
    forwarded: false,
    ...over,
  }) as unknown as Message;

describe("upsertMessages", () => {
  it("inserta ordenado de antiguo a reciente", () => {
    const next = upsertMessages([msg(1), msg(3)], [msg(2)], CHAT);
    expect(next.map((m) => m.nativeId)).toEqual(["m1", "m2", "m3"]);
  });
  it("reemplaza por clave, sin duplicar", () => {
    const next = upsertMessages([msg(1)], [msg(1, { text: "nuevo" })], CHAT);
    expect(next).toHaveLength(1);
    expect(next[0].text).toBe("nuevo");
  });
  it("ignora los de otros chats", () => {
    const base = [msg(1)];
    const other = msg(2, { chatKey: "wa-1|otro" });
    expect(upsertMessages(base, [other], CHAT)).toBe(base);
  });
  it("un upsert tardío no baja la entrega", () => {
    const next = upsertMessages([msg(1, { delivery: "read" })], [msg(1, { delivery: "sent" })], CHAT);
    expect(next[0].delivery).toBe("read");
  });
  it("mensajes con la misma hora se ordenan por clave", () => {
    const next = upsertMessages([], [msg(2, { timestamp: 5, key: "b" }), msg(1, { timestamp: 5, key: "a" })], CHAT);
    expect(next.map((m) => m.key)).toEqual(["a", "b"]);
  });
});

describe("updateMessage", () => {
  it("reemplaza el mensaje completo", () => {
    const next = updateMessage([msg(1), msg(2)], msg(2, { text: "editado", editedAt: 9 }), CHAT);
    expect(next[1].text).toBe("editado");
    expect(next[1].editedAt).toBe(9);
    expect(next[0].text).toBe("texto 1");
  });
  it("si no está o es de otro chat, no cambia nada", () => {
    const base = [msg(1)];
    expect(updateMessage(base, msg(7), CHAT)).toBe(base);
    expect(updateMessage(base, msg(1, { chatKey: "wa-1|otro" }), CHAT)).toBe(base);
  });
});

describe("recibos", () => {
  const receipt = (over: Partial<ReceiptEvent>): ReceiptEvent => ({
    chatKey: CHAT,
    messageKeys: [],
    delivery: "read",
    readerId: "p",
    at: 1,
    ...over,
  });

  it("betterDelivery solo sube", () => {
    expect(betterDelivery("sent", "delivered")).toBe("delivered");
    expect(betterDelivery("read", "delivered")).toBe("read");
    expect(betterDelivery("pending", "failed")).toBe("pending");
    expect(betterDelivery("delivered", "played")).toBe("played");
  });
  it("sube la entrega de los mensajes indicados", () => {
    const base = [msg(1, { fromMe: true }), msg(2, { fromMe: true }), msg(3, { fromMe: true })];
    const next = applyReceipt(base, receipt({ messageKeys: [base[0].key, base[1].key] }), CHAT);
    expect(next.map((m) => m.delivery)).toEqual(["read", "read", "sent"]);
  });
  it("no baja ✓✓ azul a ✓✓ gris", () => {
    const base = [msg(1, { delivery: "read" })];
    const next = applyReceipt(base, receipt({ messageKeys: [base[0].key], delivery: "delivered" }), CHAT);
    expect(next).toBe(base);
  });
  it("de otro chat o sin claves: sin cambios", () => {
    const base = [msg(1)];
    expect(applyReceipt(base, receipt({ chatKey: "otro", messageKeys: [base[0].key] }), CHAT)).toBe(base);
    expect(applyReceipt(base, receipt({ messageKeys: null }), CHAT)).toBe(base);
  });
});

describe("escribiendo", () => {
  const ev = (over: Partial<TypingEvent>): TypingEvent => ({
    chatKey: CHAT,
    peerId: "p1",
    typing: true,
    recording: false,
    ...over,
  });

  it("alta, cambio a grabando y baja", () => {
    let map: TypingMap = {};
    map = applyTyping(map, ev({}));
    expect(map).toEqual({ p1: { recording: false } });
    map = applyTyping(map, ev({ typing: false, recording: true }));
    expect(map).toEqual({ p1: { recording: true } });
    map = applyTyping(map, ev({ typing: false, recording: false }));
    expect(map).toEqual({});
  });
  it("sin cambio devuelve el mismo objeto", () => {
    const map: TypingMap = { p1: { recording: false } };
    expect(applyTyping(map, ev({}))).toBe(map);
    expect(applyTyping({}, ev({ typing: false }))).toEqual({});
  });
  it("texto de la cabecera", () => {
    expect(typingLabel({}, false)).toBe("");
    expect(typingLabel({ p1: { recording: false } }, false)).toBe("escribiendo…");
    expect(typingLabel({ p1: { recording: true } }, false)).toBe("grabando audio…");
    expect(typingLabel({ p1: { recording: false } }, true, () => "Ana")).toBe("Ana está escribiendo…");
    expect(typingLabel({ p1: { recording: false }, p2: { recording: false } }, true)).toBe("2 personas escribiendo…");
  });
});
