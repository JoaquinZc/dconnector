import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const svc = vi.hoisted(() => ({
  AccountsService: {},
  InboxService: {},
  MessagingService: {},
  StatusesService: {},
  ContactsService: {},
  CampaignsService: {},
  ScheduledService: {},
  StatsService: { Conversations: vi.fn(), Groups: vi.fn(), Chat: vi.fn() },
  DestinationsService: {},
  SettingsService: {},
  FilesService: {},
}));

vi.mock("@/api/transport", async () => (await import("@/test/svcTransport")).svcTransport(svc));
vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(() => () => undefined) } }));

import type { AccountView } from "@/api/types";
import StatsPage from "@/pages/StatsPage";
import { useAccounts } from "@/store/accounts";
import { useUI } from "@/store/ui";
import PeriodSelector from "./PeriodSelector";
import { Heatmap, Kpi } from "./StatsParts";
import { addDays, defaultPeriod, localDay } from "./periodModel";
import { normalizeHeatmap } from "./statsShape";

const T = svc.StatsService;

const view = (id: string, label: string, color: string): AccountView =>
  ({
    account: { id, channel: "whatsapp", label, color, enabled: true, selfId: "", selfName: "", selfHandle: "", createdAt: 1 },
    status: { accountId: id, state: "connected" },
    capabilities: {},
    unread: 0,
  }) as unknown as AccountView;

const summary = (p: Record<string, unknown> = {}) => ({
  sent: 40,
  received: 60,
  chats: 8,
  startedByMe: 5,
  startedByThem: 7,
  response: { mineMedianSec: 900, mineCount: 12, mineUnderHour: 0.75, theirsMedianSec: 7200, theirsCount: 9 },
  mix: { text: 90, image: 6, video: 1, voice: 3, document: 0, sticker: 0, other: 0 },
  ...p,
});

const overview = (p: Record<string, unknown> = {}) => ({
  period: { fromMs: 1, toMs: 2, prevFromMs: 0, prevToMs: 1, days: 30, granularity: "day" },
  current: summary(),
  previous: summary({ sent: 20, received: 60, chats: 8 }),
  changes: { sent: 1, received: 0, chats: 0, startedByMe: 0.25, startedByThem: null, mineMedianSec: -0.5, theirsMedianSec: 0.2 },
  groupSent: 3,
  groupReceived: 9,
  perDay: [{ day: "2026-11-01", sent: 3, received: 4 }],
  heatmap: Array.from({ length: 7 }, (_, d) => Array.from({ length: 24 }, (_, h) => (d === 2 && h === 9 ? 12 : 0))),
  topChats: [
    { chatKey: "wa-1|593@s.whatsapp.net", accountId: "wa-1", channel: "whatsapp", nativeId: "593@s.whatsapp.net", kind: "direct", title: "Ana López", avatarUrl: "", total: 30, sent: 10, received: 20, activeDays: 5, lastAt: 1 },
  ],
  frequent: [],
  ...p,
});

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  useUI.setState({ toasts: [], openChat: vi.fn() });
  useAccounts.setState({ views: { "wa-1": view("wa-1", "Ventas", "#25D366"), "wa-2": view("wa-2", "Soporte", "#8B5CF6") }, order: ["wa-1", "wa-2"], loaded: true });
  T.Conversations.mockResolvedValue(overview());
  T.Groups.mockResolvedValue({
    period: { days: 30, granularity: "day" },
    groupSent: 5,
    groupReceived: 15,
    groups: [{ chatKey: "wa-1|g1@g.us", accountId: "wa-1", channel: "whatsapp", nativeId: "g1@g.us", title: "Vecinos", avatarUrl: "", total: 20, mine: 5, authors: 6, activeDays: 4, lastAt: 1 }],
    people: [{ accountId: "wa-1", channel: "whatsapp", senderId: "p1", name: "Luis Mora", avatarUrl: "", total: 9, groups: 2, lastAt: 0 }],
  });
});
afterEach(cleanup);

