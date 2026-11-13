// Tarjeta de cuenta: sección «Historial de mensajes» (con el servicio de historial simulado).
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const svc = vi.hoisted(() => ({
  AccountsService: { List: vi.fn() },
  InboxService: {},
  MessagingService: {},
  StatusesService: {},
  ContactsService: {},
  CampaignsService: {},
  ScheduledService: {},
  StatsService: {},
  DestinationsService: {},
  SettingsService: {},
  FilesService: {},
  HistoryService: {
    Start: vi.fn(),
    Stop: vi.fn(),
    Reset: vi.fn(),
    Status: vi.fn(),
    StatusAll: vi.fn(),
    LoadMore: vi.fn(),
  },
}));

vi.mock("@/api/transport", async () => (await import("@/test/svcTransport")).svcTransport(svc));
vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(() => () => undefined) } }));

import type { AccountView, HistoryStatus } from "@/api/types";
import { emptyStatus, useHistory } from "@/store/history";
import { useUI } from "@/store/ui";
import AccountCard from "./AccountCard";
import HistorySection, { summaryParts } from "./HistorySection";

const H = svc.HistoryService;

const view = (id: string, channel: string, state = "connected", caps: Record<string, unknown> = { historyOnDemand: true }): AccountView =>
  ({
    account: { id, channel, label: `Cuenta ${id}`, color: "#25D366", enabled: true, selfName: "Yo", selfHandle: "", config: null },
    status: { accountId: id, state, need: "", since: 1 },
    capabilities: caps,
    unread: 0,
  }) as unknown as AccountView;

const st = (accountId: string, over: Partial<HistoryStatus> = {}): HistoryStatus => ({ ...emptyStatus(accountId), ...over });
const seed = (s: HistoryStatus) => act(() => useHistory.setState({ byAccount: { [s.accountId]: s }, loaded: true }));
const open = () => fireEvent.click(screen.getByRole("button", { name: /Historial de mensajes/ }));

let confirmResult = true;
beforeEach(() => {
  vi.clearAllMocks();
  confirmResult = true;
  H.Start.mockResolvedValue(undefined);
  H.Stop.mockResolvedValue(undefined);
  H.Reset.mockResolvedValue(undefined);
  H.Status.mockImplementation(async (id: string) => st(id));
  H.StatusAll.mockResolvedValue([]);
  useUI.setState({ toasts: [], confirm: vi.fn(async () => confirmResult) });
  useHistory.setState({ byAccount: {}, pending: {}, loaded: false });
});
afterEach(cleanup);

describe("summaryParts", () => {
  it("mensajes, chats recorridos y el más antiguo", () => {
    const oldest = new Date(2025, 2, 12, 10).getTime();
    expect(summaryParts(st("a", { messages: 1234, chatsDone: 45, chatsTotal: 120, oldestAt: oldest }))).toEqual([
      "1.234 mensajes guardados",
      "45 de 120 chats recorridos",
      "el más antiguo: 12/03/2025",
    ]);
  });
  it("sin chats ni fecha solo dice cuántos mensajes; uno en singular", () => {
    expect(summaryParts(st("a"))).toEqual(["0 mensajes guardados"]);
    expect(summaryParts(st("a", { messages: 1 }))).toEqual(["1 mensaje guardado"]);
  });
});

