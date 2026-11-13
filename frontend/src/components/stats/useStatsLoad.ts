// Pide una estadística al servicio cuando cambian sus parámetros y la deja lista para pintar. Una
// respuesta tardía de una petición anterior se descarta.
import { useEffect, useRef, useState } from "react";

export interface StatsLoad<T> {
  data: T | null;
  loading: boolean;
  failed: boolean;
}

/**
 * `load` hace la llamada; `read` convierte la respuesta cruda. `deps` son los valores de los que depende
 * la petición (rango, cuentas…): cuando cambian, se vuelve a pedir.
 */
export function useStatsLoad<T>(load: () => Promise<unknown>, read: (raw: unknown) => T, deps: unknown[]): StatsLoad<T> {
  const [state, setState] = useState<StatsLoad<T>>({ data: null, loading: true, failed: false });
  const seq = useRef(0);
  const loadRef = useRef(load);
  const readRef = useRef(read);
  loadRef.current = load;
  readRef.current = read;

  useEffect(() => {
    const mine = ++seq.current;
    setState((s) => ({ ...s, loading: true, failed: false }));
    loadRef
      .current()
      .then((raw) => {
        if (mine === seq.current) setState({ data: readRef.current(raw), loading: false, failed: false });
      })
      .catch(() => {
        // El aviso ya salió (api/). Se conserva lo que hubiera.
        if (mine === seq.current) setState((s) => ({ ...s, loading: false, failed: true }));
      });
    return () => {
      seq.current++;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);

  return state;
}
