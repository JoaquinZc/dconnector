import { describe, expect, it } from "vitest";
import type { Chat, ChatFilter } from "@/api/types";
import {
  DEFAULT_FILTER,
  appendChats,
  compareChats,
  matchesFilter,
  pagingCursor,
  removeChat,
  replaceChats,
  sortKeys,
  unreadFromMap,
  upsertChats,
} from "./inbox.logic";

const chat = (key: string, over: Partial<Chat> = {}): Chat =>
  ({
    key,
    accountId: "wa-1",
    channel: "whatsapp",
    nativeId: key,
    kind: "direct",
    title: `Chat ${key}`,
    avatarUrl: "",
    unreadCount: 0,
    lastMessageAt: 1000,
    lastPreview: "",
    lastFromMe: false,
    archived: false,
    pinned: false,
    mutedUntil: 0,
    readOnly: false,
    peerNativeId: "",
    contactId: "",
    ...over,
  }) as unknown as Chat;

const filter = (over: Partial<ChatFilter> = {}): ChatFilter => ({ ...DEFAULT_FILTER, ...over });

describe("orden", () => {
  it("fijados primero y luego el más reciente", () => {
    const chats = replaceChats([
      chat("a", { lastMessageAt: 100 }),
      chat("b", { lastMessageAt: 300 }),
      chat("c", { lastMessageAt: 50, pinned: true }),
      chat("d", { lastMessageAt: 200, pinned: true }),
    ]);
    expect(sortKeys(chats)).toEqual(["d", "c", "b", "a"]);
  });
  it("desempata por clave de forma estable", () => {
    expect(compareChats(chat("a"), chat("b"))).toBeLessThan(0);
    expect(compareChats(chat("b"), chat("a"))).toBeGreaterThan(0);
    expect(compareChats(chat("a"), chat("a"))).toBe(0);
  });
});

describe("matchesFilter", () => {
  it("pestaña Todos excluye archivados", () => {
    expect(matchesFilter(chat("a"), filter())).toBe(true);
    expect(matchesFilter(chat("a", { archived: true }), filter())).toBe(false);
  });
  it("pestañas No leídos, Grupos y Archivados", () => {
    expect(matchesFilter(chat("a", { unreadCount: 2 }), filter({ tab: "unread" }))).toBe(true);
    expect(matchesFilter(chat("a"), filter({ tab: "unread" }))).toBe(false);
    expect(matchesFilter(chat("a", { kind: "group" as Chat["kind"] }), filter({ tab: "groups" }))).toBe(true);
    expect(matchesFilter(chat("a"), filter({ tab: "groups" }))).toBe(false);
    expect(matchesFilter(chat("a", { archived: true }), filter({ tab: "archived" }))).toBe(true);
    expect(matchesFilter(chat("a"), filter({ tab: "archived" }))).toBe(false);
  });
  it("cuentas y canales (vacío = todos)", () => {
    expect(matchesFilter(chat("a"), filter({ accountIds: ["wa-2"] }))).toBe(false);
    expect(matchesFilter(chat("a"), filter({ accountIds: ["wa-1", "wa-2"] }))).toBe(true);
    expect(matchesFilter(chat("a"), filter({ channels: ["telegram"] }))).toBe(false);
    expect(matchesFilter(chat("a"), filter({ channels: ["whatsapp"] }))).toBe(true);
  });
  it("búsqueda por título sin importar mayúsculas", () => {
    expect(matchesFilter(chat("a", { title: "Ana López" }), filter({ query: "lópez" }))).toBe(true);
    expect(matchesFilter(chat("a", { title: "Ana López" }), filter({ query: "xyz" }))).toBe(false);
  });
});

