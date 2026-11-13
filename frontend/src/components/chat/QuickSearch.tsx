// Búsqueda rápida (Ctrl+K): busca en los mensajes de todas las cuentas con `Inbox.Search`.
import { useEffect, useMemo, useState } from "react";
import { Search } from "lucide-react";
import { api } from "@/api";
import { useInbox } from "@/store/inbox";
import type { Message } from "../../api/core/model/models.js";
import { highlightParts, snippetAround } from "./QuickSearchHelpers";
import { formatListTime, messagePreview } from "./chatFormat";
import { AccountChip, ChannelBadge, Overlay } from "./parts";

export default function QuickSearch({
  accounts,
  onClose,
  onOpen,
}: {
  accounts: Record<string, { label: string; color: string }>;
  onClose: () => void;
  onOpen: (chatKey: string, msg: Message) => void;
}) {
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<Message[]>([]);
  const [loading, setLoading] = useState(false);
  const [sel, setSel] = useState(0);
  const chats = useInbox((s) => s.chats);

  useEffect(() => {
    const q = query.trim();
    if (q.length < 2) {
      setResults([]);
      setLoading(false);
      return;
    }
    let alive = true;
    setLoading(true);
    const t = setTimeout(() => {
      api.inbox
        .Search(q, null as never, 40)
        .then((r) => {
          if (!alive) return;
          setResults(r);
          setSel(0);
        })
        .catch(() => undefined)
        .finally(() => alive && setLoading(false));
    }, 250);
    return () => {
      alive = false;
      clearTimeout(t);
    };
  }, [query]);

  const rows = useMemo(
    () =>
      results.map((m) => ({
        m,
        title: chats[m.chatKey]?.title || (m.fromMe ? "Tú" : m.senderName) || "Chat",
        snippet: snippetAround(m.text || messagePreview(m), query),
      })),
    [results, chats, query],
  );

  return (
    <Overlay onClose={onClose} width={560} label="Búsqueda rápida">
      <div className="flex items-center gap-3 border-b border-[var(--border)] px-4 py-3">
        <Search size={18} className="text-[var(--muted)]" />
        <input
          autoFocus
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "ArrowDown") {
              e.preventDefault();
              setSel((s) => Math.min(rows.length - 1, s + 1));
            } else if (e.key === "ArrowUp") {
              e.preventDefault();
              setSel((s) => Math.max(0, s - 1));
            } else if (e.key === "Enter" && rows[sel]) {
              onOpen(rows[sel].m.chatKey, rows[sel].m);
              onClose();
            }
          }}
          placeholder="Buscar en todos los mensajes"
          className="min-w-0 flex-1 bg-transparent text-[15px] outline-none"
        />
      </div>
      <div className="min-h-[120px] flex-1 overflow-y-auto">
        {rows.length === 0 && (
          <div className="p-8 text-center text-[13.5px] text-[var(--muted)]">
            {loading ? "Buscando…" : query.trim().length >= 2 ? "Sin resultados" : ""}
          </div>
        )}
        {rows.map((r, i) => {
          const acc = accounts[r.m.accountId];
          return (
            <button
              key={r.m.key}
              type="button"
              onMouseEnter={() => setSel(i)}
              onClick={() => {
                onOpen(r.m.chatKey, r.m);
                onClose();
              }}
              className={`flex w-full flex-col gap-0.5 px-4 py-2.5 text-left ${i === sel ? "bg-black/5 dark:bg-white/10" : ""}`}
            >
              <div className="flex items-center gap-2">
                <ChannelBadge channel={r.m.channel as string} size={14} />
                <span className="min-w-0 flex-1 truncate text-[14px] font-medium" title={r.title}>{r.title}</span>
                {acc && <AccountChip label={acc.label} color={acc.color} />}
                <span className="shrink-0 text-[11.5px] text-[var(--muted)]">{formatListTime(r.m.timestamp)}</span>
              </div>
              <div className="line-clamp-2 break-words text-[13px] text-[var(--muted)]">
                {r.m.fromMe && "Tú: "}
                {highlightParts(r.snippet, query).map((p, k) =>
                  p.hit ? (
                    <mark key={k} className="rounded bg-[var(--accent)]/25 text-[var(--text)]">{p.text}</mark>
                  ) : (
                    <span key={k}>{p.text}</span>
                  ),
                )}
              </div>
            </button>
          );
        })}
      </div>
    </Overlay>
  );
}
