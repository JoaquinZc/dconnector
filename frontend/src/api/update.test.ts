import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const d = vi.hoisted(() => ({
  wails: true,
  call: vi.fn(),
  handlers: {} as Record<string, (v: unknown) => void>,
}));

vi.mock("./desktop", () => ({
  hasWails: () => d.wails,
  callWails: (...a: unknown[]) => d.call(...a),
  onDesktopEvent: (name: string, cb: (v: unknown) => void) => {
    d.handlers[name] = cb;
    return () => delete d.handlers[name];
  },
}));

import { UNAVAILABLE, formatMB, isBusy, phaseText, resetUpdateStore, updateApi, updateErrorText, useUpdate, type UpdateStatus } from "./update";

const status = (p: Partial<UpdateStatus> = {}): UpdateStatus => ({
  currentVersion: "0.3.1",
  installKind: "msi",
  canInstall: true,
  lastCheck: 0,
  autoCheck: true,
  state: "idle",
  progress: { bytes: 0, total: 0, phase: "" },
  ...p,
});

beforeEach(() => {
  d.wails = true;
  d.call.mockReset();
  d.handlers = {};
  resetUpdateStore();
});
afterEach(() => resetUpdateStore());

describe("updateApi", () => {
  it("llama por nombre a UpdateService", async () => {
    d.call.mockResolvedValue(status());
    await updateApi.status();
    await updateApi.check();
    await updateApi.apply();
    await updateApi.setAutoCheck(false);
    await updateApi.openReleasePage();
    await updateApi.openUpdatesFolder();
    expect(d.call.mock.calls).toEqual([
      ["Update", "Status"],
      ["Update", "Check"],
      ["Update", "Apply"],
      ["Update", "SetAutoCheck", [false]],
      ["Update", "OpenReleasePage"],
      ["Update", "OpenUpdatesFolder"],
    ]);
  });

  it("fuera de Wails responde «no disponible»", async () => {
    d.wails = false;
    await expect(updateApi.status()).rejects.toThrow(UNAVAILABLE);
    await expect(updateApi.check()).rejects.toThrow(UNAVAILABLE);
    await expect(updateApi.apply()).rejects.toThrow(UNAVAILABLE);
    expect(d.call).not.toHaveBeenCalled();
  });
});

describe("store", () => {
  it("init lee el estado y escucha los eventos", async () => {
    d.call.mockResolvedValue(status({ state: "available", latest: { version: "0.4.0", notes: "", url: "", assetName: "a.msi", sizeBytes: 10 } }));
    useUpdate.getState().init();
    await vi.waitFor(() => expect(useUpdate.getState().status?.latest?.version).toBe("0.4.0"));
    d.handlers["update:state"](status({ state: "downloading" }));
    expect(useUpdate.getState().status?.state).toBe("downloading");
    d.handlers["update:progress"]({ bytes: 5, total: 10, phase: "downloading" });
    expect(useUpdate.getState().status?.progress).toEqual({ bytes: 5, total: 10, phase: "downloading" });
  });

  it("init es idempotente", () => {
    d.call.mockResolvedValue(status());
    useUpdate.getState().init();
    useUpdate.getState().init();
    expect(d.call).toHaveBeenCalledTimes(1);
  });

  it("sin escritorio queda como no disponible", () => {
    d.wails = false;
    useUpdate.getState().init();
    expect(useUpdate.getState()).toMatchObject({ available: false, loaded: true });
  });

  it("dismiss recuerda la versión una sola vez", () => {
    useUpdate.getState().dismiss("0.4.0");
    useUpdate.getState().dismiss("0.4.0");
    expect(useUpdate.getState().dismissed).toEqual(["0.4.0"]);
  });

  it("check guarda el error si falla sin estado de error", async () => {
    d.call.mockImplementation(async (_s: string, m: string) => {
      if (m === "Check") throw new Error("Sin conexión con GitHub");
      return status();
    });
    await useUpdate.getState().check();
    expect(useUpdate.getState().error).toBe("Sin conexión con GitHub");
  });

  it("setAutoCheck cambia al instante y vuelve atrás si falla", async () => {
    useUpdate.setState({ status: status({ autoCheck: true }) });
    d.call.mockRejectedValue(new Error("no se pudo"));
    await useUpdate.getState().setAutoCheck(false);
    expect(useUpdate.getState().status?.autoCheck).toBe(true);
    expect(useUpdate.getState().error).toBe("no se pudo");
  });
});

describe("textos", () => {
  it("formatMB", () => {
    expect(formatMB(0)).toBe("—");
    expect(formatMB(5.5 * 1024 * 1024)).toBe("5,5 MB");
    expect(formatMB(85 * 1024 * 1024)).toBe("85 MB");
  });
  it("phaseText e isBusy", () => {
    expect(phaseText(status({ state: "downloading", progress: { bytes: 1048576, total: 10485760, phase: "downloading" } }))).toBe("Descargando… 1,0 MB de 10 MB");
    expect(phaseText(status({ state: "verifying" }))).toMatch(/Verificando/);
    expect(phaseText(status({ state: "installing" }))).toMatch(/Instalando/);
    expect(isBusy("downloading")).toBe(true);
    expect(isBusy("available")).toBe(false);
    expect(isBusy(undefined)).toBe(false);
  });
  it("updateErrorText entiende el JSON de Wails", () => {
    expect(updateErrorText('{"message":"boom","cause":{"message":"causa real"}}')).toBe("causa real");
    expect(updateErrorText(new Error("x"))).toBe("x");
    expect(updateErrorText(undefined)).toMatch(/inesperado/);
  });
});