describe("HistorySection", () => {
  it("la tarjeta de la cuenta la trae, cerrada al principio", () => {
    render(
      <AccountCard
        view={view("wa-1", "whatsapp")}
        onDragStart={() => undefined}
        onDragEnter={() => undefined}
        onDragEnd={() => undefined}
        onMove={() => undefined}
        onConnect={() => undefined}
        onDisconnect={() => undefined}
        onEdit={() => undefined}
        onRelink={() => undefined}
        onLogout={() => undefined}
        onDelete={() => undefined}
      />,
    );
    expect(screen.getByTestId("history-section")).toBeInTheDocument();
    expect(screen.queryByTestId("history-summary")).toBeNull();
    open();
    expect(screen.getByTestId("history-summary")).toBeInTheDocument();
  });

  it("estado: mensajes guardados, chats recorridos y mensaje más antiguo", () => {
    seed(st("wa-1", { messages: 2048, chatsDone: 10, chatsTotal: 40, oldestAt: new Date(2024, 0, 5).getTime() }));
    render(<HistorySection view={view("wa-1", "whatsapp")} />);
    open();
    expect(screen.getByTestId("history-summary")).toHaveTextContent(
      "2.048 mensajes guardados · 10 de 40 chats recorridos · el más antiguo: 05/01/2024",
    );
  });

  it("en reposo: «Guardar historial completo» lanza Start y se actualiza el estado", async () => {
    H.Status.mockResolvedValueOnce(st("wa-1", { running: true, phase: "requesting", chatsTotal: 10, chatsDone: 1, current: "Ana" }));
    render(<HistorySection view={view("wa-1", "whatsapp")} />);
    open();
    expect(screen.queryByRole("button", { name: "Detener" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /Guardar historial completo/ }));
    await waitFor(() => expect(H.Start).toHaveBeenCalledWith("wa-1"));
    expect(await screen.findByRole("button", { name: /Detener/ })).toBeInTheDocument();
  });

  it("en marcha: barra de progreso, chat actual y «Detener»", async () => {
    seed(st("wa-1", { running: true, phase: "requesting", chatsTotal: 120, chatsDone: 45, current: "Ana Pérez", added: 312 }));
    render(<HistorySection view={view("wa-1", "whatsapp")} />);
    expect(screen.getByTestId("history-badge")).toHaveTextContent("37 %");
    open();
    const bar = screen.getByRole("progressbar", { name: "Avance del historial" });
    expect(bar).toHaveAttribute("aria-valuenow", "37");
    expect(screen.getByTestId("history-current")).toHaveTextContent("Ana Pérez");
    expect(screen.getByTestId("history-progress")).toHaveTextContent("+312 mensajes en esta tanda");
    expect(screen.queryByRole("button", { name: /Guardar historial completo/ })).toBeNull();
    expect(screen.queryByRole("button", { name: "Reiniciar" })).toBeNull();

    H.Status.mockResolvedValueOnce(st("wa-1", { running: false, chatsTotal: 120, chatsDone: 45 }));
    fireEvent.click(screen.getByRole("button", { name: /Detener/ }));
    await waitFor(() => expect(H.Stop).toHaveBeenCalledWith("wa-1"));
    await waitFor(() => expect(screen.queryByRole("progressbar")).toBeNull());
  });

  it("el evento history:sync (vía store) mueve la barra", () => {
    seed(st("wa-1", { running: true, chatsTotal: 10, chatsDone: 1 }));
    render(<HistorySection view={view("wa-1", "whatsapp")} />);
    open();
    expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "10");
    act(() => useHistory.setState({ byAccount: { "wa-1": st("wa-1", { running: true, chatsTotal: 10, chatsDone: 5 }) } }));
    expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "50");
  });

  it("muestra el motivo de detención en español", () => {
    seed(st("wa-1", { stoppedReason: "El teléfono no responde: comprueba que tenga internet y WhatsApp abierto." }));
    render(<HistorySection view={view("wa-1", "whatsapp")} />);
    open();
    expect(screen.getByTestId("history-stopped")).toHaveTextContent(
      "Se detuvo: El teléfono no responde: comprueba que tenga internet y WhatsApp abierto.",
    );
  });

  it("«Reiniciar» pide confirmación; si no se confirma no se llama", async () => {
    seed(st("wa-1", { chatsDone: 3, chatsTotal: 9, messages: 40 }));
    render(<HistorySection view={view("wa-1", "whatsapp")} />);
    open();
    confirmResult = false;
    fireEvent.click(screen.getByRole("button", { name: "Reiniciar" }));
    await waitFor(() => expect(useUI.getState().confirm).toHaveBeenCalledWith(expect.objectContaining({ danger: true })));
    expect(H.Reset).not.toHaveBeenCalled();

    confirmResult = true;
    fireEvent.click(screen.getByRole("button", { name: "Reiniciar" }));
    await waitFor(() => expect(H.Reset).toHaveBeenCalledWith("wa-1"));
  });

  it("sin avance guardado no hay «Reiniciar»", () => {
    render(<HistorySection view={view("wa-1", "whatsapp")} />);
    open();
    expect(screen.queryByRole("button", { name: "Reiniciar" })).toBeNull();
  });

  it("aviso de que va despacio a propósito, con el nombre de quien bloquea", () => {
    const { unmount } = render(<HistorySection view={view("wa-1", "whatsapp")} />);
    open();
    expect(screen.getByTestId("history-slow")).toHaveTextContent("Va despacio a propósito: así WhatsApp no bloquea la cuenta.");
    unmount();
    render(<HistorySection view={view("tg-1", "telegram")} />);
    open();
    expect(screen.getByTestId("history-slow")).toHaveTextContent("así Telegram no bloquea");
  });

  it("WhatsApp: nota sobre las cuentas ya vinculadas y sin aviso de vía no oficial", () => {
    render(<HistorySection view={view("wa-1", "whatsapp")} />);
    open();
    expect(screen.getByTestId("history-wa-note")).toHaveTextContent(/ya vinculadas solo reciben lo que el teléfono envíe/);
    expect(screen.getByTestId("history-wa-note")).toHaveTextContent(/vincular la cuenta de nuevo/);
    expect(screen.queryByTestId("history-unofficial")).toBeNull();
  });

  it.each(["messenger", "instagram"])("%s: avisa del riesgo de la vía no oficial y habla de Meta", (ch) => {
    render(<HistorySection view={view("m-1", ch)} />);
    open();
    expect(screen.getByTestId("history-unofficial")).toHaveTextContent(/vía no oficial/);
    expect(screen.getByTestId("history-slow")).toHaveTextContent("así Meta no bloquea la cuenta");
    expect(screen.queryByTestId("history-wa-note")).toBeNull();
  });

  it("cuenta desconectada: el botón está desactivado y dice por qué", () => {
    render(<HistorySection view={view("wa-1", "whatsapp", "disconnected")} />);
    open();
    expect(screen.getByRole("button", { name: /Guardar historial completo/ })).toBeDisabled();
    expect(screen.getByTestId("history-disabled")).toHaveTextContent("Conecta la cuenta");
  });

  it("canal sin historial bajo demanda: desactivado con su explicación", () => {
    render(<HistorySection view={view("x-1", "telegram", "connected", { historyOnDemand: false })} />);
    open();
    expect(screen.getByRole("button", { name: /Guardar historial completo/ })).toBeDisabled();
    expect(within(screen.getByTestId("history-section")).getByTestId("history-disabled")).toHaveTextContent(/no permite pedir/);
  });

  it("completo: insignia «Completo»", () => {
    seed(st("wa-1", { chatsTotal: 10, chatsDone: 10 }));
    render(<HistorySection view={view("wa-1", "whatsapp")} />);
    expect(screen.getByTestId("history-badge")).toHaveTextContent("Completo");
  });
});
