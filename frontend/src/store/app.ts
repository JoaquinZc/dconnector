// Arranque de los estados del cliente: carga inicial, flujo en vivo y recarga tras reconectar.
import { startLive } from "@/api/connection";
import { onEvent } from "@/api/events";
import { rpc } from "@/api/transport";
import type { AppSettings } from "@/api/types";
import { useAccounts } from "./accounts";
import { useChat } from "./chat";
import { useHistory } from "./history";
import { useInbox } from "./inbox";
import { useStatuses } from "./statuses";
import { useUI, type Theme } from "./ui";

/** Acepta la clave del chat suelta o dentro de un objeto (`{chatKey}`). */
export function chatKeyFromEvent(data: unknown): string | null {
  if (typeof data === "string") return data || null;
  if (data && typeof data === "object") {
    const o = data as { chatKey?: unknown; key?: unknown };
    const k = typeof o.chatKey === "string" ? o.chatKey : typeof o.key === "string" ? o.key : "";
    return k || null;
  }
  return null;
}

/**
 * Tras una reconexión el servidor no reenvía lo perdido: cada estado vuelve a pedir lo suyo por RPC.
 * El chat abierto se mezcla sin vaciarse (no hay parpadeo).
 */
export function reloadAll(): void {
  void useAccounts.getState().load();
  void useInbox.getState().loadFirst();
  void useInbox.getState().loadUnread();
  void useChat.getState().reload();
  void useStatuses.getState().refresh();
  void useHistory.getState().load();
}

/** Llamar una vez que la conexión está lista. Devuelve la función que cancela todo. */
export function startApp(): () => void {
  const offs = [
    useAccounts.getState().init(),
    useInbox.getState().init(),
    useChat.getState().init(),
    useStatuses.getState().init(),
    useHistory.getState().init(),
    onEvent<unknown>("ui:open-chat", (data) => {
      const key = chatKeyFromEvent(data);
      if (key) useUI.getState().openChat(key);
    }),
    onEvent("connection", (ev) => {
      if (ev.state === "connected" && ev.reconnected) reloadAll();
    }),
    startLive(),
  ];

  // El tema guardado en Ajustes manda sobre el local (sin aviso si aún no hay backend).
  void Promise.resolve(rpc<AppSettings>("Settings", "Get", []))
    .then((s) => {
      const t = s?.theme;
      if (t === "light" || t === "dark" || t === "system") useUI.getState().setTheme(t as Theme);
    })
    .catch(() => undefined);

  return () => offs.forEach((o) => o());
}
