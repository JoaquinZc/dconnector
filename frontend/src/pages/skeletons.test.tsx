// Primera visita de cada pestaña: el marco se pinta al instante y los datos esperan con esqueletos. El estado
// vacío solo aparece cuando la primera carga terminó de verdad; si falla, hay error y no un esqueleto eterno.
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

const svc = vi.hoisted(() => ({
  AccountsService: { List: vi.fn() },
  InboxService: { ListChats: vi.fn(), UnreadTotals: vi.fn(), Messages: vi.fn(), GetChat: vi.fn() },
  MessagingService: { MarkRead: vi.fn() },
  StatusesService: { Authors: vi.fn(), Mine: vi.fn(), Stats: vi.fn() },
  ContactsService: { List: vi.fn(), Lists: vi.fn() },
  CampaignsService: { List: vi.fn() },
  ScheduledService: { List: vi.fn() },
  StatsService: { Conversations: vi.fn(), Groups: vi.fn() },
  DestinationsService: { List: vi.fn(), States: vi.fn() },
  SettingsService: { Get: vi.fn(), Version: vi.fn(), DataDir: vi.fn() },
  FilesService: {},
}));

vi.mock("@/api/transport", async () => (await import("@/test/svcTransport")).svcTransport(svc));
vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(() => () => undefined) } }));

import type { AccountView, Campaign, Contact, Destination } from "@/api/types";
import { chat } from "@/components/chat/testkit";
import AccountsPage from "@/pages/AccountsPage";
import CampaignsPage from "@/pages/CampaignsPage";
import ChatsPage from "@/pages/ChatsPage";
import ContactsPage from "@/pages/ContactsPage";
import DestinationsPage from "@/pages/DestinationsPage";
import ScheduledPage from "@/pages/ScheduledPage";
import SettingsPage from "@/pages/SettingsPage";
import StatsPage from "@/pages/StatsPage";
import StatusesPage from "@/pages/StatusesPage";
import { useAccounts } from "@/store/accounts";
import { DEFAULT_FILTER, useInbox } from "@/store/inbox";
import { useStatuses } from "@/store/statuses";
import { useUI } from "@/store/ui";

function deferred<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

const view = (id: string, label: string): AccountView =>
  ({
    account: { id, channel: "whatsapp", label, color: "#25D366", enabled: true, selfId: "", selfName: "Yo", selfHandle: "+593990000000", config: null, createdAt: 1 },
    status: { accountId: id, state: "connected", need: "", since: 1 },
    capabilities: {},
    unread: 0,
  }) as unknown as AccountView;

const camp = (id: string): Campaign =>
  ({
    id,
    name: `Campaña ${id}`,
    accountId: "wa-1",
    state: "running",
    text: "Hola",
    variants: null,
    filePath: "",
    listIds: ["l1"],
    contactIds: null,
    startAt: 0,
    window: { fromMinute: 540, toMinute: 1080, weekdays: [1, 2, 3, 4, 5] },
    pacing: {},
    optOutFooter: true,
    abEnabled: false,
    brakeReason: "",
    createdAt: 1,
    updatedAt: 2,
    totals: { queued: 6, sent: 3, failed: 1, skipped: 0, replied: 2, optOut: 1 },
  }) as unknown as Campaign;

const dest = (id: string): Destination =>
  ({
    id,
    name: `Destino ${id}`,
    url: "wss://bot.example.com/ws",
    token: "t",
    enabled: true,
    allowCommands: false,
    filter: { accountIds: [], channels: [], events: [], includeFromMe: false, includeGroups: false, contactScope: { mode: "all", includeListIds: [], excludeListIds: [] } },
    createdAt: 1,
  }) as unknown as Destination;

const contact = (id: string): Contact =>
  ({ id, name: `Contacto ${id}`, phone: "+593999000111", notes: "", tags: [], identities: [], listIds: [], optedOut: false, source: "manual", createdAt: 1, updatedAt: 1 }) as unknown as Contact;

const summary = {
  sent: 40,
  received: 60,
  chats: 8,
  startedByMe: 5,
  startedByThem: 7,
  response: { mineMedianSec: 900, mineCount: 12, mineUnderHour: 0.75, theirsMedianSec: 7200, theirsCount: 9 },
  mix: { text: 90, image: 6, video: 1, voice: 3, document: 0, sticker: 0, other: 0 },
};
const overview = {
  period: { fromMs: 1, toMs: 2, prevFromMs: 0, prevToMs: 1, days: 30, granularity: "day" },
  current: summary,
  previous: summary,
  changes: {},
  groupSent: 3,
  groupReceived: 9,
  perDay: [{ day: "2026-11-01", sent: 3, received: 4 }],
  heatmap: Array.from({ length: 7 }, () => Array.from({ length: 24 }, () => 0)),
  topChats: [],
  frequent: [],
};

