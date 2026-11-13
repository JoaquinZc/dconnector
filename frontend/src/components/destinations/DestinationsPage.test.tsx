import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const hoisted = vi.hoisted(() => ({
  handlers: {} as Record<string, (e: { data: unknown }) => void>,
  svc: {
    AccountsService: { List: vi.fn() },
    InboxService: {},
    MessagingService: {},
    StatusesService: {},
    ContactsService: { Lists: vi.fn() },
    CampaignsService: {},
    ScheduledService: {},
    StatsService: {},
    DestinationsService: { List: vi.fn(), Save: vi.fn(), Delete: vi.fn(), States: vi.fn(), Test: vi.fn(), ProtocolDoc: vi.fn() },
    SettingsService: {},
    FilesService: {},
  },
}));
const { handlers, svc } = hoisted;

vi.mock("@/api/transport", async () => (await import("@/test/svcTransport")).svcTransport(hoisted.svc));
vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(() => () => undefined) } }));

import { emitEvent, hasEventListeners } from "@/api/events";
import type { AccountView, Destination } from "@/api/types";
import DestinationsPage from "@/pages/DestinationsPage";
import { useAccounts } from "@/store/accounts";
import { useUI } from "@/store/ui";

const dest = (id: string, p: Partial<Destination> = {}): Destination => ({
  id,
  name: `Destino ${id}`,
  url: "wss://bot.example.com/ws",
  token: "tok-" + id,
  enabled: true,
  allowCommands: false,
  filter: { accountIds: [], channels: [], events: [], includeFromMe: false, includeGroups: false, contactScope: { mode: "all", includeListIds: [], excludeListIds: [] } },
  createdAt: 1,
  ...p,
});

const LISTS = [
  { id: "fam", name: "Familia", color: "#e11d48", memberCount: 3 },
  { id: "ami", name: "Amigos", color: "#2563eb", memberCount: 5 },
  { id: "vip", name: "VIP", color: "#d97706", memberCount: 2 },
];
const PROTOCOL_DOC = "# Protocolo dConnector v1\n\nTexto del documento.\n\n## 1. Conexión\n\n```json\n{ \"v\": 1 }\n```\n";

const emit = (name: string, data: unknown) => act(() => emitEvent(name, data));

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  for (const k of Object.keys(handlers)) delete handlers[k];
  svc.DestinationsService.List.mockResolvedValue([dest("d1"), dest("d2", { enabled: false })]);
  svc.DestinationsService.States.mockResolvedValue([
    { destinationId: "d1", state: "online", lastError: "", connectedAt: 1, pending: 0 },
  ]);
  svc.DestinationsService.Save.mockImplementation(async (d: Destination) => ({ ...d, id: d.id || "nuevo", token: d.token || "generado" }));
  svc.DestinationsService.Delete.mockResolvedValue(undefined);
  svc.DestinationsService.Test.mockResolvedValue(undefined);
  svc.AccountsService.List.mockResolvedValue([]);
  svc.ContactsService.Lists.mockResolvedValue(LISTS);
  svc.DestinationsService.ProtocolDoc.mockResolvedValue(PROTOCOL_DOC);
  useUI.setState({ toasts: [], confirmReq: null });
  useAccounts.setState({
    views: {
      "wa-1": { account: { id: "wa-1", channel: "whatsapp", label: "Ventas", color: "#25D366", enabled: true }, status: {}, capabilities: {}, unread: 0 } as unknown as AccountView,
    },
    order: ["wa-1"],
    loaded: true,
  });
});
afterEach(cleanup);

const cards = () => screen.getAllByTestId("destination-card");

