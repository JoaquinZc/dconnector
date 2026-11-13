// «Programar» en el compositor: botón, insignia con los pendientes del chat y panel.
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const hoisted = vi.hoisted(() => ({
  svc: {
    AccountsService: {},
    InboxService: {},
    MessagingService: { Send: vi.fn(), SetTyping: vi.fn() },
    StatusesService: {},
    ContactsService: {},
    CampaignsService: {},
    ScheduledService: {
      ListForChat: vi.fn(),
      Save: vi.fn(),
      Update: vi.fn(),
      UpdateSeries: vi.fn(),
      Cancel: vi.fn(),
      CancelSeries: vi.fn(),
      Delete: vi.fn(),
      DeleteSeries: vi.fn(),
      SendNow: vi.fn(),
      PreviewRecurrence: vi.fn(),
    },
    StatsService: {},
    DestinationsService: {},
    SettingsService: {},
    FilesService: { PickFiles: vi.fn(), SaveTemp: vi.fn() },
  },
}));
const { svc } = hoisted;
const S = svc.ScheduledService;

vi.mock("@/api/transport", async () => (await import("@/test/svcTransport")).svcTransport(hoisted.svc));
vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(() => () => undefined) } }));

import type { Scheduled } from "@/api/types";
import { useChat } from "@/store/chat";
import { useUI } from "@/store/ui";
import Composer from "./Composer";
import { CHAT_SCHEDULED_POLL_MS } from "./useChatScheduled";
import { chat as makeChat, FULL_CAPS } from "./testkit";

const chat = makeChat({ key: "wa-1|c7", nativeId: "c7", title: "Ana Pérez" });
const WHEN_MS = new Date(2099, 0, 5, 9, 0).getTime();

const sch = (id: string, state: string, over: Partial<Record<keyof Scheduled, unknown>> = {}): Scheduled =>
  ({
    id,
    accountId: "wa-1",
    kind: "message",
    chatNativeId: "c7",
    chatTitle: "Ana Pérez",
    message: { kind: "text", text: `Texto ${id}` },
    sendAt: WHEN_MS,
    state,
    sentAt: 0,
    error: "",
    createdAt: 1,
    recurrence: null,
    seriesId: "",
    ...over,
  }) as unknown as Scheduled;

const draft = () => localStorage.getItem("draft:chats:wa-1|c7");
const box = () => screen.getByPlaceholderText("Escribe un mensaje") as HTMLTextAreaElement;
const type = (v: string) => fireEvent.change(box(), { target: { value: v } });

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  useChat.setState({ replyTo: null, editing: null });
  useUI.setState({ toasts: [], confirmReq: null, confirm: vi.fn().mockResolvedValue(true) });
  S.ListForChat.mockResolvedValue([]);
  S.Save.mockImplementation(async (s: Scheduled) => ({ ...s, id: "nuevo" }));
  S.Update.mockImplementation(async (s: Scheduled) => s);
  S.PreviewRecurrence.mockResolvedValue([]);
  svc.MessagingService.SetTyping.mockResolvedValue(undefined);
});
afterEach(cleanup);