const skeleton = () => screen.queryAllByRole("status", { name: "Cargando" });
const errorToast = () => useUI.getState().toasts.some((t) => t.kind === "error");

// jsdom no tiene diseño: sin esto las listas virtualizadas creen que el área visible mide 0 y no pintan filas.
beforeAll(() => {
  for (const [prop, v] of [["clientHeight", 600], ["clientWidth", 900], ["offsetHeight", 600], ["offsetWidth", 900]] as const) {
    Object.defineProperty(HTMLElement.prototype, prop, { configurable: true, get: () => v });
  }
});

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  useUI.setState({ toasts: [] });
  useAccounts.setState({ views: { "wa-1": view("wa-1", "Ventas") }, order: ["wa-1"], loaded: true });
  useInbox.setState({ chats: {}, order: [], filter: DEFAULT_FILTER, loading: false, loaded: false, hasMore: false });
  useStatuses.setState({ authors: [], authorsLoaded: false, statuses: {} });
  svc.ContactsService.Lists.mockResolvedValue([]);
  svc.InboxService.UnreadTotals.mockResolvedValue({});
  svc.DestinationsService.States.mockResolvedValue([]);
  svc.MessagingService.MarkRead.mockResolvedValue(undefined);
});
afterEach(cleanup);

describe("Chats", () => {
  const startLoad = () => act(() => void useInbox.getState().loadFirst());

  it("primera carga: marco completo y esqueletos, sin «Sin chats» ni «Aún no hay conversaciones»", async () => {
    const d = deferred<unknown[]>();
    svc.InboxService.ListChats.mockReturnValue(d.promise);
    render(<ChatsPage />);
    await startLoad();
    expect(screen.getByRole("heading", { name: "Chats" })).toBeInTheDocument();
    expect(screen.getByLabelText("Buscar chat")).toBeInTheDocument();
    expect(screen.getByRole("tablist", { name: "Vista de chats" })).toBeInTheDocument();
    expect(skeleton().length).toBeGreaterThan(0);
    expect(screen.queryByText("Sin chats")).toBeNull();
    expect(screen.queryByText("Aún no hay conversaciones")).toBeNull();
    expect(screen.queryByText("Cargando…")).toBeNull();
  });

  it("con datos: se ven los chats y desaparecen los esqueletos", async () => {
    const d = deferred<unknown[]>();
    svc.InboxService.ListChats.mockReturnValue(d.promise);
    render(<ChatsPage />);
    await startLoad();
    await act(async () => d.resolve([chat({ title: "Ana Pérez" })]));
    await screen.findByText("Ana Pérez");
    expect(skeleton()).toHaveLength(0);
  });

  it("con lista vacía: estado vacío", async () => {
    const d = deferred<unknown[]>();
    svc.InboxService.ListChats.mockReturnValue(d.promise);
    render(<ChatsPage />);
    await startLoad();
    await act(async () => d.resolve([]));
    expect(await screen.findByText("Aún no hay conversaciones")).toBeInTheDocument();
    expect(skeleton()).toHaveLength(0);
  });

  it("si falla: aviso de error y no un esqueleto eterno", async () => {
    const d = deferred<unknown[]>();
    svc.InboxService.ListChats.mockReturnValue(d.promise);
    render(<ChatsPage />);
    await startLoad();
    await act(async () => d.reject(new Error("sin servidor")));
    await waitFor(() => expect(skeleton()).toHaveLength(0));
    expect(errorToast()).toBe(true);
  });

  it("una recarga posterior conserva los chats ya mostrados (sin esqueletos)", async () => {
    svc.InboxService.ListChats.mockResolvedValueOnce([chat({ title: "Ana Pérez" })]);
    render(<ChatsPage />);
    await startLoad();
    await screen.findByText("Ana Pérez");
    const d = deferred<unknown[]>();
    svc.InboxService.ListChats.mockReturnValue(d.promise);
    await startLoad();
    expect(screen.getByText("Ana Pérez")).toBeInTheDocument();
    expect(skeleton()).toHaveLength(0);
  });
});

