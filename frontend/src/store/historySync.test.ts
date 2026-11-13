// Historial completo: lógica pura y store (StatusAll al arrancar, evento history:sync, «cargar más»).
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

type Handler = (data: never) => void;
const handlers = new Map<string, Handler>();

const statusAll = vi.fn();
const status = vi.fn();
const start = vi.fn();
const stop = vi.fn();
const reset = vi.fn();
const loadMore = vi.fn();

vi.mock("@/api/events", () => ({
  onEvents: (subs: Array<[string, Handler]>) => {
    for (const [n, cb] of subs) handlers.set(n, cb);
    return () => handlers.clear();
  },
}));

vi.mock("@/api", () => ({
  api: {
    history: {
      StatusAll: (...a: unknown[]) => statusAll(...a),
      Status: (...a: unknown[]) => status(...a),
      Start: (...a: unknown[]) => start(...a),
      Stop: (...a: unknown[]) => stop(...a),
      Reset: (...a: unknown[]) => reset(...a),
      LoadMore: (...a: unknown[]) => loadMore(...a),
    },
  },
}));

import type { HistoryStatus } from "@/api/types";
import {
  PENDING_TTL_MS,
  applySync,
  emptyStatus,
  formatCount,
  hasProgress,
  indexStatuses,
  isChatLoading,
  isComplete,
  phaseText,
  progressPercent,
  settlePending,
  useHistory,
} from "./history";

const st = (accountId: string, over: Partial<HistoryStatus> = {}): HistoryStatus => ({ ...emptyStatus(accountId), ...over });

const emit = (s: HistoryStatus) => handlers.get("history:sync")!(s as never);

beforeEach(() => {
  handlers.clear();
  [statusAll, status, start, stop, reset, loadMore].forEach((f) => f.mockReset());
  statusAll.mockResolvedValue([]);
  status.mockImplementation(async (id: string) => st(id));
  start.mockResolvedValue(undefined);
  stop.mockResolvedValue(undefined);
  reset.mockResolvedValue(undefined);
  loadMore.mockResolvedValue(undefined);
  useHistory.setState({ byAccount: {}, pending: {}, loaded: false });
});
afterEach(() => vi.useRealTimers());

describe("history.logic", () => {
  it("indexStatuses acepta null y descarta lo que no trae cuenta", () => {
    expect(indexStatuses(null)).toEqual({});
    expect(Object.keys(indexStatuses([st("wa-1"), st("tg-1"), st("")]))).toEqual(["wa-1", "tg-1"]);
  });

  it("applySync reemplaza solo esa cuenta y no toca el mapa si no hay cuenta", () => {
    const a = indexStatuses([st("wa-1", { messages: 5 }), st("tg-1")]);
    const b = applySync(a, st("wa-1", { messages: 9 }));
    expect(b["wa-1"].messages).toBe(9);
    expect(b["tg-1"]).toBe(a["tg-1"]);
    expect(a["wa-1"].messages).toBe(5); // inmutable
    expect(applySync(a, st(""))).toBe(a);
  });

  it("progressPercent: chats recorridos sobre el total, acotado a 0…100", () => {
    expect(progressPercent({ chatsTotal: 0, chatsDone: 0 })).toBe(0);
    expect(progressPercent({ chatsTotal: 120, chatsDone: 45 })).toBe(37);
    expect(progressPercent({ chatsTotal: 10, chatsDone: 10 })).toBe(100);
    expect(progressPercent({ chatsTotal: 10, chatsDone: 99 })).toBe(100);
  });

  it("phaseText por fase", () => {
    expect(phaseText({ running: false, phase: "idle" })).toBe("Detenido");
    expect(phaseText({ running: true, phase: "requesting" })).toBe("Pidiendo mensajes anteriores");
    expect(phaseText({ running: true, phase: "waiting" })).toMatch(/Esperando/);
    expect(phaseText({ running: true, phase: "queued" })).toMatch(/En cola/);
  });

  it("isComplete / hasProgress", () => {
    expect(isComplete(st("a", { chatsTotal: 4, chatsDone: 4 }))).toBe(true);
    expect(isComplete(st("a", { chatsTotal: 4, chatsDone: 4, running: true }))).toBe(false);
    expect(isComplete(st("a"))).toBe(false);
    expect(hasProgress(st("a"))).toBe(false);
    expect(hasProgress(st("a", { chatsDone: 1 }))).toBe(true);
  });

  it("formatCount con punto de miles", () => {
    expect(formatCount(0)).toBe("0");
    expect(formatCount(999)).toBe("999");
    expect(formatCount(1234)).toBe("1.234");
    expect(formatCount(1234567)).toBe("1.234.567");
    expect(formatCount(-3)).toBe("0");
  });
});

describe("settlePending (cuándo deja de decir «Cargando…»)", () => {
  const p = { chatNativeId: "c1", title: "Ana", at: 1_000, seen: false };

  it("sigue mientras la tanda trabaja en ese chat y lo anota como visto", () => {
    const next = settlePending(p, st("wa-1", { running: true, current: "Ana" }), 2_000);
    expect(next).toEqual({ ...p, seen: true });
  });

  it("termina cuando la tanda queda en reposo tras haber trabajado en el chat", () => {
    const seen = { ...p, seen: true };
    expect(settlePending(seen, st("wa-1", { running: false }), 2_000)).toBeUndefined();
  });

  it("un «en reposo» muy pegado a la petición, sin haberla visto, se ignora", () => {
    expect(settlePending(p, st("wa-1", { running: false }), 1_200)).toBe(p);
    expect(settlePending(p, st("wa-1", { running: false }), 2_600)).toBeUndefined();
  });

  it("termina cuando la tanda ya pasó a otro chat", () => {
    const seen = { ...p, seen: true };
    expect(settlePending(seen, st("wa-1", { running: true, current: "Luis" }), 2_000)).toBeUndefined();
  });

  it("si la tanda sigue en otro chat y aún no llegó al nuestro, espera", () => {
    expect(settlePending(p, st("wa-1", { running: true, current: "Luis" }), 2_000)).toBe(p);
  });

  it("caduca sola sin noticias", () => {
    expect(settlePending(p, st("wa-1", { running: true, current: "Luis" }), 1_000 + PENDING_TTL_MS + 1)).toBeUndefined();
  });

  it("isChatLoading mira cuenta y chat", () => {
    expect(isChatLoading({ "wa-1": p }, "wa-1", "c1")).toBe(true);
    expect(isChatLoading({ "wa-1": p }, "wa-1", "c2")).toBe(false);
    expect(isChatLoading({ "wa-1": p }, "tg-1", "c1")).toBe(false);
  });
});

