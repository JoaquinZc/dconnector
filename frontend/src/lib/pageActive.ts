// ¿La página está a la vista? Las páginas visitadas se mantienen montadas pero ocultas (pages/registry.tsx):
// lo que depende de que el usuario la esté viendo (marcar leído, avisar del foco…) debe mirar este valor.
import { createContext, useContext } from "react";

export const PageActiveContext = createContext(true);

/** `true` si esta página es la que se está viendo. */
export function usePageActive(): boolean {
  return useContext(PageActiveContext);
}
