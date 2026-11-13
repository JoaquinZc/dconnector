import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const svc = vi.hoisted(() => ({
  AccountsService: {},
  InboxService: {},
  MessagingService: {},
  StatusesService: { ByAuthor: vi.fn(), MarkViewed: vi.fn(), React: vi.fn() },
  ContactsService: {},
  CampaignsService: {},
  ScheduledService: {},
  StatsService: {},
  DestinationsService: {},
  SettingsService: {},
  FilesService: {},
}));

vi.mock("@/api/transport", async () => (await import("@/test/svcTransport")).svcTransport(svc));
vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(() => () => undefined) } }));

import type { CapSet, Status, StatusAuthor } from "@/api/types";
import { useChat } from "@/store/chat";
import { useUI } from "@/store/ui";
import { chatKeyForStatus, replyToStatus, statusAsQuote } from "./replyToStatus";
import StoryViewer, { HOLD_MS } from "./StoryViewer";

const NOW = Date.now();

const status = (n: number, p: Partial<Status> = {}): Status =>
  ({
    key: `wa-1|s${n}`,
    accountId: "wa-1",
    channel: "whatsapp",
    nativeId: `s${n}`,
    authorNativeId: "ana@s.whatsapp.net",
    authorName: "Ana",
    fromMe: false,
    postedAt: NOW - n * 60_000,
    expiresAt: NOW + 3_600_000,
    kind: "text",
    text: `Estado ${n}`,
    bgColor: "#128C7E",
    font: 0,
    viewed: false,
    viewCount: 0,
    reactionCount: 0,
    replyCount: 0,
    deletedAt: 0,
    delivery: "sent",
    ...p,
  }) as unknown as Status;

const author = (id: string, name: string, count: number): StatusAuthor =>
  ({ accountId: "wa-1", authorId: id, authorName: name, avatarUrl: "", count, unviewed: count, lastAt: NOW }) as unknown as StatusAuthor;

const ANA = author("ana@s.whatsapp.net", "Ana", 2);
const LUIS = author("luis@s.whatsapp.net", "Luis", 1);

const caps = (p: Partial<CapSet> = {}) => ({ statusReact: true, ...p }) as CapSet;

function flush(ms = 0) {
  return act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}

const root = () => screen.getByTestId("story-viewer");
const bars = () => screen.getAllByTestId("story-bar").map((b) => Number(b.getAttribute("data-fill")));

