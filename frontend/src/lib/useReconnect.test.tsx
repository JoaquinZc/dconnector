import { act, cleanup, render, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@wailsio/runtime", () => ({ Call: { ByName: vi.fn() }, Events: { On: vi.fn(() => () => undefined), Emit: vi.fn() } }));
const listStates = vi.hoisted(() => vi.fn());
vi.mock("@/api/transport", () => ({
  rpc: vi.fn(async (svc: string, method: string) => (svc === "Destinations" && method === "States" ? listStates() : null)),
  canUseDesktop: () => false,
  desktopCall: vi.fn(),
}));

import { emitEvent } from "@/api/events";
import { useDestinationStates } from "@/components/destinations/useDestinationStates";
import { useReloadOnReconnect } from "./useReconnect";

afterEach(cleanup);

describe("useReloadOnReconnect", () => {
  it("llama solo cuando el WebSocket vuelve tras una caída, con la versión más reciente del callback", () => {
    const first = vi.fn();
    const second = vi.fn();
    const { rerender } = renderHook(({ cb }) => useReloadOnReconnect(cb), { initialProps: { cb: first } });
    act(() => emitEvent("connection", { state: "connected", reconnected: false }));
    act(() => emitEvent("connection", { state: "reconnecting", reconnected: false }));
    expect(first).not.toHaveBeenCalled();
    rerender({ cb: second });
    act(() => emitEvent("connection", { state: "connected", reconnected: true }));
    expect(first).not.toHaveBeenCalled();
    expect(second).toHaveBeenCalledTimes(1);
  });

  it("deja de escuchar al desmontar", () => {
    const cb = vi.fn();
    const { unmount } = renderHook(() => useReloadOnReconnect(cb));
    unmount();
    act(() => emitEvent("connection", { state: "connected", reconnected: true }));
    expect(cb).not.toHaveBeenCalled();
  });
});

describe("estados de destinos", () => {
  it("vuelven a pedirse al reconectar", async () => {
    listStates.mockReturnValue([{ destinationId: "d1", state: "ok" }]);
    let current: Record<string, unknown> = {};
    function Probe() {
      current = useDestinationStates();
      return null;
    }
    render(<Probe />);
    await waitFor(() => expect(Object.keys(current)).toEqual(["d1"]));
    listStates.mockReturnValue([{ destinationId: "d1", state: "ok" }, { destinationId: "d2", state: "error" }]);
    act(() => emitEvent("connection", { state: "connected", reconnected: true }));
    await waitFor(() => expect(Object.keys(current).sort()).toEqual(["d1", "d2"]));
  });
});
