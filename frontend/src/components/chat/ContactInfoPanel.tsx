// Panel de información del chat: foto, nombre, cuenta, contacto unificado, listas, etiquetas, notas,
// archivos y estadísticas.
import { useEffect, useMemo, useState } from "react";
import { mediaUrl } from "@/api/media";
import { FileText, Play, Plus, X } from "lucide-react";
import { api } from "@/api";
import type { Contact, ContactList } from "@/api/types";
import type { Chat, Message } from "../../api/core/model/models.js";
import { digitsOf, formatBytes, formatFullDate, phoneFromNative } from "./chatFormat";
import { chatFiles } from "./chatFiles";
import { fetchChatStats } from "./useChatApi";
import type { ChatStatsView } from "./chatStats";
import { AccountChip, ChannelBadge, ChatAvatar, channelInfo, IconButton } from "./parts";

export interface InfoAccountLook {
  label: string;
  color: string;
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="border-t-8 border-[var(--bg)] bg-[var(--panel)] px-5 py-3">
      <h3 className="mb-2 text-[13px] font-medium text-[var(--muted)]">{title}</h3>
      {children}
    </section>
  );
}

function Chip({ active, onClick, children, title }: { active?: boolean; onClick?: () => void; children: React.ReactNode; title?: string }) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={title}
      aria-pressed={active}
      className={`max-w-full truncate rounded-full px-3 py-1 text-[12.5px] ${
        active ? "bg-[var(--accent)] text-white" : "bg-[var(--panel-2)] text-[var(--text)] hover:bg-black/10 dark:hover:bg-white/10"
      }`}
    >
      {children}
    </button>
  );
}

