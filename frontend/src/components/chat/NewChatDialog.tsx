// «Nuevo chat»: buscador de contactos (nombre o teléfono) para escribir a alguien que aún no tiene
// conversación. Si el contacto tiene varias identidades se elige con qué cuenta escribir; las cuentas
// que no pueden (desconectadas, canal sin «iniciar chat») salen deshabilitadas con el motivo.
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Phone, Search } from "lucide-react";
import { api } from "@/api";
import type { Contact } from "@/api/types";
import AccountPill from "@/components/common/AccountPill";
import Avatar from "@/components/common/Avatar";
import ChannelIcon from "@/components/common/ChannelIcon";
import { buildFilter, contactTitle, normalizeContact } from "@/components/contacts/contactModel";
import { useAccounts } from "@/store/accounts";
import { contactTargets, numberTargets, openChatWith, phoneCandidate, type ChatTarget } from "./newChat";
import { Overlay, OverlayHeader } from "./parts";

const PAGE = 40;

interface Props {
  onClose: () => void;
  /** Se llama con la clave del chat (existente o recién creado vacío). */
  onOpen: (chatKey: string) => void;
}

function TargetBody({ t }: { t: ChatTarget }) {
  return (
    <span className="flex min-w-0 flex-1 items-center gap-2">
      <ChannelIcon channel={t.channel} size={16} />
      <AccountPill label={t.accountLabel} color={t.accountColor} />
      {t.detail && <span className="min-w-0 truncate text-[12px] text-[var(--muted)]">{t.detail}</span>}
      {!t.enabled && <span className="shrink-0 text-[12px] text-[var(--muted)]">· {t.reason}</span>}
    </span>
  );
}

