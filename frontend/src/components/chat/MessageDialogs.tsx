// Diálogos de un mensaje: eliminar, información y reenviar (a chats de la misma cuenta).
import { useEffect, useState } from "react";
import { Search } from "lucide-react";
import type { Chat, Message } from "../../api/core/model/models.js";
import { api } from "@/api";
import { canDeleteMessageForAll, type CapsLike } from "./chatCaps";
import { formatFullDate, TICK_LABEL, deliveryTick } from "./chatFormat";
import { AccountChip, ChatAvatar, Overlay, OverlayHeader } from "./parts";

export interface AccountLook {
  label: string;
  color: string;
}

export function DeleteDialog({
  msg,
  caps,
  onClose,
  onDelete,
}: {
  msg: Message;
  caps: CapsLike | null | undefined;
  onClose: () => void;
  onDelete: (forEveryone: boolean) => void;
}) {
  const all = canDeleteMessageForAll(msg, caps);
  return (
    <Overlay onClose={onClose} width={380} label="Eliminar mensaje">
      <OverlayHeader title="¿Eliminar mensaje?" onClose={onClose} />
      <div className="flex flex-col items-stretch gap-2 p-4">
        {all && (
          <button
            type="button"
            onClick={() => onDelete(true)}
            className="rounded-full border border-[var(--border)] px-4 py-2 text-[14px] font-medium text-[var(--danger)] hover:bg-black/5 dark:hover:bg-white/10"
          >
            Eliminar para todos
          </button>
        )}
        <button
          type="button"
          onClick={() => onDelete(false)}
          className="rounded-full border border-[var(--border)] px-4 py-2 text-[14px] font-medium text-[var(--danger)] hover:bg-black/5 dark:hover:bg-white/10"
        >
          Eliminar para mí
        </button>
        <button
          type="button"
          onClick={onClose}
          className="rounded-full px-4 py-2 text-[14px] font-medium text-[var(--accent)] hover:bg-black/5 dark:hover:bg-white/10"
        >
          Cancelar
        </button>
      </div>
    </Overlay>
  );
}

function Row({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-4 py-1.5 text-[13.5px]">
      <span className="text-[var(--muted)]">{label}</span>
      <span className="min-w-0 break-words text-right">{value}</span>
    </div>
  );
}

export function InfoDialog({ msg, onClose }: { msg: Message; onClose: () => void }) {
  const tick = deliveryTick(msg);
  return (
    <Overlay onClose={onClose} width={380} label="Información del mensaje">
      <OverlayHeader title="Info. del mensaje" onClose={onClose} />
      <div className="divide-y divide-[var(--border)] overflow-y-auto px-4 py-2">
        <Row label="Fecha" value={formatFullDate(msg.timestamp)} />
        {msg.editedAt > 0 && <Row label="Editado" value={formatFullDate(msg.editedAt)} />}
        {tick && <Row label="Estado" value={TICK_LABEL[tick]} />}
        {!msg.fromMe && msg.senderName && <Row label="De" value={msg.senderName} />}
        <Row label="Tipo" value={msg.kind as string} />
        {(msg.reactions ?? []).filter((r) => r.emoji).length > 0 && (
          <Row
            label="Reacciones"
            value={(msg.reactions ?? [])
              .filter((r) => r.emoji)
              .map((r) => `${r.emoji} ${r.senderName || r.senderNativeId}`)
              .join(", ")}
          />
        )}
      </div>
    </Overlay>
  );
}

/** Selector de chat para reenviar: solo chats de la misma cuenta del mensaje (Messaging.Forward no cruza cuentas). */
export function ForwardDialog({
  msg,
  accounts,
  onClose,
  onForwarded,
}: {
  msg: Message;
  accounts: Record<string, AccountLook>;
  onClose: () => void;
  onForwarded: (chat: Chat) => void;
}) {
  const [query, setQuery] = useState("");
  const [chats, setChats] = useState<Chat[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let alive = true;
    setLoading(true);
    const t = setTimeout(() => {
      api.inbox
        .ListChats({
          accountIds: [msg.accountId],
          channels: [msg.channel],
          tab: "all",
          query: query.trim(),
          before: 0,
          limit: 60,
          // al buscar a alguien por nombre también salen los contactos sin conversación
          includeEmpty: query.trim() !== "",
        })
        .then((list) => {
          if (alive) setChats(list.filter((c) => !c.readOnly));
        })
        .catch(() => undefined)
        .finally(() => alive && setLoading(false));
    }, query ? 250 : 0);
    return () => {
      alive = false;
      clearTimeout(t);
    };
  }, [query, msg.channel, msg.accountId]);

  const pick = async (c: Chat) => {
    if (busy) return;
    setBusy(true);
    try {
      await api.messaging.Forward(msg.key, c.key);
      onForwarded(c);
      onClose();
    } catch {
      setBusy(false);
    }
  };

  return (
    <Overlay onClose={onClose} width={420} label="Reenviar mensaje">
      <OverlayHeader title="Reenviar a…" onClose={onClose} />
      <div className="border-b border-[var(--border)] p-3">
        <div className="flex items-center gap-2 rounded-lg bg-[var(--panel-2)] px-3 py-1.5">
          <Search size={16} className="text-[var(--muted)]" />
          <input
            autoFocus
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Buscar chat"
            className="min-w-0 flex-1 bg-transparent text-[14px] outline-none"
          />
        </div>
      </div>
      <div className="min-h-[200px] flex-1 overflow-y-auto">
        {chats.length === 0 && (
          <div className="p-6 text-center text-[13px] text-[var(--muted)]">{loading ? "Cargando…" : "Sin chats"}</div>
        )}
        {chats.map((c) => {
          const acc = accounts[c.accountId];
          return (
            <button
              key={c.key}
              type="button"
              disabled={busy}
              onClick={() => void pick(c)}
              className="flex w-full items-center gap-3 px-4 py-2 text-left hover:bg-black/5 disabled:opacity-60 dark:hover:bg-white/10"
            >
              <ChatAvatar title={c.title} avatarUrl={c.avatarUrl} channel={c.channel as string} size={40} />
              <span className="min-w-0 flex-1 truncate text-[14px]" title={c.title}>
                {c.title}
              </span>
              {acc && <AccountChip label={acc.label} color={acc.color} />}
            </button>
          );
        })}
      </div>
    </Overlay>
  );
}
