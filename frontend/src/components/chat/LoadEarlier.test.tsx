// «Cargar mensajes anteriores» del chat abierto.
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const svc = vi.hoisted(() => ({
  AccountsService: {},
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
  HistoryService: { LoadMore: vi.fn(), StatusAll: vi.fn(), Status: vi.fn() },
}));

vi.mock("@/api/transport", async () => (await import("@/test/svcTransport")).svcTransport(svc));
vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(() => () => undefined) } }));

import { emitEvent } from "@/api/events";
import { emptyStatus, useHistory } from "@/store/history";
import { useUI } from "@/store/ui";
import LoadEarlier from "./LoadEarlier";
import MessageList from "./MessageList";
import { chat } from "./testkit";

const c = chat({ accountId: "wa-1", nativeId: "c1", title: "Ana Pérez" });

beforeEach(() => {
  vi.clearAllMocks();
  svc.HistoryService.LoadMore.mockResolvedValue(undefined);
  useUI.setState({ toasts: [] });
  useHistory.setState({ byAccount: {}, pending: {}, loaded: true });
});
afterEach(cleanup);

describe("LoadEarlier", () => {
  it("el botón llama a LoadMore con la cuenta y el id nativo del chat y pasa a «Cargando…»", async () => {
    render(<LoadEarlier chat={c} />);
    fireEvent.click(screen.getByRole("button", { name: /Cargar mensajes anteriores/ }));
    await waitFor(() => expect(svc.HistoryService.LoadMore).toHaveBeenCalledWith("wa-1", "c1"));
    const btn = await screen.findByRole("button", { name: /Cargando…/ });
    expect(btn).toBeDisabled();
    expect(btn).toHaveAttribute("aria-busy", "true");
    fireEvent.click(btn);
    expect(svc.HistoryService.LoadMore).toHaveBeenCalledTimes(1);
  });

  it("vuelve al texto normal cuando el evento history:sync dice que la tanda del chat terminó", async () => {
    const off = useHistory.getState().init();
    render(<LoadEarlier chat={c} />);
    fireEvent.click(screen.getByRole("button", { name: /Cargar mensajes anteriores/ }));
    await screen.findByRole("button", { name: /Cargando…/ });

    act(() => emitEvent("history:sync", { ...emptyStatus("wa-1"), running: true, phase: "requesting", current: "Ana Pérez" }));
    expect(screen.getByRole("button", { name: /Cargando…/ })).toBeDisabled();

    act(() => emitEvent("history:sync", { ...emptyStatus("wa-1"), running: false }));
    expect(await screen.findByRole("button", { name: /Cargar mensajes anteriores/ })).toBeEnabled();
    off();
  });

  it("si LoadMore falla, avisa con el mensaje del error y el botón queda libre", async () => {
    svc.HistoryService.LoadMore.mockRejectedValue(new Error("La cuenta no está conectada"));
    render(<LoadEarlier chat={c} />);
    fireEvent.click(screen.getByRole("button", { name: /Cargar mensajes anteriores/ }));
    await waitFor(() => expect(useUI.getState().toasts.some((t) => t.message.includes("La cuenta no está conectada"))).toBe(true));
    expect(screen.getByRole("button", { name: /Cargar mensajes anteriores/ })).toBeEnabled();
  });

  it("otra cuenta o chat cargando no lo bloquea", () => {
    useHistory.setState({ pending: { "wa-1": { chatNativeId: "otro", title: "Luis", at: Date.now(), seen: false } } });
    render(<LoadEarlier chat={c} />);
    expect(screen.getByRole("button", { name: /Cargar mensajes anteriores/ })).toBeEnabled();
  });
});

describe("MessageList · topSlot", () => {
  it("el botón se monta arriba de la lista sin ser parte del contenido que se desplaza", () => {
    render(
      <MessageList
        chatKey="wa-1|c1"
        items={[]}
        hasMore={false}
        loading={false}
        onLoadOlder={() => undefined}
        renderItem={() => null}
        scrollTarget={null}
        topSlot={<button>Arriba</button>}
      />,
    );
    const slot = screen.getByRole("button", { name: "Arriba" });
    expect(screen.getByTestId("message-list").contains(slot)).toBe(false);
  });
});
