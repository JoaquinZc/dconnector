import { useEffect, useState } from "react";
import { Search } from "lucide-react";
import { api } from "@/api";
import type { Chat } from "@/api/types";
import Avatar from "@/components/common/Avatar";
import { truncate } from "@/lib/format";

interface Props {
  accountId: string;
  /** Chat elegido (id nativo). */
  value: string;
  title: string;
  onChange: (chat: { nativeId: string; title: string }) => void;
}

type ListChatsFilter = Parameters<typeof api.inbox.ListChats>[0];

/** Buscador de chats de una cuenta (`ListChats` con `Query`). */
export default function ChatPicker({ accountId, value, title, onChange }: Props) {
  const [query, setQuery] = useState("");
  const [chats, setChats] = useState<Chat[]>([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!accountId) {
      setChats([]);
      return;
    }
    let alive = true;
    setLoading(true);
    const t = setTimeout(() => {
      const filter = {
        accountIds: [accountId],
        channels: null,
        tab: "all",
        query: query.trim(),
        before: 0,
        limit: 30,
        // al buscar por nombre también salen los contactos sin conversación
        includeEmpty: query.trim() !== "",
      } as unknown as ListChatsFilter;
      api.inbox
        .ListChats(filter)
        .then((list) => alive && setChats(list.filter((c) => !c.readOnly)))
        .catch(() => alive && setChats([]))
        .finally(() => alive && setLoading(false));
    }, 250);
    return () => {
      alive = false;
      clearTimeout(t);
    };
  }, [accountId, query]);

  return (
    <div className="flex flex-col gap-2">
      {value && (
        <div className="flex items-center gap-2 rounded-lg border border-accent bg-[color-mix(in_srgb,var(--accent)_10%,transparent)] px-3 py-2" data-testid="chat-selected">
          <span className="min-w-0 flex-1 truncate font-medium" title={title}>
            {title || value}
          </span>
        </div>
      )}
      <div className="relative">
        <Search size={14} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-muted" />
        <input
          className="input !pl-8"
          placeholder="Buscar chat"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          aria-label="Buscar chat"
          disabled={!accountId}
        />
      </div>
      <div className="max-h-44 overflow-y-auto rounded-lg border border-line bg-panel" role="listbox" aria-label="Chats">
        {chats.length === 0 ? (
          <p className="p-3 text-center text-sm text-muted">{loading ? "…" : accountId ? "Sin chats" : "Elige una cuenta"}</p>
        ) : (
          chats.map((c) => {
            const on = c.nativeId === value;
            return (
              <button
                key={c.key}
                type="button"
                role="option"
                aria-selected={on}
                onClick={() => onChange({ nativeId: c.nativeId, title: c.title })}
                className={`flex w-full items-center gap-2 px-3 py-1.5 text-left hover:bg-[var(--hover)] ${on ? "bg-[var(--hover)]" : ""}`}
              >
                <Avatar src={c.avatarUrl} name={c.title} size={28} colorKey={c.key} />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm" title={c.title}>
                    {c.title}
                  </span>
                  {c.lastPreview && <span className="block truncate text-xs text-muted">{truncate(c.lastPreview, 60)}</span>}
                </span>
              </button>
            );
          })
        )}
      </div>
    </div>
  );
}
