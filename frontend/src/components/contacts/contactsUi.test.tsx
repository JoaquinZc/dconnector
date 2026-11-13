import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

const svc = vi.hoisted(() => ({
  AccountsService: { List: vi.fn() },
  InboxService: {},
  MessagingService: {},
  StatusesService: {},
  ContactsService: {
    List: vi.fn(),
    Get: vi.fn(),
    Save: vi.fn(),
    Delete: vi.fn(),
    Merge: vi.fn(),
    SplitIdentity: vi.fn(),
    ImportFromAccount: vi.fn(),
    ImportCSV: vi.fn(),
    SuggestMerges: vi.fn(),
    Lists: vi.fn(),
    SaveList: vi.fn(),
    DeleteList: vi.fn(),
    SetListMembers: vi.fn(),
  },
  CampaignsService: {},
  ScheduledService: {},
  StatsService: {},
  DestinationsService: {},
  SettingsService: {},
  FilesService: { PickFiles: vi.fn() },
}));

vi.mock("@/api/transport", async () => (await import("@/test/svcTransport")).svcTransport(svc));
vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(() => () => undefined) } }));

import type { AccountView, Contact, ContactList, Identity } from "@/api/types";
import ContactsPage from "@/pages/ContactsPage";
import { useAccounts } from "@/store/accounts";
import { useUI } from "@/store/ui";
import ContactPanel from "./ContactPanel";
import ImportCsvModal from "./ImportCsvModal";
import ImportFromAccountModal from "./ImportFromAccountModal";
import NewContactModal from "./NewContactModal";
import SuggestMergesModal from "./SuggestMergesModal";

const C = svc.ContactsService;

const ident = (accountId: string, peer: string, channel = "whatsapp", name = ""): Identity =>
  ({ accountId, channel, peerNativeId: peer, displayName: name, phone: "" }) as unknown as Identity;

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

const list = (id: string, name: string): ContactList => ({ id, name, color: "#25D366", memberCount: 2 });

const view = (id: string, label: string, state = "connected"): AccountView =>
  ({
    account: { id, channel: "whatsapp", label, color: "#25D366", enabled: true, selfId: "", selfName: "", selfHandle: "", createdAt: 1 },
    status: { accountId: id, state },
    capabilities: {},
    unread: 0,
  }) as unknown as AccountView;

let confirmResult = true;

// jsdom no tiene diseño: sin esto la lista virtualizada cree que el área visible mide 0 y no pinta filas.
beforeAll(() => {
  Object.defineProperty(HTMLElement.prototype, "clientHeight", { configurable: true, get: () => 600 });
  Object.defineProperty(HTMLElement.prototype, "clientWidth", { configurable: true, get: () => 900 });
});

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  confirmResult = true;
  useUI.setState({ toasts: [], confirm: vi.fn(async () => confirmResult), openChat: vi.fn() });
  useAccounts.setState({ views: { "wa-1": view("wa-1", "Ventas"), "tg-1": view("tg-1", "Soporte", "disconnected") }, order: ["wa-1", "tg-1"], loaded: true });
  C.List.mockResolvedValue({ contacts: [], total: 0 });
  C.Lists.mockResolvedValue([]);
  C.Save.mockImplementation(async (c: Contact) => ({ ...c, id: c.id || "nuevo" }));
  C.Get.mockImplementation(async (id: string) => contact(id));
  C.SetListMembers.mockResolvedValue(undefined);
});
afterEach(cleanup);