describe("lista de destinos", () => {
  it("muestra los destinos con el estado de States()", async () => {
    render(<DestinationsPage />);
    await waitFor(() => expect(cards()).toHaveLength(2));
    await waitFor(() => expect(within(cards()[0]).getByText("En línea")).toBeInTheDocument());
    expect(within(cards()[0]).getByTestId("destination-dot")).toHaveAttribute("data-tone", "ok");
    expect(within(cards()[1]).getByText("Desactivado")).toBeInTheDocument();
  });

  it("actualiza con destination:state: error y pendientes", async () => {
    render(<DestinationsPage />);
    await waitFor(() => expect(cards()).toHaveLength(2));
    await waitFor(() => expect(hasEventListeners("destination:state")).toBe(true));

    emit("destination:state", { destinationId: "d1", state: "error", lastError: "Token rechazado", connectedAt: 0, pending: 3 });
    expect(within(cards()[0]).getByText("Error")).toBeInTheDocument();
    expect(within(cards()[0]).getByText("Token rechazado")).toBeInTheDocument();
    expect(within(cards()[0]).getByText("3 pendientes")).toBeInTheDocument();
    expect(within(cards()[0]).getByTestId("destination-dot")).toHaveAttribute("data-tone", "danger");

    emit("destination:state", { destinationId: "d1", state: "connecting", lastError: "", connectedAt: 0, pending: 1 });
    expect(within(cards()[0]).getByText("Conectando…")).toBeInTheDocument();
    expect(within(cards()[0]).getByText("1 pendiente")).toBeInTheDocument();
  });

  it("sin destinos: vacío corto con botón", async () => {
    svc.DestinationsService.List.mockResolvedValue(null);
    render(<DestinationsPage />);
    await waitFor(() => expect(screen.getByText("Sin destinos")).toBeInTheDocument());
  });

  it("el interruptor activa o desactiva guardando el destino", async () => {
    render(<DestinationsPage />);
    await waitFor(() => expect(cards()).toHaveLength(2));
    fireEvent.click(within(cards()[1]).getByRole("switch", { name: "Activar" }));
    await waitFor(() => expect(svc.DestinationsService.Save).toHaveBeenCalled());
    expect(svc.DestinationsService.Save.mock.calls[0][0]).toMatchObject({ id: "d2", enabled: true });
  });

  it("eliminar pide confirmación", async () => {
    const confirm = vi.fn().mockResolvedValue(true);
    useUI.setState({ confirm });
    render(<DestinationsPage />);
    await waitFor(() => expect(cards()).toHaveLength(2));
    fireEvent.click(within(cards()[0]).getByRole("button", { name: "Eliminar" }));
    await waitFor(() => expect(svc.DestinationsService.Delete).toHaveBeenCalledWith("d1"));
    expect(confirm).toHaveBeenCalledWith(expect.objectContaining({ danger: true }));
  });
});

