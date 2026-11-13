import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const n = vi.hoisted(() => ({ Status: vi.fn(), MarkAsked: vi.fn(), OpenSystemSettings: vi.fn() }));

vi.mock("@/api", () => ({ api: { notifications: n } }));

import NotifyNotice from "./NotifyNotice";

const status = (p: object) => ({ state: "granted", reason: "", listening: true, asked: false, ...p });

beforeEach(() => {
  n.Status.mockReset();
  n.MarkAsked.mockReset().mockResolvedValue(undefined);
  n.OpenSystemSettings.mockReset().mockResolvedValue(undefined);
});
afterEach(cleanup);

describe("NotifyNotice (primer arranque)", () => {
  it("bloqueado y sin preguntar: avisa una vez, lo anota y ofrece abrir Windows", async () => {
    n.Status.mockResolvedValue(status({ state: "denied", reason: "Las notificaciones de dConnector están bloqueadas en Windows." }));
    render(<NotifyNotice />);
    expect(await screen.findByTestId("notify-notice")).toHaveTextContent("bloqueadas en Windows");
    expect(n.MarkAsked).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole("button", { name: "Abrir ajustes de Windows" }));
    expect(n.OpenSystemSettings).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(screen.queryByTestId("notify-notice")).toBeNull());
  });

  it("se puede cerrar", async () => {
    n.Status.mockResolvedValue(status({ state: "denied", reason: "Bloqueadas." }));
    render(<NotifyNotice />);
    await screen.findByTestId("notify-notice");
    fireEvent.click(screen.getByRole("button", { name: "Cerrar aviso" }));
    expect(screen.queryByTestId("notify-notice")).toBeNull();
  });

  it("concedido: lo anota y no muestra nada", async () => {
    n.Status.mockResolvedValue(status({}));
    render(<NotifyNotice />);
    await waitFor(() => expect(n.MarkAsked).toHaveBeenCalledTimes(1));
    expect(screen.queryByTestId("notify-notice")).toBeNull();
  });

  it("ya preguntado: no hace nada más aunque esté bloqueado", async () => {
    n.Status.mockResolvedValue(status({ state: "denied", reason: "x", asked: true }));
    render(<NotifyNotice />);
    await waitFor(() => expect(n.Status).toHaveBeenCalledTimes(1));
    expect(n.MarkAsked).not.toHaveBeenCalled();
    expect(screen.queryByTestId("notify-notice")).toBeNull();
  });

  it("si el estado no se puede leer no molesta ni lo anota", async () => {
    n.Status.mockRejectedValue(new Error("sin servicio"));
    render(<NotifyNotice />);
    await waitFor(() => expect(n.Status).toHaveBeenCalledTimes(1));
    expect(n.MarkAsked).not.toHaveBeenCalled();
    expect(screen.queryByTestId("notify-notice")).toBeNull();
  });
});
