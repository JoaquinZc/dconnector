import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const d = vi.hoisted(() => ({ wails: true, call: vi.fn(), handlers: {} as Record<string, (v: unknown) => void> }));

vi.mock("@/api/desktop", () => ({
  hasWails: () => d.wails,
  callWails: (...a: unknown[]) => d.call(...a),
  onDesktopEvent: (name: string, cb: (v: unknown) => void) => {
    d.handlers[name] = cb;
    return () => undefined;
  },
}));

import { resetUpdateStore, type UpdateStatus } from "@/api/update";
import UpdateNotice from "./UpdateNotice";

const latest = { version: "0.4.0", notes: "", url: "https://github.com/x/y/releases/tag/v0.4.0", assetName: "a.msi", sizeBytes: 1000 };
const st = (p: Partial<UpdateStatus> = {}): UpdateStatus => ({
  currentVersion: "0.3.1",
  installKind: "msi",
  canInstall: true,
  lastCheck: 0,
  autoCheck: true,
  state: "available",
  latest,
  progress: { bytes: 0, total: 0, phase: "" },
  ...p,
});

function setup(status: UpdateStatus) {
  d.call.mockImplementation(async (_s: string, m: string) => (m === "Status" ? status : undefined));
  return render(<UpdateNotice />);
}

beforeEach(() => {
  d.wails = true;
  d.call.mockReset();
  d.handlers = {};
  resetUpdateStore();
});
afterEach(() => {
  cleanup();
  resetUpdateStore();
});

describe("UpdateNotice", () => {
  it("avisa de la versión nueva con «Actualizar» y «Más tarde»", async () => {
    setup(st());
    expect(await screen.findByTestId("update-notice")).toHaveTextContent("Hay una versión nueva (0.4.0)");
    fireEvent.click(screen.getByRole("button", { name: "Actualizar" }));
    await waitFor(() => expect(d.call).toHaveBeenCalledWith("Update", "Apply"));
  });

  it("«Más tarde» lo cierra y no vuelve a salir para esa versión en la sesión", async () => {
    setup(st());
    fireEvent.click(await screen.findByRole("button", { name: "Más tarde" }));
    expect(screen.queryByTestId("update-notice")).toBeNull();
    act(() => d.handlers["update:state"](st({ lastCheck: 5 })));
    expect(screen.queryByTestId("update-notice")).toBeNull();
    // Una versión aún más nueva sí avisa.
    act(() => d.handlers["update:state"](st({ latest: { ...latest, version: "0.5.0" } })));
    expect(await screen.findByTestId("update-notice")).toHaveTextContent("0.5.0");
  });

  it("no sale si está al día, si no hay escritorio o mientras busca", async () => {
    setup(st({ state: "uptodate", latest: null }));
    await waitFor(() => expect(d.call).toHaveBeenCalled());
    expect(screen.queryByTestId("update-notice")).toBeNull();
    cleanup();
    resetUpdateStore();
    d.wails = false;
    render(<UpdateNotice />);
    expect(screen.queryByTestId("update-notice")).toBeNull();
  });

  it("al actualizar muestra la fase y oculta los botones", async () => {
    setup(st());
    await screen.findByTestId("update-notice");
    act(() => d.handlers["update:state"](st({ state: "downloading", progress: { bytes: 500, total: 1000, phase: "downloading" } })));
    expect(await screen.findByTestId("update-notice-phase")).toHaveTextContent("Descargando");
    expect(screen.queryByRole("button", { name: "Más tarde" })).toBeNull();
    expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "50");
  });

  it("si falla, dice el motivo y ofrece reintentar", async () => {
    setup(st({ state: "error", error: "Se cortó la descarga." }));
    expect(await screen.findByTestId("update-notice-error")).toHaveTextContent("Se cortó la descarga.");
    expect(screen.getByRole("button", { name: "Reintentar" })).toBeInTheDocument();
  });

  it("copia que no se puede instalar: «Ver versión» abre la página", async () => {
    setup(st({ canInstall: false, installKind: "other" }));
    fireEvent.click(await screen.findByRole("button", { name: "Ver versión" }));
    await waitFor(() => expect(d.call).toHaveBeenCalledWith("Update", "OpenReleasePage"));
  });
});
