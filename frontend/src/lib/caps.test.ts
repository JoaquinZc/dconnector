import { describe, expect, it } from "vitest";
import type { CapSet, Message } from "@/api/types";
import { canDeleteForEveryone, canEdit, canForward, canReact, canReply, canSendKind } from "./caps";

const caps = (over: Partial<CapSet> = {}): CapSet =>
  ({
    sendText: true,
    sendImage: true,
    sendVideo: false,
    sendAudio: false,
    sendVoice: true,
    sendDocument: true,
    sendSticker: false,
    sendLocation: false,
    sendContact: false,
    sendPoll: true,
    votePoll: true,
    reply: true,
    forward: true,
    react: true,
    maxReactionsPerUser: 1,
    edit: true,
    editWindowMs: 15 * 60_000,
    deleteForEveryone: true,
    deleteWindowMs: 0,
    ...over,
  }) as CapSet;

const NOW = 1_800_000_000_000;
const msg = (over: Partial<Pick<Message, "fromMe" | "deletedAt" | "timestamp">> = {}) => ({
  fromMe: true,
  deletedAt: 0,
  timestamp: NOW - 60_000,
  ...over,
});

describe("canSendKind", () => {
  it("sigue las capacidades de envío", () => {
    const c = caps();
    expect(canSendKind(c, "text")).toBe(true);
    expect(canSendKind(c, "image")).toBe(true);
    expect(canSendKind(c, "video")).toBe(false);
    expect(canSendKind(c, "voice")).toBe(true);
    expect(canSendKind(c, "poll")).toBe(true);
    expect(canSendKind(c, "sticker")).toBe(false);
  });
  it("sistema, no compatible y desconocidos nunca se envían", () => {
    expect(canSendKind(caps(), "system")).toBe(false);
    expect(canSendKind(caps(), "unsupported")).toBe(false);
    expect(canSendKind(caps(), "otra-cosa")).toBe(false);
  });
  it("sin capacidades no se puede nada", () => {
    expect(canSendKind(null, "text")).toBe(false);
    expect(canSendKind(undefined, "text")).toBe(false);
  });
});

describe("responder, reaccionar, reenviar", () => {
  it("lee su capacidad", () => {
    expect(canReply(caps())).toBe(true);
    expect(canReply(caps({ reply: false }))).toBe(false);
    expect(canReact(caps({ react: false }))).toBe(false);
    expect(canForward(caps({ forward: false }))).toBe(false);
    expect(canReply(null)).toBe(false);
    expect(canReact(undefined)).toBe(false);
    expect(canForward(null)).toBe(false);
  });
});

describe("canEdit", () => {
  it("dentro de la ventana", () => {
    expect(canEdit(msg(), caps(), NOW)).toBe(true);
  });
  it("fuera de la ventana", () => {
    expect(canEdit(msg({ timestamp: NOW - 16 * 60_000 }), caps(), NOW)).toBe(false);
  });
  it("justo en el límite todavía vale", () => {
    expect(canEdit(msg({ timestamp: NOW - 15 * 60_000 }), caps(), NOW)).toBe(true);
  });
  it("ventana 0 = sin límite", () => {
    expect(canEdit(msg({ timestamp: NOW - 400 * 86_400_000 }), caps({ editWindowMs: 0 }), NOW)).toBe(true);
  });
  it("solo mensajes míos y no borrados", () => {
    expect(canEdit(msg({ fromMe: false }), caps(), NOW)).toBe(false);
    expect(canEdit(msg({ deletedAt: NOW - 1000 }), caps(), NOW)).toBe(false);
  });
  it("sin la capacidad", () => {
    expect(canEdit(msg(), caps({ edit: false }), NOW)).toBe(false);
    expect(canEdit(msg(), null, NOW)).toBe(false);
  });
  it("usa la hora actual si no se pasa `now`", () => {
    expect(canEdit(msg({ timestamp: Date.now() - 1000 }), caps())).toBe(true);
    expect(canEdit(msg({ timestamp: Date.now() - 3_600_000 }), caps())).toBe(false);
  });
});

describe("canDeleteForEveryone", () => {
  it("sin límite (0) por defecto en el ejemplo", () => {
    expect(canDeleteForEveryone(msg({ timestamp: NOW - 30 * 86_400_000 }), caps(), NOW)).toBe(true);
  });
  it("respeta deleteWindowMs", () => {
    const c = caps({ deleteWindowMs: 2 * 86_400_000 });
    expect(canDeleteForEveryone(msg({ timestamp: NOW - 86_400_000 }), c, NOW)).toBe(true);
    expect(canDeleteForEveryone(msg({ timestamp: NOW - 3 * 86_400_000 }), c, NOW)).toBe(false);
  });
  it("solo míos y no borrados, y con la capacidad", () => {
    expect(canDeleteForEveryone(msg({ fromMe: false }), caps(), NOW)).toBe(false);
    expect(canDeleteForEveryone(msg({ deletedAt: 1 }), caps(), NOW)).toBe(false);
    expect(canDeleteForEveryone(msg(), caps({ deleteForEveryone: false }), NOW)).toBe(false);
    expect(canDeleteForEveryone(msg(), undefined, NOW)).toBe(false);
  });
});
