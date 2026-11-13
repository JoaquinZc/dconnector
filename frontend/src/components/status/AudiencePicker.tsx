import { useEffect, useRef, useState } from "react";
import { Search, X } from "lucide-react";
import { api } from "@/api";
import type { Contact, ContactList } from "@/api/types";
import { cx } from "@/lib/cx";
import type { AudienceMode } from "./statusForm";

interface Props {
  accountId: string;
  /** `statusAudience`: se puede elegir. Si no, texto fijo. */
  choice: boolean;
  mode: AudienceMode;
  listIds: string[];
  peerIds: string[];
  onChange: (patch: { audienceMode?: AudienceMode; listIds?: string[]; peerIds?: string[] }) => void;
}

const MODES: Array<{ id: AudienceMode; label: string }> = [
  { id: "contacts", label: "Mis contactos" },
  { id: "allow", label: "Solo compartir con…" },
  { id: "deny", label: "Mis contactos excepto…" },
];

/** El id de esa persona en la cuenta, si el contacto tiene identidad ahí. */
export function peerOf(c: Contact, accountId: string): string | null {
  const id = (c.identities ?? []).find((i) => String(i.accountId) === accountId);
  return id ? id.peerNativeId : null;
}

export default function AudiencePicker({ accountId, choice, mode, listIds, peerIds, onChange }: Props) {
  const [lists, setLists] = useState<ContactList[]>([]);
  const [query, setQuery] = useState("");
  const [found, setFound] = useState<Contact[]>([]);
  const names = useRef<Record<string, string>>({});

  const picking = choice && mode !== "contacts";

  useEffect(() => {
    if (!picking) return;
    let alive = true;
    api.contacts
      .Lists()
      .then((l) => alive && setLists(l))
      .catch(() => undefined);
    return () => {
      alive = false;
    };
  }, [picking]);

  useEffect(() => {
    if (!picking) return;
    let alive = true;
    const t = setTimeout(() => {
      api.contacts
        .List({ query: query.trim(), listId: "", tag: "", offset: 0, limit: 40 })
        .then((page) => {
          if (!alive) return;
          const list = (page.contacts ?? []).filter((c) => peerOf(c, accountId));
          for (const c of list) names.current[peerOf(c, accountId)!] = c.name || c.phone || "Contacto";
          setFound(list);
        })
        .catch(() => undefined);
    }, 250);
    return () => {
      alive = false;
      clearTimeout(t);
    };
  }, [picking, query, accountId]);

  if (!choice) {
    return <p className="text-sm text-muted">Se usa la privacidad de estados del teléfono</p>;
  }

  const toggleList = (id: string) =>
    onChange({ listIds: listIds.includes(id) ? listIds.filter((x) => x !== id) : [...listIds, id] });
  const togglePeer = (id: string) =>
    onChange({ peerIds: peerIds.includes(id) ? peerIds.filter((x) => x !== id) : [...peerIds, id] });

  return (
    <div className="flex flex-col gap-3">
      <div role="radiogroup" aria-label="Audiencia" className="flex flex-col gap-1">
        {MODES.map((m) => (
          <label key={m.id} className="flex cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 hover:bg-[var(--hover)]">
            <input
              type="radio"
              name="status-audience"
              checked={mode === m.id}
              onChange={() => onChange({ audienceMode: m.id })}
              className="accent-[var(--accent)]"
            />
            <span>{m.label}</span>
          </label>
        ))}
      </div>

      {picking && (
        <div className="flex flex-col gap-3 rounded-lg border border-line bg-panel2 p-3">
          {lists.length > 0 && (
            <div>
              <span className="label">Listas</span>
              <div className="flex flex-wrap gap-1.5">
                {lists.map((l) => {
                  const on = listIds.includes(l.id);
                  return (
                    <button
                      key={l.id}
                      type="button"
                      aria-pressed={on}
                      onClick={() => toggleList(l.id)}
                      className={cx(
                        "rounded-full border px-2.5 py-1 text-xs font-medium",
                        on ? "border-accent bg-accent text-white" : "border-line bg-panel text-ink hover:bg-[var(--hover)]",
                      )}
                    >
                      {l.name}
                    </button>
                  );
                })}
              </div>
            </div>
          )}

          <div>
            <span className="label">Contactos</span>
            <div className="relative">
              <Search size={14} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-muted" />
              <input
                className="input !pl-8"
                placeholder="Buscar"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                aria-label="Buscar contactos"
              />
            </div>
            <div className="mt-2 max-h-40 overflow-y-auto rounded-lg border border-line bg-panel">
              {found.length === 0 ? (
                <p className="p-3 text-center text-sm text-muted">Sin contactos</p>
              ) : (
                found.map((c) => {
                  const pid = peerOf(c, accountId)!;
                  return (
                    <label key={c.id} className="flex cursor-pointer items-center gap-2 px-3 py-1.5 hover:bg-[var(--hover)]">
                      <input
                        type="checkbox"
                        checked={peerIds.includes(pid)}
                        onChange={() => togglePeer(pid)}
                        className="accent-[var(--accent)]"
                      />
                      <span className="min-w-0 flex-1 truncate" title={c.name}>
                        {c.name || c.phone}
                      </span>
                      {c.phone && c.name && <span className="text-xs text-muted">{c.phone}</span>}
                    </label>
                  );
                })
              )}
            </div>
          </div>

          {peerIds.length > 0 && (
            <div className="flex flex-wrap gap-1.5">
              {peerIds.map((id) => (
                <span key={id} className="inline-flex items-center gap-1 rounded-full bg-panel px-2 py-0.5 text-xs">
                  <span className="max-w-[140px] truncate" title={names.current[id] ?? id}>
                    {names.current[id] ?? id}
                  </span>
                  <button type="button" className="text-muted hover:text-ink" aria-label="Quitar" onClick={() => togglePeer(id)}>
                    <X size={12} />
                  </button>
                </span>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