describe("upsertChats", () => {
  it("añade y actualiza", () => {
    const base = replaceChats([chat("a", { lastMessageAt: 100 })]);
    const next = upsertChats(base, [chat("a", { lastMessageAt: 500, lastPreview: "hola" }), chat("b")], filter());
    expect(next.a.lastMessageAt).toBe(500);
    expect(next.a.lastPreview).toBe("hola");
    expect(Object.keys(next).sort()).toEqual(["a", "b"]);
    expect(base.a.lastMessageAt).toBe(100); // no muta
  });
  it("un chat que ya no cumple el filtro sale de la lista", () => {
    const base = replaceChats([chat("a", { unreadCount: 3 })]);
    const next = upsertChats(base, [chat("a", { unreadCount: 0 })], filter({ tab: "unread" }));
    expect(next.a).toBeUndefined();
  });
  it("un chat nuevo que no cumple el filtro no entra", () => {
    const next = upsertChats({}, [chat("z", { accountId: "wa-9" })], filter({ accountIds: ["wa-1"] }));
    expect(next).toEqual({});
  });
  it("lista vacía = mismo objeto", () => {
    const base = replaceChats([chat("a")]);
    expect(upsertChats(base, [], filter())).toBe(base);
  });
});

describe("removeChat", () => {
  it("quita la clave", () => {
    const base = replaceChats([chat("a"), chat("b")]);
    expect(Object.keys(removeChat(base, "a"))).toEqual(["b"]);
    expect(removeChat(base, "nope")).toBe(base);
  });
});

describe("paginación", () => {
  it("appendChats suma la siguiente página sin duplicar", () => {
    const base = replaceChats([chat("a", { lastMessageAt: 300 }), chat("b", { lastMessageAt: 200 })]);
    const next = appendChats(base, [chat("b", { lastMessageAt: 200 }), chat("c", { lastMessageAt: 100 })]);
    expect(sortKeys(next)).toEqual(["a", "b", "c"]);
  });
  it("el cursor es el lastMessageAt del último chat no fijado", () => {
    const chats = replaceChats([
      chat("p", { pinned: true, lastMessageAt: 10 }),
      chat("a", { lastMessageAt: 300 }),
      chat("b", { lastMessageAt: 200 }),
    ]);
    expect(pagingCursor(chats, sortKeys(chats))).toBe(200);
    expect(pagingCursor({}, [])).toBe(0);
  });
});

describe("unreadFromMap", () => {
  it("suma por cuenta", () => {
    expect(unreadFromMap({ "wa-1": 2, "tg-1": 3 })).toEqual({ total: 5, byAccount: { "wa-1": 2, "tg-1": 3 } });
  });
  it("respeta la clave «total» si viene", () => {
    expect(unreadFromMap({ "wa-1": 2, total: 9 })).toEqual({ total: 9, byAccount: { "wa-1": 2 } });
  });
  it("nulo = ceros", () => {
    expect(unreadFromMap(null)).toEqual({ total: 0, byAccount: {} });
  });
});

describe("chats sin actividad (solo conversaciones con mensajes)", () => {
  const empty = (key: string, over: Partial<Chat> = {}) => chat(key, { lastMessageAt: 0, ...over });

  it("un directo vacío no entra en la lista (ni por evento chat:upsert)", () => {
    expect(matchesFilter(empty("a"), filter())).toBe(false);
    expect(matchesFilter(empty("a", { kind: "self" as never }), filter())).toBe(false);
    const chats = upsertChats({}, [empty("a"), chat("b")], filter());
    expect(Object.keys(chats)).toEqual(["b"]);
  });

  it("fijado, grupo y canal vacíos sí se muestran", () => {
    expect(matchesFilter(empty("a", { pinned: true }), filter())).toBe(true);
    expect(matchesFilter(empty("g", { kind: "group" as never }), filter())).toBe(true);
    expect(matchesFilter(empty("c", { kind: "channel" as never }), filter())).toBe(true);
  });

  it("con includeEmpty entran todos", () => {
    expect(matchesFilter(empty("a"), filter({ includeEmpty: true }))).toBe(true);
  });

  it("un chat vacío que recibe su primer mensaje entra en la lista", () => {
    const first = upsertChats({}, [empty("a")], filter());
    expect(first).toEqual({});
    const after = upsertChats(first, [chat("a", { lastMessageAt: 5000 })], filter());
    expect(Object.keys(after)).toEqual(["a"]);
  });
});
