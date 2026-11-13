import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const svc = vi.hoisted(() => ({
  AccountsService: { List: vi.fn() },
  InboxService: { ListChats: vi.fn(), UnreadTotals: vi.fn(), OpenChat: vi.fn() },
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

import { useAccounts } from "@/store/accounts";
import { DEFAULT_FILTER, useInbox } from "@/store/inbox";
import ChatList from "./ChatList";
import ChatSidebar from "./ChatSidebar";

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  svc.InboxService.ListChats.mockResolvedValue([]);
  svc.ContactsService.List.mockResolvedValue({ contacts: [], total: 0 });
  useAccounts.setState({ views: {}, order: [], loaded: true });
  useInbox.setState({ chats: {}, order: [], filter: DEFAULT_FILTER, loading: false, hasMore: false });
});
afterEach(cleanup);

describe("lista de chats vacía", () => {
  it("sin actividad: explica cómo empezar y ofrece «Nuevo chat»", () => {
    const onNewChat = vi.fn();
    render(<ChatList selectedKey={null} accounts={{}} onOpen={vi.fn()} onNewChat={onNewChat} />);
    expect(screen.getByText("Aún no hay conversaciones")).toBeInTheDocument();
    expect(screen.getByText("Pulsa «Nuevo chat» para escribir a un contacto")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Nuevo chat" }));
    expect(onNewChat).toHaveBeenCalled();
  });

  it("con una búsqueda o en otra pestaña solo dice «Sin chats»", () => {
    useInbox.setState({ filter: { ...DEFAULT_FILTER, query: "zzz" } });
    const { unmount } = render(<ChatList selectedKey={null} accounts={{}} onOpen={vi.fn()} onNewChat={vi.fn()} />);
    expect(screen.getByText("Sin chats")).toBeInTheDocument();
    unmount();
    useInbox.setState({ filter: { ...DEFAULT_FILTER, tab: "unread" } });
    render(<ChatList selectedKey={null} accounts={{}} onOpen={vi.fn()} onNewChat={vi.fn()} />);
    expect(screen.getByText("Sin chats")).toBeInTheDocument();
  });

  it("cargando no muestra el mensaje de «aún no hay»", () => {
    useInbox.setState({ loading: true });
    render(<ChatList selectedKey={null} accounts={{}} onOpen={vi.fn()} onNewChat={vi.fn()} />);
    expect(screen.getByText("Cargando…")).toBeInTheDocument();
    expect(screen.queryByText("Aún no hay conversaciones")).toBeNull();
  });
});

describe("ChatSidebar: botón «Nuevo chat»", () => {
  it("junto al buscador abre el diálogo de nuevo chat", async () => {
    render(<ChatSidebar selectedKey={null} onOpen={vi.fn()} />);
    // el botón junto al buscador (el del estado vacío tiene el mismo nombre accesible)
    const buttons = screen.getAllByRole("button", { name: "Nuevo chat" });
    fireEvent.click(buttons[0]);
    expect(await screen.findByRole("dialog", { name: "Nuevo chat" })).toBeInTheDocument();
    expect(screen.getByLabelText("Buscar contacto")).toBeInTheDocument();
  });
});
