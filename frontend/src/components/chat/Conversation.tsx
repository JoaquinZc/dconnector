// Conversación: cabecera, mensajes virtualizados, menú de mensaje, diálogos y compositor.
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Archive, ArchiveRestore, Bell, BellOff, ChevronDown, ChevronUp, PanelRight, Search, X } from "lucide-react";
import { api } from "@/api";
import { useAccounts } from "@/store/accounts";
import { useChat, typingLabel } from "@/store/chat";
import { useInbox } from "@/store/inbox";
import { useUI } from "@/store/ui";
import type { Chat, ContactCard, Message } from "../../api/core/model/models.js";
import Bubble from "./Bubble";
import Composer, { type ComposerHandle } from "./Composer";
import ImageViewer from "./ImageViewer";
import LoadEarlier from "./LoadEarlier";
import MessageList, { type ScrollTarget } from "./MessageList";
import MessageMenu from "./MessageMenu";
import { DeleteDialog, ForwardDialog, InfoDialog } from "./MessageDialogs";
import { muteMenuItems } from "./ChatList";
import { digitsOf, isMuted, sameId } from "./chatFormat";
import { buildTimeline, indexOfNative, searchIndexes, type TimelineItem } from "./timeline";
import { AccountChip, ChatAvatar, ChannelBadge, IconButton, PopMenu } from "./parts";
import { useChatCaps } from "./useChatApi";

interface Look {
  label: string;
  color: string;
}

/** ¿Hay un chat directo de esta cuenta con ese teléfono? (para «Abrir chat» en tarjetas de contacto) */
export function findDirectChatByPhone(chats: Record<string, Chat>, accountId: string, phone: string): string | undefined {
  const d = digitsOf(phone);
  if (d.length < 6) return undefined;
  for (const c of Object.values(chats)) {
    if (c.accountId !== accountId || (c.kind as string) !== "direct") continue;
    const peer = digitsOf(c.peerNativeId.split("@")[0]);
    if (peer && (peer === d || peer.endsWith(d) || d.endsWith(peer))) return c.key;
  }
  return undefined;
}

function Header({
  chat,
  account,
  subtitle,
  searchOpen,
  infoOpen,
  onToggleSearch,
  onToggleInfo,
}: {
  chat: Chat;
  account?: Look;
  subtitle: string;
  searchOpen: boolean;
  infoOpen: boolean;
  onToggleSearch: () => void;
  onToggleInfo: () => void;
}) {
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null);
  const muted = isMuted(chat.mutedUntil);
  return (
    <div className="flex h-[59px] shrink-0 items-center gap-3 border-b border-[var(--border)] bg-[var(--panel-2)] px-4">
      <button type="button" onClick={onToggleInfo} className="flex min-w-0 flex-1 items-center gap-3 text-left" title="Información">
        <ChatAvatar title={chat.title} avatarUrl={chat.avatarUrl} channel={chat.channel as string} size={40} badge={false} />
        <div className="min-w-0">
          <div className="truncate text-[16px]" title={chat.title}>{chat.title}</div>
          <div className="flex items-center gap-1.5 text-[12.5px] text-[var(--muted)]">
            <ChannelBadge channel={chat.channel as string} size={14} />
            {account && <AccountChip label={account.label} color={account.color} />}
            {subtitle && <span className="truncate text-[var(--accent)]">{subtitle}</span>}
          </div>
        </div>
      </button>
      <IconButton title="Buscar en el chat" active={searchOpen} onClick={onToggleSearch}>
        <Search size={20} />
      </IconButton>
      <IconButton
        title={chat.archived ? "Desarchivar" : "Archivar"}
        onClick={() => void api.inbox.Archive(chat.key, !chat.archived)}
      >
        {chat.archived ? <ArchiveRestore size={20} /> : <Archive size={20} />}
      </IconButton>
      <IconButton
        title={muted ? "Activar sonido" : "Silenciar"}
        active={muted}
        onClick={(e) => {
          if (muted) void api.inbox.Mute(chat.key, 0);
          else {
            const r = e.currentTarget.getBoundingClientRect();
            setMenu({ x: r.left - 120, y: r.bottom + 4 });
          }
        }}
      >
        {muted ? <BellOff size={20} /> : <Bell size={20} />}
      </IconButton>
      <IconButton title="Información" active={infoOpen} onClick={onToggleInfo}>
        <PanelRight size={20} />
      </IconButton>
      {menu && <PopMenu at={menu} items={muteMenuItems(chat)} onClose={() => setMenu(null)} />}
    </div>
  );
}

