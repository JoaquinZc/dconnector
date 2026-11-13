import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const hoisted = vi.hoisted(() => ({
  svc: {
    AccountsService: { List: vi.fn() },
    InboxService: { ListChats: vi.fn() },
    MessagingService: {},
    StatusesService: {},
    ContactsService: {},
    CampaignsService: {},
    ScheduledService: { List: vi.fn(), Save: vi.fn(), Cancel: vi.fn(), Delete: vi.fn(), SendNow: vi.fn() },
    StatsService: {},
    DestinationsService: {},
    SettingsService: {},
    FilesService: { PickFiles: vi.fn() },
  },
}));
const { svc } = hoisted;

vi.mock("@/api/transport", async () => (await import("@/test/svcTransport")).svcTransport(hoisted.svc));
vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(() => () => undefined) } }));

import type { AccountView, CapSet, Chat, Scheduled } from "@/api/types";
import ScheduledPage from "@/pages/ScheduledPage";
import { useAccounts } from "@/store/accounts";
import { useUI } from "@/store/ui";

const HOUR = 3_600_000;

const sch = (id: string, state: string, sendAt: number, over: Partial<Record<keyof Scheduled, unknown>> = {}): Scheduled =>
  ({
    id,
    accountId: "wa-1",
    kind: "message",
    chatNativeId: `c-${id}`,
    chatTitle: `Chat ${id}`,
    message: { kind: "text", text: `Texto ${id}` },
    sendAt,
    state,
    sentAt: 0,
    error: "",
    createdAt: 1,
    ...over,
  }) as unknown as Scheduled;

const chat = (nativeId: string, title: string, over: Partial<Record<keyof Chat, unknown>> = {}): Chat =>
  ({ key: `wa-1|${nativeId}`, accountId: "wa-1", channel: "whatsapp", nativeId, title, avatarUrl: "", lastPreview: "", readOnly: false, ...over }) as unknown as Chat;

const CAPS = {
  sendText: true,
  sendImage: true,
  sendVideo: false,
  sendAudio: true,
  sendDocument: true,
  maxTextLen: 0,
  maxFileSize: 0,
  maxImageSize: 0,
  maxVideoSize: 0,
} as unknown as CapSet;

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  const now = Date.now();
  svc.ScheduledService.List.mockResolvedValue([
    sch("p1", "pending", now + 2 * HOUR),
    sch("s1", "sent", now - HOUR, { sentAt: now - HOUR }),
    sch("f1", "failed", now - 2 * HOUR, { error: "La cuenta no estuvo conectada" }),
    sch("c1", "canceled", now - 3 * HOUR),
    sch("st1", "pending", now + 5 * HOUR, { kind: "status", chatNativeId: "", chatTitle: "", message: null, status: { kind: "text", text: "Hoy abrimos" } }),
  ]);
  svc.ScheduledService.Save.mockImplementation(async (s: Scheduled) => ({ ...s, id: s.id || "nuevo" }));
  svc.ScheduledService.Cancel.mockResolvedValue(undefined);
  svc.ScheduledService.Delete.mockResolvedValue(undefined);
  svc.ScheduledService.SendNow.mockResolvedValue(undefined);
  svc.InboxService.ListChats.mockResolvedValue([chat("c-ana", "Ana Pérez"), chat("c-ro", "Solo lectura", { readOnly: true }), chat("c-luis", "Luis")]);
  svc.FilesService.PickFiles.mockResolvedValue([]);
  svc.AccountsService.List.mockResolvedValue([]);
  useUI.setState({ toasts: [], confirmReq: null, confirm: vi.fn().mockResolvedValue(true) });
  useAccounts.setState({
    views: {
      "wa-1": {
        account: { id: "wa-1", channel: "whatsapp", label: "Ventas", color: "#25D366", enabled: true },
        status: {},
        capabilities: CAPS,
        unread: 0,
      } as unknown as AccountView,
    },
    order: ["wa-1"],
    loaded: true,
  });
});
afterEach(cleanup);

const rows = () => screen.getAllByTestId("scheduled-row");
const rowOf = (text: string) => rows().find((r) => within(r).queryByText(text))!;

