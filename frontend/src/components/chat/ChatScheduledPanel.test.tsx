import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const hoisted = vi.hoisted(() => ({
  svc: {
    AccountsService: {},
    InboxService: {},
    MessagingService: {},
    StatusesService: {},
    ContactsService: {},
    CampaignsService: {},
    ScheduledService: {
      Cancel: vi.fn(),
      CancelSeries: vi.fn(),
      Delete: vi.fn(),
      DeleteSeries: vi.fn(),
      SendNow: vi.fn(),
    },
    StatsService: {},
    DestinationsService: {},
    SettingsService: {},
    FilesService: {},
  },
}));
const S = hoisted.svc.ScheduledService;

vi.mock("@/api/transport", async () => (await import("@/test/svcTransport")).svcTransport(hoisted.svc));
vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(() => () => undefined) } }));

import type { Scheduled } from "@/api/types";
import { useUI } from "@/store/ui";
import ChatScheduledPanel from "./ChatScheduledPanel";

const MON_9 = new Date(2099, 0, 5, 9, 0).getTime();
const RULE = { frequency: "weekly", every: 1, weekdays: [1, 4], endAt: 0, maxCount: 0, count: 0, firstAt: MON_9 };

const sch = (id: string, state: string, sendAt: number, over: Partial<Record<keyof Scheduled, unknown>> = {}): Scheduled =>
  ({
    id,
    accountId: "wa-1",
    kind: "message",
    chatNativeId: "c1",
    chatTitle: "Ana",
    message: { kind: "text", text: `Texto ${id}` },
    sendAt,
    state,
    sentAt: 0,
    error: "",
    createdAt: 1,
    recurrence: null,
    seriesId: "",
    ...over,
  }) as unknown as Scheduled;

const ITEMS = [
  sch("sent1", "sent", MON_9 - 86_400_000, { seriesId: "s1", recurrence: RULE }),
  sch("p2", "pending", MON_9 + 5 * 86_400_000),
  sch("p1", "pending", MON_9, { seriesId: "s1", recurrence: RULE }),
  sch("f1", "failed", MON_9 - 2 * 86_400_000, { error: "sin conexión" }),
];

function setup(items: Scheduled[] = ITEMS) {
  const props = { onClose: vi.fn(), onNew: vi.fn(), onEdit: vi.fn(), onChanged: vi.fn() };
  render(<ChatScheduledPanel items={items} loaded {...props} />);
  return props;
}
const rows = () => screen.getAllByTestId("chat-scheduled-item");
const rowOf = (text: string) => rows().find((r) => within(r).queryByText(text))!;
const btn = (row: HTMLElement, name: string) => within(row).queryByRole("button", { name });

beforeEach(() => {
  vi.clearAllMocks();
  useUI.setState({ toasts: [], confirmReq: null, confirm: vi.fn().mockResolvedValue(true) });
  for (const f of Object.values(S)) f.mockResolvedValue(undefined);
});
afterEach(cleanup);