describe("Contactos", () => {
  it("primera carga: marco, cabecera de la tabla y esqueletos, sin «Sin contactos»", async () => {
    svc.ContactsService.List.mockReturnValue(deferred().promise);
    render(<ContactsPage />);
    expect(screen.getByRole("heading", { name: "Contactos" })).toBeInTheDocument();
    expect(screen.getByLabelText("Buscar contactos")).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: "Nombre" })).toBeInTheDocument();
    expect(skeleton().length).toBeGreaterThan(0);
    expect(screen.queryByText("Sin contactos")).toBeNull();
    expect(screen.queryByText("Cargando…")).toBeNull();
  });

  it("con datos se ven; vacía, estado vacío; si falla, error con «Reintentar»", async () => {
    const d = deferred<unknown>();
    svc.ContactsService.List.mockReturnValue(d.promise);
    const { unmount } = render(<ContactsPage />);
    await act(async () => d.resolve({ contacts: [contact("c1")], total: 1 }));
    await screen.findByText("Contacto c1");
    expect(skeleton()).toHaveLength(0);
    unmount();

    svc.ContactsService.List.mockResolvedValue({ contacts: [], total: 0 });
    const second = render(<ContactsPage />);
    expect(await screen.findByText("Sin contactos")).toBeInTheDocument();
    expect(skeleton()).toHaveLength(0);
    second.unmount();

    svc.ContactsService.List.mockRejectedValue(new Error("sin servidor"));
    render(<ContactsPage />);
    expect(await screen.findByText("No se pudieron cargar los contactos")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Reintentar" })).toBeInTheDocument();
    expect(screen.queryByText("Sin contactos")).toBeNull();
    expect(skeleton()).toHaveLength(0);
  });
});

describe("Cuentas", () => {
  const fresh = () => useAccounts.setState({ views: {}, order: [], loaded: false });

  it("primera carga: cabecera y esqueletos, sin «Sin cuentas»", async () => {
    fresh();
    svc.AccountsService.List.mockReturnValue(deferred().promise);
    render(<AccountsPage />);
    act(() => void useAccounts.getState().load());
    expect(screen.getByRole("heading", { name: "Cuentas" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Añadir cuenta/ })).toBeInTheDocument();
    expect(skeleton().length).toBeGreaterThan(0);
    expect(screen.queryByText("Sin cuentas")).toBeNull();
  });

  it("con datos, vacía y con error", async () => {
    fresh();
    const d = deferred<unknown[]>();
    svc.AccountsService.List.mockReturnValue(d.promise);
    const { unmount } = render(<AccountsPage />);
    act(() => void useAccounts.getState().load());
    await act(async () => d.resolve([view("wa-1", "Ventas")]));
    await screen.findByText("Ventas");
    expect(skeleton()).toHaveLength(0);
    unmount();

    fresh();
    svc.AccountsService.List.mockResolvedValue([]);
    const second = render(<AccountsPage />);
    await act(async () => void (await useAccounts.getState().load()));
    expect(await screen.findByText("Sin cuentas")).toBeInTheDocument();
    second.unmount();

    fresh();
    svc.AccountsService.List.mockRejectedValue(new Error("sin servidor"));
    render(<AccountsPage />);
    await act(async () => void (await useAccounts.getState().load()));
    await waitFor(() => expect(skeleton()).toHaveLength(0));
    expect(errorToast()).toBe(true);
  });
});

describe("Campañas", () => {
  it("primera carga: cabecera y esqueletos, sin «Sin campañas»", () => {
    svc.CampaignsService.List.mockReturnValue(deferred().promise);
    render(<CampaignsPage />);
    expect(screen.getByRole("heading", { name: "Campañas" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Nueva campaña/ })).toBeInTheDocument();
    expect(skeleton().length).toBeGreaterThan(0);
    expect(screen.queryByText("Sin campañas")).toBeNull();
  });

  it("con datos, vacía y con error", async () => {
    svc.CampaignsService.List.mockResolvedValue([camp("a")]);
    const first = render(<CampaignsPage />);
    await screen.findByText("Campaña a");
    expect(skeleton()).toHaveLength(0);
    first.unmount();

    svc.CampaignsService.List.mockResolvedValue([]);
    const second = render(<CampaignsPage />);
    expect(await screen.findByText("Sin campañas")).toBeInTheDocument();
    second.unmount();

    svc.CampaignsService.List.mockRejectedValue(new Error("sin servidor"));
    render(<CampaignsPage />);
    expect(await screen.findByText("No se pudieron cargar las campañas")).toBeInTheDocument();
    expect(skeleton()).toHaveLength(0);
  });
});