describe("Heatmap", () => {
  it("pinta 7 filas × 24 celdas y marca el pico", () => {
    const grid = normalizeHeatmap(overview().heatmap);
    render(<Heatmap grid={grid} />);
    expect(screen.getAllByTestId("heat-cell")).toHaveLength(7 * 24);
    expect(screen.getByTestId("heat-peak")).toHaveTextContent("Más actividad: Mié de 9:00 a 10:00");
    expect(screen.getByTitle("Mié 9:00 · 12 mensajes")).toBeInTheDocument();
    expect(screen.getByTitle("Lun 0:00 · 0 mensajes")).toBeInTheDocument();
  });

  it("sin mensajes no hay pico", () => {
    render(<Heatmap grid={normalizeHeatmap(null)} />);
    expect(screen.getAllByTestId("heat-cell")).toHaveLength(168);
    expect(screen.queryByTestId("heat-peak")).toBeNull();
  });
});

describe("Kpi", () => {
  it("muestra la variación con su tono y nada si no hay base", () => {
    render(
      <>
        <Kpi label="Enviados" value="40" change={1} />
        <Kpi label="Tu tiempo de respuesta" value="15 min" change={-0.5} lowerIsBetter />
        <Kpi label="Chats" value="8" change={null} />
      </>,
    );
    expect(screen.getByTestId("change-Enviados")).toHaveTextContent("+100 %");
    expect(screen.getByTestId("change-Enviados")).toHaveAttribute("data-tone", "good");
    expect(screen.getByTestId("change-Tu tiempo de respuesta")).toHaveTextContent("−50 %");
    expect(screen.getByTestId("change-Tu tiempo de respuesta")).toHaveAttribute("data-tone", "good"); // bajar el tiempo es bueno
    expect(screen.queryByTestId("change-Chats")).toBeNull();
  });
});

describe("PeriodSelector", () => {
  it("«Personalizado» muestra dos fechas y avisa de un rango al revés", () => {
    const onChange = vi.fn();
    const p = { ...defaultPeriod(), preset: "custom" as const, from: "2026-11-05", to: "2026-11-01" };
    render(<PeriodSelector value={p} onChange={onChange} />);
    expect(screen.getByRole("alert")).toHaveTextContent("posterior");
    fireEvent.change(screen.getByLabelText("Desde"), { target: { value: "2026-10-30" } });
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ from: "2026-10-30" }));
    fireEvent.click(screen.getByRole("radio", { name: "7 días" }));
    expect(onChange).toHaveBeenLastCalledWith(expect.objectContaining({ preset: 7 }));
  });
});