beforeEach(() => {
  vi.useFakeTimers();
  vi.clearAllMocks();
  svc.StatusesService.ByAuthor.mockImplementation(async (_acc: string, authorId: string) =>
    authorId === ANA.authorId ? [status(2), status(1)] : [status(5, { authorNativeId: LUIS.authorId, authorName: "Luis" })],
  );
  svc.StatusesService.MarkViewed.mockResolvedValue(undefined);
  svc.StatusesService.React.mockResolvedValue(undefined);
  useUI.setState({ toasts: [], page: "statuses", openChatKey: null });
  useChat.setState({ chatKey: null, replyTo: null });
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("StoryViewer · avance", () => {
  it("empieza en el primero, marca visto y llena la barra con el tiempo (5 s)", async () => {
    render(<StoryViewer authors={[ANA]} startIndex={0} caps={caps()} onClose={() => undefined} />);
    await flush();
    expect(root().getAttribute("data-item")).toBe("0");
    expect(svc.StatusesService.MarkViewed).toHaveBeenCalledWith("wa-1", "wa-1|s2"); // el más antiguo primero
    expect(bars()).toHaveLength(2);
    await flush(2500);
    const [first] = bars();
    expect(first).toBeGreaterThan(0.4);
    expect(first).toBeLessThan(0.6);
  });

  it("a los 5 s pasa al siguiente estado y marca el nuevo como visto", async () => {
    render(<StoryViewer authors={[ANA]} startIndex={0} caps={caps()} onClose={() => undefined} />);
    await flush();
    await flush(5200);
    expect(root().getAttribute("data-item")).toBe("1");
    expect(bars()[0]).toBe(1);
    expect(svc.StatusesService.MarkViewed).toHaveBeenCalledWith("wa-1", "wa-1|s1");
    expect(svc.StatusesService.MarkViewed).toHaveBeenCalledTimes(2);
  });

  it("al terminar el último estado del último autor se cierra", async () => {
    const onClose = vi.fn();
    render(<StoryViewer authors={[LUIS]} startIndex={0} caps={caps()} onClose={onClose} />);
    await flush();
    expect(onClose).not.toHaveBeenCalled();
    await flush(5200);
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("al terminar un autor pasa al siguiente", async () => {
    render(<StoryViewer authors={[ANA, LUIS]} startIndex={0} caps={caps()} onClose={() => undefined} />);
    await flush();
    await flush(5200);
    await flush(5200);
    expect(root().getAttribute("data-group")).toBe("1");
    expect(root().getAttribute("data-item")).toBe("0");
    expect(screen.getByText("Estado 5")).toBeInTheDocument();
  });

  it("los estados míos no se marcan como vistos", async () => {
    svc.StatusesService.ByAuthor.mockResolvedValue([status(1, { fromMe: true })]);
    render(<StoryViewer authors={[ANA]} startIndex={0} caps={caps()} onClose={() => undefined} />);
    await flush();
    expect(svc.StatusesService.MarkViewed).not.toHaveBeenCalled();
  });

  it("empieza por el primer estado no visto", async () => {
    svc.StatusesService.ByAuthor.mockResolvedValue([status(3, { viewed: true }), status(2, { viewed: true }), status(1)]);
    render(<StoryViewer authors={[ANA]} startIndex={0} caps={caps()} onClose={() => undefined} />);
    await flush();
    expect(root().getAttribute("data-item")).toBe("2");
  });

  it("un autor sin estados vigentes se salta", async () => {
    svc.StatusesService.ByAuthor.mockImplementation(async (_a: string, id: string) =>
      id === ANA.authorId ? [] : [status(5, { authorNativeId: LUIS.authorId })],
    );
    render(<StoryViewer authors={[ANA, LUIS]} startIndex={0} caps={caps()} onClose={() => undefined} />);
    await flush();
    await flush();
    expect(root().getAttribute("data-group")).toBe("1");
  });
});

describe("StoryViewer · pausa y teclado", () => {
  it("mantener pulsado pausa el avance y soltar lo reanuda", async () => {
    render(<StoryViewer authors={[ANA]} startIndex={0} caps={caps()} onClose={() => undefined} />);
    await flush();
    await flush(1000);
    const stage = screen.getByTestId("story-stage");

    fireEvent.mouseDown(stage);
    await flush(HOLD_MS + 50);
    expect(root().getAttribute("data-paused")).toBe("1");
    const frozen = bars()[0];
    await flush(8000); // mucho más que 5 s
    expect(root().getAttribute("data-item")).toBe("0");
    expect(bars()[0]).toBe(frozen);

    fireEvent.mouseUp(stage);
    expect(root().getAttribute("data-paused")).toBe("0");
    await flush(1000);
    expect(bars()[0]).toBeGreaterThan(frozen);
    // soltar tras mantener no cuenta como toque: sigue en el mismo estado
    expect(root().getAttribute("data-item")).toBe("0");
  });

  it("un toque corto a la derecha avanza y a la izquierda retrocede", async () => {
    render(<StoryViewer authors={[ANA]} startIndex={0} caps={caps()} onClose={() => undefined} />);
    await flush();
    const stage = screen.getByTestId("story-stage");
    stage.getBoundingClientRect = () => ({ left: 0, width: 100, top: 0, height: 100, right: 100, bottom: 100, x: 0, y: 0, toJSON: () => ({}) });

    fireEvent.mouseDown(stage, { clientX: 90 });
    fireEvent.mouseUp(stage, { clientX: 90 });
    expect(root().getAttribute("data-item")).toBe("1");

    fireEvent.mouseDown(stage, { clientX: 10 });
    fireEvent.mouseUp(stage, { clientX: 10 });
    expect(root().getAttribute("data-item")).toBe("0");
  });

  it("← → mueven y Esc cierra", async () => {
    const onClose = vi.fn();
    render(<StoryViewer authors={[ANA]} startIndex={0} caps={caps()} onClose={onClose} />);
    await flush();
    fireEvent.keyDown(window, { key: "ArrowRight" });
    expect(root().getAttribute("data-item")).toBe("1");
    fireEvent.keyDown(window, { key: "ArrowLeft" });
    expect(root().getAttribute("data-item")).toBe("0");
    fireEvent.keyDown(window, { key: "ArrowLeft" });
    expect(root().getAttribute("data-item")).toBe("0"); // en el primero se queda
    fireEvent.keyDown(window, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("al cambiar de estado la barra vuelve a cero", async () => {
    render(<StoryViewer authors={[ANA]} startIndex={0} caps={caps()} onClose={() => undefined} />);
    await flush();
    await flush(3000);
    fireEvent.keyDown(window, { key: "ArrowRight" });
    const [a, b] = bars();
    expect(a).toBe(1);
    expect(b).toBe(0);
  });
});

describe("StoryViewer · reaccionar y responder", () => {
  it("reacciona solo si la cuenta lo permite", async () => {
    const { unmount } = render(
      <StoryViewer authors={[ANA]} startIndex={0} caps={caps({ statusReact: false })} onClose={() => undefined} />,
    );
    await flush();
    expect(screen.queryByRole("group", { name: "Reaccionar" })).toBeNull();
    unmount();

    render(<StoryViewer authors={[ANA]} startIndex={0} caps={caps()} onClose={() => undefined} />);
    await flush();
    fireEvent.click(screen.getByRole("button", { name: "Reaccionar 🔥" }));
    await flush();
    expect(svc.StatusesService.React).toHaveBeenCalledWith("wa-1", "wa-1|s2", "🔥");
  });

  it("responder cierra el visor, abre el chat del autor y deja el estado como cita", async () => {
    const onClose = vi.fn();
    render(<StoryViewer authors={[ANA]} startIndex={0} caps={caps()} onClose={onClose} />);
    await flush();
    fireEvent.click(screen.getByRole("button", { name: /Responder/ }));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(useUI.getState().openChatKey).toBe("wa-1|ana@s.whatsapp.net");
    expect(useUI.getState().page).toBe("chats");

    // ChatsPage abre el chat: la cita se pone al aparecer
    act(() => useChat.setState({ chatKey: "wa-1|ana@s.whatsapp.net", replyTo: null }));
    expect(useChat.getState().replyTo?.nativeId).toBe("s2");
    expect(useChat.getState().replyTo?.text).toBe("Estado 2");
  });
});

describe("replyToStatus", () => {
  it("si el chat ya está abierto pone la cita enseguida", () => {
    const s = status(1);
    useChat.setState({ chatKey: chatKeyForStatus(s), replyTo: null });
    replyToStatus(s);
    expect(useChat.getState().replyTo?.fromStatusId).toBe("s1");
  });

  it("la cita de un estado con foto lleva una vista previa legible", () => {
    const q = statusAsQuote(status(1, { kind: "image" as never, text: "" }));
    expect(q.text).toBe("📷 Foto");
    expect(q.senderName).toBe("Ana");
    expect(q.chatKey).toBe("wa-1|ana@s.whatsapp.net");
  });

  it("se puede cancelar la espera", () => {
    const s = status(1);
    const cancel = replyToStatus(s);
    cancel();
    useChat.setState({ chatKey: chatKeyForStatus(s), replyTo: null });
    expect(useChat.getState().replyTo).toBeNull();
  });
});

describe("StoryViewer · en línea y varias cuentas", () => {
  const TG_ANA = { ...author("marta@t", "Marta", 1), accountId: "tg-1" } as StatusAuthor;
  const accounts = {
    "wa-1": { label: "Ventas", channel: "whatsapp", caps: caps({ statusReact: false }) },
    "tg-1": { label: "Canal Tg", channel: "telegram", caps: caps({ statusReact: true }) },
  };

  beforeEach(() => {
    svc.StatusesService.ByAuthor.mockImplementation(async (acc: string, authorId: string) =>
      authorId === ANA.authorId
        ? [status(2), status(1)]
        : [status(7, { accountId: acc, key: `${acc}|s-${authorId}`, authorNativeId: authorId, text: `De ${authorId}` })],
    );
  });

  it("no es un diálogo: sin portal, sin cerrar y con la etiqueta de canal y cuenta", async () => {
    render(<StoryViewer inline authors={[ANA]} accounts={accounts} selectedKey={`wa-1|${ANA.authorId}`} />);
    await flush();
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(screen.queryByRole("button", { name: "Cerrar" })).toBeNull();
    expect(screen.getByRole("region", { name: "Estados de Ana" })).toBe(root());
    expect(screen.getByTestId("story-account")).toHaveTextContent("Ventas");
  });

  it("al terminar el último autor se queda en el último y no cierra ni falla", async () => {
    const onClose = vi.fn();
    render(<StoryViewer inline authors={[ANA]} accounts={accounts} onClose={onClose} />);
    await flush();
    await flush(5200);
    await flush(5200);
    await flush(5200);
    expect(onClose).not.toHaveBeenCalled();
    expect(root().getAttribute("data-item")).toBe("1");
    expect(bars()).toEqual([1, 1]);
  });

  it("pide, marca visto y reacciona con la cuenta de cada autor y sus capacidades", async () => {
    render(<StoryViewer inline authors={[ANA, TG_ANA]} accounts={accounts} />);
    await flush();
    expect(svc.StatusesService.ByAuthor).toHaveBeenCalledWith("wa-1", ANA.authorId);
    expect(svc.StatusesService.ByAuthor).toHaveBeenCalledWith("tg-1", "marta@t"); // precarga del siguiente
    expect(screen.queryByRole("group", { name: "Reaccionar" })).toBeNull(); // WhatsApp: no

    fireEvent.click(screen.getByRole("button", { name: "Autor siguiente" }));
    await flush();
    expect(root().getAttribute("data-group")).toBe("1");
    expect(svc.StatusesService.MarkViewed).toHaveBeenCalledWith("tg-1", "tg-1|s-marta@t");
    expect(screen.getByTestId("story-account")).toHaveTextContent("Canal Tg");
    fireEvent.click(screen.getByRole("button", { name: "Reaccionar 🔥" }));
    await flush();
    expect(svc.StatusesService.React).toHaveBeenCalledWith("tg-1", "tg-1|s-marta@t", "🔥");
  });

  it("avisa de cada autor visto y del cambio de autor", async () => {
    const onViewed = vi.fn();
    const onSelect = vi.fn();
    render(
      <StoryViewer inline authors={[ANA, TG_ANA]} accounts={accounts} selectedKey={`wa-1|${ANA.authorId}`} onViewed={onViewed} onSelect={onSelect} />,
    );
    await flush();
    expect(onViewed).toHaveBeenCalledWith(expect.objectContaining({ accountId: "wa-1", authorId: ANA.authorId }));
    expect(onSelect).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Autor siguiente" }));
    await flush();
    expect(onSelect).toHaveBeenCalledWith("tg-1|marta@t");
  });

  it("si cambia el autor elegido desde fuera salta a él; si la lista se reordena, no se mueve", async () => {
    const { rerender } = render(
      <StoryViewer inline authors={[ANA, TG_ANA]} accounts={accounts} selectedKey={`wa-1|${ANA.authorId}`} />,
    );
    await flush();
    rerender(<StoryViewer inline authors={[ANA, TG_ANA]} accounts={accounts} selectedKey="tg-1|marta@t" />);
    await flush();
    expect(root().getAttribute("data-group")).toBe("1");
    expect(screen.getByText("De marta@t")).toBeInTheDocument();

    rerender(<StoryViewer inline authors={[TG_ANA, ANA]} accounts={accounts} selectedKey="tg-1|marta@t" />);
    await flush();
    expect(root().getAttribute("data-group")).toBe("0");
    expect(screen.getByText("De marta@t")).toBeInTheDocument();
  });

  it("las flechas del teclado no actúan escribiendo en un campo ni con un diálogo abierto", async () => {
    render(<StoryViewer inline authors={[ANA]} accounts={accounts} />);
    await flush();
    const input = document.createElement("input");
    document.body.appendChild(input);
    fireEvent.keyDown(input, { key: "ArrowRight" });
    expect(root().getAttribute("data-item")).toBe("0");
    input.remove();

    const dlg = document.createElement("div");
    dlg.setAttribute("role", "dialog");
    dlg.setAttribute("aria-modal", "true");
    document.body.appendChild(dlg);
    fireEvent.keyDown(window, { key: "ArrowRight" });
    expect(root().getAttribute("data-item")).toBe("0");
    dlg.remove();

    fireEvent.keyDown(window, { key: "ArrowRight" });
    expect(root().getAttribute("data-item")).toBe("1");
  });

  it("la pausa de fuera detiene el avance", async () => {
    render(<StoryViewer inline authors={[ANA]} accounts={accounts} paused />);
    await flush();
    await flush(8000);
    expect(root().getAttribute("data-item")).toBe("0");
    expect(root().getAttribute("data-paused")).toBe("1");
  });
});