describe("panel «Programados de este chat»", () => {
  it("pendientes primero (por fecha) y luego los recientes", () => {
    setup();
    expect(screen.getByRole("dialog", { name: "Programados de este chat" })).toBeInTheDocument();
    expect(rows().map((r) => r.getAttribute("data-state"))).toEqual(["pending", "pending", "sent", "failed"]);
    expect(within(rows()[0]).getByText("Texto p1")).toBeInTheDocument();
    expect(within(rows()[1]).getByText("Texto p2")).toBeInTheDocument();
    expect(within(rowOf("Texto p1")).getByText("Pendiente")).toBeInTheDocument();
    expect(within(rowOf("Texto f1")).getByText("Fallido")).toBeInTheDocument();
    expect(within(rowOf("Texto f1")).getByText("sin conexión")).toBeInTheDocument();
    expect(within(rowOf("Texto p1")).getByText(/^\d{2}\/\d{2}\/\d{4} \d{2}:\d{2}$/)).toBeInTheDocument();
  });

  it("la serie lleva su regla; el suelto, no", () => {
    setup();
    expect(within(rowOf("Texto p1")).getByTestId("chat-scheduled-chip")).toHaveTextContent("Se repite: cada semana, lunes y jueves, a las 09:00");
    expect(within(rowOf("Texto p2")).queryByTestId("chat-scheduled-chip")).toBeNull();
    expect(within(rowOf("Texto sent1")).getByText("Ocurrencia de una serie")).toBeInTheDocument();
  });

  it("acciones según el estado y si es serie", () => {
    setup();
    const p1 = rowOf("Texto p1"); // pendiente de una serie
    expect(["Editar", "Enviar ahora", "Cancelar esta", "Cancelar serie", "Eliminar", "Eliminar serie"].map((n) => !!btn(p1, n))).toEqual([true, true, true, true, true, true]);
    const p2 = rowOf("Texto p2"); // pendiente suelto
    expect(["Editar", "Enviar ahora", "Cancelar", "Cancelar serie", "Eliminar", "Eliminar serie"].map((n) => !!btn(p2, n))).toEqual([true, true, true, false, true, false]);
    const f1 = rowOf("Texto f1");
    expect(["Editar", "Enviar ahora", "Cancelar", "Eliminar"].map((n) => !!btn(f1, n))).toEqual([true, true, false, true]);
    const sent = rowOf("Texto sent1");
    expect(["Editar", "Enviar ahora", "Cancelar esta"].map((n) => !!btn(sent, n))).toEqual([false, false, false]);
    expect(!!btn(sent, "Eliminar")).toBe(true);
  });

  it("editar avisa con el programado", () => {
    const p = setup();
    fireEvent.click(btn(rowOf("Texto p1"), "Editar")!);
    expect(p.onEdit).toHaveBeenCalledWith(expect.objectContaining({ id: "p1" }));
  });

  it("enviar ahora, cancelar esta, cancelar serie y eliminar llaman al servicio y recargan", async () => {
    const p = setup();
    fireEvent.click(btn(rowOf("Texto p1"), "Enviar ahora")!);
    await waitFor(() => expect(S.SendNow).toHaveBeenCalledWith("p1"));
    await waitFor(() => expect(p.onChanged).toHaveBeenCalledTimes(1));
    fireEvent.click(btn(rowOf("Texto p1"), "Cancelar esta")!);
    await waitFor(() => expect(S.Cancel).toHaveBeenCalledWith("p1"));
    fireEvent.click(btn(rowOf("Texto p1"), "Cancelar serie")!);
    await waitFor(() => expect(S.CancelSeries).toHaveBeenCalledWith("s1"));
    fireEvent.click(btn(rowOf("Texto p2"), "Eliminar")!);
    await waitFor(() => expect(S.Delete).toHaveBeenCalledWith("p2"));
    fireEvent.click(btn(rowOf("Texto sent1"), "Eliminar serie")!);
    await waitFor(() => expect(S.DeleteSeries).toHaveBeenCalledWith("s1"));
    await waitFor(() => expect(p.onChanged).toHaveBeenCalledTimes(5));
  });

  it("sin confirmar no se cancela ni se borra", async () => {
    useUI.setState({ confirm: vi.fn().mockResolvedValue(false) });
    setup();
    fireEvent.click(btn(rowOf("Texto p1"), "Cancelar serie")!);
    fireEvent.click(btn(rowOf("Texto p2"), "Eliminar")!);
    await Promise.resolve();
    await Promise.resolve();
    expect(S.CancelSeries).not.toHaveBeenCalled();
    expect(S.Delete).not.toHaveBeenCalled();
  });

  it("vacío, «Programar» y cerrar", () => {
    const p = setup([]);
    expect(screen.getByText("Nada programado para este chat.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Programar" }));
    expect(p.onNew).toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Cerrar" }));
    expect(p.onClose).toHaveBeenCalled();
  });

  it("Escape lo cierra y el foco entra al abrir", () => {
    const p = setup();
    expect(screen.getByRole("dialog", { name: "Programados de este chat" })).toHaveFocus();
    fireEvent.keyDown(window, { key: "Escape" });
    expect(p.onClose).toHaveBeenCalledTimes(1);
  });
});
