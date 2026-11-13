import { useState } from "react";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
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
import AccountFilter from "./AccountFilter";
import { EMPTY_SELECTION, type AccountLite, type AccountSelection } from "./accountSelection";
import ChatSidebar from "./ChatSidebar";

const ACCOUNTS: AccountLite[] = [
  { id: "wa-1", label: "Personal", color: "#1a8f4f", channel: "whatsapp" },
  { id: "wa-2", label: "Tienda", color: "#d83a52", channel: "whatsapp" },
  { id: "ig-1", label: "Tienda", color: "#d83a52", channel: "instagram" },
  { id: "tg-1", label: "Soporte", color: "#1e88c9", channel: "telegram" },
];

/** Dueño del estado, como en la barra lateral. */
function Harness({ accounts = ACCOUNTS, onChange }: { accounts?: AccountLite[]; onChange?: (s: AccountSelection) => void }) {
  const [sel, setSel] = useState<AccountSelection>(EMPTY_SELECTION);
  return (
    <div>
      <AccountFilter
        accounts={accounts}
        selection={sel}
        onChange={(s) => {
          setSel(s);
          onChange?.(s);
        }}
      />
      <button type="button">fuera</button>
    </div>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  svc.InboxService.ListChats.mockResolvedValue([]);
  svc.ContactsService.List.mockResolvedValue({ contacts: [], total: 0 });
  useAccounts.setState({ views: {}, order: [], loaded: true });
  useInbox.setState({ chats: {}, order: [], filter: DEFAULT_FILTER, loading: false, hasMore: false, unread: { total: 0, byAccount: {} } });
});
afterEach(cleanup);

describe("AccountFilter: chips de canal", () => {
  it("sin cuentas no se muestra la fila", () => {
    const { container } = render(<Harness accounts={[]} />);
    expect(container.querySelector('[aria-label="Filtrar por canal"]')).toBeNull();
    expect(screen.queryByText("Todas")).toBeNull();
  });

  it("«Todas» activa y un chip con icono por canal presente (sin repetir)", () => {
    render(<Harness />);
    expect(screen.getByRole("button", { name: "Todas" })).toHaveAttribute("aria-pressed", "true");
    for (const name of ["WhatsApp", "Instagram", "Telegram"]) {
      const chip = screen.getByRole("button", { name: name });
      expect(chip).toHaveAttribute("aria-pressed", "false");
      expect(chip.querySelector("svg")).not.toBeNull();
      expect(chip.textContent).toBe(""); // solo icono
    }
    expect(screen.queryByRole("button", { name: "Messenger" })).toBeNull();
    expect(screen.getAllByRole("button", { name: "WhatsApp" })).toHaveLength(1);
  });

  it("clic en un canal lo activa y desactiva «Todas»; otro clic lo quita; «Todas» limpia", () => {
    const onChange = vi.fn();
    render(<Harness onChange={onChange} />);
    const wa = screen.getByRole("button", { name: "WhatsApp" });
    const tg = screen.getByRole("button", { name: "Telegram" });
    fireEvent.click(wa);
    fireEvent.click(tg);
    expect(wa).toHaveAttribute("aria-pressed", "true");
    expect(tg).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "Todas" })).toHaveAttribute("aria-pressed", "false");
    expect(onChange).toHaveBeenLastCalledWith({ channels: ["whatsapp", "telegram"], accounts: [] });
    fireEvent.click(wa);
    expect(wa).toHaveAttribute("aria-pressed", "false");
    fireEvent.click(screen.getByRole("button", { name: "Todas" }));
    expect(tg).toHaveAttribute("aria-pressed", "false");
    expect(screen.getByRole("button", { name: "Todas" })).toHaveAttribute("aria-pressed", "true");
  });

  it("la fila no se desplaza: sin overflow-x ni scrollbar oculta", () => {
    const { container } = render(<Harness />);
    const group = container.querySelector('[aria-label="Filtrar por canal"]') as HTMLElement;
    expect(group.className).not.toContain("overflow-x");
    expect(group.className).not.toContain("scrollbar");
  });
});