function SearchBar({
  query,
  onQuery,
  count,
  pos,
  hasMore,
  onStep,
  onClose,
  onMore,
}: {
  query: string;
  onQuery: (q: string) => void;
  count: number;
  pos: number;
  hasMore: boolean;
  onStep: (d: 1 | -1) => void;
  onClose: () => void;
  onMore: () => void;
}) {
  return (
    <div className="flex shrink-0 items-center gap-2 border-b border-[var(--border)] bg-[var(--panel)] px-4 py-2">
      <Search size={16} className="text-[var(--muted)]" />
      <input
        autoFocus
        value={query}
        onChange={(e) => onQuery(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") onStep(e.shiftKey ? -1 : 1);
          if (e.key === "Escape") onClose();
        }}
        placeholder="Buscar en el chat"
        className="min-w-0 flex-1 bg-transparent text-[14px] outline-none"
      />
      {query && (
        <span className="shrink-0 text-[12.5px] text-[var(--muted)]">{count ? `${pos + 1} de ${count}` : "Sin resultados"}</span>
      )}
      {query && count === 0 && hasMore && (
        <button type="button" onClick={onMore} className="shrink-0 text-[12.5px] font-medium text-[var(--accent)]">
          Buscar más atrás
        </button>
      )}
      <IconButton title="Anterior" onClick={() => onStep(-1)} disabled={!count}>
        <ChevronUp size={18} />
      </IconButton>
      <IconButton title="Siguiente" onClick={() => onStep(1)} disabled={!count}>
        <ChevronDown size={18} />
      </IconButton>
      <IconButton title="Cerrar búsqueda" onClick={onClose}>
        <X size={18} />
      </IconButton>
    </div>
  );
}

