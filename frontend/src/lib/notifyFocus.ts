// Foco para las notificaciones: le dice al servicio de Go qué chat está abierto y si la ventana tiene el
// foco, para que no avise de lo que ya se está viendo.
import { useEffect } from "react";
import { api } from "@/api";

/** ¿La ventana está a la vista y con el foco? */
export function windowIsFocused(): boolean {
  if (typeof document === "undefined") return false;
  return document.visibilityState !== "hidden" && document.hasFocus();
}

/** Informa del foco sin molestar: un fallo aquí nunca se muestra. */
export function reportFocus(chatKey: string | null, focused: boolean = windowIsFocused()): void {
  try {
    void Promise.resolve(api.notifications.SetFocus(chatKey ?? "", focused)).catch(() => undefined);
  } catch {
    /* sin enlaces (pruebas) */
  }
}

/**
 * Mientras la pantalla de chats está montada, informa del chat abierto y de si la ventana gana o pierde el
 * foco (`focus`, `blur` y `visibilitychange`). Al desmontarse, avisa de que ya no hay chat abierto.
 */
export function useNotifyFocus(chatKey: string | null): void {
  useEffect(() => {
    const send = () => reportFocus(chatKey);
    send();
    window.addEventListener("focus", send);
    window.addEventListener("blur", send);
    document.addEventListener("visibilitychange", send);
    return () => {
      window.removeEventListener("focus", send);
      window.removeEventListener("blur", send);
      document.removeEventListener("visibilitychange", send);
    };
  }, [chatKey]);

  useEffect(() => () => reportFocus(null, false), []);
}
