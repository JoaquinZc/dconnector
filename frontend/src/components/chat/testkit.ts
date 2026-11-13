// Fábricas de datos para las pruebas de la pantalla de chats.
import type { Chat, Message } from "../../api/core/model/models.js";

/** 11-nov-2026 15:30 hora local. */
export const NOW = new Date(2026, 10, 11, 15, 30, 0).getTime();

export function at(day: number, h = 12, m = 0, month = 10, year = 2026): number {
  return new Date(year, month, day, h, m, 0).getTime();
}

let seq = 0;

export function msg(over: Partial<Record<keyof Message, unknown>> = {}): Message {
  seq += 1;
  return {
    key: `wa-1|c1|m${seq}`,
    chatKey: "wa-1|c1",
    accountId: "wa-1",
    channel: "whatsapp",
    nativeId: `m${seq}`,
    chatNativeId: "c1",
    senderNativeId: "5939@s.whatsapp.net",
    senderName: "Ana",
    fromMe: false,
    timestamp: at(11, 10, seq % 50),
    kind: "text",
    text: "Hola",
    forwarded: false,
    editedAt: 0,
    deletedAt: 0,
    delivery: "sent",
    ...over,
  } as unknown as Message;
}

export function chat(over: Partial<Record<keyof Chat, unknown>> = {}): Chat {
  seq += 1;
  return {
    key: `wa-1|c${seq}`,
    accountId: "wa-1",
    channel: "whatsapp",
    nativeId: `c${seq}`,
    kind: "direct",
    title: "Ana Pérez",
    avatarUrl: "",
    unreadCount: 0,
    lastMessageAt: at(11, 14, 5),
    lastPreview: "Hola, ¿cómo estás?",
    lastFromMe: false,
    archived: false,
    pinned: false,
    mutedUntil: 0,
    readOnly: false,
    peerNativeId: "593999111222@s.whatsapp.net",
    contactId: "",
    ...over,
  } as unknown as Chat;
}

export const FULL_CAPS = {
  sendText: true,
  sendImage: true,
  sendVideo: true,
  sendVoice: true,
  sendDocument: true,
  reply: true,
  forward: true,
  react: true,
  edit: true,
  editWindowMs: 15 * 60_000,
  deleteForEveryone: true,
  deleteWindowMs: 2 * 24 * 3_600_000,
  votePoll: true,
};
