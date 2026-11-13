// Columna izquierda: buscador, filtro de cuentas, pestañas y lista de chats.
import { useEffect, useMemo, useState } from "react";
import { MessageSquarePlus, Search, X } from "lucide-react";
import { useAccounts } from "@/store/accounts";
import { useInbox } from "@/store/inbox";
import type { Chat } from "../../api/core/model/models.js";
import AccountFilter from "./AccountFilter";
import ChatList from "./ChatList";
import NewChatDialog from "./NewChatDialog";
import {
  loadSelection,
  pruneSelection,
  resolveAccountIds,
  sameIds,
  saveSelection,
  type AccountLite,
  type AccountSelection,
} from "./accountSelection";

export const TABS = [
  { id: "all", label: "Todos" },
  { id: "unread", label: "No leídos" },
  { id: "groups", label: "Grupos" },
  { id: "archived", label: "Archivados" },
] as const;

export default function ChatSidebar({
  selectedKey,
  lastDelivery,
  onOpen,
}: {
  selectedKey: string | null;
  lastDelivery?: (chat: Chat) => string | undefined;
  onOpen: (key: string) => void;
}) {
  const views = useAccounts((s) => s.views);
  const order = useAccounts((s) => s.order);
  const loaded = useAccounts((s) => s.loaded);
  const filter = useInbox((s) => s.filter);
  const setFilter = useInbox((s) => s.setFilter);
  const unread = useInbox((s) => s.unread);

  const accounts: AccountLite[] = useMemo(
    () =>
      order
        .map((id) => views[id])
        .filter(Boolean)
        .map((v) => ({ id: v.account.id, label: v.account.label, color: v.account.color, channel: v.account.channel as string })),
    [order, views],
  );
  const looks = useMemo(() => {
    const m: Record<string, { label: string; color: string }> = {};
    for (const a of accounts) m[a.id] = { label: a.label, color: a.color };
    return m;
  }, [accounts]);

  const [selection, setSelection] = useState<AccountSelection>(loadSelection);
  const [query, setQuery] = useState(filter.query ?? "");
  const [newChat, setNewChat] = useState(false);

  // al conocer las cuentas, se limpia lo que ya no existe
  useEffect(() => {
    if (!loaded) return;
    const pruned = pruneSelection(selection, accounts);
    if (pruned !== selection) {
      setSelection(pruned);
      saveSelection(pruned);
    }
  }, [loaded, accounts, selection]);

  // la selección se traduce a cuentas en el filtro de la bandeja
  useEffect(() => {
    const ids = resolveAccountIds(selection, accounts);
    if (!sameIds(ids, filter.accountIds as string[] | null)) setFilter({ accountIds: ids, channels: [] });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selection, accounts]);

  // buscador por título, con espera
  useEffect(() => {
    if (query === (filter.query ?? "")) return;
    const t = setTimeout(() => setFilter({ query }), 250);
    return () => clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query]);

  const ids = resolveAccountIds(selection, accounts);
  const unreadCount = ids.length === 0 ? unread.total : ids.reduce((n, id) => n + (unread.byAccount[id] ?? 0), 0);

  return (
    <div className="flex h-full w-[360px] shrink-0 flex-col border-r border-[var(--border)] bg-[var(--panel)]">
      <header className="flex h-[68px] shrink-0 items-center justify-between pr-4 pl-5">
        <h1 className="text-[22px] font-semibold tracking-[-0.2px]">Chats</h1>
        <button
          type="button"
          title="Nuevo chat"
          aria-label="Nuevo chat"
          onClick={() => setNewChat(true)}
          className="flex h-[38px] w-[38px] shrink-0 items-center justify-center rounded-xl bg-[var(--accent)] text-[#04201a] hover:bg-[color-mix(in_srgb,var(--accent)_86%,white)] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--accent)]"
        >
          <MessageSquarePlus size={20} aria-hidden="true" />
        </button>
      </header>
      <div className="shrink-0 px-4 pb-3">
        <label className="flex h-10 items-center gap-2.5 rounded-xl border border-transparent bg-[var(--panel-2)] px-3 text-[var(--muted)] focus-within:border-[var(--accent)] focus-within:bg-[var(--panel)]">
          <Search size={16} aria-hidden="true" className="shrink-0" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Buscar chat"
            aria-label="Buscar chat"
            className="min-w-0 flex-1 bg-transparent text-[14px] text-[var(--text)] outline-none placeholder:text-[var(--muted)]"
          />
          {query && (
            <button
              type="button"
              title="Borrar"
              aria-label="Borrar búsqueda"
              onClick={() => setQuery("")}
              className="shrink-0 text-[var(--muted)] hover:text-[var(--text)]"
            >
              <X size={16} />
            </button>
          )}
        </label>
      </div>
      <AccountFilter
        accounts={accounts}
        selection={selection}
        onChange={(s) => {
          setSelection(s);
          saveSelection(s);
        }}
      />
      <div className="flex h-[42px] shrink-0 gap-5 border-b border-[var(--border)] px-5" role="tablist" aria-label="Vista de chats">
        {TABS.map((t) => {
          const active = (filter.tab || "all") === t.id;
          return (
            <button
              key={t.id}
              type="button"
              role="tab"
              aria-selected={active}
              onClick={() => setFilter({ tab: t.id })}
              className={`-mb-px flex h-[42px] shrink-0 items-center gap-1.5 border-b-2 text-[13.5px] whitespace-nowrap focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-[var(--accent)] ${
                active
                  ? "border-[var(--accent)] font-semibold text-[var(--text)]"
                  : "border-transparent font-medium text-[var(--muted)] hover:text-[var(--text)]"
              }`}
            >
              {t.label}
              {t.id === "unread" && unreadCount > 0 && (
                <span
                  data-testid="tab-unread-count"
                  className="h-[18px] min-w-5 rounded-[9px] bg-[var(--badge-bg)] px-1.5 text-center text-[11px] leading-[18px] font-bold text-[var(--badge-fg)]"
                >
                  {unreadCount > 99 ? "99+" : unreadCount}
                </span>
              )}
            </button>
          );
        })}
      </div>
      <ChatList
        selectedKey={selectedKey}
        accounts={looks}
        lastDelivery={lastDelivery}
        onOpen={onOpen}
        onNewChat={() => setNewChat(true)}
      />
      {newChat && <NewChatDialog onClose={() => setNewChat(false)} onOpen={onOpen} />}
    </div>
  );
}
