import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const hoisted = vi.hoisted(() => ({
  svc: {
    AccountsService: { List: vi.fn() },
    InboxService: { ListChats: vi.fn() },
    MessagingService: {},
    StatusesService: {},
    ContactsService: {},
    CampaignsService: {},
    ScheduledService: {
      List: vi.fn(),
      Save: vi.fn(),
      Update: vi.fn(),
      UpdateSeries: vi.fn(),
      Cancel: vi.fn(),
      CancelSeries: vi.fn(),
      Delete: vi.fn(),
      SendNow: vi.fn(),
      PreviewRecurrence: vi.fn(),
    },
    StatsService: {},
    DestinationsService: {},
    SettingsService: {},
    FilesService: { PickFiles: vi.fn() },
  },
}));
const { svc } = hoisted;
const S = svc.ScheduledService;

vi.mock("@/api/transport", async () => (await import("@/test/svcTransport")).svcTransport(hoisted.svc));
vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(() => () => undefined) } }));

import type { AccountView, CapSet, Chat, Scheduled } from "@/api/types";
import ScheduledPage from "@/pages/ScheduledPage";
import { useAccounts } from "@/store/accounts";
import { useUI } from "@/store/ui";

const HOUR = 3_600_000;
const DAY = 24 * HOUR;
const TIMEOUT = { timeout: 3000 };

const RULE = { frequency: "daily", every: 1, endAt: 0, maxCount: 0, count: 0, firstAt: 0 };

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
    recurrence: null,
    seriesId: "",
    ...over,
  }) as unknown as Scheduled;

const chat = (nativeId: string, title: string): Chat =>
  ({ key: `wa-1|${nativeId}`, accountId: "wa-1", channel: "whatsapp", nativeId, title, avatarUrl: "", lastPreview: "", readOnly: false }) as unknown as Chat;

const CAPS = { sendText: true, sendImage: true, sendVideo: true, sendAudio: true, sendDocument: true, maxTextLen: 0, maxFileSize: 0, maxImageSize: 0, maxVideoSize: 0 } as unknown as CapSet;

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  const now = Date.now();
  S.List.mockResolvedValue([
    sch("a1", "pending", now + 2 * HOUR, { seriesId: "s1", recurrence: { ...RULE, firstAt: now + 2 * HOUR } }),
    sch("a0", "sent", now - 22 * HOUR, { seriesId: "s1", recurrence: { ...RULE, firstAt: now - 22 * HOUR } }),
    sch("u1", "pending", now + 5 * HOUR),
  ]);
  S.Save.mockImplementation(async (s: Scheduled) => ({ ...s, id: s.id || "nuevo" }));
  S.UpdateSeries.mockImplementation(async (_id: string, s: Scheduled) => s);
  for (const f of [S.Cancel, S.CancelSeries, S.Delete, S.SendNow]) f.mockResolvedValue(undefined);
  S.PreviewRecurrence.mockImplementation(async (_r: unknown, first: number, n: number) => Array.from({ length: n }, (_, i) => first + i * DAY));
  svc.InboxService.ListChats.mockResolvedValue([chat("c-ana", "Ana Pérez")]);
  svc.FilesService.PickFiles.mockResolvedValue([]);
  svc.AccountsService.List.mockResolvedValue([]);
  useUI.setState({ toasts: [], confirmReq: null, confirm: vi.fn().mockResolvedValue(true) });
  useAccounts.setState({
    views: {
      "wa-1": { account: { id: "wa-1", channel: "whatsapp", label: "Ventas", color: "#25D366", enabled: true }, status: {}, capabilities: CAPS, unread: 0 } as unknown as AccountView,
    },
    order: ["wa-1"],
    loaded: true,
  });
});
afterEach(cleanup);

const rows = () => screen.getAllByTestId("scheduled-row");
const rowOf = (text: string) => rows().find((r) => within(r).queryByText(text))!;

