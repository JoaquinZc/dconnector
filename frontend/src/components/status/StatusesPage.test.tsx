import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const svc = vi.hoisted(() => ({
  AccountsService: { List: vi.fn() },
  InboxService: {},
  MessagingService: {},
  StatusesService: {
    Authors: vi.fn(),
    Mine: vi.fn(),
    ByAuthor: vi.fn(),
    MarkViewed: vi.fn(),
    React: vi.fn(),
    Stats: vi.fn(),
  },
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

import type { AccountView, Status, StatusAuthor } from "@/api/types";
import StatusesPage from "@/pages/StatusesPage";
import { useAccounts } from "@/store/accounts";
import { useStatuses } from "@/store/statuses";

const NOW = Date.now();

const account = (
  id: string,
  label: string,
  channel: string,
  caps: Record<string, boolean>,
  selfId = `yo@${id}`,
): AccountView =>
  ({
    account: { id, channel, label, color: channel === "telegram" ? "#229ED9" : "#25D366", enabled: true, selfId, selfName: "Yo" },
    status: { accountId: id, state: "connected" },
    capabilities: caps,
    unread: 0,
  }) as unknown as AccountView;

const WA_CAPS = { statuses: true, postStatus: true, statusViewers: true, statusReact: false };
const TG_CAPS = { statuses: true, postStatus: true, statusReact: true };

const author = (accountId: string, id: string, name: string, count: number, unviewed: number): StatusAuthor =>
  ({ accountId, authorId: id, authorName: name, avatarUrl: "", count, unviewed, lastAt: NOW }) as unknown as StatusAuthor;

const mineStatus = (accountId: string, n: number): Status =>
  ({
    key: `${accountId}|m${n}`,
    accountId,
    nativeId: `m${n}`,
    fromMe: true,
    postedAt: NOW - 60_000,
    expiresAt: NOW + 3_600_000,
    kind: "text",
    text: "Mío",
    bgColor: "#128C7E",
    font: 0,
    viewCount: n * 2,
    deletedAt: 0,
  }) as unknown as Status;

const theirs = (accountId: string, authorId: string, name: string, text: string): Status =>
  ({
    key: `${accountId}|s-${authorId}`,
    accountId,
    nativeId: `s-${authorId}`,
    authorNativeId: authorId,
    authorName: name,
    fromMe: false,
    postedAt: NOW - 1000,
    expiresAt: NOW + 1e6,
    kind: "text",
    text,
    bgColor: "#128C7E",
    font: 0,
    viewed: false,
    deletedAt: 0,
  }) as unknown as Status;

function seed(views: AccountView[]) {
  const map: Record<string, AccountView> = {};
  views.forEach((v) => (map[v.account.id] = v));
  act(() => useAccounts.setState({ views: map, order: views.map((v) => v.account.id), loaded: true }));
}

const waTg = () => [account("wa-1", "Ventas", "whatsapp", WA_CAPS), account("tg-1", "Canal Tg", "telegram", TG_CAPS)];

const list = () => within(screen.getByTestId("status-list"));
const chips = () => within(screen.getByRole("group", { name: "Filtrar por canal" }));

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  svc.StatusesService.Authors.mockResolvedValue([
    author("wa-1", "ana@s", "Ana", 3, 2),
    author("wa-1", "luis@s", "Luis", 1, 0),
    author("wa-1", "yo@wa-1", "Yo mismo", 1, 0),
    author("tg-1", "marta@t", "Marta", 1, 1),
    author("tg-1", "pedro@t", "Pedro", 2, 0),
  ]);
  svc.StatusesService.Mine.mockImplementation(async (id: string) => [mineStatus(id, 1), mineStatus(id, 2)]);
  svc.StatusesService.ByAuthor.mockImplementation(async (acc: string, authorId: string) => [
    theirs(acc, authorId, authorId.split("@")[0], `Hola desde ${authorId.split("@")[0]}`),
  ]);
  svc.StatusesService.MarkViewed.mockResolvedValue(undefined);
  svc.StatusesService.React.mockResolvedValue(undefined);
  svc.StatusesService.Stats.mockResolvedValue({});
  act(() => useStatuses.setState({ authors: [], statuses: {} }));
  seed(waTg());
});
afterEach(cleanup);

describe("StatusesPage · lista por canal y cuenta", () => {
  it("carga todas las cuentas con estados y agrupa por canal, cuenta, recientes y vistos", async () => {
    render(<StatusesPage />);
    await waitFor(() => expect(list().getByText("Ana")).toBeInTheDocument());
    expect(svc.StatusesService.Authors).toHaveBeenCalledWith(["wa-1", "tg-1"]);

    const wa = list().getByRole("region", { name: "WhatsApp" });
    const tg = list().getByRole("region", { name: "Telegram" });
    expect(within(wa).getByRole("button", { name: /WhatsApp/ })).toHaveAttribute("aria-expanded", "true");
    expect(within(wa).getByRole("group", { name: "Ventas" })).toBeInTheDocument();
    expect(within(tg).getByRole("group", { name: "Canal Tg" })).toBeInTheDocument();

    // píldora con el total de nuevos por canal
    expect(within(wa).getByLabelText("2 nuevos en WhatsApp")).toHaveTextContent("2");
    expect(within(tg).getByLabelText("1 nuevo en Telegram")).toHaveTextContent("1");

    expect(within(wa).getByText("Recientes")).toBeInTheDocument();
    expect(within(wa).getByText("Vistos")).toBeInTheDocument();
    expect(within(wa).getByText("Ana")).toBeInTheDocument();
    expect(within(wa).getByText("Luis")).toBeInTheDocument();
    expect(within(tg).getByText("Marta")).toBeInTheDocument();
    expect(within(tg).getByText("Pedro")).toBeInTheDocument();
    expect(list().queryByText("Yo mismo")).toBeNull();

    // fila de Ana: «3 estados · 2 nuevos · ahora» y anillo de 3 segmentos (1 visto, 2 sin ver)
    const row = within(wa).getByText("Ana").closest("button")!;
    expect(row).toHaveTextContent("3 estados · 2 nuevos · ahora");
    const segs = within(row).getByTestId("status-ring").querySelectorAll("circle");
    expect(Array.from(segs).map((c) => c.getAttribute("data-viewed"))).toEqual(["1", "0", "0"]);
  });

  it("«Mis estados» resume las cuentas y los estados activos", async () => {
    render(<StatusesPage />);
    await waitFor(() => expect(screen.getByText("2 cuentas · 4 activos")).toBeInTheDocument());
  });

  it("plegar un canal oculta a sus autores y lo anuncia con aria-expanded", async () => {
    render(<StatusesPage />);
    await waitFor(() => expect(list().getByText("Ana")).toBeInTheDocument());
    const head = within(list().getByRole("region", { name: "WhatsApp" })).getByRole("button", { name: /WhatsApp/ });
    fireEvent.click(head);
    expect(head).toHaveAttribute("aria-expanded", "false");
    expect(list().queryByText("Ana")).toBeNull();
    expect(list().getByText("Marta")).toBeInTheDocument();
    fireEvent.click(head);
    expect(list().getByText("Ana")).toBeInTheDocument();
  });

  it("el filtro por canal deja solo ese canal y «Todos» lo quita", async () => {
    render(<StatusesPage />);
    await waitFor(() => expect(list().getByText("Ana")).toBeInTheDocument());
    expect(chips().getByRole("button", { name: "Todos" })).toHaveAttribute("aria-pressed", "true");

    fireEvent.click(chips().getByRole("button", { name: "Telegram" }));
    expect(chips().getByRole("button", { name: "Telegram" })).toHaveAttribute("aria-pressed", "true");
    expect(chips().getByRole("button", { name: "Todos" })).toHaveAttribute("aria-pressed", "false");
    expect(list().queryByText("Ana")).toBeNull();
    expect(list().getByText("Marta")).toBeInTheDocument();

    fireEvent.click(chips().getByRole("button", { name: "Todos" }));
    expect(list().getByText("Ana")).toBeInTheDocument();
  });

  it("«Ocultar vistos» quita a los vistos y se recuerda", async () => {
    const { unmount } = render(<StatusesPage />);
    await waitFor(() => expect(list().getByText("Luis")).toBeInTheDocument());
    fireEvent.click(chips().getByRole("button", { name: "Ocultar vistos" }));
    expect(list().queryByText("Luis")).toBeNull();
    expect(list().queryByText("Pedro")).toBeNull();
    expect(list().queryByText("Vistos")).toBeNull();
    expect(list().getByText("Ana")).toBeInTheDocument();
    expect(localStorage.getItem("dc:statuses:hideSeen")).toBe("1");
    unmount();

    render(<StatusesPage />);
    await waitFor(() => expect(list().getByText("Ana")).toBeInTheDocument());
    expect(chips().getByRole("button", { name: "Ocultar vistos" })).toHaveAttribute("aria-pressed", "true");
    expect(list().queryByText("Luis")).toBeNull();
    fireEvent.click(chips().getByRole("button", { name: "Ocultar vistos" }));
    expect(list().getByText("Luis")).toBeInTheDocument();
    expect(localStorage.getItem("dc:statuses:hideSeen")).toBe("0");
  });

  it("la nota de Instagram y Messenger solo sale si hay cuentas de esos canales", async () => {
    const { unmount } = render(<StatusesPage />);
    await waitFor(() => expect(list().getByText("Ana")).toBeInTheDocument());
    expect(screen.queryByText(/no permiten ver historias/)).toBeNull();
    unmount();

    seed([...waTg(), account("ig-1", "Insta", "instagram", { statuses: false })]);
    render(<StatusesPage />);
    await waitFor(() => expect(list().getByText("Ana")).toBeInTheDocument());
    expect(screen.getByText("Instagram y Messenger no permiten ver historias desde dConnector.")).toBeInTheDocument();
    // Instagram no entra en los filtros ni en la carga de estados
    expect(chips().queryByRole("button", { name: "Instagram" })).toBeNull();
    expect(svc.StatusesService.Authors).toHaveBeenLastCalledWith(["wa-1", "tg-1"]);
  });
});

describe("StatusesPage · visor en línea", () => {
  it("sin nadie elegido muestra el estado vacío", async () => {
    render(<StatusesPage />);
    await waitFor(() => expect(list().getByText("Ana")).toBeInTheDocument());
    expect(screen.getByText("Sin estados por ver")).toBeInTheDocument();
    expect(screen.queryByTestId("story-viewer")).toBeNull();
  });

  it("sin estados de nadie sigue mostrando «Sin estados por ver»", async () => {
    svc.StatusesService.Authors.mockResolvedValue([]);
    render(<StatusesPage />);
    await waitFor(() => expect(svc.StatusesService.Authors).toHaveBeenCalled());
    expect(screen.getByText("Sin estados por ver")).toBeInTheDocument();
    expect(list().getByText("Sin estados")).toBeInTheDocument();
  });

  it("elegir a alguien pinta su tarjeta y pide sus estados con la cuenta de ESE autor", async () => {
    render(<StatusesPage />);
    fireEvent.click(await list().findByText("Ana"));
    expect(await screen.findByTestId("story-viewer")).toBeInTheDocument();
    expect(await screen.findByText("Hola desde ana")).toBeInTheDocument();
    expect(svc.StatusesService.ByAuthor).toHaveBeenCalledWith("wa-1", "ana@s");
    expect(screen.getByTestId("story-account")).toHaveTextContent("Ventas");
    expect(screen.getByText("Ana", { selector: "p" })).toBeInTheDocument();
    expect(list().getByText("Ana").closest("button")).toHaveAttribute("aria-current", "true");
    await waitFor(() => expect(svc.StatusesService.MarkViewed).toHaveBeenCalledWith("wa-1", "wa-1|s-ana@s"));

    // otra cuenta: ByAuthor, MarkViewed y la etiqueta salen de la cuenta de Telegram
    fireEvent.click(list().getByText("Marta"));
    expect(await screen.findByText("Hola desde marta")).toBeInTheDocument();
    expect(svc.StatusesService.ByAuthor).toHaveBeenCalledWith("tg-1", "marta@t");
    await waitFor(() => expect(svc.StatusesService.MarkViewed).toHaveBeenCalledWith("tg-1", "tg-1|s-marta@t"));
    expect(screen.getByTestId("story-account")).toHaveTextContent("Canal Tg");
    expect(list().getByText("Marta").closest("button")).toHaveAttribute("aria-current", "true");
    expect(list().getByText("Ana").closest("button")).not.toHaveAttribute("aria-current");
  });

  it("tras ver un estado la lista se recarga y la selección se conserva", async () => {
    render(<StatusesPage />);
    fireEvent.click(await list().findByText("Marta"));
    await screen.findByText("Hola desde marta");
    const calls = svc.StatusesService.Authors.mock.calls.length;
    // la recarga trae a Marta ya vista: pasa de «Recientes» a «Vistos»
    svc.StatusesService.Authors.mockResolvedValue([
      author("wa-1", "ana@s", "Ana", 3, 2),
      author("tg-1", "marta@t", "Marta", 1, 0),
    ]);
    await waitFor(() => expect(svc.StatusesService.Authors.mock.calls.length).toBeGreaterThan(calls));
    await waitFor(() => expect(list().getByText("Marta").closest("button")).toHaveAttribute("aria-current", "true"));
    expect(screen.getByText("Hola desde marta")).toBeInTheDocument();
  });

  it("las reacciones solo salen si la cuenta de ese autor lo permite", async () => {
    render(<StatusesPage />);
    fireEvent.click(await list().findByText("Ana")); // WhatsApp: statusReact = false
    await screen.findByText("Hola desde ana");
    expect(screen.queryByRole("group", { name: "Reaccionar" })).toBeNull();
    expect(screen.getByRole("button", { name: /Responder/ })).toBeInTheDocument();

    fireEvent.click(list().getByText("Marta")); // Telegram: statusReact = true
    await screen.findByText("Hola desde marta");
    fireEvent.click(await screen.findByRole("button", { name: "Reaccionar 🔥" }));
    await waitFor(() => expect(svc.StatusesService.React).toHaveBeenCalledWith("tg-1", "tg-1|s-marta@t", "🔥"));
  });

  it("las flechas de autor navegan por la lista y las del teclado no actúan al escribir", async () => {
    render(<StatusesPage />);
    fireEvent.click(await list().findByText("Ana"));
    await screen.findByText("Hola desde ana");
    const next = screen.getByRole("button", { name: "Autor siguiente" });
    fireEvent.click(next);
    await screen.findByText("Hola desde luis"); // el siguiente de Ana en la lista es Luis (vistos de WhatsApp)
    expect(list().getByText("Luis").closest("button")).toHaveAttribute("aria-current", "true");
    expect(screen.getByRole("button", { name: "Autor anterior" })).toBeEnabled();

    const input = document.createElement("input");
    document.body.appendChild(input);
    input.focus();
    fireEvent.keyDown(input, { key: "ArrowLeft", bubbles: true });
    expect(screen.getByTestId("story-viewer").getAttribute("data-group")).toBe("1");
    input.remove();
  });
});

describe("StatusesPage · pestañas y publicar", () => {
  it("«Mis estados» y «Estadísticas» eligen la cuenta con chips solo en esas pestañas", async () => {
    render(<StatusesPage />);
    await waitFor(() => expect(list().getByText("Ana")).toBeInTheDocument());
    expect(screen.getByRole("tab", { name: "Ver estados" })).toHaveAttribute("aria-selected", "true");
    expect(screen.queryByRole("radiogroup", { name: "Cuenta" })).toBeNull();

    fireEvent.click(screen.getByRole("tab", { name: "Mis estados" }));
    const group = screen.getByRole("radiogroup", { name: "Cuenta" });
    expect(within(group).getAllByRole("radio").map((r) => r.textContent)).toEqual(["Ventas", "Canal Tg"]);
    expect(within(group).getByRole("radio", { name: "Ventas" })).toHaveAttribute("aria-checked", "true");
    expect(screen.getAllByRole("button", { name: /^Estado / })).toHaveLength(2);

    fireEvent.click(within(group).getByRole("radio", { name: "Canal Tg" }));
    expect(localStorage.getItem("dc:statuses:account")).toBe("tg-1");

    fireEvent.click(screen.getByRole("tab", { name: "Estadísticas" }));
    await waitFor(() => expect(svc.StatusesService.Stats).toHaveBeenCalledWith("tg-1", { days: 7, from: "", to: "" }));
    expect(screen.getByRole("radiogroup", { name: "Cuenta" })).toBeInTheDocument();
  });

  it("el botón «Mis estados» de la lista abre esa pestaña", async () => {
    render(<StatusesPage />);
    await waitFor(() => expect(list().getByText("Ana")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: "Mis estados" }));
    expect(screen.getByRole("tab", { name: "Mis estados" })).toHaveAttribute("aria-selected", "true");
  });

  it("«Nuevo estado» abre el formulario solo si alguna cuenta puede publicar", async () => {
    const { unmount } = render(<StatusesPage />);
    fireEvent.click((await screen.findAllByRole("button", { name: "Nuevo estado" }))[0]);
    expect(await screen.findByRole("dialog")).toBeInTheDocument();
    unmount();

    seed([account("wa-1", "Ventas", "whatsapp", { statuses: true, postStatus: false })]);
    render(<StatusesPage />);
    await waitFor(() => expect(list().getByText("Ana")).toBeInTheDocument());
    expect(screen.queryAllByRole("button", { name: "Nuevo estado" })).toHaveLength(0);
  });

  it("solo usa cuentas con la capacidad de estados", async () => {
    seed([
      account("wa-1", "Ventas", "whatsapp", WA_CAPS),
      account("tg-1", "Telegram", "telegram", { statuses: false }),
      account("wa-2", "Personal", "whatsapp", { statuses: true }),
    ]);
    render(<StatusesPage />);
    await waitFor(() => expect(svc.StatusesService.Authors).toHaveBeenCalledWith(["wa-1", "wa-2"]));
    expect(svc.StatusesService.Mine).not.toHaveBeenCalledWith("tg-1", expect.anything(), expect.anything());
  });

  it("sin cuentas con estados: vacío corto", () => {
    seed([account("tg-1", "Telegram", "telegram", { statuses: false })]);
    render(<StatusesPage />);
    expect(screen.getByText("Sin cuentas con estados")).toBeInTheDocument();
  });
});
