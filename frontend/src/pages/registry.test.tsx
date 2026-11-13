import { act, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

// Cada página real se sustituye por una pequeña que dice si está activa y cuántas veces se montó.
const { mounts, fake } = vi.hoisted(() => {
  const mounts = { n: {} as Record<string, number> };
  const fake = (name: string) => async () => {
    const React = await import("react");
    const { usePageActive } = await import("@/lib/pageActive");
    return {
      default: function Fake() {
        const active = usePageActive();
        React.useEffect(() => {
          mounts.n[name] = (mounts.n[name] ?? 0) + 1;
        }, []);
        return React.createElement("div", { "data-testid": name, "data-active": String(active) });
      },
    };
  };
  return { mounts, fake };
});
vi.mock("./ChatsPage", fake("chats"));
vi.mock("./AccountsPage", fake("accounts"));
vi.mock("./StatusesPage", fake("statuses"));
vi.mock("./CampaignsPage", fake("campaigns"));
vi.mock("./ScheduledPage", fake("scheduled"));
vi.mock("./ContactsPage", fake("contacts"));
vi.mock("./StatsPage", fake("stats"));
vi.mock("./DestinationsPage", fake("destinations"));
vi.mock("./SettingsPage", fake("settings"));

import { PageView, preloadPages } from "./registry";

describe("PageView", () => {
  it("mantiene montada, pero oculta, la página visitada y solo monta las que se visitan", async () => {
    const { rerender } = render(<PageView page="chats" />);
    await waitFor(() => expect(screen.getByTestId("chats")).toBeTruthy());
    expect(screen.getByTestId("chats").dataset.active).toBe("true");
    expect(screen.queryByTestId("statuses")).toBeNull(); // no visitada: no se monta (no consulta nada)

    rerender(<PageView page="statuses" />);
    await waitFor(() => expect(screen.getByTestId("statuses")).toBeTruthy());
    // Chats sigue montada, oculta y sabe que no está activa.
    const chats = screen.getByTestId("chats");
    expect(chats.dataset.active).toBe("false");
    expect((chats.parentElement as HTMLElement).hidden).toBe(true);
    expect(screen.getByTestId("statuses").dataset.active).toBe("true");

    // Volver a Chats no la monta otra vez.
    rerender(<PageView page="chats" />);
    expect(screen.getByTestId("chats").dataset.active).toBe("true");
    expect(mounts.n.chats).toBe(1);
  });

  it("desmonta las páginas con temporizador propio al salir (Programados, Ajustes)", async () => {
    const { rerender } = render(<PageView page="scheduled" />);
    await waitFor(() => expect(screen.getByTestId("scheduled")).toBeTruthy());
    rerender(<PageView page="contacts" />);
    await waitFor(() => expect(screen.getByTestId("contacts")).toBeTruthy());
    expect(screen.queryByTestId("scheduled")).toBeNull();
  });
});

describe("preloadPages", () => {
  it("descarga el código de todas las páginas una sola vez y sin montar nada", async () => {
    vi.useFakeTimers();
    try {
      const before = { ...mounts.n };
      preloadPages();
      preloadPages(); // idempotente
      await act(async () => {
        await vi.advanceTimersByTimeAsync(10_000);
      });
      expect(mounts.n).toEqual(before); // cargar el módulo no monta la página
    } finally {
      vi.useRealTimers();
    }
  });
});
