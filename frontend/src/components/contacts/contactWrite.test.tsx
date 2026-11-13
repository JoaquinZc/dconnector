import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";

const svc = vi.hoisted(() => ({
  AccountsService: { List: vi.fn() },
  InboxService: { OpenChat: vi.fn() },
  MessagingService: {},
  StatusesService: {},
  ContactsService: { List: vi.fn(), Lists: vi.fn(), Get: vi.fn(), Save: vi.fn() },
  CampaignsService: {},
  ScheduledService: {},
  StatsService: {},
  DestinationsService: {},
  SettingsService: {},
  FilesService: {},
}));

vi.mock("@/api/transport", async () => (await import("@/test/svcTransport")).svcTransport(svc));
vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(() => () => undefined) } }));

import type { AccountView, Contact, Identity } from "@/api/types";
import ContactsPage from "@/pages/ContactsPage";
import { useAccounts } from "@/store/accounts";
import { useUI } from "@/store/ui";
import ContactPanel from "./ContactPanel";

const C = svc.ContactsService;
const I = svc.InboxService;

const ident = (accountId: string, peer: string, channel = "whatsapp"): Identity =>
  ({ accountId, channel, peerNativeId: peer, displayName: "", phone: "" }) as unknown as Identity;

const contact = (id: string, name: string, p: Partial<Contact> = {}): Contact => ({
  id,
  name,
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

const view = (id: string, channel: string, label: string, state = "connected"): AccountView =>
  ({
    account: { id, channel, label, color: "#25D366", enabled: true, selfId: "", selfName: "", selfHandle: "", createdAt: 1 },
    status: { accountId: id, state },
    capabilities: { startChat: true, sendText: true },
    unread: 0,
  }) as unknown as AccountView;

// jsdom no tiene diseño: sin esto la tabla virtualizada no pinta filas.
beforeAll(() => {
  Object.defineProperty(HTMLElement.prototype, "clientHeight", { configurable: true, get: () => 600 });
  Object.defineProperty(HTMLElement.prototype, "clientWidth", { configurable: true, get: () => 900 });
});

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  useUI.setState({ toasts: [], page: "contacts", openChatKey: null });
  useAccounts.setState({
    views: { "wa-1": view("wa-1", "whatsapp", "Ventas"), "tg-1": view("tg-1", "telegram", "Soporte", "disconnected") },
    order: ["wa-1", "tg-1"],
    loaded: true,
  });
  C.Lists.mockResolvedValue([]);
  C.List.mockResolvedValue({ contacts: [], total: 0 });
});
afterEach(cleanup);

const props = (c: Contact) => ({
  contact: c,
  lists: [],
  onClose: vi.fn(),
  onChanged: vi.fn(),
  onDeleted: vi.fn(),
  onMerge: vi.fn(),
});

describe("botón «Escribir» de la ficha del contacto", () => {
  it("con una sola cuenta: OpenChat y va a Chats con el chat abierto", async () => {
    I.OpenChat.mockResolvedValue({ key: "wa-1|593@s.whatsapp.net" });
    render(<ContactPanel {...props(contact("c1", "Ana", { identities: [ident("wa-1", "593@s.whatsapp.net")] }))} />);
    fireEvent.click(screen.getByRole("button", { name: "Escribir" }));
    await waitFor(() => expect(I.OpenChat).toHaveBeenCalledWith("wa-1", "593@s.whatsapp.net"));
    await waitFor(() => expect(useUI.getState().openChatKey).toBe("wa-1|593@s.whatsapp.net"));
    expect(useUI.getState().page).toBe("chats");
  });

  it("con varias identidades ofrece un menú para elegir la cuenta (la desconectada, deshabilitada con motivo)", async () => {
    I.OpenChat.mockResolvedValue({ key: "wa-1|593@s.whatsapp.net" });
    render(
      <ContactPanel
        {...props(contact("c1", "Ana", { identities: [ident("wa-1", "593@s.whatsapp.net"), ident("tg-1", "u:77", "telegram")] }))}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Escribir" }));
    expect(I.OpenChat).not.toHaveBeenCalled();
    const menu = await screen.findByRole("menu");
    const tg = within(menu).getByRole("menuitem", { name: /Soporte/ });
    expect(tg).toBeDisabled();
    expect(tg.textContent).toContain("Cuenta desconectada");
    fireEvent.click(within(menu).getByRole("menuitem", { name: /Ventas/ }));
    await waitFor(() => expect(I.OpenChat).toHaveBeenCalledWith("wa-1", "593@s.whatsapp.net"));
    await waitFor(() => expect(useUI.getState().openChatKey).toBe("wa-1|593@s.whatsapp.net"));
  });

  it("un contacto solo con teléfono se escribe por WhatsApp con ese número", async () => {
    I.OpenChat.mockResolvedValue({ key: "wa-1|593999000111@s.whatsapp.net" });
    render(<ContactPanel {...props(contact("c2", "Beto"))} />);
    fireEvent.click(screen.getByRole("button", { name: "Escribir" }));
    await waitFor(() => expect(I.OpenChat).toHaveBeenCalledWith("wa-1", "+593999000111"));
  });

  it("sin identidades ni teléfono, o con la única cuenta desconectada: deshabilitado con el motivo", () => {
    const { unmount } = render(<ContactPanel {...props(contact("c3", "Sin datos", { phone: "" }))} />);
    const btn = screen.getByRole("button", { name: "Escribir" });
    expect(btn).toBeDisabled();
    expect(btn).toHaveAttribute("title", "Sin cuenta ni teléfono para escribir");
    unmount();
    render(<ContactPanel {...props(contact("c4", "Dani", { identities: [ident("tg-1", "u:1", "telegram")] }))} />);
    const b2 = screen.getByRole("button", { name: "Escribir" });
    expect(b2).toBeDisabled();
    expect(b2).toHaveAttribute("title", "Cuenta desconectada");
  });

  it("si OpenChat falla no cambia de pantalla", async () => {
    I.OpenChat.mockRejectedValue(new Error("no se pudo"));
    render(<ContactPanel {...props(contact("c1", "Ana", { identities: [ident("wa-1", "593@s.whatsapp.net")] }))} />);
    fireEvent.click(screen.getByRole("button", { name: "Escribir" }));
    await waitFor(() => expect(I.OpenChat).toHaveBeenCalled());
    await waitFor(() => expect(useUI.getState().toasts.length).toBeGreaterThan(0));
    expect(useUI.getState().page).toBe("contacts");
    expect(useUI.getState().openChatKey).toBeNull();
  });
});

describe("Contactos: fila → ficha → «Escribir»", () => {
  it("desde la pantalla de contactos abre el chat en Chats", async () => {
    C.List.mockResolvedValue({ contacts: [contact("c1", "Ana Pérez", { identities: [ident("wa-1", "593@s.whatsapp.net")] })], total: 1 });
    I.OpenChat.mockResolvedValue({ key: "wa-1|593@s.whatsapp.net" });
    render(<ContactsPage />);
    fireEvent.click(await screen.findByTestId("contact-row"));
    fireEvent.click(await screen.findByRole("button", { name: "Escribir" }));
    await waitFor(() => expect(I.OpenChat).toHaveBeenCalledWith("wa-1", "593@s.whatsapp.net"));
    await waitFor(() => expect(useUI.getState().page).toBe("chats"));
    expect(useUI.getState().openChatKey).toBe("wa-1|593@s.whatsapp.net");
  });
});
