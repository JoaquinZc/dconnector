// Programados del chat abierto: lista (pendientes + cerrados recientes) y recuentos para la insignia.
// El backend no emite un evento de programados: se recarga al cambiar de chat, cada 60 s, cuando
// llega un mensaje propio de este chat (puede ser el programado que acaba de salir) y bajo demanda.
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api, EV, onEvent } from "@/api";
import type { Message, Scheduled } from "@/api/types";
import { scheduleCounts } from "./scheduleModel";

export const CHAT_SCHEDULED_POLL_MS = 60_000;
const AFTER_SEND_MS = 800;

export interface ChatScheduled {
  items: Scheduled[];
  loaded: boolean;
  pending: number;
  failed: number;
  reload: () => Promise<void>;
}

export function useChatScheduled(accountId: string, chatNativeId: string, chatKey: string): ChatScheduled {
  const [items, setItems] = useState<Scheduled[]>([]);
  const [loaded, setLoaded] = useState(false);
  const seq = useRef(0);

  const reload = useCallback(async () => {
    const n = ++seq.current;
    try {
      const list = await api.scheduled.ListForChat(accountId, chatNativeId, true);
      if (n === seq.current) {
        setItems(list);
        setLoaded(true);
      }
    } catch {
      /* en segundo plano: sin aviso */
    }
  }, [accountId, chatNativeId]);

  useEffect(() => {
    setItems([]);
    setLoaded(false);
    void reload();
    const t = setInterval(() => void reload(), CHAT_SCHEDULED_POLL_MS);
    return () => {
      clearInterval(t);
      seq.current++; // descarta lo que aún esté en vuelo para otro chat
    };
  }, [reload]);

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    const off = onEvent(EV.messageUpsert, (msgs: Message[]) => {
      if (!msgs.some((m) => m.chatKey === chatKey && m.fromMe)) return;
      clearTimeout(timer);
      timer = setTimeout(() => void reload(), AFTER_SEND_MS);
    });
    return () => {
      off();
      clearTimeout(timer);
    };
  }, [chatKey, reload]);

  const { pending, failed } = useMemo(() => scheduleCounts(items), [items]);
  return { items, loaded, pending, failed, reload };
}