describe("tabla de programados", () => {
  it("fecha, cuenta, destino, tipo, contenido y estado; los pendientes primero", async () => {
    render(<ScheduledPage />);
    await screen.findAllByTestId("scheduled-row");
    expect(rows()).toHaveLength(5);
    expect(rows().map((r) => r.getAttribute("data-state"))).toEqual(["pending", "pending", "sent", "failed", "canceled"]);
    const p1 = rowOf("Chat p1");
    expect(within(p1).getByText("Ventas")).toBeInTheDocument();
    expect(within(p1).getByText("Mensaje")).toBeInTheDocument();
    expect(within(p1).getByText("Texto p1")).toBeInTheDocument();
    expect(within(p1).getByText("Pendiente")).toBeInTheDocument();
    expect(within(p1).getByText(/^\d{2}\/\d{2}\/\d{4} \d{2}:\d{2}$/)).toBeInTheDocument();
    // Los de tipo estado se ven, con destino «Estados».
    const st = rowOf("Estados");
    expect(within(st).getByText("Estado")).toBeInTheDocument();
    expect(within(st).getByText("Hoy abrimos")).toBeInTheDocument();
  });

  it("filtros por estado con su recuento", async () => {
    render(<ScheduledPage />);
    await screen.findAllByTestId("scheduled-row");
    expect(screen.getByTestId("count-all")).toHaveTextContent("5");
    expect(screen.getByTestId("count-pending")).toHaveTextContent("2");
    expect(screen.getByTestId("count-sent")).toHaveTextContent("1");
    expect(screen.getByTestId("count-failed")).toHaveTextContent("1");
    expect(screen.getByTestId("count-canceled")).toHaveTextContent("1");
    fireEvent.click(screen.getByRole("tab", { name: /Fallidos/ }));
    expect(rows()).toHaveLength(1);
    expect(within(rows()[0]).getByText("Fallido")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab", { name: /Pendientes/ }));
    expect(rows()).toHaveLength(2);
    fireEvent.click(screen.getByRole("tab", { name: /Cancelados/ }));
    expect(rows()).toHaveLength(1);
    fireEvent.click(screen.getByRole("tab", { name: /Todos/ }));
    expect(rows()).toHaveLength(5);
  });

  it("vacío corto cuando el filtro no tiene nada", async () => {
    svc.ScheduledService.List.mockResolvedValue([sch("p1", "pending", Date.now() + HOUR)]);
    render(<ScheduledPage />);
    await screen.findAllByTestId("scheduled-row");
    fireEvent.click(screen.getByRole("tab", { name: /Enviados/ }));
    expect(screen.getByText("Nada con este estado")).toBeInTheDocument();
  });

  it("sin programados", async () => {
    svc.ScheduledService.List.mockResolvedValue([]);
    render(<ScheduledPage />);
    expect(await screen.findByText("Sin programados")).toBeInTheDocument();
  });

  it("acciones según estado y tipo", async () => {
    render(<ScheduledPage />);
    await screen.findAllByTestId("scheduled-row");
    const has = (row: HTMLElement, name: string) => within(row).queryByRole("button", { name }) !== null;
    const p1 = rowOf("Chat p1");
    expect([has(p1, "Editar"), has(p1, "Enviar ahora"), has(p1, "Cancelar"), has(p1, "Borrar")]).toEqual([true, true, true, true]);
    const f1 = rowOf("Chat f1");
    expect([has(f1, "Editar"), has(f1, "Enviar ahora"), has(f1, "Cancelar"), has(f1, "Borrar")]).toEqual([true, true, false, true]);
    const s1 = rowOf("Chat s1");
    expect([has(s1, "Editar"), has(s1, "Enviar ahora"), has(s1, "Cancelar"), has(s1, "Borrar")]).toEqual([false, false, false, true]);
    const st = rowOf("Estados");
    expect([has(st, "Editar"), has(st, "Cancelar"), has(st, "Borrar")]).toEqual([false, true, true]);
  });

  it("enviar ahora, cancelar y borrar llaman al servicio", async () => {
    render(<ScheduledPage />);
    await screen.findAllByTestId("scheduled-row");
    fireEvent.click(within(rowOf("Chat p1")).getByRole("button", { name: "Enviar ahora" }));
    await waitFor(() => expect(svc.ScheduledService.SendNow).toHaveBeenCalledWith("p1"));
    fireEvent.click(within(rowOf("Chat p1")).getByRole("button", { name: "Cancelar" }));
    await waitFor(() => expect(svc.ScheduledService.Cancel).toHaveBeenCalledWith("p1"));
    fireEvent.click(within(rowOf("Chat s1")).getByRole("button", { name: "Borrar" }));
    await waitFor(() => expect(svc.ScheduledService.Delete).toHaveBeenCalledWith("s1"));
    // Los de tipo estado también se cancelan.
    fireEvent.click(within(rowOf("Estados")).getByRole("button", { name: "Cancelar" }));
    await waitFor(() => expect(svc.ScheduledService.Cancel).toHaveBeenCalledWith("st1"));
  });

  it("sin confirmar no se borra", async () => {
    useUI.setState({ confirm: vi.fn().mockResolvedValue(false) });
    render(<ScheduledPage />);
    await screen.findAllByTestId("scheduled-row");
    fireEvent.click(within(rowOf("Chat s1")).getByRole("button", { name: "Borrar" }));
    await Promise.resolve();
    expect(svc.ScheduledService.Delete).not.toHaveBeenCalled();
  });
});

describe("«Nuevo»", () => {
  const open = async () => {
    render(<ScheduledPage />);
    await screen.findAllByTestId("scheduled-row");
    fireEvent.click(screen.getByRole("button", { name: /Nuevo/ }));
    await screen.findByRole("dialog");
  };

  it("cuenta → chat (buscador con Query) → mensaje → fecha, y guarda un mensaje pendiente", async () => {
    await open();
    // Una sola cuenta: ya viene elegida.
    expect(screen.getByLabelText("Cuenta")).toHaveValue("wa-1");
    await waitFor(() => expect(svc.InboxService.ListChats).toHaveBeenCalled());
    expect(svc.InboxService.ListChats.mock.calls.at(-1)![0]).toMatchObject({ accountIds: ["wa-1"], query: "" });
    // Los chats de solo lectura no se ofrecen.
    expect(await screen.findByRole("option", { name: /Ana Pérez/ })).toBeInTheDocument();
    expect(screen.queryByRole("option", { name: /Solo lectura/ })).toBeNull();

    fireEvent.change(screen.getByLabelText("Buscar chat"), { target: { value: "ana" } });
    await waitFor(() => expect(svc.InboxService.ListChats.mock.calls.at(-1)![0]).toMatchObject({ query: "ana" }));

    fireEvent.click(await screen.findByRole("option", { name: /Ana Pérez/ }));
    expect(screen.getByTestId("chat-selected")).toHaveTextContent("Ana Pérez");
    fireEvent.change(screen.getByLabelText("Mensaje"), { target: { value: "Recuerda la cita" } });
    fireEvent.change(screen.getByLabelText("Fecha y hora"), { target: { value: "2099-01-02T08:30" } });
    fireEvent.click(screen.getByRole("button", { name: "Programar" }));

    await waitFor(() => expect(svc.ScheduledService.Save).toHaveBeenCalledTimes(1));
    expect(svc.ScheduledService.Save.mock.calls[0][0]).toMatchObject({
      id: "",
      accountId: "wa-1",
      kind: "message",
      chatNativeId: "c-ana",
      chatTitle: "Ana Pérez",
      message: { kind: "text", text: "Recuerda la cita" },
      sendAt: new Date(2099, 0, 2, 8, 30).getTime(),
      state: "pending",
    });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(localStorage.getItem("draft:scheduled:new")).toBeNull();
  });

  it("dice qué falta y no guarda", async () => {
    await open();
    fireEvent.click(screen.getByRole("button", { name: "Programar" }));
    expect(screen.getByRole("alert")).toHaveTextContent("Elige un chat");
    fireEvent.click(await screen.findByRole("option", { name: /Luis/ }));
    fireEvent.click(screen.getByRole("button", { name: "Programar" }));
    expect(screen.getByRole("alert")).toHaveTextContent("Escribe un mensaje o adjunta un archivo");
    fireEvent.change(screen.getByLabelText("Mensaje"), { target: { value: "Hola" } });
    fireEvent.change(screen.getByLabelText("Fecha y hora"), { target: { value: "2020-01-01T08:00" } });
    fireEvent.click(screen.getByRole("button", { name: "Programar" }));
    expect(screen.getByRole("alert")).toHaveTextContent("La fecha ya pasó");
    expect(svc.ScheduledService.Save).not.toHaveBeenCalled();
  });

  it("un archivo según el tipo y las capacidades de la cuenta", async () => {
    svc.FilesService.PickFiles.mockResolvedValue([{ path: "C:\\x\\clip.mp4", name: "clip.mp4", mime: "video/mp4", size: 100 }]);
    await open();
    fireEvent.click(await screen.findByRole("option", { name: /Luis/ }));
    fireEvent.click(screen.getByRole("button", { name: /Adjuntar/ }));
    expect(await screen.findByText("clip.mp4")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Fecha y hora"), { target: { value: "2099-01-02T08:30" } });
    fireEvent.click(screen.getByRole("button", { name: "Programar" }));
    expect(screen.getByRole("alert")).toHaveTextContent("Esta cuenta no envía vídeos");

    svc.FilesService.PickFiles.mockResolvedValue([{ path: "C:\\x\\oferta.jpg", name: "oferta.jpg", mime: "image/jpeg", size: 100 }]);
    fireEvent.click(screen.getByRole("button", { name: "Quitar archivo" }));
    fireEvent.click(screen.getByRole("button", { name: /Adjuntar/ }));
    expect(await screen.findByText("oferta.jpg")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Mensaje"), { target: { value: "Pie" } });
    fireEvent.click(screen.getByRole("button", { name: "Programar" }));
    await waitFor(() => expect(svc.ScheduledService.Save).toHaveBeenCalledTimes(1));
    expect(svc.ScheduledService.Save.mock.calls[0][0].message).toEqual({
      kind: "image",
      text: "Pie",
      filePath: "C:\\x\\oferta.jpg",
      fileName: "oferta.jpg",
      mime: "image/jpeg",
    });
  });

  it("borrador en draft:scheduled:new y se restaura", async () => {
    await open();
    fireEvent.change(screen.getByLabelText("Mensaje"), { target: { value: "A medias" } });
    expect(JSON.parse(localStorage.getItem("draft:scheduled:new")!)).toMatchObject({ text: "A medias" });
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Cancelar" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    fireEvent.click(screen.getByRole("button", { name: /Nuevo/ }));
    expect(await screen.findByLabelText("Mensaje")).toHaveValue("A medias");
  });

  it("abrir y cerrar sin tocar nada no deja borrador", async () => {
    await open();
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Cancelar" }));
    expect(localStorage.getItem("draft:scheduled:new")).toBeNull();
  });
});

describe("editar", () => {
  it("un fallido se corrige y vuelve a pendiente con su id", async () => {
    render(<ScheduledPage />);
    await screen.findAllByTestId("scheduled-row");
    fireEvent.click(within(rowOf("Chat f1")).getByRole("button", { name: "Editar" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("heading", { name: "Editar programado" })).toBeInTheDocument();
    expect(within(dialog).getByLabelText("Mensaje")).toHaveValue("Texto f1");
    expect(within(dialog).getByTestId("chat-selected")).toHaveTextContent("Chat f1");
    fireEvent.change(within(dialog).getByLabelText("Mensaje"), { target: { value: "Corregido" } });
    fireEvent.change(within(dialog).getByLabelText("Fecha y hora"), { target: { value: "2099-05-05T10:00" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(svc.ScheduledService.Save).toHaveBeenCalledTimes(1));
    expect(svc.ScheduledService.Save.mock.calls[0][0]).toMatchObject({
      id: "f1",
      chatNativeId: "c-f1",
      state: "pending",
      error: "",
      message: { kind: "text", text: "Corregido" },
      sendAt: new Date(2099, 4, 5, 10, 0).getTime(),
    });
    expect(localStorage.getItem("draft:scheduled:f1")).toBeNull();
  });
});

describe("recarga", () => {
  it("la tabla se actualiza sola cada 15 s", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      render(<ScheduledPage />);
      await screen.findAllByTestId("scheduled-row");
      const calls = svc.ScheduledService.List.mock.calls.length;
      await act(async () => {
        await vi.advanceTimersByTimeAsync(15_100);
      });
      expect(svc.ScheduledService.List.mock.calls.length).toBeGreaterThan(calls);
    } finally {
      vi.useRealTimers();
    }
  });
});