describe("StatsPage", () => {
  const lastRange = () => T.Conversations.mock.calls[T.Conversations.mock.calls.length - 1];

  it("Conversaciones: pide 30 días a todas las cuentas y pinta KPIs con su comparación", async () => {
    render(<StatsPage />);
    await screen.findByTestId("conversations");
    expect(T.Conversations).toHaveBeenCalledWith(null, { days: 30, from: "", to: "" });
    expect(screen.getByTestId("kpi-Enviados")).toHaveTextContent("40");
    expect(screen.getByTestId("change-Enviados")).toHaveTextContent("+100 %");
    expect(screen.getByTestId("kpi-Tu tiempo de respuesta")).toHaveTextContent("15 min");
    expect(screen.getByTestId("change-Tu tiempo de respuesta")).toHaveAttribute("data-tone", "good");
    expect(screen.getByTestId("kpi-Te responden en")).toHaveTextContent("2 h");
    expect(screen.getByTestId("kpi-Respondes en menos de 1 h")).toHaveTextContent("75 %");
    expect(screen.queryByTestId("change-Conversaciones que inician")).toBeNull(); // null = sin base
    expect(screen.getByText("Incluye grupos: 3 enviados · 9 recibidos")).toBeInTheDocument();
    expect(screen.getAllByTestId("heat-cell")).toHaveLength(168);
  });

  it("el ranking abre el chat con su clave", async () => {
    render(<StatsPage />);
    fireEvent.click(await screen.findByRole("button", { name: /Ana López/ }));
    expect(useUI.getState().openChat).toHaveBeenCalledWith("wa-1|593@s.whatsapp.net");
  });

  it("cambiar el periodo vuelve a pedir: 7 días y personalizado (to = último día + 1)", async () => {
    render(<StatsPage />);
    await screen.findByTestId("conversations");

    fireEvent.click(screen.getByRole("radio", { name: "7 días" }));
    await waitFor(() => expect(lastRange()[1]).toEqual({ days: 7, from: "", to: "" }));

    fireEvent.click(screen.getByRole("radio", { name: "Personalizado" }));
    const today = localDay(new Date());
    // por defecto: dos semanas hasta hoy
    await waitFor(() => expect(lastRange()[1]).toEqual({ days: 0, from: addDays(today, -13), to: addDays(today, 1) }));

    fireEvent.change(screen.getByLabelText("Desde"), { target: { value: "2026-10-01" } });
    fireEvent.change(screen.getByLabelText("Hasta"), { target: { value: "2026-10-31" } });
    await waitFor(() => expect(lastRange()[1]).toEqual({ days: 0, from: "2026-10-01", to: "2026-11-01" }));
  });

  it("un rango personalizado inválido no pide nada", async () => {
    render(<StatsPage />);
    await screen.findByTestId("conversations");
    fireEvent.click(screen.getByRole("radio", { name: "Personalizado" }));
    await waitFor(() => expect(T.Conversations).toHaveBeenCalledTimes(2));
    // «Hasta» primero: así el rango queda al revés desde el primer cambio.
    fireEvent.change(screen.getByLabelText("Hasta"), { target: { value: "2026-10-01" } });
    fireEvent.change(screen.getByLabelText("Desde"), { target: { value: "2026-10-31" } });
    expect(await screen.findByRole("alert")).toBeInTheDocument();
    await new Promise((r) => setTimeout(r, 30));
    expect(T.Conversations).toHaveBeenCalledTimes(2);
  });

  it("el selector de cuentas filtra y se recuerda", async () => {
    render(<StatsPage />);
    await screen.findByTestId("conversations");
    const group = screen.getByRole("group", { name: "Cuentas" });
    fireEvent.click(within(group).getByRole("button", { name: "Soporte" }));
    await waitFor(() => expect(lastRange()[0]).toEqual(["wa-2"]));
    expect(localStorage.getItem("dc:stats:accounts")).toBe(JSON.stringify(["wa-2"]));
    fireEvent.click(within(group).getByRole("button", { name: "Todas" }));
    await waitFor(() => expect(lastRange()[0]).toBeNull());
  });

  it("sin mensajes: vacío corto", async () => {
    T.Conversations.mockResolvedValue({});
    render(<StatsPage />);
    expect(await screen.findByText("Sin mensajes en este periodo")).toBeInTheDocument();
  });

  it("Grupos: más activos y personas más activas", async () => {
    render(<StatsPage />);
    await screen.findByTestId("conversations");
    fireEvent.click(screen.getByRole("tab", { name: "Grupos" }));
    await screen.findByTestId("groups");
    expect(T.Groups).toHaveBeenCalledWith(null, { days: 30, from: "", to: "" });
    expect(screen.getByTestId("kpi-Mensajes en grupos")).toHaveTextContent("20");
    expect(screen.getByText("Vecinos")).toBeInTheDocument();
    expect(screen.getByText("Luis Mora")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Vecinos/ }));
    expect(useUI.getState().openChat).toHaveBeenCalledWith("wa-1|g1@g.us");
  });

  it("Grupos sin actividad: vacío corto", async () => {
    T.Groups.mockResolvedValue({});
    render(<StatsPage />);
    fireEvent.click(screen.getByRole("tab", { name: "Grupos" }));
    expect(await screen.findByText("Sin actividad en grupos")).toBeInTheDocument();
  });
});
