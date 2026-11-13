// Contactos automáticos en la pantalla: insignia, filtro «Origen», total, «Actualizar desde las cuentas»
// y estado vacío.
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

const svc = vi.hoisted(() => ({
  AccountsService: { List: vi.fn() },
  InboxService: {},
  MessagingService: {},
  StatusesService: {},
  ContactsService: {
    List: vi.fn(),
    Lists: vi.fn(),
    SyncAll: vi.fn(),
  },
  CampaignsService: {},
  ScheduledService: {},
  StatsService: {},
  DestinationsService: {},
  SettingsService: {},
  FilesService: {},
}));

vi.mock("@/api/transport", async () => (await import("@/test/svcTransport")).svcTransport(svc));
vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(() => () => undefined) } }));

import type { Contact } from "@/api/types";
import ContactsPage from "@/pages/ContactsPage";
import { useAccounts } from "@/store/accounts";
import { useUI } from "@/store/ui";

const C = svc.ContactsService;

const contact = (id: string, p: Partial<Contact> = {}): Contact => ({
  id,
  name: `Contacto ${id}`,
  phone: "+593999000111",
  notes: "",
  tags: [],
  identities: [],
  listIds: [],
  optedOut: false,
  source: "manual",
  createdAt: 1,
  updatedAt: 1,
  ...p,
});

const three = () => [
  contact("a", { name: "Ana", source: "auto" }),
  contact("b", { name: "Beto", source: "manual" }),
  contact("c", { name: "Carla", source: "import" }),
];

// jsdom no tiene diseño: sin esto la lista virtualizada cree que el área visible mide 0.
beforeAll(() => {
  Object.defineProperty(HTMLElement.prototype, "clientHeight", { configurable: true, get: () => 600 });
  Object.defineProperty(HTMLElement.prototype, "clientWidth", { configurable: true, get: () => 900 });
});

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  useUI.setState({ toasts: [] });
  useAccounts.setState({ views: {}, order: [], loaded: true });
  C.List.mockResolvedValue({ contacts: [], total: 0 });
  C.Lists.mockResolvedValue([]);
});
afterEach(cleanup);