describe("Programados: series", () => {
  it("la serie muestra «Se repite: …» y sus ocurrencias van marcadas; el suelto no", async () => {
    render(<ScheduledPage />);
    await screen.findAllByTestId("scheduled-row");
    const a1 = rowOf("Texto a1");
    expect(within(a1).getByTestId("recurrence-chip")).toHaveTextContent(/^Se repite: cada día, a (las|la) \d{2}:\d{2}$/);
    expect(a1.getAttribute("data-series")).toBe("s1");
    // la ocurrencia ya enviada de la misma serie también está marcada
    expect(rowOf("Texto a0").getAttribute("data-series")).toBe("s1");
    expect(within(rowOf("Texto u1")).queryByTestId("recurrence-chip")).toBeNull();
  });

  it("«Cancelar serie» solo en la pendiente de una serie y llama a CancelSeries", async () => {
    render(<ScheduledPage />);
    await screen.findAllByTestId("scheduled-row");
    expect(within(rowOf("Texto u1")).queryByRole("button", { name: "Cancelar serie" })).toBeNull();
    expect(within(rowOf("Texto a0")).queryByRole("button", { name: "Cancelar serie" })).toBeNull();
    fireEvent.click(within(rowOf("Texto a1")).getByRole("button", { name: "Cancelar serie" }));
    await waitFor(() => expect(S.CancelSeries).toHaveBeenCalledWith("s1"));
  });

  it("editar una serie usa UpdateSeries", async () => {
    render(<ScheduledPage />);
    await screen.findAllByTestId("scheduled-row");
    fireEvent.click(within(rowOf("Texto a1")).getByRole("button", { name: "Editar" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByLabelText("Repetir")).toHaveValue("daily");
    fireEvent.change(within(dialog).getByLabelText("Mensaje"), { target: { value: "Cambiado" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(S.UpdateSeries).toHaveBeenCalledTimes(1));
    expect(S.UpdateSeries.mock.calls[0][0]).toBe("s1");
    expect(S.UpdateSeries.mock.calls[0][1]).toMatchObject({ id: "a1", seriesId: "s1", message: { text: "Cambiado" }, recurrence: { frequency: "daily" } });
    expect(S.Save).not.toHaveBeenCalled();
  });
});

describe("«Nuevo» con repetición", () => {
  it("el formulario reutiliza el editor y guarda la regla", async () => {
    render(<ScheduledPage />);
    await screen.findAllByTestId("scheduled-row");
    fireEvent.click(screen.getByRole("button", { name: /Nuevo/ }));
    await screen.findByRole("dialog");
    fireEvent.click(await screen.findByRole("option", { name: /Ana Pérez/ }));
    fireEvent.change(screen.getByLabelText("Mensaje"), { target: { value: "Buenos días" } });
    fireEvent.change(screen.getByLabelText("Fecha y hora"), { target: { value: "2099-01-05T09:00" } });
    fireEvent.change(screen.getByLabelText("Repetir"), { target: { value: "weekly" } });
    fireEvent.click(screen.getByRole("button", { name: "jueves" }));
    expect(screen.getByTestId("recurrence-summary")).toHaveTextContent("Cada semana, lunes y jueves, a las 09:00");
    await screen.findByTestId("recurrence-preview", {}, TIMEOUT);
    fireEvent.click(screen.getByRole("button", { name: "Programar" }));
    await waitFor(() => expect(S.Save).toHaveBeenCalledTimes(1));
    expect(S.Save.mock.calls[0][0]).toMatchObject({
      id: "",
      chatNativeId: "c-ana",
      message: { kind: "text", text: "Buenos días" },
      sendAt: new Date(2099, 0, 5, 9, 0).getTime(),
      recurrence: { frequency: "weekly", every: 1, weekdays: [1, 4] },
    });
  });

  it("una repetición inválida no se guarda y lo dice", async () => {
    render(<ScheduledPage />);
    await screen.findAllByTestId("scheduled-row");
    fireEvent.click(screen.getByRole("button", { name: /Nuevo/ }));
    await screen.findByRole("dialog");
    fireEvent.click(await screen.findByRole("option", { name: /Ana Pérez/ }));
    fireEvent.change(screen.getByLabelText("Mensaje"), { target: { value: "Hola" } });
    fireEvent.change(screen.getByLabelText("Fecha y hora"), { target: { value: "2099-01-05T09:00" } });
    fireEvent.change(screen.getByLabelText("Repetir"), { target: { value: "custom" } });
    fireEvent.change(screen.getByLabelText("Unidad"), { target: { value: "minutes" } });
    fireEvent.change(screen.getByLabelText("Cada"), { target: { value: "2" } });
    fireEvent.click(screen.getByRole("button", { name: "Programar" }));
    expect(screen.getByRole("alert")).toHaveTextContent("La repetición mínima es cada 5 minutos");
    expect(S.Save).not.toHaveBeenCalled();
  });
});
