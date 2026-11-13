import { useEffect, useState } from "react";
import { ArrowLeftRight } from "lucide-react";
import { api } from "@/api";
import type { Contact } from "@/api/types";
import Modal from "@/components/common/Modal";
import { cx } from "@/lib/cx";
import ContactPreview from "./ContactPreview";
import { buildFilter, contactTitle, normalizeContact } from "./contactModel";

interface Props {
  /** El contacto abierto en la ficha. */
  contact: Contact;
  onClose: () => void;
  /** `kept` es el contacto resultante y `absorbedId` el que desaparece. */
  onMerged: (kept: Contact, absorbedId: string) => void;
}

/** Tarjeta de uno de los dos contactos, marcada con lo que pasará con él. */
export function MergeCard({ contact, keeps }: { contact: Contact | null; keeps: boolean }) {
  return (
    <div className={cx("rounded-xl border", keeps ? "border-[var(--accent)]" : "border-line opacity-70")}>
      <ContactPreview contact={contact} />
      <p className="px-3 pb-2 text-xs font-semibold text-muted">{keeps ? "Se conserva" : "Se absorbe"}</p>
    </div>
  );
}

/** Une el contacto abierto con otro elegido con el buscador. */
export default function MergeModal({ contact, onClose, onMerged }: Props) {
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<Contact[]>([]);
  const [searching, setSearching] = useState(false);
  const [other, setOther] = useState<Contact | null>(null);
  // `true` = el contacto abierto es el que se conserva.
  const [keepCurrent, setKeepCurrent] = useState(true);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let alive = true;
    setSearching(true);
    const t = setTimeout(() => {
      api.contacts
        .List(buildFilter({ query, listId: "", tag: "" }, 0, 20))
        .then((p) => alive && setResults((p?.contacts ?? []).map(normalizeContact).filter((c) => c.id !== contact.id)))
        .catch(() => undefined)
        .finally(() => alive && setSearching(false));
    }, query ? 250 : 0);
    return () => {
      alive = false;
      clearTimeout(t);
    };
  }, [query, contact.id]);

  const merge = async () => {
    if (!other || busy) return;
    const keep = keepCurrent ? contact : other;
    const absorbed = keepCurrent ? other : contact;
    setBusy(true);
    try {
      const merged = await api.contacts.Merge(keep.id, absorbed.id);
      onMerged(normalizeContact(merged), absorbed.id);
    } catch {
      /* el aviso ya salió */
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal
      open
      size="lg"
      title="Unir contactos"
      onClose={onClose}
      footer={
        <>
          <button className="btn" onClick={onClose}>
            Cancelar
          </button>
          <button className="btn btn-primary" disabled={!other || busy} onClick={() => void merge()}>
            Unir
          </button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <div>
          <label className="label" htmlFor="merge-search">
            Buscar contacto
          </label>
          <input
            id="merge-search"
            className="input"
            placeholder="Nombre o teléfono"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            data-autofocus
          />
        </div>

        {!other ? (
          <ul className="max-h-72 overflow-y-auto rounded-lg border border-line" aria-label="Resultados">
            {results.map((c) => (
              <li key={c.id}>
                <button
                  type="button"
                  className="flex w-full items-center gap-3 border-b border-line px-3 py-2 text-left last:border-b-0 hover:bg-[var(--hover)]"
                  onClick={() => setOther(c)}
                >
                  <span className="min-w-0 flex-1 truncate font-medium">{contactTitle(c)}</span>
                  <span className="text-sm text-muted">{c.phone}</span>
                </button>
              </li>
            ))}
            {results.length === 0 && (
              <li className="px-3 py-6 text-center text-sm text-muted">{searching ? "Buscando…" : "Sin resultados"}</li>
            )}
          </ul>
        ) : (
          <>
            <div className="grid grid-cols-[1fr_auto_1fr] items-stretch gap-2">
              <MergeCard contact={contact} keeps={keepCurrent} />
              <button
                className="icon-btn self-center"
                onClick={() => setKeepCurrent((v) => !v)}
                aria-label="Cambiar cuál se conserva"
                title="Cambiar cuál se conserva"
              >
                <ArrowLeftRight size={18} />
              </button>
              <MergeCard contact={other} keeps={!keepCurrent} />
            </div>
            <button className="btn btn-sm self-start" onClick={() => setOther(null)}>
              Elegir otro
            </button>
          </>
        )}
      </div>
    </Modal>
  );
}