describe("ContactsPage · contactos automáticos", () => {
  it("muestra «Automático» solo en los de origen auto y el total con su palabra", async () => {
    C.List.mockResolvedValue({ contacts: three(), total: 3 });
    render(<ContactsPage />);
    await screen.findByText("Ana");
    const badges = screen.getAllByTestId("auto-badge");
    expect(badges).toHaveLength(1);
    expect(badges[0]).toHaveTextContent("Automático");
    expect(within(screen.getAllByTestId("contact-row")[0]).getByTestId("auto-badge")).toBeInTheDocument();
    expect(screen.getByTestId("contacts-total")).toHaveTextContent("3 contactos");
  });

  it("el selector «Origen» filtra en el cliente sin cambiar lo que se pide al backend", async () => {
    C.List.mockResolvedValue({ contacts: three(), total: 3 });
    render(<ContactsPage />);
    await screen.findByText("Ana");
    const select = screen.getByLabelText("Filtrar por origen");
    expect(within(select).getAllByRole("option").map((o) => o.textContent)).toEqual([
      "Origen: todos",
      "Automáticos",
      "Manuales",
      "Importados",
    ]);

    fireEvent.change(select, { target: { value: "auto" } });
    expect(screen.getByText("Ana")).toBeInTheDocument();
    expect(screen.queryByText("Beto")).toBeNull();
    expect(screen.queryByText("Carla")).toBeNull();
    expect(screen.getByTestId("contacts-total")).toHaveTextContent("1 contacto");

    fireEvent.change(select, { target: { value: "import" } });
    expect(screen.getByText("Carla")).toBeInTheDocument();
    expect(screen.queryByText("Ana")).toBeNull();

    fireEvent.change(select, { target: { value: "manual" } });
    expect(screen.getByText("Beto")).toBeInTheDocument();
    for (const call of C.List.mock.calls) expect(call[0]).not.toHaveProperty("source");

    fireEvent.change(select, { target: { value: "all" } });
    expect(screen.getByText("Ana")).toBeInTheDocument();
    expect(screen.getByTestId("contacts-total")).toHaveTextContent("3 contactos");
  });

  it("con un origen elegido pide solas las páginas que faltan", async () => {
    C.List.mockImplementation(async (f: { offset: number }) =>
      f.offset === 0
        ? { contacts: [contact("a", { name: "Ana", source: "manual" })], total: 2 }
        : { contacts: [contact("z", { name: "Zoe", source: "auto" })], total: 2 },
    );
    render(<ContactsPage />);
    await screen.findByText("Ana");
    fireEvent.change(screen.getByLabelText("Filtrar por origen"), { target: { value: "auto" } });
    expect(await screen.findByText("Zoe")).toBeInTheDocument();
    expect(screen.queryByText("Ana")).toBeNull();
  });

  it("sin coincidencias con el origen elegido: «Sin resultados»", async () => {
    C.List.mockResolvedValue({ contacts: [contact("b", { name: "Beto", source: "manual" })], total: 1 });
    render(<ContactsPage />);
    await screen.findByText("Beto");
    fireEvent.change(screen.getByLabelText("Filtrar por origen"), { target: { value: "auto" } });
    expect(await screen.findByText("Sin resultados")).toBeInTheDocument();
    expect(screen.getByText(/Ningún contacto tiene ese origen/)).toBeInTheDocument();
  });

  it("«Actualizar desde las cuentas» llama a SyncAll, avisa cuántos y recarga la tabla", async () => {
    C.List.mockResolvedValue({ contacts: three(), total: 3 });
    C.SyncAll.mockResolvedValue(5);
    render(<ContactsPage />);
    await screen.findByText("Ana");
    const before = C.List.mock.calls.length;
    fireEvent.click(screen.getByRole("button", { name: /Actualizar desde las cuentas/ }));
    await waitFor(() => expect(C.SyncAll).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(useUI.getState().toasts.some((t) => t.message === "Se agregaron 5 contactos nuevos")).toBe(true));
    await waitFor(() => expect(C.List.mock.calls.length).toBeGreaterThan(before));
  });

  it("si no hay nada nuevo lo dice y no recarga", async () => {
    C.List.mockResolvedValue({ contacts: three(), total: 3 });
    C.SyncAll.mockResolvedValue(0);
    render(<ContactsPage />);
    await screen.findByText("Ana");
    const before = C.List.mock.calls.length;
    fireEvent.click(screen.getByRole("button", { name: /Actualizar desde las cuentas/ }));
    await waitFor(() => expect(useUI.getState().toasts.some((t) => t.message === "Los contactos ya están al día")).toBe(true));
    expect(C.List.mock.calls.length).toBe(before);
  });

  it("un fallo de SyncAll deja el botón libre", async () => {
    C.List.mockResolvedValue({ contacts: three(), total: 3 });
    C.SyncAll.mockRejectedValue(new Error("sin cuentas cargadas"));
    render(<ContactsPage />);
    await screen.findByText("Ana");
    fireEvent.click(screen.getByRole("button", { name: /Actualizar desde las cuentas/ }));
    await waitFor(() => expect(useUI.getState().toasts.some((t) => t.kind === "error")).toBe(true));
    expect(screen.getByRole("button", { name: /Actualizar desde las cuentas/ })).toBeEnabled();
  });

  it("el estado vacío explica que se llenan solos y ofrece actualizar", async () => {
    C.SyncAll.mockResolvedValue(2);
    render(<ContactsPage />);
    await screen.findByText("Sin contactos");
    expect(screen.getByText(/se agregan solos al vincular tus cuentas/)).toBeInTheDocument();
    const buttons = screen.getAllByRole("button", { name: /Actualizar desde las cuentas/ });
    expect(buttons.length).toBe(2); // encabezado y estado vacío
    fireEvent.click(buttons[1]);
    await waitFor(() => expect(C.SyncAll).toHaveBeenCalled());
  });

  it("con 275 contactos muestra el total y solo pinta las filas visibles (virtualización)", async () => {
    const page = (offset: number) =>
      Array.from({ length: Math.min(100, 275 - offset) }, (_, i) =>
        contact(`c${offset + i}`, { name: `Persona ${offset + i}`, source: i % 2 ? "auto" : "manual" }),
      );
    C.List.mockImplementation(async (f: { offset: number }) => ({ contacts: page(f.offset), total: 275 }));
    render(<ContactsPage />);
    await screen.findByText("Persona 0");
    expect(screen.getByTestId("contacts-total")).toHaveTextContent("275 contactos");
    expect(screen.getAllByTestId("contact-row").length).toBeLessThan(60);
  });

  it("buscar por nombre o teléfono sigue pidiendo al backend con la consulta", async () => {
    C.List.mockResolvedValue({ contacts: three(), total: 3 });
    render(<ContactsPage />);
    await screen.findByText("Ana");
    fireEvent.change(screen.getByLabelText("Buscar contactos"), { target: { value: "0999" } });
    await waitFor(() => expect(C.List).toHaveBeenCalledWith(expect.objectContaining({ query: "0999", offset: 0 })));
  });
});