export default function Conversation({
  chat,
  account,
  accounts,
  infoOpen,
  onToggleInfo,
  onOpenChat,
}: {
  chat: Chat;
  account?: Look;
  accounts: Record<string, Look>;
  infoOpen: boolean;
  onToggleInfo: () => void;
  onOpenChat: (chatKey: string) => void;
}) {
  const messages = useChat((s) => s.messages);
  const hasMore = useChat((s) => s.hasMore);
  const loading = useChat((s) => s.loading);
  const typing = useChat((s) => s.typing);
  const loadOlder = useChat((s) => s.loadOlder);
  const setReplyTo = useChat((s) => s.setReplyTo);
  const setEditing = useChat((s) => s.setEditing);
  const chats = useInbox((s) => s.chats);
  const selfId = useAccounts((s) => s.views[chat.accountId]?.account.selfId ?? "");
  const toast = useUI((s) => s.toast);
  const caps = useChatCaps(chat.key, chat.accountId);

  const composerRef = useRef<ComposerHandle>(null);
  const [menu, setMenu] = useState<{ msg: Message; x: number; y: number } | null>(null);
  const [dialog, setDialog] = useState<{ kind: "delete" | "info" | "forward"; msg: Message } | null>(null);
  const [viewer, setViewer] = useState<string | null>(null);
  const [dragging, setDragging] = useState(false);
  const [highlight, setHighlight] = useState<string | null>(null);
  const [target, setTarget] = useState<ScrollTarget | null>(null);
  const [search, setSearch] = useState<{ open: boolean; q: string; pos: number }>({ open: false, q: "", pos: 0 });

  const isGroup = (chat.kind as string) !== "direct" && (chat.kind as string) !== "self";
  const items = useMemo(() => buildTimeline(messages, isGroup), [messages, isGroup]);
  const hits = useMemo(() => (search.open ? searchIndexes(items, search.q) : []), [items, search.open, search.q]);

  // cerrar lo abierto al cambiar de chat
  useEffect(() => {
    setMenu(null);
    setDialog(null);
    setViewer(null);
    setSearch({ open: false, q: "", pos: 0 });
    setHighlight(null);
  }, [chat.key]);

  const flash = useCallback((key: string) => {
    setHighlight(key);
    window.setTimeout(() => setHighlight((h) => (h === key ? null : h)), 1800);
  }, []);

  const goTo = useCallback(
    (index: number) => {
      const it = items[index];
      if (!it || it.type !== "msg") return;
      setTarget((t) => ({ index, nonce: (t?.nonce ?? 0) + 1 }));
      flash(it.key);
    },
    [items, flash],
  );

  const goToQuoted = useCallback(
    (nativeId: string) => {
      const i = nativeId ? indexOfNative(items, nativeId) : -1;
      if (i >= 0) goTo(i);
      else toast("info", "El mensaje citado no está cargado");
    },
    [items, goTo, toast],
  );

  const stepSearch = (d: 1 | -1) => {
    if (!hits.length) return;
    const pos = (search.pos + d + hits.length) % hits.length;
    setSearch((s) => ({ ...s, pos }));
    goTo(hits[pos]);
  };

  useEffect(() => {
    if (search.open && hits.length) {
      const pos = Math.min(search.pos, hits.length - 1);
      goTo(hits[pos]);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [search.q]);

  const myReaction = (m: Message) => (m.reactions ?? []).find((r) => r.emoji && sameId(r.senderNativeId, selfId))?.emoji;

  const react = (m: Message, emoji: string) => {
    const same = myReaction(m) === emoji;
    void api.messaging.React(m.key, same ? "" : emoji).catch(() => undefined);
  };

  /** Guarda el archivo; `false` si falló (el documento de la burbuja dice entonces «No disponible»). */
  const saveAs = async (m: Message): Promise<boolean> => {
    if (!m.media) return true;
    try {
      const path = await api.messaging.SaveMediaAs(m.accountId, m.media.id, m.media.fileName);
      if (path) toast("success", "Archivo guardado");
      return true;
    } catch {
      /* el aviso ya salió */
      return false;
    }
  };

  const images = useMemo(
    () => messages.filter((m) => (m.kind as string) === "image" && m.media && m.deletedAt === 0),
    [messages],
  );

  const nameOf = (peerId: string) => {
    const m = messages.find((x) => sameId(x.senderNativeId, peerId) && x.senderName);
    return m?.senderName ?? "";
  };
  const subtitle = typingLabel(typing, isGroup, nameOf);

  const findContactChat = useCallback(
    (card: ContactCard) => findDirectChatByPhone(chats, chat.accountId, card.phone),
    [chats, chat.accountId],
  );

  const canVote = !!caps?.votePoll;

  const renderItem = (it: TimelineItem) => {
    if (it.type === "day") {
      return (
        <div className="flex justify-center py-2">
          <span className="rounded-lg bg-[var(--panel)]/90 px-3 py-1 text-[12.5px] text-[var(--muted)] shadow-sm">{it.label}</span>
        </div>
      );
    }
    return (
      <Bubble
        msg={it.message}
        showSender={it.showSender}
        first={it.first}
        selfId={selfId}
        canVote={canVote}
        highlighted={highlight === it.key}
        onMenu={(msg, at) => setMenu({ msg, x: at.x, y: at.y })}
        onOpenImage={(m) => setViewer(m.key)}
        onGoToQuoted={goToQuoted}
        onSaveAs={(m) => saveAs(m)}
        onVote={(m, options) => void api.messaging.VotePoll(m.key, options).catch(() => undefined)}
        findContactChat={findContactChat}
        onOpenChat={onOpenChat}
      />
    );
  };

  return (
    <div
      className="relative flex h-full min-w-0 flex-1 flex-col bg-[var(--chat-bg)]"
      onDragOver={(e) => {
        if (chat.readOnly || !Array.from(e.dataTransfer.types).includes("Files")) return;
        e.preventDefault();
        setDragging(true);
      }}
      onDragLeave={(e) => {
        if (e.currentTarget === e.target) setDragging(false);
      }}
      onDrop={(e) => {
        if (chat.readOnly) return;
        e.preventDefault();
        setDragging(false);
        const files = Array.from(e.dataTransfer.files);
        if (files.length) composerRef.current?.addBrowserFiles(files);
      }}
    >
      <Header
        chat={chat}
        account={account}
        subtitle={subtitle}
        searchOpen={search.open}
        infoOpen={infoOpen}
        onToggleSearch={() => setSearch((s) => ({ open: !s.open, q: "", pos: 0 }))}
        onToggleInfo={onToggleInfo}
      />
      {search.open && (
        <SearchBar
          query={search.q}
          onQuery={(q) => setSearch({ open: true, q, pos: 0 })}
          count={hits.length}
          pos={Math.min(search.pos, Math.max(0, hits.length - 1))}
          hasMore={hasMore}
          onStep={stepSearch}
          onClose={() => setSearch({ open: false, q: "", pos: 0 })}
          onMore={() => void loadOlder()}
        />
      )}
      <MessageList
        chatKey={chat.key}
        items={items}
        hasMore={hasMore}
        loading={loading}
        onLoadOlder={() => void loadOlder()}
        renderItem={renderItem}
        scrollTarget={target}
        topSlot={caps?.historyOnDemand && !hasMore ? <LoadEarlier chat={chat} /> : undefined}
      />
      <Composer chat={chat} caps={caps} handleRef={composerRef} />

      {dragging && (
        <div className="pointer-events-none absolute inset-0 z-30 flex items-center justify-center bg-[var(--accent)]/15 ring-4 ring-inset ring-[var(--accent)]">
          <span className="rounded-full bg-[var(--panel)] px-6 py-3 text-[15px] font-medium shadow-lg">Soltar para adjuntar</span>
        </div>
      )}

      {menu && (
        <MessageMenu
          msg={menu.msg}
          at={menu}
          caps={caps}
          myReaction={myReaction(menu.msg)}
          onClose={() => setMenu(null)}
          onReply={() => setReplyTo(menu.msg)}
          onReact={(emoji) => react(menu.msg, emoji)}
          onForward={() => setDialog({ kind: "forward", msg: menu.msg })}
          onCopy={() => {
            void navigator.clipboard?.writeText(menu.msg.text ?? "").then(
              () => toast("success", "Copiado"),
              () => toast("error", "No se pudo copiar"),
            );
          }}
          onEdit={() => setEditing(menu.msg)}
          onDelete={() => setDialog({ kind: "delete", msg: menu.msg })}
          onInfo={() => setDialog({ kind: "info", msg: menu.msg })}
        />
      )}
      {dialog?.kind === "delete" && (
        <DeleteDialog
          msg={dialog.msg}
          caps={caps}
          onClose={() => setDialog(null)}
          onDelete={(all) => {
            const m = dialog.msg;
            setDialog(null);
            void api.messaging.Delete(m.key, all).catch(() => undefined);
          }}
        />
      )}
      {dialog?.kind === "info" && <InfoDialog msg={dialog.msg} onClose={() => setDialog(null)} />}
      {dialog?.kind === "forward" && (
        <ForwardDialog
          msg={dialog.msg}
          accounts={accounts}
          onClose={() => setDialog(null)}
          onForwarded={(c) => toast("success", `Reenviado a ${c.title}`)}
        />
      )}
      {viewer && <ImageViewer images={images} startKey={viewer} onClose={() => setViewer(null)} onSaveAs={(m) => void saveAs(m)} />}
    </div>
  );
}
