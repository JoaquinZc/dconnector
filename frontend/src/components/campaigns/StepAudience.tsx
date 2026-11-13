import { useEffect, useRef, useState } from "react";
import { Loader2, Search, X } from "lucide-react";
import { api } from "@/api";
import type { CampaignReview, Contact, ContactList } from "@/api/types";
import { cx } from "@/lib/cx";
import { riskCounts, skippedCounts, type CampaignForm } from "./campaignModel";

interface Props {
  form: CampaignForm;
  patch: (p: Partial<CampaignForm>) => void;
  review: CampaignReview | null;
  reviewing: boolean;
}

/** El contacto tiene identidad en esa cuenta. */
export function hasIdentity(c: Contact, accountId: string): boolean {
  return (c.identities ?? []).some((i) => String(i.accountId) === accountId);
}

export function contactName(c: Contact): string {
  return c.name || c.phone || "Contacto";
}

/** Paso 2: listas y contactos sueltos, con el recuento por riesgo y los omitidos. */
export default function StepAudience({ form, patch, review, reviewing }: Props) {
  const [lists, setLists] = useState<ContactList[]>([]);
  const [query, setQuery] = useState("");
  const [found, setFound] = useState<Contact[]>([]);
  const names = useRef<Record<string, string>>({});
  const [, bump] = useState(0);

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
    const t = setTimeout(() => {
      api.contacts
        .List({ query: query.trim(), listId: "", tag: "", offset: 0, limit: 40 })
        .then((page) => {
          if (!alive) return;
          const list = page.contacts ?? [];
          for (const c of list) names.current[c.id] = contactName(c);
          setFound(list);
        })
        .catch(() => undefined);
    }, 250);
    return () => {
      alive = false;
      clearTimeout(t);
    };
  }, [query]);

  // Nombres de los contactos ya elegidos (borrador o campaña guardada) que aún no se han visto.
  useEffect(() => {
    const missing = form.contactIds.filter((id) => !names.current[id]).slice(0, 30);
    if (missing.length === 0) return;
    let alive = true;
    void Promise.all(
      missing.map((id) =>
        api.contacts
          .Get(id)
          .then((c) => {
            names.current[id] = contactName(c);
          })
          .catch(() => {
            names.current[id] = "Contacto";
          }),
      ),
    ).then(() => alive && bump((n) => n + 1));
    return () => {
      alive = false;
    };
  }, [form.contactIds]);

  const toggleList = (id: string) =>
    patch({ listIds: form.listIds.includes(id) ? form.listIds.filter((x) => x !== id) : [...form.listIds, id] });
  const toggleContact = (id: string) =>
    patch({ contactIds: form.contactIds.includes(id) ? form.contactIds.filter((x) => x !== id) : [...form.contactIds, id] });

  const risks = riskCounts(review);
  const skipped = skippedCounts(review);

  return (
    <div className="grid grid-cols-1 gap-5 md:grid-cols-[1fr_240px]">
      <div className="flex min-w-0 flex-col gap-4">
        <div>
          <span className="label">Listas</span>
          {lists.length === 0 ? (
            <p className="text-sm text-muted">Sin listas</p>
          ) : (
            <div className="flex flex-wrap gap-1.5">
              {lists.map((l) => {
                const on = form.listIds.includes(l.id);
                return (
                  <button
                    key={l.id}
                    type="button"
                    aria-pressed={on}
                    onClick={() => toggleList(l.id)}
                    title={l.name}
                    className={cx(
                      "inline-flex max-w-[220px] items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium",
                      on ? "border-accent bg-accent text-white" : "border-line bg-panel hover:bg-[var(--hover)]",
                    )}
                  >
                    {l.color && <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: l.color }} />}
                    <span className="truncate">{l.name}</span>
                    <span className={on ? "opacity-80" : "text-muted"}>{l.memberCount}</span>
                  </button>
                );
              })}
            </div>
          )}
        </div>

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
          <div className="mt-2 max-h-52 overflow-y-auto rounded-lg border border-line bg-panel">
            {found.length === 0 ? (
              <p className="p-3 text-center text-sm text-muted">Sin contactos</p>
            ) : (
              found.map((c) => {
                const ok = hasIdentity(c, form.accountId);
                return (
                  <label key={c.id} className="flex cursor-pointer items-center gap-2 px-3 py-1.5 hover:bg-[var(--hover)]">
                    <input
                      type="checkbox"
                      checked={form.contactIds.includes(c.id)}
                      onChange={() => toggleContact(c.id)}
                      className="accent-[var(--accent)]"
                    />
                    <span className="min-w-0 flex-1 truncate" title={contactName(c)}>
                      {contactName(c)}
                    </span>
                    {c.optedOut && <span className="text-xs text-danger">Baja</span>}
                    {!ok && !c.optedOut && <span className="text-xs text-muted">Sin identidad</span>}
                    {c.phone && c.name && <span className="text-xs text-muted">{c.phone}</span>}
                  </label>
                );
              })
            )}
          </div>
        </div>

        {form.contactIds.length > 0 && (
          <div className="flex flex-wrap gap-1.5">
            {form.contactIds.map((id) => (
              <span key={id} className="inline-flex items-center gap-1 rounded-full bg-panel2 px-2 py-0.5 text-xs">
                <span className="max-w-[160px] truncate" title={names.current[id] ?? id}>
                  {names.current[id] ?? "…"}
                </span>
                <button type="button" className="text-muted hover:text-ink" aria-label="Quitar" onClick={() => toggleContact(id)}>
                  <X size={12} />
                </button>
              </span>
            ))}
          </div>
        )}
      </div>

      <aside className="h-fit rounded-xl border border-line bg-panel2 p-4" data-testid="audience-counts">
        <div className="mb-3 flex items-center gap-2">
          <h3 className="flex-1 text-sm font-semibold">Destinatarios</h3>
          {reviewing && <Loader2 size={14} className="animate-spin text-muted" aria-label="Revisando" />}
        </div>
        <p className="mb-3 text-3xl font-semibold" data-testid="recipients-count">
          {review ? review.recipients : "—"}
        </p>
        <ul className="mb-3 flex flex-col gap-1.5">
          {risks.map((r) => (
            <li key={r.id} className="flex items-center gap-2 text-sm" data-testid={`risk-${r.id}`}>
              <span className="h-2.5 w-2.5 shrink-0 rounded-full" style={{ background: r.color }} aria-hidden />
              <span className="flex-1">{r.label}</span>
              <span className="font-medium">{r.count}</span>
            </li>
          ))}
        </ul>
        <div className="border-t border-line pt-3">
          <span className="label">Omitidos</span>
          <ul className="flex flex-col gap-1">
            {skipped.map((s) => (
              <li key={s.id} className="flex items-center gap-2 text-sm" data-testid={`skipped-${s.id}`}>
                <span className="flex-1 text-muted">{s.label}</span>
                <span className="font-medium">{s.count}</span>
              </li>
            ))}
          </ul>
        </div>
      </aside>
    </div>
  );
}