describe("formulario de destino", () => {
  const open = async () => {
    render(<DestinationsPage />);
    await waitFor(() => expect(cards()).toHaveLength(2));
    fireEvent.click(screen.getAllByRole("button", { name: /Nuevo destino/ })[0]);
    await screen.findByRole("dialog");
  };

  it("valida nombre y dirección antes de guardar", async () => {
    await open();
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    expect(screen.getByText("Falta el nombre")).toBeInTheDocument();
    expect(screen.getByText("Falta la dirección")).toBeInTheDocument();
    expect(svc.DestinationsService.Save).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText("Nombre"), { target: { value: "Mi bot" } });
    fireEvent.change(screen.getByLabelText("Dirección"), { target: { value: "ws://servidor.com" } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    expect(screen.getByText(/Debe empezar por wss/)).toBeInTheDocument();
    expect(svc.DestinationsService.Save).not.toHaveBeenCalled();
  });

  it("ws://localhost se acepta y se guarda con el token vacío", async () => {
    await open();
    fireEvent.change(screen.getByLabelText("Nombre"), { target: { value: "Local" } });
    fireEvent.change(screen.getByLabelText("Dirección"), { target: { value: "ws://localhost:8787" } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(svc.DestinationsService.Save).toHaveBeenCalledTimes(1));
    const sent = svc.DestinationsService.Save.mock.calls[0][0] as Destination;
    expect(sent).toMatchObject({ id: "", name: "Local", url: "ws://localhost:8787", token: "", enabled: true, allowCommands: false });
    expect(sent.filter.events).toEqual([]); // todos
    expect(sent.filter.accountIds).toEqual([]);
  });

  it("el token está oculto y se puede mostrar", async () => {
    await open();
    const token = screen.getByLabelText("Token");
    expect(token).toHaveAttribute("type", "password");
    fireEvent.click(screen.getByRole("button", { name: "Mostrar token" }));
    expect(token).toHaveAttribute("type", "text");
  });

  it("cuentas, canales y eventos se eligen y viajan en el filtro", async () => {
    await open();
    fireEvent.change(screen.getByLabelText("Nombre"), { target: { value: "Filtrado" } });
    fireEvent.change(screen.getByLabelText("Dirección"), { target: { value: "wss://a.com" } });
    fireEvent.click(within(screen.getByRole("group", { name: "Cuentas" })).getByRole("button", { name: "Ventas" }));
    fireEvent.click(within(screen.getByRole("group", { name: "Canales" })).getByRole("button", { name: "Telegram" }));
    fireEvent.click(screen.getByRole("button", { name: "Ninguno" }));
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    expect(screen.getByText("Elige al menos un evento")).toBeInTheDocument();

    const events = within(screen.getByRole("group", { name: "Eventos" }));
    fireEvent.click(events.getByRole("checkbox", { name: /Mensaje recibido/ }));
    fireEvent.click(events.getByRole("checkbox", { name: /Reacción/ }));
    fireEvent.click(screen.getByRole("switch", { name: "Incluir los míos" }));
    fireEvent.click(screen.getByRole("switch", { name: "Incluir grupos" }));
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));

    await waitFor(() => expect(svc.DestinationsService.Save).toHaveBeenCalledTimes(1));
    const sent = svc.DestinationsService.Save.mock.calls[0][0] as Destination;
    expect(sent.filter).toEqual({
      accountIds: ["wa-1"],
      channels: ["telegram"],
      events: ["message.received", "reaction"],
      includeFromMe: true,
      includeGroups: true,
      contactScope: { mode: "all", includeListIds: [], excludeListIds: [] },
    });
  });

  it("«Permitir comandos» muestra el aviso", async () => {
    await open();
    expect(screen.queryByText("El servidor podrá enviar mensajes en tu nombre")).toBeNull();
    fireEvent.click(screen.getByRole("switch", { name: "Permitir comandos" }));
    expect(screen.getByText("El servidor podrá enviar mensajes en tu nombre")).toBeInTheDocument();
  });

  it("«Probar conexión» guarda y prueba con el id guardado", async () => {
    await open();
    fireEvent.change(screen.getByLabelText("Nombre"), { target: { value: "Prueba" } });
    fireEvent.change(screen.getByLabelText("Dirección"), { target: { value: "wss://a.com" } });
    fireEvent.click(screen.getByRole("button", { name: "Probar conexión" }));
    await waitFor(() => expect(svc.DestinationsService.Test).toHaveBeenCalledWith("nuevo"));
    expect(svc.DestinationsService.Save).toHaveBeenCalledTimes(1);
    // el token que generó el servicio aparece en el formulario
    await waitFor(() => expect(screen.getByLabelText("Token")).toHaveValue("generado"));
    expect(useUI.getState().toasts.some((t) => t.message === "Conexión correcta")).toBe(true);
    // una segunda prueba no crea otro destino
    fireEvent.click(screen.getByRole("button", { name: "Probar conexión" }));
    await waitFor(() => expect(svc.DestinationsService.Test).toHaveBeenCalledTimes(2));
    expect(svc.DestinationsService.Save.mock.calls[1][0].id).toBe("nuevo");
  });

  it("probar con datos inválidos no llama al servicio", async () => {
    await open();
    fireEvent.click(screen.getByRole("button", { name: "Probar conexión" }));
    expect(screen.getByText("Falta el nombre")).toBeInTheDocument();
    expect(svc.DestinationsService.Test).not.toHaveBeenCalled();
  });

  it("el enlace «Ver el protocolo» abre el documento completo en un diálogo", async () => {
    await open();
    fireEvent.click(screen.getByRole("button", { name: /Ver el protocolo/ }));
    expect(await screen.findByText("Protocolo v1 — documento completo")).toBeInTheDocument();
    expect(await screen.findByRole("heading", { name: "1. Conexión" })).toBeInTheDocument();
    expect(svc.DestinationsService.ProtocolDoc).toHaveBeenCalledTimes(1);
    // El formulario sigue debajo.
    expect(screen.getAllByRole("dialog")).toHaveLength(2);
  });

  it("guarda borrador sin el token y lo restaura", async () => {
    await open();
    fireEvent.change(screen.getByLabelText("Nombre"), { target: { value: "A medias" } });
    fireEvent.change(screen.getByLabelText("Token"), { target: { value: "secreto" } });
    const raw = localStorage.getItem("draft:destinations:new")!;
    expect(JSON.parse(raw).name).toBe("A medias");
    expect(raw).not.toContain("secreto");
    cleanup();
    await open();
    expect(screen.getByLabelText("Nombre")).toHaveValue("A medias");
  });

  it("editar carga los valores del destino", async () => {
    render(<DestinationsPage />);
    await waitFor(() => expect(cards()).toHaveLength(2));
    fireEvent.click(within(cards()[0]).getByRole("button", { name: "Editar" }));
    await screen.findByRole("dialog");
    expect(screen.getByLabelText("Nombre")).toHaveValue("Destino d1");
    expect(screen.getByLabelText("Token")).toHaveValue("tok-d1");
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(svc.DestinationsService.Save).toHaveBeenCalled());
    expect(svc.DestinationsService.Save.mock.calls[0][0]).toMatchObject({ id: "d1", token: "tok-d1", createdAt: 1 });
  });
});

