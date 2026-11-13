// Lista de chats virtualizada con paginación al bajar y menú contextual (fijar, archivar, silenciar).
import { useCallback, useEffect, useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { MessageSquarePlus } from "lucide-react";
import { api } from "@/api";
import EmptyState from "@/components/common/EmptyState";
import { useInbox } from "@/store/inbox";
import type { Chat } from "../../api/core/model/models.js";
import ChatRow, { ROW_HEIGHT } from "./ChatRow";
import { isMuted, muteUntil } from "./chatFormat";
import { PopMenu, type PopItem } from "./parts";

export function muteMenuItems(chat: Chat, now = Date.now()): PopItem[] {
  if (isMuted(chat.mutedUntil, now)) {
    return [{ label: "Activar sonido", onClick: () => void api.inbox.Mute(chat.key, 0) }];
  }
  return [
    { label: "Silenciar 8 horas", onClick: () => void api.inbox.Mute(chat.key, muteUntil("8h")) },
    { label: "Silenciar 1 semana", onClick: () => void api.inbox.Mute(chat.key, muteUntil("week")) },
    { label: "Silenciar siempre", onClick: () => void api.inbox.Mute(chat.key, muteUntil("always")) },
  ];
}

export function chatMenuItems(chat: Chat, now = Date.now()): PopItem[] {
  return [
    { label: chat.pinned ? "Desfijar chat" : "Fijar chat", onClick: () => void api.inbox.Pin(chat.key, !chat.pinned) },
    { label: chat.archived ? "Desarchivar chat" : "Archivar chat", onClick: () => void api.inbox.Archive(chat.key, !chat.archived) },
    ...muteMenuItems(chat, now),
  ];
}

export default function ChatList({
  selectedKey,
  accounts,
  lastDelivery,
  onOpen,
  onNewChat,
}: {
  selectedKey: string | null;
  accounts: Record<string, { label: string; color: string }>;
  lastDelivery?: (chat: Chat) => string | undefined;
  onOpen: (key: string) => void;
  /** Abre «Nuevo chat»: se ofrece en el estado vacío de la lista. */
  onNewChat?: () => void;
}) {
  const order = useInbox((s) => s.order);
  const chats = useInbox((s) => s.chats);
  const loading = useInbox((s) => s.loading);
  const filter = useInbox((s) => s.filter);
  const hasMore = useInbox((s) => s.hasMore);
  const loadMore = useInbox((s) => s.loadMore);
  const parentRef = useRef<HTMLDivElement>(null);
  const [menu, setMenu] = useState<{ chat: Chat; x: number; y: number } | null>(null);

  const virtualizer = useVirtualizer({
    count: order.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 8,
    getItemKey: (i) => order[i],
  });
  const items = virtualizer.getVirtualItems();
  const lastIndex = items.length ? items[items.length - 1].index : -1;

  // Paginación al bajar: al acercarse al final se pide la página siguiente.
  useEffect(() => {
    if (lastIndex >= 0 && lastIndex >= order.length - 12 && hasMore && !loading) void loadMore();
  }, [lastIndex, order.length, hasMore, loading, loadMore]);

  const closeMenu = useCallback(() => setMenu(null), []);

  if (order.length === 0) {
    // Sin ninguna conversación todavía (pestaña «Todos», sin búsqueda): se explica cómo empezar.
    const noActivityYet = !loading && (filter.tab || "all") === "all" && !(filter.query ?? "").trim();
    if (noActivityYet && onNewChat) {
      return (
        <EmptyState
          icon={MessageSquarePlus}
          title="Aún no hay conversaciones"
          description="Pulsa «Nuevo chat» para escribir a un contacto"
          className="flex-1"
          action={
            <button type="button" className="btn btn-primary" onClick={onNewChat}>
              <MessageSquarePlus size={16} />
              Nuevo chat
            </button>
          }
        />
      );
    }
    return (
      <div className="flex flex-1 items-center justify-center p-8 text-center text-[14px] text-[var(--muted)]">
        {loading ? "Cargando…" : "Sin chats"}
      </div>
    );
  }

  return (
    <>
      <div ref={parentRef} className="min-h-0 flex-1 overflow-y-auto" data-testid="chat-list">
        <div style={{ height: virtualizer.getTotalSize(), position: "relative", width: "100%" }}>
          {items.map((vi) => {
            const chat = chats[order[vi.index]];
            if (!chat) return null;
            return (
              <div
                key={vi.key}
                style={{ position: "absolute", top: 0, left: 0, width: "100%", height: vi.size, transform: `translateY(${vi.start}px)` }}
              >
                <ChatRow
                  chat={chat}
                  account={accounts[chat.accountId]}
                  selected={chat.key === selectedKey}
                  lastDelivery={lastDelivery?.(chat)}
                  onClick={() => onOpen(chat.key)}
                  onContextMenu={(e) => {
                    e.preventDefault();
                    setMenu({ chat, x: e.clientX, y: e.clientY });
                  }}
                />
              </div>
            );
          })}
        </div>
        {loading && <div className="py-2 text-center text-[12px] text-[var(--muted)]">Cargando…</div>}
      </div>
      {menu && <PopMenu at={menu} items={chatMenuItems(menu.chat)} onClose={closeMenu} />}
    </>
  );
}