export default function NewChatDialog({ onClose, onOpen }: Props) {
  const views = useAccounts((s) => s.views);
  const loadAccounts = useAccounts((s) => s.load);
  const accountsLoaded = useAccounts((s) => s.loaded);

  const [query, setQuery] = useState("");
  const [debounced, setDebounced] = useState("");
  const [rows, setRows] = useState<Contact[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const [busy, setBusy] = useState(false);
  const seq = useRef(0);
  const loadingRef = useRef(false);
  const rowsRef = useRef<Contact[]>([]);
  rowsRef.current = rows;

  useEffect(() => {
    if (!accountsLoaded) void loadAccounts();
  }, [accountsLoaded, loadAccounts]);

  useEffect(() => {
    const t = setTimeout(() => setDebounced(query), 250);
    return () => clearTimeout(t);
  }, [query]);

  // primera página de cada búsqueda
  useEffect(() => {
    const mine = ++seq.current;
    loadingRef.current = true;
    setLoading(true);
    api.contacts
      .List(buildFilter({ query: debounced, listId: "", tag: "" }, 0, PAGE))
      .then((page) => {
        if (mine !== seq.current) return;
        setRows((page?.contacts ?? []).map(normalizeContact));
        setTotal(page?.total ?? 0);
      })
      .catch(() => {
        if (mine === seq.current) setRows([]);
      })
      .finally(() => {
        if (mine === seq.current) {
          loadingRef.current = false;
          setLoading(false);
          setLoaded(true);
        }
      });
  }, [debounced]);

  const more = rows.length < total;
  const loadMore = useCallback(() => {
    if (loadingRef.current || rowsRef.current.length >= total) return;
    const mine = seq.current;
    loadingRef.current = true;
    setLoading(true);
    api.contacts
      .List(buildFilter({ query: debounced, listId: "", tag: "" }, rowsRef.current.length, PAGE))
      .then((page) => {
        if (mine !== seq.current) return;
        const next = (page?.contacts ?? []).map(normalizeContact);
        setRows((cur) => {
          const seen = new Set(cur.map((c) => c.id));
          return [...cur, ...next.filter((c) => !seen.has(c.id))];
        });
        setTotal(page?.total ?? total);
      })
      .catch(() => undefined)
      .finally(() => {
        if (mine === seq.current) {
          loadingRef.current = false;
          setLoading(false);
        }
      });
  }, [debounced, total]);

  const number = phoneCandidate(query);
  const byNumber = useMemo(() => (number ? numberTargets(number, views) : []), [number, views]);

  const choose = async (t: ChatTarget) => {
    if (busy || !t.enabled) return;
    setBusy(true);
    const chat = await openChatWith(t);
    if (!chat) {
      setBusy(false); // el aviso de error ya salió; el diálogo sigue abierto
      return;
    }
    onOpen(chat.key);
    onClose();
  };

  return (
    <Overlay onClose={onClose} width={480} label="Nuevo chat">
      <OverlayHeader title="Nuevo chat" onClose={onClose} />
      <div className="border-b border-[var(--border)] p-3">
        <div className="flex items-center gap-2 rounded-lg bg-[var(--panel-2)] px-3 py-1.5">
          <Search size={16} className="text-[var(--muted)]" />
          <input
            autoFocus
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Buscar contacto por nombre o teléfono"
            aria-label="Buscar contacto"
            className="min-w-0 flex-1 bg-transparent text-[14px] outline-none"
          />
        </div>
      </div>
      <div
        className="h-[420px] max-h-[60vh] overflow-y-auto"
        data-testid="new-chat-list"
        onScroll={(e) => {
          const el = e.currentTarget;
          if (more && el.scrollHeight - el.scrollTop - el.clientHeight < 160) loadMore();
        }}
      >
        {byNumber.length > 0 && (
          <section aria-label="Escribir a un número" className="border-b border-[var(--border)] px-4 py-2">
            <p className="mb-1 flex items-center gap-1.5 text-[12px] text-[var(--muted)]">
              <Phone size={13} /> Escribir al número {number} (si no está en tus contactos)
            </p>
            {byNumber.map((t) => (
              <button
                key={t.accountId}
                type="button"
                disabled={busy || !t.enabled}
                aria-label={`Escribir al número ${number} con ${t.accountLabel}`}
                onClick={() => void choose(t)}
                className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left hover:bg-black/5 disabled:cursor-not-allowed disabled:opacity-60 dark:hover:bg-white/10"
              >
                <TargetBody t={{ ...t, detail: "" }} />
              </button>
            ))}
          </section>
        )}

        {loaded && rows.length === 0 && (
          <div className="p-6 text-center text-[13px] text-[var(--muted)]">
            {debounced.trim() ? "Ningún contacto coincide" : "Aún no tienes contactos"}
          </div>
        )}

        <ul>
          {rows.map((c) => {
            const title = contactTitle(c);
            const targets = contactTargets(c, views);
            const header = (
              <span className="flex min-w-0 items-center gap-3">
                <Avatar name={title} colorKey={c.id} size={40} />
                <span className="min-w-0">
                  <span className="block truncate text-[14px] font-medium" title={title}>
                    {title}
                  </span>
                  {c.phone && <span className="block truncate text-[12px] text-[var(--muted)]">{c.phone}</span>}
                </span>
              </span>
            );
            if (targets.length === 0) {
              return (
                <li key={c.id} className="flex items-center gap-3 px-4 py-2 opacity-60" data-testid="new-chat-row">
                  {header}
                  <span className="ml-auto shrink-0 text-[12px] text-[var(--muted)]">Sin cuenta ni teléfono</span>
                </li>
              );
            }
            if (targets.length === 1) {
              const t = targets[0];
              return (
                <li key={c.id} data-testid="new-chat-row">
                  <button
                    type="button"
                    disabled={busy || !t.enabled}
                    aria-label={`Escribir a ${title} con ${t.accountLabel}`}
                    onClick={() => void choose(t)}
                    className="flex w-full items-center gap-3 px-4 py-2 text-left hover:bg-black/5 disabled:cursor-not-allowed disabled:opacity-60 dark:hover:bg-white/10"
                  >
                    {header}
                    <span className="ml-auto flex min-w-0 max-w-[55%] flex-col items-end gap-0.5">
                      <TargetBody t={{ ...t, detail: "" }} />
                    </span>
                  </button>
                </li>
              );
            }
            return (
              <li key={c.id} className="px-4 py-2" data-testid="new-chat-row">
                {header}
                <div className="mt-1 flex flex-col gap-0.5 pl-[52px]">
                  {targets.map((t) => (
                    <button
                      key={`${t.accountId}|${t.peerNativeId}`}
                      type="button"
                      disabled={busy || !t.enabled}
                      aria-label={`Escribir a ${title} con ${t.accountLabel}`}
                      onClick={() => void choose(t)}
                      className="flex items-center gap-2 rounded-lg px-2 py-1.5 text-left hover:bg-black/5 disabled:cursor-not-allowed disabled:opacity-60 dark:hover:bg-white/10"
                    >
                      <TargetBody t={t} />
                    </button>
                  ))}
                </div>
              </li>
            );
          })}
        </ul>

        {more && (
          <div className="p-3 text-center">
            <button type="button" className="text-[13px] text-[var(--accent)] hover:underline" disabled={loading} onClick={loadMore}>
              {loading ? "Cargando…" : "Mostrar más contactos"}
            </button>
          </div>
        )}
        {!loaded && loading && <div className="p-6 text-center text-[13px] text-[var(--muted)]">Cargando…</div>}
      </div>
    </Overlay>
  );
}