export default function ContactInfoPanel({
  chat,
  account,
  accounts,
  messages,
  onClose,
  onOpenChat,
  onOpenImage,
}: {
  chat: Chat;
  account?: InfoAccountLook;
  accounts: Record<string, InfoAccountLook>;
  messages: Message[];
  onClose: () => void;
  onOpenChat: (chatKey: string) => void;
  onOpenImage: (m: Message) => void;
}) {
  const [contact, setContact] = useState<Contact | null>(null);
  const [lists, setLists] = useState<ContactList[]>([]);
  const [stats, setStats] = useState<ChatStatsView | null>(null);
  const [notes, setNotes] = useState("");
  const [tag, setTag] = useState("");
  const direct = (chat.kind as string) === "direct";
  const phone = phoneFromNative(chat.peerNativeId);

  // contacto unificado del chat
  useEffect(() => {
    let alive = true;
    setContact(null);
    if (!chat.contactId) return;
    api.contacts
      .Get(chat.contactId)
      .then((c) => alive && setContact(c))
      .catch(() => undefined);
    return () => {
      alive = false;
    };
  }, [chat.contactId, chat.key]);

  useEffect(() => setNotes(contact?.notes ?? ""), [contact?.id, contact?.notes]);

  useEffect(() => {
    let alive = true;
    api.contacts
      .Lists()
      .then((l) => alive && setLists(l))
      .catch(() => undefined);
    return () => {
      alive = false;
    };
  }, []);

  useEffect(() => {
    let alive = true;
    setStats(null);
    void fetchChatStats(chat.key).then((s) => alive && setStats(s));
    return () => {
      alive = false;
    };
  }, [chat.key]);

  const files = useMemo(() => chatFiles(messages), [messages]);
  const others = (contact?.identities ?? []).filter(
    (i) => !(i.accountId === chat.accountId && (i.peerNativeId === chat.peerNativeId || digitsOf(i.peerNativeId) === digitsOf(chat.peerNativeId))),
  );

  const save = async (patch: Partial<Contact>) => {
    if (!contact) return;
    try {
      setContact(await api.contacts.Save({ ...contact, ...patch }));
    } catch {
      /* el aviso ya salió */
    }
  };

  const create = async () => {
    try {
      setContact(await api.contacts.FromChat(chat.key));
    } catch {
      /* el aviso ya salió */
    }
  };

  const toggleList = async (l: ContactList, member: boolean) => {
    if (!contact) return;
    try {
      await api.contacts.SetListMembers(l.id, member ? null : [contact.id], member ? [contact.id] : null);
      const ids = member ? (contact.listIds ?? []).filter((x) => x !== l.id) : [...(contact.listIds ?? []), l.id];
      setContact({ ...contact, listIds: ids });
    } catch {
      /* el aviso ya salió */
    }
  };

  const addTag = () => {
    const t = tag.trim();
    setTag("");
    if (!t || !contact || (contact.tags ?? []).includes(t)) return;
    void save({ tags: [...(contact.tags ?? []), t] });
  };

  const channel = channelInfo(chat.channel as string);
  return (
    <aside className="flex h-full w-[360px] shrink-0 flex-col border-l border-[var(--border)] bg-[var(--bg)]" aria-label="Información del chat">
      <div className="flex h-[59px] shrink-0 items-center gap-3 border-b border-[var(--border)] bg-[var(--panel-2)] px-4">
        <IconButton title="Cerrar" onClick={onClose}>
          <X size={20} />
        </IconButton>
        <span className="text-[16px] font-medium">Información</span>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="flex flex-col items-center gap-2 bg-[var(--panel)] px-5 pb-5 pt-6">
          <ChatAvatar title={chat.title} avatarUrl={chat.avatarUrl} channel={chat.channel as string} size={120} badge={false} />
          <div className="max-w-full truncate text-[20px]" title={chat.title}>{chat.title}</div>
          {(phone || contact?.phone) && <div className="text-[14px] text-[var(--muted)]">{contact?.phone || phone}</div>}
          <div className="flex items-center gap-1.5 text-[12.5px] text-[var(--muted)]">
            <ChannelBadge channel={chat.channel as string} size={16} />
            {channel.name}
            {account && <AccountChip label={account.label} color={account.color} />}
          </div>
        </div>

        {direct && (
          <Section title="Contacto">
            {contact ? (
              <div className="flex flex-col gap-2">
                <div className="text-[14px]" title={contact.name}>{contact.name}</div>
                {others.length > 0 && (
                  <div className="flex flex-col gap-1.5">
                    {others.map((i) => {
                      const acc = accounts[i.accountId];
                      return (
                        <div key={`${i.accountId}-${i.peerNativeId}`} className="flex items-center gap-2 text-[13px]">
                          <ChannelBadge channel={i.channel as string} size={16} />
                          <span className="min-w-0 flex-1 truncate" title={i.displayName || i.phone}>{i.displayName || i.phone || i.peerNativeId}</span>
                          {acc && <AccountChip label={acc.label} color={acc.color} />}
                          <button
                            type="button"
                            onClick={() => onOpenChat(`${i.accountId}|${i.peerNativeId}`)}
                            className="shrink-0 rounded-full px-2.5 py-0.5 text-[12px] font-medium text-[var(--accent)] hover:bg-black/5 dark:hover:bg-white/10"
                          >
                            Abrir chat
                          </button>
                        </div>
                      );
                    })}
                  </div>
                )}
              </div>
            ) : (
              <button
                type="button"
                onClick={() => void create()}
                className="rounded-full border border-[var(--border)] px-4 py-1.5 text-[13px] font-medium text-[var(--accent)] hover:bg-black/5 dark:hover:bg-white/10"
              >
                Crear contacto
              </button>
            )}
          </Section>
        )}

        {contact && (
          <>
            <Section title="Listas">
              {lists.length === 0 ? (
                <span className="text-[13px] text-[var(--muted)]">Sin listas</span>
              ) : (
                <div className="flex flex-wrap gap-1.5">
                  {lists.map((l) => {
                    const member = (contact.listIds ?? []).includes(l.id);
                    return (
                      <Chip key={l.id} active={member} title={l.name} onClick={() => void toggleList(l, member)}>
                        {l.name}
                      </Chip>
                    );
                  })}
                </div>
              )}
            </Section>
            <Section title="Etiquetas">
              <div className="flex flex-wrap items-center gap-1.5">
                {(contact.tags ?? []).map((t) => (
                  <span key={t} className="flex max-w-full items-center gap-1 rounded-full bg-[var(--panel-2)] py-1 pl-3 pr-1.5 text-[12.5px]">
                    <span className="truncate" title={t}>{t}</span>
                    <button
                      type="button"
                      title="Quitar etiqueta"
                      onClick={() => void save({ tags: (contact.tags ?? []).filter((x) => x !== t) })}
                      className="text-[var(--muted)] hover:text-[var(--danger)]"
                    >
                      <X size={13} />
                    </button>
                  </span>
                ))}
                <span className="flex items-center rounded-full bg-[var(--panel-2)] pl-3 pr-1">
                  <input
                    value={tag}
                    onChange={(e) => setTag(e.target.value)}
                    onKeyDown={(e) => e.key === "Enter" && addTag()}
                    placeholder="Etiqueta"
                    className="w-20 bg-transparent py-1 text-[12.5px] outline-none"
                  />
                  <button type="button" title="Añadir etiqueta" onClick={addTag} className="p-1 text-[var(--muted)] hover:text-[var(--accent)]">
                    <Plus size={14} />
                  </button>
                </span>
              </div>
            </Section>
            <Section title="Notas">
              <textarea
                value={notes}
                onChange={(e) => setNotes(e.target.value)}
                onBlur={() => notes !== (contact.notes ?? "") && void save({ notes })}
                rows={3}
                placeholder="Notas"
                className="w-full resize-none rounded-lg bg-[var(--panel-2)] px-3 py-2 text-[13.5px] outline-none"
              />
            </Section>
          </>
        )}

        <Section title="Archivos">
          {files.visual.length === 0 && files.documents.length === 0 ? (
            <span className="text-[13px] text-[var(--muted)]">Sin archivos</span>
          ) : (
            <div className="flex flex-col gap-3">
              {files.visual.length > 0 && (
                <div className="grid grid-cols-3 gap-1">
                  {files.visual.slice(0, 30).map((m) => (
                    <button
                      key={m.key}
                      type="button"
                      onClick={() => (m.kind as string) === "image" && onOpenImage(m)}
                      className="relative aspect-square overflow-hidden rounded bg-[var(--panel-2)]"
                      title={formatFullDate(m.timestamp)}
                    >
                      {m.media?.thumb || (m.kind as string) === "image" ? (
                        <img src={mediaUrl(m.media?.thumb || m.media?.url)} alt="" loading="lazy" className="h-full w-full object-cover" />
                      ) : null}
                      {(m.kind as string) === "video" && (
                        <span className="absolute inset-0 flex items-center justify-center bg-black/25 text-white">
                          <Play size={20} />
                        </span>
                      )}
                    </button>
                  ))}
                </div>
              )}
              {files.documents.slice(0, 20).map((m) => (
                <div key={m.key} className="flex items-center gap-2 text-[13px]" title={m.media?.fileName}>
                  <FileText size={18} className="shrink-0 text-[var(--muted)]" />
                  <span className="min-w-0 flex-1 truncate">{m.media?.fileName || "Documento"}</span>
                  <span className="shrink-0 text-[11.5px] text-[var(--muted)]">{formatBytes(m.media?.size ?? 0)}</span>
                </div>
              ))}
            </div>
          )}
        </Section>

        {stats && (
          <Section title="Estadísticas">
            <div className="grid grid-cols-3 gap-2 text-center">
              <Stat label="Enviados" value={stats.sent} />
              <Stat label="Recibidos" value={stats.received} />
              <Stat label="Total" value={stats.total} />
            </div>
            <div className="mt-2 flex flex-col gap-0.5 text-[12.5px] text-[var(--muted)]">
              {stats.first !== null && <span>Primer mensaje: {formatFullDate(stats.first)}</span>}
              {stats.last !== null && <span>Último mensaje: {formatFullDate(stats.last)}</span>}
              {stats.activeDays !== null && <span>Días con conversación: {stats.activeDays}</span>}
            </div>
          </Section>
        )}
      </div>
    </aside>
  );
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div className="rounded-lg bg-[var(--panel-2)] py-2">
      <div className="text-[18px] font-semibold">{value}</div>
      <div className="text-[11.5px] text-[var(--muted)]">{label}</div>
    </div>
  );
}