describe("AccountFilter: menú «Cuentas»", () => {
  const opener = () => screen.getByRole("button", { name: /Cuentas/ });

  it("abre y cierra con el botón; expone aria-haspopup y aria-expanded", () => {
    render(<Harness />);
    expect(opener()).toHaveAttribute("aria-haspopup", "true");
    expect(opener()).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByRole("group", { name: "Elegir cuentas" })).toBeNull();
    fireEvent.click(opener());
    expect(opener()).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByRole("group", { name: "Elegir cuentas" })).toBeInTheDocument();
    fireEvent.click(opener());
    expect(screen.queryByRole("group", { name: "Elegir cuentas" })).toBeNull();
  });

  it("agrupa por canal con una casilla por cuenta, todas marcadas al inicio", () => {
    render(<Harness />);
    fireEvent.click(opener());
    const menu = screen.getByRole("group", { name: "Elegir cuentas" });
    expect(within(menu).getAllByRole("checkbox")).toHaveLength(4);
    for (const cb of within(menu).getAllByRole("checkbox")) expect(cb).toHaveAttribute("aria-checked", "true");
    const wa = within(menu).getByRole("group", { name: "WhatsApp" });
    expect(within(wa).getAllByRole("checkbox").map((c) => c.getAttribute("aria-label"))).toEqual([
      "Personal (WhatsApp)",
      "Tienda (WhatsApp)",
    ]);
  });

  it("marcar y desmarcar cuentas cambia la selección; la última marcada no se puede quitar", () => {
    const onChange = vi.fn();
    render(<Harness onChange={onChange} />);
    fireEvent.click(opener());
    fireEvent.click(screen.getByRole("checkbox", { name: "Tienda (WhatsApp)" }));
    expect(screen.getByRole("checkbox", { name: "Tienda (WhatsApp)" })).toHaveAttribute("aria-checked", "false");
    expect(screen.getByRole("checkbox", { name: "Personal (WhatsApp)" })).toHaveAttribute("aria-checked", "true");
    // Instagram y Telegram quedan completos (una cuenta cada uno), WhatsApp solo con «Personal»
    expect(onChange).toHaveBeenLastCalledWith({ channels: ["instagram", "telegram"], accounts: ["wa-1"] });
    expect(screen.getByRole("button", { name: "Instagram" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "WhatsApp" })).toHaveAttribute("aria-pressed", "false");
    expect(screen.getByRole("button", { name: "Todas" })).toHaveAttribute("aria-pressed", "false");

    fireEvent.click(screen.getByRole("checkbox", { name: "Tienda (WhatsApp)" })); // vuelve a marcarla
    expect(screen.getByRole("checkbox", { name: "Tienda (WhatsApp)" })).toHaveAttribute("aria-checked", "true");
    fireEvent.click(screen.getByRole("checkbox", { name: "Tienda (Instagram)" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Soporte (Telegram)" }));
    expect(screen.getByRole("button", { name: "WhatsApp" })).toHaveAttribute("aria-pressed", "true");
    // Instagram y Telegram ya están sin marcar: al quitar «Personal» solo queda «Tienda (WhatsApp)»
    fireEvent.click(screen.getByRole("checkbox", { name: "Personal (WhatsApp)" }));
    expect(onChange).toHaveBeenLastCalledWith({ channels: [], accounts: ["wa-2"] });
    const calls = onChange.mock.calls.length;
    fireEvent.click(screen.getByRole("checkbox", { name: "Tienda (WhatsApp)" })); // la última: no se quita
    expect(screen.getByRole("checkbox", { name: "Tienda (WhatsApp)" })).toHaveAttribute("aria-checked", "true");
    expect(onChange.mock.calls.length).toBe(calls);
  });

  it("marcar de nuevo todas las cuentas equivale a «Todas»", () => {
    render(<Harness />);
    fireEvent.click(opener());
    fireEvent.click(screen.getByRole("checkbox", { name: "Soporte (Telegram)" }));
    expect(screen.getByRole("button", { name: "Todas" })).toHaveAttribute("aria-pressed", "false");
    fireEvent.click(screen.getByRole("checkbox", { name: "Soporte (Telegram)" }));
    expect(screen.getByRole("button", { name: "Todas" })).toHaveAttribute("aria-pressed", "true");
  });

  it("se cierra con Escape", () => {
    render(<Harness />);
    fireEvent.click(opener());
    fireEvent.keyDown(document.body, { key: "Escape" });
    expect(screen.queryByRole("group", { name: "Elegir cuentas" })).toBeNull();
    expect(opener()).toHaveAttribute("aria-expanded", "false");
  });

  it("se cierra al hacer clic fuera, pero no al hacer clic dentro", () => {
    render(<Harness />);
    fireEvent.click(opener());
    fireEvent.mouseDown(screen.getByRole("checkbox", { name: "Personal (WhatsApp)" }));
    expect(screen.getByRole("group", { name: "Elegir cuentas" })).toBeInTheDocument();
    fireEvent.mouseDown(screen.getByRole("button", { name: "fuera" }));
    expect(screen.queryByRole("group", { name: "Elegir cuentas" })).toBeNull();
  });

  it("«Listo» cierra el menú y conserva la selección", () => {
    render(<Harness />);
    fireEvent.click(opener());
    fireEvent.click(screen.getByRole("checkbox", { name: "Soporte (Telegram)" }));
    fireEvent.click(screen.getByRole("button", { name: "Listo" }));
    expect(screen.queryByRole("group", { name: "Elegir cuentas" })).toBeNull();
    expect(screen.getByRole("button", { name: "Todas" })).toHaveAttribute("aria-pressed", "false");
  });
});

describe("ChatSidebar: cabecera y pestañas", () => {
  it("las cuatro pestañas están en una lista de pestañas, sin desplazamiento horizontal", () => {
    render(<ChatSidebar selectedKey={null} onOpen={vi.fn()} />);
    const list = screen.getByRole("tablist", { name: "Vista de chats" });
    expect(list.className).not.toContain("overflow-x");
    expect(list.className).not.toContain("scrollbar");
    const tabs = within(list).getAllByRole("tab");
    expect(tabs.map((t) => t.textContent)).toEqual(["Todos", "No leídos", "Grupos", "Archivados"]);
    expect(tabs[0]).toHaveAttribute("aria-selected", "true");
    expect(tabs.slice(1).every((t) => t.getAttribute("aria-selected") === "false")).toBe(true);
  });

  it("cambia de pestaña al pulsar y marca la activa", () => {
    render(<ChatSidebar selectedKey={null} onOpen={vi.fn()} />);
    fireEvent.click(screen.getByRole("tab", { name: "Archivados" }));
    expect(useInbox.getState().filter.tab).toBe("archived");
    expect(screen.getByRole("tab", { name: "Archivados" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: "Todos" })).toHaveAttribute("aria-selected", "false");
  });

  it("el contador de no leídos es una píldora; con 0 no aparece; 99+ se acorta", () => {
    render(<ChatSidebar selectedKey={null} onOpen={vi.fn()} />);
    expect(screen.queryByTestId("tab-unread-count")).toBeNull();
    act(() => useInbox.setState({ unread: { total: 7, byAccount: {} } }));
    const pill = screen.getByTestId("tab-unread-count");
    expect(pill.textContent).toBe("7");
    expect(pill.className).toContain("bg-[var(--badge-bg)]");
    expect(within(screen.getByRole("tab", { name: /No leídos/ })).getByTestId("tab-unread-count")).toBe(pill);
    act(() => useInbox.setState({ unread: { total: 250, byAccount: {} } }));
    expect(screen.getByTestId("tab-unread-count").textContent).toBe("99+");
  });

  it("el botón «Nuevo chat» es cuadrado, con fondo de acento, y está en la cabecera junto al título", () => {
    render(<ChatSidebar selectedKey={null} onOpen={vi.fn()} />);
    const btn = screen.getAllByRole("button", { name: "Nuevo chat" })[0];
    expect(btn.className).toContain("h-[38px]");
    expect(btn.className).toContain("w-[38px]");
    expect(btn.className).toContain("bg-[var(--accent)]");
    expect(screen.getByRole("heading", { name: "Chats" }).parentElement).toContainElement(btn);
  });

  it("el buscador ocupa su propia fila y es accesible", () => {
    render(<ChatSidebar selectedKey={null} onOpen={vi.fn()} />);
    const input = screen.getByRole("textbox", { name: "Buscar chat" });
    expect(input).toBeInTheDocument();
    expect(input.closest("header")).toBeNull();
  });

  it("con cuentas conectadas muestra la fila de canales y el menú de cuentas", () => {
    useAccounts.setState({
      loaded: true,
      order: ["wa-1", "tg-1"],
      views: {
        "wa-1": { account: { id: "wa-1", label: "Personal", color: "#1a8f4f", channel: "whatsapp" } },
        "tg-1": { account: { id: "tg-1", label: "Soporte", color: "#1e88c9", channel: "telegram" } },
      } as never,
    });
    render(<ChatSidebar selectedKey={null} onOpen={vi.fn()} />);
    expect(screen.getByRole("button", { name: "WhatsApp" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Telegram" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Cuentas/ }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Soporte (Telegram)" }));
    // la selección se guarda para la próxima vez
    expect(JSON.parse(localStorage.getItem("dc:chats:accountFilter") ?? "{}")).toEqual({ channels: ["whatsapp"], accounts: [] });
  });
});
