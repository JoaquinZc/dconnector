import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const svc = vi.hoisted(() => ({
  AccountsService: { List: vi.fn() },
  InboxService: {},
  MessagingService: {},
  StatusesService: { Authors: vi.fn() },
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

import type { AccountView, StatusAuthor } from "@/api/types";
import { useAccounts } from "@/store/accounts";
import { useInbox } from "@/store/inbox";
import { useStatuses } from "@/store/statuses";
import { useUI } from "@/store/ui";
import Rail from "./Rail";
import { PAGES } from "./pages";

const view = (id: string, state: string, caps: Record<string, boolean> = { statuses: true }, enabled = true): AccountView =>
  ({
    account: { id, channel: "whatsapp", label: id, color: "#25D366", enabled, selfId: "", selfName: "" },
    status: { accountId: id, state },
    capabilities: caps,
    unread: 0,
  }) as unknown as AccountView;

const author = (accountId: string, authorId: string, unviewed: number): StatusAuthor =>
  ({ accountId, authorId, authorName: authorId, avatarUrl: "", count: 3, unviewed, lastAt: 1 }) as unknown as StatusAuthor;

function seed(views: AccountView[]) {
  const map: Record<string, AccountView> = {};
  views.forEach((v) => (map[v.account.id] = v));
  act(() => useAccounts.setState({ views: map, order: views.map((v) => v.account.id), loaded: true }));
}

const loadAuthors = vi.fn(async () => undefined);

beforeEach(() => {
  loadAuthors.mockClear();
  act(() => {
    useAccounts.setState({ views: {}, order: [], loaded: true });
    useInbox.setState({ unread: { total: 0 } } as never);
    useStatuses.setState({ authors: [], loadAuthors } as never);
    useUI.setState({ page: "chats" });
  });
});

afterEach(cleanup);

describe("Rail", () => {
  it("muestra la etiqueta de cada página y conserva título con atajo", () => {
    render(<Rail />);
    for (const p of PAGES) {
      const btn = screen.getByRole("button", { name: p.label });
      expect(btn.textContent).toContain(p.shortLabel ?? p.label);
      expect(btn.getAttribute("title")).toBe(`${p.label} (Ctrl+${p.shortcut})`);
    }
    expect(screen.getAllByTestId("rail-label")).toHaveLength(9);
    expect(screen.getByTestId("rail-logo").getAttribute("aria-hidden")).toBe("true");
  });

  it("marca la página activa con aria-current y navega al hacer clic", () => {
    render(<Rail />);
    expect(screen.getByRole("button", { name: "Chats" }).getAttribute("aria-current")).toBe("page");
    expect(screen.getByRole("button", { name: "Contactos" }).getAttribute("aria-current")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Contactos" }));
    expect(useUI.getState().page).toBe("contacts");
    expect(screen.getByRole("button", { name: "Contactos" }).getAttribute("aria-current")).toBe("page");
    expect(screen.getByRole("button", { name: "Chats" }).getAttribute("aria-current")).toBeNull();
  });

  it("insignia de Chats con los no leídos y 99+", () => {
    render(<Rail />);
    expect(screen.queryByTestId("rail-badge")).toBeNull();
    act(() => useInbox.setState({ unread: { total: 7 } } as never));
    expect(screen.getByTestId("rail-badge").textContent).toBe("7");
    act(() => useInbox.setState({ unread: { total: 150 } } as never));
    expect(screen.getByTestId("rail-badge").textContent).toBe("99+");
    expect(screen.getByRole("button", { name: "Chats" }).contains(screen.getByTestId("rail-badge"))).toBe(true);
  });

  it("insignia de Estados con la suma de no vistos de todos los autores", () => {
    render(<Rail />);
    expect(screen.queryByTestId("rail-badge")).toBeNull();
    act(() => useStatuses.setState({ authors: [author("a", "x", 2), author("a", "y", 0), author("b", "z", 3)] }));
    const badge = screen.getByTestId("rail-badge");
    expect(badge.textContent).toBe("5");
    expect(screen.getByRole("button", { name: "Estados" }).contains(badge)).toBe(true);
    act(() => useStatuses.setState({ authors: [author("a", "x", 70), author("b", "z", 40)] }));
    expect(screen.getByTestId("rail-badge").textContent).toBe("99+");
  });

  it("las dos insignias conviven", () => {
    act(() => {
      useInbox.setState({ unread: { total: 4 } } as never);
      useStatuses.setState({ authors: [author("a", "x", 2)] });
    });
    render(<Rail />);
    const badges = screen.getAllByTestId("rail-badge");
    expect(badges.map((b) => b.textContent)).toEqual(["4", "2"]);
  });

  it("carga los autores con las cuentas habilitadas que tienen estados", () => {
    seed([
      view("wa-1", "connected"),
      view("wa-2", "connected", { statuses: false }),
      view("wa-3", "connected", { statuses: true }, false),
      view("wa-4", "connected"),
    ]);
    render(<Rail />);
    expect(loadAuthors).toHaveBeenCalledTimes(1);
    expect(loadAuthors).toHaveBeenCalledWith(["wa-1", "wa-4"]);
  });

  it("no pide estados si ninguna cuenta los tiene", () => {
    seed([view("wa-2", "connected", { statuses: false })]);
    render(<Rail />);
    expect(loadAuthors).not.toHaveBeenCalled();
  });

  it("indicador de conexión: nada sin cuentas", () => {
    render(<Rail />);
    expect(screen.queryByTestId("rail-conn")).toBeNull();
    expect(screen.queryByTestId("rail-dot")).toBeNull();
  });

  it.each([
    ["connected", "green", "Conectado"],
    ["connecting", "amber", "Reconectando"],
    ["error", "red", "Sin conexión"],
    ["logged_out", "red", "Sin conexión"],
  ])("indicador de conexión: %s -> %s", (state, health, text) => {
    seed([view("wa-1", state)]);
    render(<Rail />);
    const conn = screen.getByTestId("rail-conn");
    expect(conn.getAttribute("data-health")).toBe(health);
    expect(conn.textContent).toBe(text);
    expect(screen.getByTestId("rail-conn-dot").getAttribute("style")).toContain(
      health === "green" ? "--ok" : health === "amber" ? "--warn" : "--danger",
    );
    expect(screen.getByTestId("rail-dot").getAttribute("data-health")).toBe(health);
  });

  it("el indicador toma el peor estado y cambia en vivo", () => {
    seed([view("wa-1", "connected"), view("wa-2", "connecting")]);
    render(<Rail />);
    expect(screen.getByTestId("rail-conn").textContent).toBe("Reconectando");
    seed([view("wa-1", "connected"), view("wa-2", "connected")]);
    expect(screen.getByTestId("rail-conn").textContent).toBe("Conectado");
  });
});
