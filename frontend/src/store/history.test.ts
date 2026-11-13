// history:progress (11 §2): el historial llega al almacén sin datos hacia la ventana, así que los
// stores de bandeja y de conversación vuelven a pedir lo que les toca.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

type Handler = (data: never) => void;
const handlers = new Map<string, Handler>();

const listChats = vi.fn();
const unreadTotals = vi.fn();
const messages = vi.fn();

vi.mock("@/api/events", () => ({
  onEvents: (subs: Array<[string, Handler]>) => {
    for (const [n, cb] of subs) handlers.set(n, cb);
    return () => handlers.clear();
  },
}));

vi.mock("@/api", () => ({
  api: {
    inbox: {
      ListChats: (...a: unknown[]) => listChats(...a),
      UnreadTotals: (...a: unknown[]) => unreadTotals(...a),
      Messages: (...a: unknown[]) => messages(...a),
      LoadOlder: vi.fn().mockResolvedValue(undefined),
    },
  },
}));

const chat = (key: string, at: number) => ({ key, lastMessageAt: at, pinned: false, unreadCount: 0 });
const msg = (n: number) => ({
  key: `wa-1|c1|m${n}`,
  chatKey: "wa-1|c1",
  accountId: "wa-1",
  nativeId: `m${n}`,
  chatNativeId: "c1",
  timestamp: n * 1000,
  delivery: "sent",
});

beforeEach(() => {
  handlers.clear();
  listChats.mockReset();
  unreadTotals.mockReset().mockResolvedValue({});
  messages.mockReset();
  vi.useFakeTimers();
});
afterEach(() => vi.useRealTimers());

describe("history:progress", () => {
  it("la bandeja vuelve a pedir la primera página sin vaciar la lista", async () => {
    const { useInbox } = await import("./inbox");
    listChats.mockResolvedValueOnce([chat("wa-1|a", 10)]);
    const off = useInbox.getState().init();
    await vi.advanceTimersByTimeAsync(0);
    expect(Object.keys(useInbox.getState().chats)).toEqual(["wa-1|a"]);

    listChats.mockResolvedValueOnce([chat("wa-1|b", 20)]);
    const h = handlers.get("history:progress");
    expect(h).toBeTypeOf("function");
    h!({ accountId: "wa-1", progress: 0.5 } as never);
    h!({ accountId: "wa-1", progress: 1 } as never);
    expect(listChats).toHaveBeenCalledTimes(1); // todavía agrupando
    await vi.advanceTimersByTimeAsync(2000);
    expect(listChats).toHaveBeenCalledTimes(2); // un solo repaso para los dos avisos
    expect(Object.keys(useInbox.getState().chats).sort()).toEqual(["wa-1|a", "wa-1|b"]);
    off();
  });

  it("la conversación abierta pide su página más antigua y la mezcla", async () => {
    const { useChat } = await import("./chat");
    messages.mockResolvedValueOnce({ messages: [msg(5)], hasMore: false });
    const off = useChat.getState().init();
    await useChat.getState().open("wa-1|c1");
    expect(useChat.getState().messages.map((m) => m.nativeId)).toEqual(["m5"]);

    messages.mockResolvedValueOnce({ messages: [msg(3), msg(4)], hasMore: true });
    handlers.get("history:progress")!({ accountId: "wa-1", progress: -1 } as never);
    await vi.advanceTimersByTimeAsync(1000);
    expect(messages).toHaveBeenLastCalledWith("wa-1|c1", 5000, 50);
    expect(useChat.getState().messages.map((m) => m.nativeId)).toEqual(["m3", "m4", "m5"]);
    expect(useChat.getState().hasMore).toBe(true);
    off();
  });

  it("ignora el historial de otra cuenta", async () => {
    const { useChat } = await import("./chat");
    messages.mockResolvedValueOnce({ messages: [msg(5)], hasMore: false });
    const off = useChat.getState().init();
    await useChat.getState().open("wa-1|c1");
    messages.mockClear();
    handlers.get("history:progress")!({ accountId: "tg-9", progress: 1 } as never);
    await vi.advanceTimersByTimeAsync(2000);
    expect(messages).not.toHaveBeenCalled();
    off();
  });
});
