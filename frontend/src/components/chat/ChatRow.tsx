// Fila de la lista de chats (72 px).
import React from "react";
import { BellOff, Check, CheckCheck, Pin } from "lucide-react";
import type { Chat } from "../../api/core/model/models.js";
import { channelUi, ChannelGlyph } from "./channelUi";
import { deliveryTick, emptyPreview, formatListTime, isMuted, previewParts } from "./chatFormat";
import { channelInfo, ChatAvatar } from "./parts";

export const ROW_HEIGHT = 72;

export interface ChatRowProps {
  chat: Chat;
  account?: { label: string; color: string };
  selected?: boolean;
  /** entrega del último mensaje si es mío y se conoce (el `Chat` no la trae) */
  lastDelivery?: string;
  now?: number;
  onClick: () => void;
  onContextMenu?: (e: React.MouseEvent) => void;
}

function ChatRowImpl({ chat, account, selected, lastDelivery, now, onClick, onContextMenu }: ChatRowProps) {
  const { prefix, text } = previewParts(chat);
  // Sin vista previa no se deja un hueco: el teléfono o «Sin mensajes cargados», en la misma línea.
  const empty = text ? null : emptyPreview(chat);
  const tick = chat.lastFromMe && text
    ? deliveryTick({ fromMe: true, delivery: (lastDelivery ?? "sent") as never, kind: "text" as never })
    : null;
  const unread = chat.unreadCount > 0;
  const muted = isMuted(chat.mutedUntil, now);
  const channel = chat.channel as string;
  return (
    <div
      role="button"
      tabIndex={0}
      onClick={onClick}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onClick();
        }
      }}
      onContextMenu={onContextMenu}
      data-testid="chat-row"
      aria-current={selected ? "true" : undefined}
      // --rowbg: el fondo de la fila por estado; la insignia del canal lo usa de borde para «recortar» el avatar
      className={`relative flex w-full cursor-pointer items-center gap-3 bg-[var(--rowbg)] px-4 text-left focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-[var(--accent)] ${
        selected
          ? "[--rowbg:var(--sel)]"
          : "[--rowbg:var(--panel)] hover:[--rowbg:color-mix(in_srgb,var(--text)_5%,var(--panel))]"
      }`}
      style={{ height: ROW_HEIGHT }}
    >
      {selected && (
        <span
          aria-hidden="true"
          data-testid="row-selected-bar"
          className="absolute top-3.5 bottom-3.5 left-0 w-[3px] rounded-r-[3px] bg-[var(--accent)]"
        />
      )}
      <span className="relative h-12 w-12 shrink-0">
        <ChatAvatar title={chat.title} avatarUrl={chat.avatarUrl} channel={channel} size={48} badge={false} />
        <span
          data-testid="row-channel"
          title={channelInfo(channel).name}
          className="absolute -right-1 -bottom-1 flex h-[22px] w-[22px] items-center justify-center rounded-full border-2 border-[var(--rowbg)] text-white"
          style={{ background: channelUi(channel).fill }}
        >
          <ChannelGlyph channel={channel} size={12} strokeWidth={2.2} />
        </span>
      </span>
      <div className="flex min-w-0 flex-1 flex-col justify-center gap-[3px] border-b border-[var(--border)]" style={{ height: ROW_HEIGHT }}>
        <div className="flex min-w-0 items-center gap-2">
          <span className={`min-w-0 flex-1 truncate text-[15px] ${unread ? "font-semibold" : "font-medium"}`} title={chat.title}>
            {chat.title || "Sin nombre"}
          </span>
          <span
            data-testid="row-time"
            className={`shrink-0 text-[12px] ${unread && !muted ? "font-semibold text-[var(--accent-text)]" : "text-[var(--muted)]"}`}
          >
            {formatListTime(chat.lastMessageAt, now)}
          </span>
        </div>
        <div className="flex min-w-0 items-center gap-2">
          <div className="flex min-w-0 flex-1 items-center gap-1 text-[13.5px] text-[var(--muted)]">
            {tick && tick !== "pending" && tick !== "failed" && (
              <span className="shrink-0" data-testid="row-tick" data-tick={tick}>
                {tick === "sent" ? (
                  <Check size={16} className="text-[var(--muted)]" />
                ) : (
                  <CheckCheck size={16} className={tick === "read" ? "text-[#1d9bd1] dark:text-[#53bdeb]" : "text-[var(--muted)]"} />
                )}
              </span>
            )}
            {empty ? (
              <span
                className={`min-w-0 truncate leading-5 opacity-80 ${empty.placeholder ? "italic" : ""}`}
                title={empty.text}
                data-testid="row-preview-empty"
              >
                {empty.text}
              </span>
            ) : (
              <span className="min-w-0 truncate leading-5" title={prefix + text}>
                {prefix}
                {text}
              </span>
            )}
          </div>
          {account && (
            <span
              data-testid="row-account"
              title={account.label}
              className="flex max-w-[76px] shrink-0 items-center gap-[5px] text-[11.5px] text-[var(--muted)]"
            >
              <i className="h-[7px] w-[7px] shrink-0 rounded-full" style={{ background: account.color || "#8696a0" }} aria-hidden="true" />
              <span className="min-w-0 truncate">{account.label}</span>
            </span>
          )}
          {chat.pinned && (
            <span title="Fijado" role="img" aria-label="Fijado" className="shrink-0 text-[var(--muted)]">
              <Pin size={16} aria-hidden="true" />
            </span>
          )}
          {muted && (
            <span title="Silenciado" role="img" aria-label="Silenciado" className="shrink-0 text-[var(--muted)]">
              <BellOff size={16} aria-hidden="true" />
            </span>
          )}
          {unread && (
            <span
              data-testid="row-unread"
              className={`h-5 min-w-5 shrink-0 rounded-full px-1.5 text-center text-[11.5px] leading-5 font-bold ${
                muted ? "bg-[var(--mute-bg)] text-[var(--mute-fg)]" : "bg-[var(--badge-bg)] text-[var(--badge-fg)]"
              }`}
            >
              {chat.unreadCount > 99 ? "99+" : chat.unreadCount}
            </span>
          )}
        </div>
      </div>
    </div>
  );
}

export const ChatRow = React.memo(ChatRowImpl);
export default ChatRow;