describe("alcance de contactos", () => {
  const open = async () => {
    render(<DestinationsPage />);
    await waitFor(() => expect(cards()).toHaveLength(2));
    fireEvent.click(screen.getAllByRole("button", { name: /Nuevo destino/ })[0]);
    await screen.findByRole("dialog");
    // Las listas ya cargadas.
    await within(screen.getByRole("group", { name: "Excluir estas listas" })).findByRole("button", { name: "Familia" });
  };
  const summary = () => screen.getByTestId("contact-scope-summary");
  const fill = () => {
    fireEvent.change(screen.getByLabelText("Nombre"), { target: { value: "Con listas" } });
    fireEvent.change(screen.getByLabelText("Dirección"), { target: { value: "wss://a.com" } });
  };

  it("por defecto recibe de todos los contactos y no ofrece las listas incluidas", async () => {
    await open();
    expect(screen.getByLabelText("Contactos")).toHaveValue("all");
    expect(summary()).toHaveTextContent("Recibe: todos los contactos");
    expect(screen.queryByRole("group", { name: "Incluir estas listas" })).toBeNull();
  });

  it("todos menos una lista: frase y filtro guardado", async () => {
    await open();
    fill();
    const exclude = within(screen.getByRole("group", { name: "Excluir estas listas" }));
    fireEvent.click(exclude.getByRole("button", { name: "Familia" }));
    expect(summary()).toHaveTextContent("Recibe: todos los contactos, excepto la lista Familia");
    fireEvent.click(exclude.getByRole("button", { name: "Amigos" }));
    expect(summary()).toHaveTextContent("Recibe: todos los contactos, excepto las listas Familia y Amigos");
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(svc.DestinationsService.Save).toHaveBeenCalledTimes(1));
    expect((svc.DestinationsService.Save.mock.calls[0][0] as Destination).filter.contactScope).toEqual({
      mode: "all",
      includeListIds: [],
      excludeListIds: ["fam", "ami"],
    });
  });

  it("«Solo estas listas»: elige listas, deshabilita la misma en el otro lado y guarda", async () => {
    await open();
    fill();
    fireEvent.change(screen.getByLabelText("Contactos"), { target: { value: "lists" } });
    const include = within(screen.getByRole("group", { name: "Incluir estas listas" }));
    const exclude = within(screen.getByRole("group", { name: "Excluir estas listas" }));
    expect(summary()).toHaveTextContent("No recibirá nada");

    fireEvent.click(include.getByRole("button", { name: "VIP" }));
    fireEvent.click(include.getByRole("button", { name: "Familia" }));
    // Las incluidas no se pueden excluir.
    expect(exclude.getByRole("button", { name: "VIP" })).toBeDisabled();
    expect(exclude.getByRole("button", { name: "Familia" })).toBeDisabled();
    expect(exclude.getByRole("button", { name: "Amigos" })).toBeEnabled();
    fireEvent.click(exclude.getByRole("button", { name: "Amigos" }));
    // Y las excluidas no se pueden incluir.
    expect(include.getByRole("button", { name: "Amigos" })).toBeDisabled();
    expect(summary()).toHaveTextContent("Recibe: solo los contactos de las listas VIP y Familia, excepto la lista Amigos");

    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(svc.DestinationsService.Save).toHaveBeenCalledTimes(1));
    expect((svc.DestinationsService.Save.mock.calls[0][0] as Destination).filter.contactScope).toEqual({
      mode: "lists",
      includeListIds: ["vip", "fam"],
      excludeListIds: ["ami"],
    });
  });

  it("«Solo estas listas» sin ninguna lista no deja guardar y avisa", async () => {
    await open();
    fill();
    fireEvent.change(screen.getByLabelText("Contactos"), { target: { value: "lists" } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    expect(screen.getByRole("alert")).toHaveTextContent(/al menos una lista/);
    expect(svc.DestinationsService.Save).not.toHaveBeenCalled();
    // Al elegir una lista, se puede guardar.
    fireEvent.click(within(screen.getByRole("group", { name: "Incluir estas listas" })).getByRole("button", { name: "VIP" }));
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(svc.DestinationsService.Save).toHaveBeenCalledTimes(1));
  });

  it("volver a «Todos» no guarda las listas incluidas", async () => {
    await open();
    fill();
    fireEvent.change(screen.getByLabelText("Contactos"), { target: { value: "lists" } });
    fireEvent.click(within(screen.getByRole("group", { name: "Incluir estas listas" })).getByRole("button", { name: "VIP" }));
    fireEvent.change(screen.getByLabelText("Contactos"), { target: { value: "all" } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(svc.DestinationsService.Save).toHaveBeenCalledTimes(1));
    expect((svc.DestinationsService.Save.mock.calls[0][0] as Destination).filter.contactScope.includeListIds).toEqual([]);
  });

  it("sin listas creadas lo explica", async () => {
    svc.ContactsService.Lists.mockResolvedValue([]);
    render(<DestinationsPage />);
    await waitFor(() => expect(cards()).toHaveLength(2));
    fireEvent.click(screen.getAllByRole("button", { name: /Nuevo destino/ })[0]);
    expect(await screen.findByText(/Aún no hay listas de contactos/)).toBeInTheDocument();
  });

  it("editar un destino carga su alcance y descarta las listas que ya no existen", async () => {
    svc.DestinationsService.List.mockResolvedValue([
      dest("d1", {
        filter: { accountIds: [], channels: [], events: [], includeFromMe: false, includeGroups: false, contactScope: { mode: "lists", includeListIds: ["vip", "borrada"], excludeListIds: ["fam"] } },
      }),
    ]);
    render(<DestinationsPage />);
    await waitFor(() => expect(cards()).toHaveLength(1));
    fireEvent.click(within(cards()[0]).getByRole("button", { name: "Editar" }));
    await screen.findByRole("dialog");
    await waitFor(() => expect(screen.getByLabelText("Contactos")).toHaveValue("lists"));
    await waitFor(() => expect(summary()).toHaveTextContent("Recibe: solo los contactos de la lista VIP, excepto la lista Familia"));
    expect(within(screen.getByRole("group", { name: "Incluir estas listas" })).getByRole("button", { name: "VIP" })).toHaveAttribute("aria-pressed", "true");
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(svc.DestinationsService.Save).toHaveBeenCalled());
    expect((svc.DestinationsService.Save.mock.calls[0][0] as Destination).filter.contactScope).toEqual({
      mode: "lists",
      includeListIds: ["vip"],
      excludeListIds: ["fam"],
    });
  });

  it("la tarjeta muestra la frase del alcance", async () => {
    svc.DestinationsService.List.mockResolvedValue([
      dest("d1"),
      dest("d2", {
        filter: { accountIds: [], channels: [], events: [], includeFromMe: false, includeGroups: false, contactScope: { mode: "all", includeListIds: [], excludeListIds: ["fam", "ami"] } },
      }),
    ]);
    render(<DestinationsPage />);
    await waitFor(() => expect(cards()).toHaveLength(2));
    expect(within(cards()[0]).getByTestId("destination-scope")).toHaveTextContent("Recibe: todos los contactos");
    await waitFor(() =>
      expect(within(cards()[1]).getByTestId("destination-scope")).toHaveTextContent("Recibe: todos los contactos, excepto las listas Familia y Amigos"),
    );
  });
});
