// Pantalla Chats: lista (360 px) · conversación · información (360 px, plegable).
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { MessageSquare } from "lucide-react";
import { api } from "@/api";
import { useNotifyFocus } from "@/lib/notifyFocus";
import { useAccounts } from "@/store/accounts";
import { useChat } from "@/store/chat";
import { useInbox } from "@/store/inbox";
import { useUI } from "@/store/ui";
import type { Chat, Message } from "../api/core/model/models.js";
import ChatSidebar from "@/components/chat/ChatSidebar";
import ContactInfoPanel from "@/components/chat/ContactInfoPanel";
import Conversation from "@/components/chat/Conversation";
import ImageViewer from "@/components/chat/ImageViewer";
import QuickSearch from "@/components/chat/QuickSearch";

const INFO_KEY = "dc:chats:info";
const QUICK_SEARCH_EVENT = "dc:quick-search";

function readInfoOpen(): boolean {
  try {
    const v = localStorage.getItem(INFO_KEY);
    if (v === "1") return true;
    if (v === "0") return false;
  } catch {
    /* sin almacenamiento */
  }
  return typeof window !== "undefined" ? window.innerWidth >= 1360 : false;
}

export default function ChatsPage() {
  const chatKey = useChat((s) => s.chatKey);
  const messages = useChat((s) => s.messages);
  const openChat = useChat((s) => s.open);
  const stored = useInbox((s) => (chatKey ? s.chats[chatKey] : undefined));
  const views = useAccounts((s) => s.views);
  const order = useAccounts((s) => s.order);
  const pending = useUI((s) => s.openChatKey);
  const clearPending = useUI((s) => s.clearOpenChat);
  const requestOpen = useUI((s) => s.openChat);

  const [fetched, setFetched] = useState<Chat | null>(null);
  const [infoOpen, setInfoOpen] = useState(readInfoOpen);
  const [quick, setQuick] = useState(false);
  const [viewer, setViewer] = useState<string | null>(null);
  const [focused, setFocused] = useState(() => (typeof document !== "undefined" ? document.hasFocus() : true));
  const markedFor = useRef<string | null>(null);

  const accounts = useMemo(() => {
    const m: Record<string, { label: string; color: string }> = {};
    for (const id of order) {
      const a = views[id]?.account;
      if (a) m[id] = { label: a.label, color: a.color };
    }
    return m;
  }, [order, views]);

  // chat que pidió abrir otra pantalla (notificación, «Abrir chat»…)
  useEffect(() => {
    if (!pending) return;
    void openChat(pending);
    clearPending();
  }, [pending, openChat, clearPending]);

  // chat abierto que no está en la lista cargada (búsqueda, contacto en otra cuenta…)
  useEffect(() => {
    if (!chatKey || stored) return;
    let alive = true;
    api.inbox
      .GetChat(chatKey)
      .then((c) => alive && setFetched(c))
      .catch(() => undefined);
    return () => {
      alive = false;
    };
  }, [chatKey, stored]);

  const chat: Chat | null = stored ?? (fetched && fetched.key === chatKey ? fetched : null);

  // búsqueda rápida (la dispara A4 con Ctrl+K)
  useEffect(() => {
    const on = () => setQuick(true);
    window.addEventListener(QUICK_SEARCH_EVENT, on);
    return () => window.removeEventListener(QUICK_SEARCH_EVENT, on);
  }, []);

  // foco de la ventana
  useEffect(() => {
    const f = () => setFocused(true);
    const b = () => setFocused(false);
    window.addEventListener("focus", f);
    window.addEventListener("blur", b);
    return () => {
      window.removeEventListener("focus", f);
      window.removeEventListener("blur", b);
    };
  }, []);

  // el servicio de notificaciones sabe qué chat se está viendo (y si la ventana tiene el foco)
  useNotifyFocus(chatKey);

  // marcar leído al abrir (y mientras lleguen mensajes) si la ventana tiene foco
  const unreadCount = chat?.unreadCount ?? 0;
  useEffect(() => {
    if (!chatKey || !focused) return;
    if (markedFor.current !== chatKey || unreadCount > 0) {
      markedFor.current = chatKey;
      void api.messaging.MarkRead(chatKey).catch(() => undefined);
    }
  }, [chatKey, focused, unreadCount]);

  // ventana estrecha: el panel de información se pliega
  useEffect(() => {
    const on = () => {
      if (window.innerWidth < 1180) setInfoOpen(false);
    };
    window.addEventListener("resize", on);
    return () => window.removeEventListener("resize", on);
  }, []);

  const toggleInfo = useCallback(() => {
    setInfoOpen((v) => {
      try {
        localStorage.setItem(INFO_KEY, v ? "0" : "1");
      } catch {
        /* sin almacenamiento */
      }
      return !v;
    });
  }, []);

  const lastDelivery = useCallback(
    (c: Chat) => {
      if (c.key !== chatKey || !c.lastFromMe) return undefined;
      for (let i = messages.length - 1; i >= 0; i--) if (messages[i].fromMe) return messages[i].delivery as string;
      return undefined;
    },
    [chatKey, messages],
  );

  const images = useMemo(
    () => messages.filter((m: Message) => (m.kind as string) === "image" && m.media && m.deletedAt === 0),
    [messages],
  );

  return (
    <div className="flex h-full min-w-0 bg-[var(--bg)] text-[var(--text)]">
      <ChatSidebar selectedKey={chatKey} lastDelivery={lastDelivery} onOpen={(k) => void openChat(k)} />
      {chat ? (
        <>
          <Conversation
            chat={chat}
            account={accounts[chat.accountId]}
            accounts={accounts}
            infoOpen={infoOpen}
            onToggleInfo={toggleInfo}
            onOpenChat={(k) => requestOpen(k)}
          />
          {infoOpen && (
            <ContactInfoPanel
              chat={chat}
              account={accounts[chat.accountId]}
              accounts={accounts}
              messages={messages}
              onClose={toggleInfo}
              onOpenChat={(k) => requestOpen(k)}
              onOpenImage={(m) => setViewer(m.key)}
            />
          )}
        </>
      ) : (
        <div className="flex min-w-0 flex-1 flex-col items-center justify-center gap-3 border-b-[6px] border-[var(--accent)] bg-[var(--panel-2)] text-[var(--muted)]">
          <MessageSquare size={64} strokeWidth={1.2} />
          <span className="text-[22px] font-light">dConnector</span>
        </div>
      )}
      {quick && (
        <QuickSearch
          accounts={accounts}
          onClose={() => setQuick(false)}
          onOpen={(key) => void openChat(key)}
        />
      )}
      {viewer && <ImageViewer images={images} startKey={viewer} onClose={() => setViewer(null)} onSaveAs={(m) => m.media && void api.messaging.SaveMediaAs(m.accountId, m.media.id, m.media.fileName).catch(() => undefined)} />}
    </div>
  );
}
