import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const svc = vi.hoisted(() => ({
  AccountsService: { List: vi.fn() },
  InboxService: { OpenChat: vi.fn(), ListChats: vi.fn() },
  MessagingService: {},
  StatusesService: {},
  ContactsService: { List: vi.fn() },
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
import { useAccounts } from "@/store/accounts";
import { useUI } from "@/store/ui";
import NewChatDialog from "./NewChatDialog";
import { accountBlock, contactTargets, numberTargets, phoneCandidate } from "./newChat";
import { chat } from "./testkit";

const C = svc.ContactsService;
const I = svc.InboxService;

const ident = (accountId: string, peer: string, channel = "whatsapp", name = "", phone = ""): Identity =>
  ({ accountId, channel, peerNativeId: peer, displayName: name, phone }) as unknown as Identity;

const contact = (id: string, name: string, p: Partial<Contact> = {}): Contact => ({
  id,
  name,
  phone: "",
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

const view = (id: string, channel: string, label: string, over: { state?: string; enabled?: boolean; caps?: object } = {}): AccountView =>
  ({
    account: { id, channel, label, color: "#25D366", enabled: over.enabled ?? true, selfId: "", selfName: "", selfHandle: "", createdAt: 1 },
    status: { accountId: id, state: over.state ?? "connected" },
    capabilities: { startChat: true, sendText: true, ...(over.caps ?? {}) },
    unread: 0,
  }) as unknown as AccountView;

function setViews(vs: AccountView[]) {
  const views: Record<string, AccountView> = {};
  for (const v of vs) views[String(v.account.id)] = v;
  useAccounts.setState({ views, order: vs.map((v) => String(v.account.id)), loaded: true });
}

beforeEach(() => {
  vi.clearAllMocks();
  useUI.setState({ toasts: [] });
  setViews([
    view("wa-1", "whatsapp", "Ventas"),
    view("wa-2", "whatsapp", "Personal"),
    view("ig-1", "instagram", "Insta", { caps: { startChat: false } }),
    view("tg-1", "telegram", "Soporte", { state: "disconnected" }),
  ]);
  C.List.mockResolvedValue({ contacts: [], total: 0 });
});
afterEach(cleanup);

const ana = contact("c1", "Ana Pérez", { phone: "+593999111222", identities: [ident("wa-1", "593999111222@s.whatsapp.net", "whatsapp", "Ana")] });
const multi = contact("c2", "Beto Ruiz", {
  phone: "+593988000111",
  identities: [ident("wa-1", "593988000111@s.whatsapp.net"), ident("wa-2", "593988000111@s.whatsapp.net"), ident("ig-1", "beto_ig", "instagram")],
});

describe("newChat (lógica)", () => {
  it("motivos: cuenta que no existe, deshabilitada, canal sin iniciar chat y desconectada", () => {
    expect(accountBlock(undefined, "whatsapp")).toBe("La cuenta ya no existe");
    expect(accountBlock(view("a", "whatsapp", "A", { enabled: false }), "whatsapp")).toBe("Cuenta deshabilitada");
    expect(accountBlock(view("a", "instagram", "A", { caps: { startChat: false } }), "instagram")).toBe("Instagram no permite iniciar chats nuevos");
    expect(accountBlock(view("a", "whatsapp", "A", { state: "connecting" }), "whatsapp")).toBe("Cuenta desconectada");
    expect(accountBlock(view("a", "whatsapp", "A"), "whatsapp")).toBe("");
  });

  it("un contacto sin identidades usa su teléfono por las cuentas de WhatsApp; sin teléfono no hay nada", () => {
    const views = useAccounts.getState().views;
    const t = contactTargets({ identities: [], phone: "+593 99 000 1111" }, views);
    expect(t.map((x) => x.accountId).sort()).toEqual(["wa-1", "wa-2"]);
    expect(t[0].peerNativeId).toBe("+593 99 000 1111");
    expect(contactTargets({ identities: [], phone: "" }, views)).toEqual([]);
    expect(numberTargets("+593999", views).every((x) => x.channel === "whatsapp")).toBe(true);
  });

  it("phoneCandidate: números sí, nombres no", () => {
    expect(phoneCandidate("+593 99 123 4567")).toBe("+593 99 123 4567");
    expect(phoneCandidate("0991234567")).toBe("0991234567");
    expect(phoneCandidate("123")).toBeNull();
    expect(phoneCandidate("Ana 12345678")).toBeNull();
    expect(phoneCandidate("")).toBeNull();
  });
});

describe("NewChatDialog", () => {
  it("carga los contactos, busca por texto y muestra nombre, teléfono y la cuenta", async () => {
    C.List.mockImplementation(async (f: { query: string }) =>
      f.query === "ana" ? { contacts: [ana], total: 1 } : { contacts: [ana, multi], total: 2 },
    );
    render(<NewChatDialog onClose={vi.fn()} onOpen={vi.fn()} />);
    expect(await screen.findByText("Ana Pérez")).toBeInTheDocument();
    expect(screen.getByText("Beto Ruiz")).toBeInTheDocument();
    expect(screen.getByText("+593999111222")).toBeInTheDocument();
    expect(C.List.mock.calls[0][0]).toMatchObject({ query: "", offset: 0, limit: 40 });

    fireEvent.change(screen.getByLabelText("Buscar contacto"), { target: { value: "ana" } });
    await waitFor(() => expect(C.List.mock.calls.at(-1)![0]).toMatchObject({ query: "ana", offset: 0 }));
    await waitFor(() => expect(screen.queryByText("Beto Ruiz")).toBeNull());
    expect(screen.getByText("Ana Pérez")).toBeInTheDocument();
  });

  it("elegir un contacto con una sola cuenta: OpenChat, onOpen con la clave y se cierra", async () => {
    C.List.mockResolvedValue({ contacts: [ana], total: 1 });
    I.OpenChat.mockResolvedValue(chat({ key: "wa-1|593999111222@s.whatsapp.net", lastMessageAt: 0 }));
    const onOpen = vi.fn();
    const onClose = vi.fn();
    render(<NewChatDialog onClose={onClose} onOpen={onOpen} />);
    fireEvent.click(await screen.findByRole("button", { name: "Escribir a Ana Pérez con Ventas" }));
    await waitFor(() => expect(onOpen).toHaveBeenCalledWith("wa-1|593999111222@s.whatsapp.net"));
    expect(I.OpenChat).toHaveBeenCalledWith("wa-1", "593999111222@s.whatsapp.net");
    expect(onClose).toHaveBeenCalled();
  });

  it("con varias identidades se elige con qué cuenta escribir; las que no pueden salen deshabilitadas con el motivo", async () => {
    C.List.mockResolvedValue({ contacts: [multi], total: 1 });
    I.OpenChat.mockResolvedValue(chat({ key: "wa-2|593988000111@s.whatsapp.net" }));
    const onOpen = vi.fn();
    render(<NewChatDialog onClose={vi.fn()} onOpen={onOpen} />);
    const row = await screen.findByTestId("new-chat-row");
    expect(within(row).getAllByRole("button")).toHaveLength(3);

    const ig = within(row).getByRole("button", { name: "Escribir a Beto Ruiz con Insta" });
    expect(ig).toBeDisabled();
    expect(within(ig).getByText(/Instagram no permite iniciar chats nuevos/)).toBeInTheDocument();
    fireEvent.click(ig);
    expect(I.OpenChat).not.toHaveBeenCalled();

    fireEvent.click(within(row).getByRole("button", { name: "Escribir a Beto Ruiz con Personal" }));
    await waitFor(() => expect(I.OpenChat).toHaveBeenCalledWith("wa-2", "593988000111@s.whatsapp.net"));
    await waitFor(() => expect(onOpen).toHaveBeenCalledWith("wa-2|593988000111@s.whatsapp.net"));
  });

  it("cuenta desconectada: deshabilitada con el motivo", async () => {
    C.List.mockResolvedValue({ contacts: [contact("c3", "Carla", { identities: [ident("tg-1", "u:5", "telegram")] })], total: 1 });
    render(<NewChatDialog onClose={vi.fn()} onOpen={vi.fn()} />);
    const btn = await screen.findByRole("button", { name: "Escribir a Carla con Soporte" });
    expect(btn).toBeDisabled();
    expect(within(btn).getByText(/Cuenta desconectada/)).toBeInTheDocument();
  });

  it("si OpenChat falla el diálogo sigue abierto y no se abre nada", async () => {
    C.List.mockResolvedValue({ contacts: [ana], total: 1 });
    I.OpenChat.mockRejectedValue(new Error("La cuenta no está conectada"));
    const onOpen = vi.fn();
    const onClose = vi.fn();
    render(<NewChatDialog onClose={onClose} onOpen={onOpen} />);
    fireEvent.click(await screen.findByRole("button", { name: "Escribir a Ana Pérez con Ventas" }));
    await waitFor(() => expect(I.OpenChat).toHaveBeenCalled());
    await waitFor(() => expect(useUI.getState().toasts.length).toBeGreaterThan(0));
    expect(onOpen).not.toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled();
    await waitFor(() => expect(screen.getByRole("button", { name: "Escribir a Ana Pérez con Ventas" })).not.toBeDisabled());
  });

  it("un número que no está en los contactos se ofrece por cada cuenta de WhatsApp", async () => {
    I.OpenChat.mockResolvedValue(chat({ key: "wa-1|593991234567@s.whatsapp.net" }));
    const onOpen = vi.fn();
    render(<NewChatDialog onClose={vi.fn()} onOpen={onOpen} />);
    fireEvent.change(screen.getByLabelText("Buscar contacto"), { target: { value: "+593 99 123 4567" } });
    const section = await screen.findByRole("region", { name: "Escribir a un número" });
    expect(within(section).getAllByRole("button")).toHaveLength(2); // solo WhatsApp: wa-1 y wa-2
    fireEvent.click(within(section).getByRole("button", { name: "Escribir al número +593 99 123 4567 con Ventas" }));
    await waitFor(() => expect(I.OpenChat).toHaveBeenCalledWith("wa-1", "+593 99 123 4567"));
    await waitFor(() => expect(onOpen).toHaveBeenCalledWith("wa-1|593991234567@s.whatsapp.net"));
  });

  it("sin contactos lo dice", async () => {
    render(<NewChatDialog onClose={vi.fn()} onOpen={vi.fn()} />);
    expect(await screen.findByText("Aún no tienes contactos")).toBeInTheDocument();
  });

  it("la lista pagina con «Mostrar más contactos»", async () => {
    const page = (from: number, n: number) =>
      Array.from({ length: n }, (_, i) => contact(`k${from + i}`, `Persona ${from + i}`, { identities: [ident("wa-1", `p${from + i}`)] }));
    C.List.mockImplementation(async (f: { offset: number }) => ({ contacts: f.offset === 0 ? page(0, 40) : page(40, 10), total: 50 }));
    render(<NewChatDialog onClose={vi.fn()} onOpen={vi.fn()} />);
    expect(await screen.findByText("Persona 0")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Mostrar más contactos" }));
    expect(await screen.findByText("Persona 49")).toBeInTheDocument();
    expect(C.List.mock.calls.at(-1)![0]).toMatchObject({ offset: 40 });
    expect(screen.queryByRole("button", { name: "Mostrar más contactos" })).toBeNull();
  });
});