describe("ContactPanel (ficha)", () => {
  const two = contact("c1", { name: "Ana", identities: [ident("wa-1", "593@s.whatsapp.net", "whatsapp", "Ana WA"), ident("tg-1", "u:77", "telegram", "ana_tg")] });
  const props = (c: Contact, extra = {}) => ({
    contact: c,
    lists: [list("l1", "Clientes"), list("l2", "Proveedores")],
    onClose: vi.fn(),
    onChanged: vi.fn(),
    onDeleted: vi.fn(),
    onMerge: vi.fn(),
    ...extra,
  });

  it("«Abrir chat» abre la clave cuenta|id del par", () => {
    render(<ContactPanel {...props(two)} />);
    fireEvent.click(screen.getByRole("button", { name: "Abrir chat con ana_tg" }));
    expect(useUI.getState().openChat).toHaveBeenCalledWith("tg-1|u:77");
  });

  it("«Separar» pide confirmación y llama a SplitIdentity; con una sola identidad no aparece", async () => {
    C.SplitIdentity.mockResolvedValue(contact("nuevo"));
    C.Get.mockResolvedValue(contact("c1", { name: "Ana", identities: [ident("wa-1", "593@s.whatsapp.net")] }));
    const onSplit = vi.fn();
    const p = props(two, { onSplit });
    const { unmount } = render(<ContactPanel {...p} />);
    fireEvent.click(screen.getByRole("button", { name: "Separar ana_tg" }));
    await waitFor(() => expect(C.SplitIdentity).toHaveBeenCalledWith("c1", "tg-1", "u:77"));
    expect(useUI.getState().confirm).toHaveBeenCalled();
    await waitFor(() => expect(p.onChanged).toHaveBeenCalledWith(expect.objectContaining({ id: "c1" })));
    expect(C.Get).toHaveBeenCalledWith("c1");
    expect(onSplit).toHaveBeenCalled();
    unmount();

    render(<ContactPanel {...props(contact("c2", { identities: [ident("wa-1", "1")] }))} />);
    expect(screen.queryByRole("button", { name: /Separar/ })).toBeNull();
  });

  it("si no se confirma, no se separa", async () => {
    confirmResult = false;
    render(<ContactPanel {...props(two)} />);
    fireEvent.click(screen.getByRole("button", { name: "Separar ana_tg" }));
    await waitFor(() => expect(useUI.getState().confirm).toHaveBeenCalled());
    expect(C.SplitIdentity).not.toHaveBeenCalled();
  });

  it("editar guarda borrador mientras hay cambios, y Guardar lo borra y llama a Save", async () => {
    const p = props(two);
    render(<ContactPanel {...p} />);
    expect(screen.getByRole("button", { name: "Guardar" })).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Nombre"), { target: { value: "Ana María" } });
    expect(localStorage.getItem("draft:contacts:c1")).toContain("Ana María");
    expect(screen.getByText("Cambios sin guardar")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(C.Save).toHaveBeenCalledTimes(1));
    expect(C.Save.mock.calls[0][0]).toMatchObject({ id: "c1", name: "Ana María", identities: two.identities });
    await waitFor(() => expect(p.onChanged).toHaveBeenCalled());
    expect(localStorage.getItem("draft:contacts:c1")).toBeNull();
  });

  it("restaura el borrador al abrir la ficha", () => {
    localStorage.setItem("draft:contacts:c1", JSON.stringify({ name: "Borrador", phone: "+593999000111", tags: [], notes: "", optedOut: false, listIds: [] }));
    render(<ContactPanel {...props(two)} />);
    expect(screen.getByLabelText("Nombre")).toHaveValue("Borrador");
    expect(screen.getByRole("button", { name: "Guardar" })).toBeEnabled();
  });

  it("Descartar vuelve a los datos guardados y borra el borrador", () => {
    render(<ContactPanel {...props(two)} />);
    fireEvent.change(screen.getByLabelText("Nombre"), { target: { value: "Otro" } });
    fireEvent.click(screen.getByRole("button", { name: "Descartar" }));
    expect(screen.getByLabelText("Nombre")).toHaveValue("Ana");
    expect(localStorage.getItem("draft:contacts:c1")).toBeNull();
  });

  it("etiquetas, listas y «No recibe campañas» viajan en el guardado; las listas por SetListMembers", async () => {
    const p = props(contact("c3", { name: "Luis", listIds: ["l2"] }));
    render(<ContactPanel {...p} />);
    const tags = screen.getByLabelText("Etiquetas");
    fireEvent.change(tags, { target: { value: "vip, nuevo" } });
    fireEvent.click(within(screen.getByRole("group", { name: "Listas del contacto" })).getByRole("checkbox", { name: "Clientes" }));
    fireEvent.click(within(screen.getByRole("group", { name: "Listas del contacto" })).getByRole("checkbox", { name: "Proveedores" }));
    fireEvent.click(screen.getByRole("switch", { name: "No recibe campañas" }));
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));

    await waitFor(() => expect(C.Save).toHaveBeenCalled());
    expect(C.Save.mock.calls[0][0]).toMatchObject({ tags: ["vip", "nuevo"], optedOut: true, listIds: ["l1"] });
    await waitFor(() => expect(C.SetListMembers).toHaveBeenCalledTimes(2));
    expect(C.SetListMembers).toHaveBeenCalledWith("l1", ["c3"], null);
    expect(C.SetListMembers).toHaveBeenCalledWith("l2", null, ["c3"]);
    expect(C.Get).toHaveBeenCalledWith("c3"); // se relee con las listas al día
  });

  it("«Unir con otro» y «Eliminar» (con confirmación)", async () => {
    C.Delete.mockResolvedValue(undefined);
    const p = props(two);
    render(<ContactPanel {...p} />);
    fireEvent.click(screen.getByRole("button", { name: /Unir con otro/ }));
    expect(p.onMerge).toHaveBeenCalledWith(two);
    fireEvent.click(screen.getByRole("button", { name: /Eliminar/ }));
    await waitFor(() => expect(C.Delete).toHaveBeenCalledWith("c1"));
    expect(useUI.getState().confirm).toHaveBeenCalledWith(expect.objectContaining({ danger: true }));
    expect(p.onDeleted).toHaveBeenCalledWith("c1");
  });
});

