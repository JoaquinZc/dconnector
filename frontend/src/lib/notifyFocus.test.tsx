import { cleanup, fireEvent, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const setFocus = vi.hoisted(() => vi.fn());

vi.mock("@/api", () => ({ api: { notifications: { SetFocus: setFocus } } }));

import { reportFocus, useNotifyFocus, windowIsFocused } from "./notifyFocus";

function Probe({ chatKey }: { chatKey: string | null }) {
  useNotifyFocus(chatKey);
  return null;
}

let hasFocus = true;
const setVisibility = (v: "visible" | "hidden") =>
  Object.defineProperty(document, "visibilityState", { configurable: true, get: () => v });

beforeEach(() => {
  setFocus.mockReset().mockResolvedValue(undefined);
  hasFocus = true;
  vi.spyOn(document, "hasFocus").mockImplementation(() => hasFocus);
  setVisibility("visible");
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("notifyFocus", () => {
  it("windowIsFocused exige ventana visible y con foco", () => {
    expect(windowIsFocused()).toBe(true);
    hasFocus = false;
    expect(windowIsFocused()).toBe(false);
    hasFocus = true;
    setVisibility("hidden");
    expect(windowIsFocused()).toBe(false);
  });

  it("al montar informa del chat abierto y del foco", () => {
    render(<Probe chatKey="wa-1|123" />);
    expect(setFocus).toHaveBeenLastCalledWith("wa-1|123", true);
  });

  it("al cambiar de chat informa del nuevo", () => {
    const { rerender } = render(<Probe chatKey="wa-1|123" />);
    rerender(<Probe chatKey="wa-1|456" />);
    expect(setFocus).toHaveBeenLastCalledWith("wa-1|456", true);
  });

  it("sin chat abierto manda la clave vacía", () => {
    render(<Probe chatKey={null} />);
    expect(setFocus).toHaveBeenLastCalledWith("", true);
  });

  it("blur y focus de la ventana, y visibilitychange, vuelven a informar", () => {
    render(<Probe chatKey="wa-1|123" />);
    hasFocus = false;
    fireEvent.blur(window);
    expect(setFocus).toHaveBeenLastCalledWith("wa-1|123", false);
    hasFocus = true;
    fireEvent.focus(window);
    expect(setFocus).toHaveBeenLastCalledWith("wa-1|123", true);
    setVisibility("hidden");
    document.dispatchEvent(new Event("visibilitychange"));
    expect(setFocus).toHaveBeenLastCalledWith("wa-1|123", false);
  });

  it("al desmontar avisa de que ya no hay chat abierto y deja de escuchar", () => {
    const { unmount } = render(<Probe chatKey="wa-1|123" />);
    unmount();
    expect(setFocus).toHaveBeenLastCalledWith("", false);
    const calls = setFocus.mock.calls.length;
    fireEvent.focus(window);
    expect(setFocus.mock.calls.length).toBe(calls);
  });

  it("un fallo del servicio no rompe nada", () => {
    setFocus.mockRejectedValue(new Error("x"));
    expect(() => reportFocus("wa-1|1", true)).not.toThrow();
    setFocus.mockImplementation(() => {
      throw new Error("sin enlaces");
    });
    expect(() => reportFocus("wa-1|1", true)).not.toThrow();
  });
});