describe("useHistory", () => {
  it("init carga StatusAll y se suscribe a history:sync", async () => {
    statusAll.mockResolvedValue([st("wa-1", { messages: 10, chatsTotal: 3 }), st("tg-1")]);
    const off = useHistory.getState().init();
    await vi.waitFor(() => expect(useHistory.getState().loaded).toBe(true));
    expect(useHistory.getState().byAccount["wa-1"].messages).toBe(10);
    expect(handlers.has("history:sync")).toBe(true);
    off();
    expect(handlers.size).toBe(0);
  });

  it("StatusAll que falla deja la pantalla cargada y sin datos", async () => {
    statusAll.mockRejectedValue(new Error("x"));
    useHistory.getState().init();
    await vi.waitFor(() => expect(useHistory.getState().loaded).toBe(true));
    expect(useHistory.getState().byAccount).toEqual({});
  });

  it("un history:sync actualiza la cuenta; uno que llegó antes que StatusAll no se pisa", async () => {
    let resolve!: (v: HistoryStatus[]) => void;
    statusAll.mockReturnValue(new Promise((r) => (resolve = r)));
    useHistory.getState().init();
    emit(st("wa-1", { running: true, messages: 50, chatsTotal: 10, chatsDone: 2 }));
    resolve([st("wa-1", { messages: 1 }), st("tg-1", { messages: 7 })]);
    await vi.waitFor(() => expect(useHistory.getState().loaded).toBe(true));
    const s = useHistory.getState();
    expect(s.byAccount["wa-1"].messages).toBe(50);
    expect(s.byAccount["wa-1"].running).toBe(true);
    expect(s.byAccount["tg-1"].messages).toBe(7);
  });

  it("statusOf devuelve un estado en reposo si la cuenta no tiene dato", () => {
    const s = useHistory.getState().statusOf("nueva");
    expect(s).toMatchObject({ accountId: "nueva", running: false, messages: 0 });
  });

  it("start / stop / reset llaman al servicio y releen el estado", async () => {
    status.mockResolvedValueOnce(st("wa-1", { running: true, phase: "requesting" }));
    await useHistory.getState().start("wa-1");
    expect(start).toHaveBeenCalledWith("wa-1");
    expect(useHistory.getState().byAccount["wa-1"].running).toBe(true);

    status.mockResolvedValueOnce(st("wa-1", { running: false, stoppedReason: "Detenido" }));
    await useHistory.getState().stop("wa-1");
    expect(stop).toHaveBeenCalledWith("wa-1");
    expect(useHistory.getState().byAccount["wa-1"].running).toBe(false);

    status.mockResolvedValueOnce(st("wa-1"));
    await useHistory.getState().reset("wa-1");
    expect(reset).toHaveBeenCalledWith("wa-1");
    expect(useHistory.getState().byAccount["wa-1"].chatsDone).toBe(0);
  });

  it("start que falla no rompe (el aviso lo da el envoltorio de api)", async () => {
    start.mockRejectedValue(new Error("no conectada"));
    await expect(useHistory.getState().start("wa-1")).resolves.toBeUndefined();
  });

  describe("loadMore", () => {
    beforeEach(() => vi.useFakeTimers());

    it("marca el chat como «cargando» y lo suelta cuando la tanda queda en reposo", async () => {
      useHistory.getState().init();
      await vi.advanceTimersByTimeAsync(0);
      await useHistory.getState().loadMore("wa-1", "c1", "Ana");
      expect(loadMore).toHaveBeenCalledWith("wa-1", "c1");
      expect(isChatLoading(useHistory.getState().pending, "wa-1", "c1")).toBe(true);

      emit(st("wa-1", { running: true, phase: "requesting", current: "Ana" }));
      expect(isChatLoading(useHistory.getState().pending, "wa-1", "c1")).toBe(true);
      emit(st("wa-1", { running: false }));
      expect(isChatLoading(useHistory.getState().pending, "wa-1", "c1")).toBe(false);
    });

    it("si la tanda acaba sin avisos, una revisión posterior suelta el chat", async () => {
      useHistory.getState().init();
      await vi.advanceTimersByTimeAsync(0);
      await useHistory.getState().loadMore("wa-1", "c1", "Ana");
      expect(isChatLoading(useHistory.getState().pending, "wa-1", "c1")).toBe(true);
      await vi.advanceTimersByTimeAsync(1_800);
      expect(isChatLoading(useHistory.getState().pending, "wa-1", "c1")).toBe(false);
    });

    it("si LoadMore falla, no queda «cargando» y el error se relanza", async () => {
      loadMore.mockRejectedValue(new Error("cuenta desconectada"));
      await expect(useHistory.getState().loadMore("wa-1", "c1", "Ana")).rejects.toThrow("cuenta desconectada");
      expect(useHistory.getState().pending).toEqual({});
    });
  });
});