describe("NewContactModal", () => {
  it("sin nombre ni teléfono no guarda y muestra el error", async () => {
    render(<NewContactModal onClose={() => undefined} onCreated={() => undefined} />);
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    expect(await screen.findByText("Escribe un nombre o un teléfono")).toBeInTheDocument();
    expect(C.Save).not.toHaveBeenCalled();
  });

  it("guarda con nombre, teléfono, etiquetas y notas; el borrador `draft:contacts:new` se limpia", async () => {
    const onCreated = vi.fn();
    render(<NewContactModal onClose={() => undefined} onCreated={onCreated} />);
    fireEvent.change(screen.getByLabelText("Nombre"), { target: { value: "Pedro" } });
    fireEvent.change(screen.getByLabelText("Teléfono"), { target: { value: "+593 99 111 2222" } });
    fireEvent.change(screen.getByLabelText("Etiquetas"), { target: { value: "vip," } });
    fireEvent.change(screen.getByLabelText("Notas"), { target: { value: "Llamar mañana" } });
    expect(localStorage.getItem("draft:contacts:new")).toContain("Pedro");

    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(onCreated).toHaveBeenCalled());
    expect(C.Save.mock.calls[0][0]).toMatchObject({ id: "", name: "Pedro", phone: "+593 99 111 2222", tags: ["vip"], notes: "Llamar mañana" });
    expect(localStorage.getItem("draft:contacts:new")).toBeNull();
  });

  it("un teléfono corto es un error", async () => {
    render(<NewContactModal onClose={() => undefined} onCreated={() => undefined} />);
    fireEvent.change(screen.getByLabelText("Nombre"), { target: { value: "Pedro" } });
    fireEvent.change(screen.getByLabelText("Teléfono"), { target: { value: "123" } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    expect(await screen.findByText("Teléfono no válido")).toBeInTheDocument();
    expect(C.Save).not.toHaveBeenCalled();
  });

  it("restaura el borrador", () => {
    localStorage.setItem("draft:contacts:new", JSON.stringify({ name: "Guardado antes", phone: "", tags: ["x"], notes: "", optedOut: false, listIds: [] }));
    render(<NewContactModal onClose={() => undefined} onCreated={() => undefined} />);
    expect(screen.getByLabelText("Nombre")).toHaveValue("Guardado antes");
  });
});

describe("ImportCsvModal", () => {
  it("elige archivo, indica columnas por número y llama a ImportCSV con índices base 0 (-1 = no)", async () => {
    svc.FilesService.PickFiles.mockResolvedValue([{ path: "C:\\datos\\clientes.csv", name: "clientes.csv", mime: "text/csv", size: 2048 }]);
    C.ImportCSV.mockResolvedValue({ Created: 4, Updated: 1, Skipped: 2 });
    const onDone = vi.fn();
    render(<ImportCsvModal lists={[list("l1", "Clientes")]} onClose={() => undefined} onDone={onDone} />);

    fireEvent.click(screen.getByRole("button", { name: "Importar" }));
    expect(await screen.findByText("Elige un archivo")).toBeInTheDocument();
    expect(C.ImportCSV).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Elegir archivo" }));
    expect(await screen.findByText("clientes.csv")).toBeInTheDocument();
    expect(screen.getByText("2 KB")).toBeInTheDocument();
    expect(svc.FilesService.PickFiles).toHaveBeenCalledWith("csv");

    fireEvent.change(screen.getByLabelText("Nombre"), { target: { value: "2" } });
    fireEvent.change(screen.getByLabelText("Teléfono"), { target: { value: "3" } });
    fireEvent.change(screen.getByLabelText("Etiquetas"), { target: { value: "0" } });
    fireEvent.change(screen.getByLabelText("Notas"), { target: { value: "5" } });
    fireEvent.change(screen.getByLabelText("Añadir a la lista"), { target: { value: "l1" } });
    fireEvent.click(screen.getByLabelText("Tiene encabezado"));
    fireEvent.click(screen.getByRole("button", { name: "Importar" }));

    await waitFor(() => expect(C.ImportCSV).toHaveBeenCalledTimes(1));
    expect(C.ImportCSV).toHaveBeenCalledWith("C:\\datos\\clientes.csv", { Name: 1, Phone: 2, Tags: -1, Notes: 4, ListID: "l1", HasHeader: false });
    expect(await screen.findByTestId("import-result")).toHaveTextContent("4 creados · 1 actualizado · 2 omitidos");
    expect(onDone).toHaveBeenCalled();
  });

  it("sin columna de nombre ni de teléfono no importa", async () => {
    svc.FilesService.PickFiles.mockResolvedValue([{ path: "a.csv", name: "a.csv", mime: "", size: 10 }]);
    render(<ImportCsvModal lists={[]} onClose={() => undefined} onDone={() => undefined} />);
    fireEvent.click(screen.getByRole("button", { name: "Elegir archivo" }));
    await screen.findByText("a.csv");
    fireEvent.change(screen.getByLabelText("Nombre"), { target: { value: "0" } });
    fireEvent.change(screen.getByLabelText("Teléfono"), { target: { value: "0" } });
    fireEvent.click(screen.getByRole("button", { name: "Importar" }));
    expect(await screen.findByText("Indica la columna del nombre o del teléfono")).toBeInTheDocument();
    expect(C.ImportCSV).not.toHaveBeenCalled();
  });
});

describe("ImportFromAccountModal", () => {
  it("solo se pueden elegir cuentas conectadas", async () => {
    C.ImportFromAccount.mockResolvedValue({ Created: 2, Updated: 0, Skipped: 0 });
    const onDone = vi.fn();
    render(<ImportFromAccountModal onClose={() => undefined} onDone={onDone} />);
    expect(screen.getByRole("radio", { name: /Soporte/ })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Importar" })).toBeDisabled();
    fireEvent.click(screen.getByRole("radio", { name: /Ventas/ }));
    fireEvent.click(screen.getByRole("button", { name: "Importar" }));
    await waitFor(() => expect(C.ImportFromAccount).toHaveBeenCalledWith("wa-1"));
    expect(await screen.findByTestId("import-result")).toHaveTextContent("2 creados");
    expect(onDone).toHaveBeenCalled();
  });
});

describe("SuggestMergesModal", () => {
  it("muestra los pares lado a lado y «Unir» conserva el primero", async () => {
    C.SuggestMerges.mockResolvedValue([["a", "b"], ["b", "a"], ["c", "d"]]);
    C.Merge.mockImplementation(async (keep: string) => contact(keep, { name: "Unido" }));
    const onMerged = vi.fn();
    render(<SuggestMergesModal onClose={() => undefined} onMerged={onMerged} />);

    const pairs = await screen.findAllByTestId("merge-pair");
    expect(pairs).toHaveLength(2); // el par repetido al revés se descarta
    await waitFor(() => expect(within(pairs[0]).getAllByTestId("contact-preview")).toHaveLength(2));
    expect(within(pairs[0]).getByText("Contacto a")).toBeInTheDocument();
    expect(within(pairs[0]).getByText("Contacto b")).toBeInTheDocument();

    fireEvent.click(within(pairs[0]).getByRole("button", { name: "Unir" }));
    await waitFor(() => expect(C.Merge).toHaveBeenCalledWith("a", "b"));
    await waitFor(() => expect(onMerged).toHaveBeenCalledWith(expect.objectContaining({ id: "a" }), "b"));
    await waitFor(() => expect(screen.getAllByTestId("merge-pair")).toHaveLength(1));
  });

  it("«Cambiar cuál se conserva» invierte los ids", async () => {
    C.SuggestMerges.mockResolvedValue([["a", "b"]]);
    C.Merge.mockImplementation(async (keep: string) => contact(keep));
    render(<SuggestMergesModal onClose={() => undefined} onMerged={() => undefined} />);
    const pair = await screen.findByTestId("merge-pair");
    await waitFor(() => expect(within(pair).getAllByTestId("contact-preview")).toHaveLength(2));
    fireEvent.click(within(pair).getByRole("button", { name: "Cambiar cuál se conserva" }));
    fireEvent.click(within(pair).getByRole("button", { name: "Unir" }));
    await waitFor(() => expect(C.Merge).toHaveBeenCalledWith("b", "a"));
  });

  it("«Ignorar» quita el par y sin sugerencias hay un vacío corto", async () => {
    C.SuggestMerges.mockResolvedValue([["a", "b"]]);
    render(<SuggestMergesModal onClose={() => undefined} onMerged={() => undefined} />);
    const pair = await screen.findByTestId("merge-pair");
    fireEvent.click(within(pair).getByRole("button", { name: "Ignorar" }));
    expect(await screen.findByText("Sin sugerencias")).toBeInTheDocument();
  });
});

describe("ContactsPage", () => {
  it("lista contactos con canales y listas, y pide más al acercarse al final", async () => {
    C.Lists.mockResolvedValue([list("l1", "Clientes")]);
    C.List.mockImplementation(async (f: { offset: number }) =>
      f.offset === 0
        ? {
            contacts: [
              contact("a", { name: "Ana", listIds: ["l1"], tags: ["vip"], identities: [ident("wa-1", "1")] }),
              contact("b", { name: "Beto", identities: [ident("tg-1", "2", "telegram")] }),
            ],
            total: 3,
          }
        : { contacts: [contact("c", { name: "Carla" })], total: 3 },
    );
    render(<ContactsPage />);

    expect(await screen.findByText("Ana")).toBeInTheDocument();
    expect(screen.getByText("Beto")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("Carla")).toBeInTheDocument());
    expect(C.List).toHaveBeenCalledWith(expect.objectContaining({ offset: 0, limit: 100 }));
    expect(C.List).toHaveBeenCalledWith(expect.objectContaining({ offset: 2, limit: 100 }));
    expect(screen.getByTestId("contacts-total")).toHaveTextContent("3");
    expect(screen.getAllByText("Clientes").length).toBeGreaterThan(1); // barra lateral y chip de la fila
  });

  it("el buscador y el filtro por etiqueta vuelven a pedir desde el principio", async () => {
    C.List.mockResolvedValue({ contacts: [contact("a", { name: "Ana", tags: ["vip"] })], total: 1 });
    render(<ContactsPage />);
    await screen.findByText("Ana");

    fireEvent.change(screen.getByLabelText("Buscar contactos"), { target: { value: " ana " } });
    await waitFor(() => expect(C.List).toHaveBeenCalledWith(expect.objectContaining({ query: "ana", offset: 0 })));

    fireEvent.change(screen.getByLabelText("Filtrar por etiqueta"), { target: { value: "vip" } });
    await waitFor(() => expect(C.List).toHaveBeenCalledWith(expect.objectContaining({ query: "ana", tag: "vip", offset: 0 })));
  });

  it("elegir una lista filtra por esa lista y «Todos» la quita", async () => {
    C.Lists.mockResolvedValue([list("l1", "Clientes")]);
    C.List.mockResolvedValue({ contacts: [contact("a", { name: "Ana" })], total: 1 });
    render(<ContactsPage />);
    await screen.findByText("Ana");
    fireEvent.click(await screen.findByTitle("Clientes"));
    await waitFor(() => expect(C.List).toHaveBeenCalledWith(expect.objectContaining({ listId: "l1" })));
    fireEvent.click(screen.getByRole("button", { name: "Todos" }));
    await waitFor(() => expect(C.List).toHaveBeenLastCalledWith(expect.objectContaining({ listId: "" })));
  });

  it("clic en una fila abre la ficha", async () => {
    C.List.mockResolvedValue({ contacts: [contact("a", { name: "Ana", identities: [ident("wa-1", "1", "whatsapp", "Ana WA")] })], total: 1 });
    render(<ContactsPage />);
    fireEvent.click(await screen.findByText("Ana"));
    const ficha = await screen.findByRole("complementary", { name: "Ficha del contacto" });
    expect(within(ficha).getByLabelText("Nombre")).toHaveValue("Ana");
    expect(within(ficha).getByText("Ana WA")).toBeInTheDocument();
  });

  it("crear una lista pide nombre y llama a SaveList; borrar pide confirmación", async () => {
    C.Lists.mockResolvedValue([list("l1", "Clientes")]);
    C.SaveList.mockImplementation(async (l: ContactList) => ({ ...l, id: "l9" }));
    C.DeleteList.mockResolvedValue(undefined);
    render(<ContactsPage />);
    await screen.findByText("Sin contactos");

    fireEvent.click(screen.getByRole("button", { name: "Nueva lista" }));
    const dlg = await screen.findByRole("dialog");
    fireEvent.change(within(dlg).getByLabelText("Nombre"), { target: { value: "Clientes" } });
    fireEvent.click(within(dlg).getByRole("button", { name: "Guardar" }));
    expect(await within(dlg).findByText("Ya existe una lista con ese nombre")).toBeInTheDocument();
    fireEvent.change(within(dlg).getByLabelText("Nombre"), { target: { value: "Proveedores" } });
    fireEvent.click(within(dlg).getByRole("radio", { name: "Color #8B5CF6" }));
    fireEvent.click(within(dlg).getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(C.SaveList).toHaveBeenCalledWith({ id: "", name: "Proveedores", color: "#8B5CF6", memberCount: 0 }));

    fireEvent.click(await screen.findByRole("button", { name: "Eliminar Clientes" }));
    await waitFor(() => expect(C.DeleteList).toHaveBeenCalledWith("l1"));
    expect(useUI.getState().confirm).toHaveBeenCalledWith(expect.objectContaining({ danger: true }));
  });

  it("el menú ⋮ ofrece importar de una cuenta, importar CSV y sugerencias de unión", async () => {
    render(<ContactsPage />);
    await screen.findByText("Sin contactos");
    fireEvent.click(screen.getByRole("button", { name: "Más opciones" }));
    expect(screen.getByRole("menuitem", { name: /Importar de una cuenta/ })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: /Importar CSV/ })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: /Sugerencias de unión/ })).toBeInTheDocument();
    C.SuggestMerges.mockResolvedValue([]);
    fireEvent.click(screen.getByRole("menuitem", { name: /Sugerencias de unión/ }));
    expect(await screen.findByText("Sin sugerencias")).toBeInTheDocument();
  });
});
