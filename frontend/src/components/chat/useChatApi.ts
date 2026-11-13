// Costuras locales de la pantalla de chats con A4 (api/store) y con los enlaces de Wails.
// Todo lo que A4 no ofrece directamente vive aquí.
import { useEffect, useState } from "react";
import { rpc } from "@/api/transport";
import { api, onEvent } from "@/api";
import type { CapSet } from "@/api/types";
import { useAccounts } from "@/store/accounts";
import { extractChatStats, type ChatStatsView } from "./chatStats";

/** Estadísticas del chat sin aviso de error (si el servicio aún no responde, simplemente no hay datos). */
export async function fetchChatStats(chatKey: string): Promise<ChatStatsView | null> {
  try {
    const raw = await rpc<unknown>("Stats", "Chat", [chatKey, { days: 3650, from: "", to: "" }]);
    return extractChatStats(raw);
  } catch {
    return null;
  }
}

/**
 * Capacidades del chat abierto: las del chat (`Capabilities(chatKey)`, que ya considera canales de
 * solo lectura) y, mientras llegan, las de la cuenta. Se refrescan con `account:caps`.
 */
export function useChatCaps(chatKey: string | null, accountId: string | null): CapSet | null {
  const accountCaps = useAccounts((s) => (accountId ? s.views[accountId]?.capabilities : undefined)) as CapSet | undefined;
  const [caps, setCaps] = useState<{ key: string; value: CapSet } | null>(null);

  useEffect(() => {
    if (!chatKey) return;
    let alive = true;
    const load = () => {
      api.accounts
        .Capabilities(chatKey)
        .then((value) => alive && setCaps({ key: chatKey, value }))
        .catch(() => undefined);
    };
    load();
    const off = onEvent("account:caps", (ev) => {
      if (ev.accountId === accountId) load();
    });
    return () => {
      alive = false;
      off();
    };
  }, [chatKey, accountId]);

  if (caps && caps.key === chatKey && caps.value) return caps.value;
  return accountCaps ?? null;
}
