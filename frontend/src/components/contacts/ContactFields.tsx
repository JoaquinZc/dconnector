import { useState } from "react";
import { X } from "lucide-react";
import type { ContactList } from "@/api/types";
import Switch from "@/components/settings/Switch";
import { cx } from "@/lib/cx";
import { addTags, removeTag, sortLists, toggleId, type ContactForm, type FormErrors } from "./contactModel";

interface Props {
  form: ContactForm;
  errors?: FormErrors;
  lists?: ContactList[];
  /** Muestra listas y «No recibe campañas» (la ficha; el alta manual no). */
  advanced?: boolean;
  onChange: (f: ContactForm) => void;
}

/** Campos del contacto: nombre, teléfono, etiquetas, notas y, en la ficha, listas y baja de campañas. */
export default function ContactFields({ form, errors, lists = [], advanced, onChange }: Props) {
  const [tagText, setTagText] = useState("");
  const set = (p: Partial<ContactForm>) => onChange({ ...form, ...p });

  const commitTag = () => {
    if (!tagText.trim()) return;
    set({ tags: addTags(form.tags, tagText) });
    setTagText("");
  };

  return (
    <div className="flex flex-col gap-4">
      <div>
        <label className="label" htmlFor="contact-name">
          Nombre
        </label>
        <input
          id="contact-name"
          className="input"
          value={form.name}
          maxLength={120}
          onChange={(e) => set({ name: e.target.value })}
          aria-invalid={!!errors?.name}
        />
        {errors?.name && <p className="mt-1 text-xs text-danger">{errors.name}</p>}
      </div>

      <div>
        <label className="label" htmlFor="contact-phone">
          Teléfono
        </label>
        <input
          id="contact-phone"
          className="input"
          inputMode="tel"
          placeholder="+593…"
          value={form.phone}
          maxLength={32}
          onChange={(e) => set({ phone: e.target.value })}
          aria-invalid={!!errors?.phone}
        />
        {errors?.phone && <p className="mt-1 text-xs text-danger">{errors.phone}</p>}
      </div>

      <div>
        <label className="label" htmlFor="contact-tags">
          Etiquetas
        </label>
        <div className="flex flex-wrap gap-1.5">
          {form.tags.map((t) => (
            <span key={t} className="inline-flex items-center gap-1 rounded-full bg-panel2 py-0.5 pl-2.5 pr-1 text-xs">
              <span className="max-w-[160px] truncate" title={t}>
                {t}
              </span>
              <button
                type="button"
                className="rounded-full p-0.5 text-muted hover:text-ink"
                aria-label={`Quitar etiqueta ${t}`}
                onClick={() => set({ tags: removeTag(form.tags, t) })}
              >
                <X size={12} />
              </button>
            </span>
          ))}
        </div>
        <input
          id="contact-tags"
          className="input mt-2"
          placeholder="Añadir etiqueta"
          value={tagText}
          onChange={(e) => {
            const v = e.target.value;
            if (/[,;]/.test(v)) {
              set({ tags: addTags(form.tags, v) });
              setTagText("");
            } else setTagText(v);
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              commitTag();
            }
          }}
          onBlur={commitTag}
        />
      </div>

      <div>
        <label className="label" htmlFor="contact-notes">
          Notas
        </label>
        <textarea
          id="contact-notes"
          className="input"
          rows={3}
          value={form.notes}
          onChange={(e) => set({ notes: e.target.value })}
        />
      </div>

      {advanced && (
        <>
          <div>
            <span className="label">Listas</span>
            {lists.length === 0 ? (
              <p className="text-sm text-muted">Sin listas</p>
            ) : (
              <div className="flex flex-wrap gap-1.5" role="group" aria-label="Listas del contacto">
                {sortLists(lists).map((l) => {
                  const on = form.listIds.includes(l.id);
                  return (
                    <button
                      key={l.id}
                      type="button"
                      role="checkbox"
                      aria-checked={on}
                      onClick={() => set({ listIds: toggleId(form.listIds, l.id) })}
                      className={cx(
                        "inline-flex max-w-full items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs",
                        on ? "border-transparent text-white" : "border-line text-muted hover:text-ink",
                      )}
                      style={on ? { background: l.color } : undefined}
                      title={l.name}
                    >
                      {!on && <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: l.color }} />}
                      <span className="truncate">{l.name}</span>
                    </button>
                  );
                })}
              </div>
            )}
          </div>

          <div className="flex items-center gap-3">
            <span className="flex-1">No recibe campañas</span>
            <Switch checked={form.optedOut} onChange={(v) => set({ optedOut: v })} label="No recibe campañas" />
          </div>
        </>
      )}
    </div>
  );
}
