import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
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
import UpdatesSection, { noteExcerpt } from "./UpdatesSection";

const st = (p: Partial<UpdateStatus> = {}): UpdateStatus => ({
  currentVersion: "0.3.1",
  installKind: "msi",
  canInstall: true,
  lastCheck: 0,
  autoCheck: true,
  state: "idle",
  progress: { bytes: 0, total: 0, phase: "" },
  ...p,
});
const latest = { version: "0.4.0", notes: "Novedades\n- uno\n- dos", url: "https://github.com/x/y/releases/tag/v0.4.0", assetName: "dConnector-0.4.0-x64.msi", sizeBytes: 90 * 1024 * 1024 };

function setup(status: UpdateStatus, extra?: (m: string) => unknown) {
  d.call.mockImplementation(async (_s: string, m: string) => {
    const r = extra?.(m);
    if (r !== undefined) return r;
    return m === "Status" || m === "Check" ? status : undefined;
  });
  return render(<UpdatesSection />);
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

describe("UpdatesSection", () => {
  it("fuera del escritorio dice que no está disponible", async () => {
    d.wails = false;
    resetUpdateStore();
    render(<UpdatesSection />);
    expect(await screen.findByTestId("upd-unavailable")).toHaveTextContent("solo están disponibles en la aplicación de escritorio");
  });

  it("muestra la versión instalada y «Estás al día»", async () => {
    setup(st({ state: "uptodate", lastCheck: Date.now() }));
    expect(await screen.findByTestId("upd-current")).toHaveTextContent("0.3.1");
    expect(screen.getByTestId("upd-uptodate")).toHaveTextContent("Estás al día");
    expect(screen.queryByTestId("upd-apply")).toBeNull();
  });

  it("versión nueva: notas en texto plano y botón «Actualizar a X»", async () => {
    setup(st({ state: "available", latest: { ...latest, notes: "<img src=x onerror=alert(1)> Hola\n- dos" } }));
    const notes = await screen.findByTestId("upd-notes");
    expect(notes.textContent).toContain("<img src=x onerror=alert(1)>");
    expect(notes.querySelector("img")).toBeNull();
    fireEvent.click(screen.getByTestId("upd-apply"));
    expect(screen.getByTestId("upd-apply")).toHaveTextContent("Actualizar a 0.4.0");
    await waitFor(() => expect(d.call).toHaveBeenCalledWith("Update", "Apply"));
  });

  it("«Buscar actualizaciones» llama a Check", async () => {
    setup(st({ state: "uptodate" }));
    fireEvent.click(await screen.findByTestId("upd-check"));
    await waitFor(() => expect(d.call).toHaveBeenCalledWith("Update", "Check"));
  });

  it("descargando: fase en español y barra de progreso; no se puede volver a buscar", async () => {
    setup(st({ state: "downloading", latest, progress: { bytes: 45 * 1024 * 1024, total: 90 * 1024 * 1024, phase: "downloading" } }));
    expect(await screen.findByTestId("upd-phase")).toHaveTextContent("Descargando… 45 MB de 90 MB");
    expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "50");
    expect(screen.getByTestId("upd-check")).toBeDisabled();
    expect(screen.queryByTestId("upd-apply")).toBeNull();
  });

  it("los eventos actualizan la pantalla", async () => {
    setup(st({ state: "available", latest }));
    await screen.findByTestId("upd-apply");
    d.handlers["update:state"](st({ state: "verifying", latest }));
    expect(await screen.findByTestId("upd-phase")).toHaveTextContent("Verificando");
  });

  it("error: mensaje, «Reintentar» y «Abrir carpeta»", async () => {
    setup(st({ state: "error", latest, error: "La descarga no coincide con la suma de verificación.", folder: "C:\\datos\\updates\\0.4.0" }));
    expect(await screen.findByTestId("upd-error")).toHaveTextContent("no coincide");
    expect(screen.getByTestId("upd-folder")).toHaveTextContent("updates");
    fireEvent.click(screen.getByTestId("upd-open-folder"));
    await waitFor(() => expect(d.call).toHaveBeenCalledWith("Update", "OpenUpdatesFolder"));
    fireEvent.click(screen.getByTestId("upd-retry"));
    await waitFor(() => expect(d.call).toHaveBeenCalledWith("Update", "Apply"));
  });

  it("error sin versión: «Reintentar» vuelve a buscar y no hay carpeta", async () => {
    setup(st({ state: "error", error: "No se pudo conectar con GitHub." }));
    expect(await screen.findByTestId("upd-error")).toHaveTextContent("No se pudo conectar");
    expect(screen.queryByTestId("upd-open-folder")).toBeNull();
    fireEvent.click(screen.getByTestId("upd-retry"));
    await waitFor(() => expect(d.call).toHaveBeenCalledWith("Update", "Check"));
  });

  it("copia que no se puede instalar: ofrece la página de la versión", async () => {
    setup(st({ state: "available", installKind: "other", canInstall: false, latest }));
    expect(await screen.findByTestId("upd-cannot")).toHaveTextContent("no se actualiza desde aquí");
    expect(screen.queryByTestId("upd-apply")).toBeNull();
    fireEvent.click(screen.getByTestId("upd-page"));
    await waitFor(() => expect(d.call).toHaveBeenCalledWith("Update", "OpenReleasePage"));
  });

  it("compilación de desarrollo", async () => {
    setup(st({ currentVersion: "dev", installKind: "other", canInstall: false, state: "available", latest }));
    expect(await screen.findByTestId("upd-cannot")).toHaveTextContent("desarrollo");
  });

  it("el interruptor «Buscar al abrir» guarda el ajuste", async () => {
    setup(st({ state: "idle", autoCheck: true }));
    const sw = await screen.findByRole("switch", { name: "Buscar al abrir" });
    expect(sw).toHaveAttribute("aria-checked", "true");
    fireEvent.click(sw);
    await waitFor(() => expect(d.call).toHaveBeenCalledWith("Update", "SetAutoCheck", [false]));
    expect(screen.getByRole("switch", { name: "Buscar al abrir" })).toHaveAttribute("aria-checked", "false");
  });
});

describe("noteExcerpt", () => {
  it("recorta a las primeras líneas y a un tope de caracteres", () => {
    const many = Array.from({ length: 30 }, (_, i) => `línea ${i}`).join("\n");
    const out = noteExcerpt(many);
    expect(out.split("\n").length).toBeLessThanOrEqual(8);
    expect(out.endsWith("…")).toBe(true);
    expect(noteExcerpt("corto")).toBe("corto");
    expect(noteExcerpt("x".repeat(2000)).length).toBeLessThanOrEqual(701);
  });
});