describe("Estadísticas", () => {
  it("primera carga: cabecera, pestañas y periodo ya pintados, con esqueleto en el contenido", () => {
    svc.StatsService.Conversations.mockReturnValue(deferred().promise);
    render(<StatsPage />);
    expect(screen.getByRole("heading", { name: "Estadísticas" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Conversaciones" })).toBeInTheDocument();
    expect(skeleton().length).toBeGreaterThan(0);
    expect(screen.queryByText("Sin mensajes en este periodo")).toBeNull();
  });

  it("con datos, vacía y con error", async () => {
    svc.StatsService.Conversations.mockResolvedValue(overview);
    const first = render(<StatsPage />);
    await screen.findByTestId("conversations");
    expect(skeleton()).toHaveLength(0);
    first.unmount();

    svc.StatsService.Conversations.mockResolvedValue({});
    const second = render(<StatsPage />);
    expect(await screen.findByText("Sin mensajes en este periodo")).toBeInTheDocument();
    second.unmount();

    svc.StatsService.Conversations.mockRejectedValue(new Error("sin servidor"));
    render(<StatsPage />);
    expect(await screen.findByText("No se pudieron cargar las estadísticas de conversaciones")).toBeInTheDocument();
    expect(skeleton()).toHaveLength(0);
  });
});

describe("Destinos", () => {
  it("primera carga: cabecera y esqueletos, sin «Sin destinos»", () => {
    svc.DestinationsService.List.mockReturnValue(deferred().promise);
    render(<DestinationsPage />);
    expect(screen.getByRole("heading", { name: "Destinos" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Nuevo destino/ })).toBeInTheDocument();
    expect(skeleton().length).toBeGreaterThan(0);
    expect(screen.queryByText("Sin destinos")).toBeNull();
  });

  it("con datos, vacía y con error", async () => {
    svc.DestinationsService.List.mockResolvedValue([dest("d1")]);
    const first = render(<DestinationsPage />);
    await screen.findByText("Destino d1");
    expect(skeleton()).toHaveLength(0);
    first.unmount();

    svc.DestinationsService.List.mockResolvedValue([]);
    const second = render(<DestinationsPage />);
    expect(await screen.findByText("Sin destinos")).toBeInTheDocument();
    second.unmount();

    svc.DestinationsService.List.mockRejectedValue(new Error("sin servidor"));
    render(<DestinationsPage />);
    expect(await screen.findByText("No se pudieron cargar los destinos")).toBeInTheDocument();
    expect(skeleton()).toHaveLength(0);
  });
});

describe("Programados", () => {
  it("primera carga con esqueletos; vacía y con error", async () => {
    svc.ScheduledService.List.mockReturnValue(deferred().promise);
    const first = render(<ScheduledPage />);
    expect(screen.getByRole("heading", { name: "Programados" })).toBeInTheDocument();
    expect(skeleton().length).toBeGreaterThan(0);
    expect(screen.queryByText("Sin programados")).toBeNull();
    first.unmount();

    svc.ScheduledService.List.mockResolvedValue([]);
    const second = render(<ScheduledPage />);
    expect(await screen.findByText("Sin programados")).toBeInTheDocument();
    second.unmount();

    svc.ScheduledService.List.mockRejectedValue(new Error("sin servidor"));
    render(<ScheduledPage />);
    expect(await screen.findByText("No se pudieron cargar los programados")).toBeInTheDocument();
  });
});

describe("Ajustes", () => {
  it("primera carga: cabecera y secciones ya pintadas, con esqueleto; si falla, error", async () => {
    svc.SettingsService.Get.mockReturnValue(deferred().promise);
    svc.SettingsService.Version.mockResolvedValue("0.3.0");
    svc.SettingsService.DataDir.mockResolvedValue("C:/datos");
    const first = render(<SettingsPage />);
    expect(screen.getByRole("heading", { name: "Ajustes" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Notificaciones" })).toBeInTheDocument();
    expect(skeleton().length).toBeGreaterThan(0);
    first.unmount();

    svc.SettingsService.Get.mockRejectedValue(new Error("sin servidor"));
    render(<SettingsPage />);
    expect(await screen.findByText("No se pudieron cargar los ajustes")).toBeInTheDocument();
    expect(skeleton()).toHaveLength(0);
  });
});

describe("Estados", () => {
  it("primera carga: marco y esqueletos en vez de «Sin estados»; vacío al terminar", async () => {
    useAccounts.setState({
      views: { "wa-1": { ...view("wa-1", "Ventas"), capabilities: { statuses: true, postStatus: true } } as unknown as AccountView },
      order: ["wa-1"],
      loaded: true,
    });
    const authors = deferred<unknown[]>();
    svc.StatusesService.Authors.mockReturnValue(authors.promise);
    svc.StatusesService.Mine.mockResolvedValue([]);
    render(<StatusesPage />);
    expect(screen.getByRole("tab", { name: "Mis estados" })).toBeInTheDocument();
    expect(skeleton().length).toBeGreaterThan(0);
    expect(screen.queryByText("Sin estados")).toBeNull();
    expect(screen.queryByText("Sin estados por ver")).toBeNull();
    await act(async () => authors.resolve([]));
    expect(await screen.findByText("Sin estados por ver")).toBeInTheDocument();
    expect(screen.getByText("Sin estados")).toBeInTheDocument();
  });
});