describe("botón «Programar» del compositor", () => {
  it("pide los programados del chat y no muestra insignia si no hay", async () => {
    render(<Composer chat={chat} caps={FULL_CAPS} />);
    await waitFor(() => expect(S.ListForChat).toHaveBeenCalledWith("wa-1", "c7", true));
    expect(screen.getByRole("button", { name: "Programar mensaje" })).toBeInTheDocument();
    expect(screen.queryByTestId("scheduled-badge")).toBeNull();
  });

  it("programa lo escrito: el diálogo trae el texto y el borrador se vacía al guardar", async () => {
    render(<Composer chat={chat} caps={FULL_CAPS} />);
    type("Recuérdame mañana");
    expect(draft()).toBe("Recuérdame mañana");
    fireEvent.click(screen.getByRole("button", { name: "Programar mensaje" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByLabelText("Mensaje")).toHaveValue("Recuérdame mañana");
    fireEvent.change(within(dialog).getByLabelText("Fecha y hora"), { target: { value: "2099-01-05T09:00" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Programar" }));
    await waitFor(() => expect(S.Save).toHaveBeenCalledTimes(1));
    expect(S.Save.mock.calls[0][0]).toMatchObject({
      accountId: "wa-1",
      chatNativeId: "c7",
      chatTitle: "Ana Pérez",
      message: { kind: "text", text: "Recuérdame mañana" },
      sendAt: WHEN_MS,
    });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(box()).toHaveValue("");
    expect(draft()).toBeNull();
    // no se envió nada: solo se programó
    expect(svc.MessagingService.Send).not.toHaveBeenCalled();
  });

  it("con el compositor vacío se escribe en el diálogo y el compositor no cambia", async () => {
    render(<Composer chat={chat} caps={FULL_CAPS} />);
    fireEvent.click(screen.getByRole("button", { name: "Programar mensaje" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByLabelText("Mensaje")).toHaveValue("");
    fireEvent.change(within(dialog).getByLabelText("Mensaje"), { target: { value: "Desde el diálogo" } });
    fireEvent.change(within(dialog).getByLabelText("Fecha y hora"), { target: { value: "2099-01-05T09:00" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Programar" }));
    await waitFor(() => expect(S.Save).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(box()).toHaveValue("");
  });

  it("cancelar el diálogo deja el borrador intacto", async () => {
    render(<Composer chat={chat} caps={FULL_CAPS} />);
    type("Borrador");
    fireEvent.click(screen.getByRole("button", { name: "Programar mensaje" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancelar" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(box()).toHaveValue("Borrador");
    expect(draft()).toBe("Borrador");
    expect(S.Save).not.toHaveBeenCalled();
  });
});

describe("insignia y panel de programados del chat", () => {
  beforeEach(() => {
    S.ListForChat.mockResolvedValue([sch("p1", "pending"), sch("p2", "pending"), sch("s1", "sent")]);
  });

  it("la insignia cuenta los pendientes y abre el panel", async () => {
    render(<Composer chat={chat} caps={FULL_CAPS} />);
    const badge = await screen.findByTestId("scheduled-badge");
    expect(badge).toHaveTextContent("2");
    expect(badge).toHaveAccessibleName("Programados de este chat: 2 pendientes");
    fireEvent.click(badge);
    const panel = await screen.findByRole("dialog", { name: "Programados de este chat" });
    expect(within(panel).getAllByTestId("chat-scheduled-item")).toHaveLength(3);
    // se refresca al abrir
    await waitFor(() => expect(S.ListForChat.mock.calls.length).toBeGreaterThanOrEqual(2));
  });

  it("un fallido sin pendientes se avisa con «!»", async () => {
    S.ListForChat.mockResolvedValue([sch("f1", "failed", { error: "sin conexión" })]);
    render(<Composer chat={chat} caps={FULL_CAPS} />);
    const badge = await screen.findByTestId("scheduled-badge");
    expect(badge).toHaveTextContent("!");
    expect(badge).toHaveAccessibleName("Programados de este chat: 0 pendientes, 1 fallidos");
  });

  it("editar desde el panel abre el diálogo con los datos y guarda con Update; el compositor no se toca", async () => {
    render(<Composer chat={chat} caps={FULL_CAPS} />);
    type("Mi borrador");
    fireEvent.click(await screen.findByTestId("scheduled-badge"));
    const panel = await screen.findByRole("dialog", { name: "Programados de este chat" });
    const row = within(panel).getAllByTestId("chat-scheduled-item")[0];
    fireEvent.click(within(row).getByRole("button", { name: "Editar" }));
    const dialog = await screen.findByRole("heading", { name: "Editar programado" }).then((h) => h.closest("[role=dialog]") as HTMLElement);
    expect(within(dialog).getByLabelText("Mensaje")).toHaveValue("Texto p1");
    fireEvent.change(within(dialog).getByLabelText("Mensaje"), { target: { value: "Editado" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(S.Update).toHaveBeenCalledTimes(1));
    expect(S.Update.mock.calls[0][0]).toMatchObject({ id: "p1", message: { text: "Editado" } });
    await waitFor(() => expect(screen.queryByRole("heading", { name: "Editar programado" })).toBeNull());
    expect(box()).toHaveValue("Mi borrador");
    // el panel vuelve a abrirse
    expect(await screen.findByRole("dialog", { name: "Programados de este chat" })).toBeInTheDocument();
  });

  it("«Programar» del panel abre el diálogo para uno nuevo", async () => {
    render(<Composer chat={chat} caps={FULL_CAPS} />);
    fireEvent.click(await screen.findByTestId("scheduled-badge"));
    const panel = await screen.findByRole("dialog", { name: "Programados de este chat" });
    fireEvent.click(within(panel).getByRole("button", { name: "Programar" }));
    expect(await screen.findByRole("heading", { name: "Programar mensaje" })).toBeInTheDocument();
  });

  it("se actualiza solo cada 60 s", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      render(<Composer chat={chat} caps={FULL_CAPS} />);
      await screen.findByTestId("scheduled-badge");
      const calls = S.ListForChat.mock.calls.length;
      S.ListForChat.mockResolvedValue([sch("p1", "pending")]);
      await vi.advanceTimersByTimeAsync(CHAT_SCHEDULED_POLL_MS + 100);
      expect(S.ListForChat.mock.calls.length).toBeGreaterThan(calls);
      await waitFor(() => expect(screen.getByTestId("scheduled-badge")).toHaveTextContent("1"));
    } finally {
      vi.useRealTimers();
    }
  });
});
