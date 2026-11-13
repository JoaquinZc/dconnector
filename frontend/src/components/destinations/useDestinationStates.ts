import { useCallback, useEffect, useState } from "react";
import { api, onEvent } from "@/api";
import { useReloadOnReconnect } from "@/lib/useReconnect";
import { applyState, indexStates, type StateMap } from "./destinationModel";

/** Estado en vivo de los destinos: `States()` al cargar y `destination:state` después. */
export function useDestinationStates(): StateMap {
  const [states, setStates] = useState<StateMap>({});

  useEffect(() => {
    let alive = true;
    // La suscripción va primero para no perder un cambio que llegue mientras carga `States()`.
    const live: StateMap = {};
    const off = onEvent("destination:state", (s) => {
      live[s.destinationId] = s;
      setStates((cur) => applyState(cur, s));
    });
    api.destinations
      .States()
      .then((list) => alive && setStates({ ...indexStates(list), ...live }))
      .catch(() => undefined);
    return () => {
      alive = false;
      off();
    };
  }, []);

  // Tras reconectar se vuelven a pedir los estados (los cambios perdidos no se reenvían).
  const refresh = useCallback(() => {
    api.destinations
      .States()
      .then((list) => setStates((cur) => ({ ...cur, ...indexStates(list) })))
      .catch(() => undefined);
  }, []);
  useReloadOnReconnect(refresh);

  return states;
}
