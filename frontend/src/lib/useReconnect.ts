import { useEffect, useRef } from "react";
import { onEvent } from "@/api";

/**
 * Llama a `cb` cada vez que el WebSocket vuelve tras una caída o tras re-apuntar al servidor local reiniciado:
 * el servidor no reenvía lo perdido, así que cada pantalla vuelve a pedir lo suyo. Siempre usa la versión más
 * reciente de `cb` (no hace falta memoizarla).
 */
export function useReloadOnReconnect(cb: () => void): void {
  const ref = useRef(cb);
  ref.current = cb;
  useEffect(
    () =>
      onEvent("connection", (ev) => {
        if (ev.state === "connected" && ev.reconnected) ref.current();
      }),
    [],
  );
}
