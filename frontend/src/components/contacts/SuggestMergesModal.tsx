import { useEffect, useState } from "react";
import { ArrowLeftRight, GitMerge, Loader2 } from "lucide-react";
import { api } from "@/api";
import type { Contact } from "@/api/types";
import EmptyState from "@/components/common/EmptyState";
import Modal from "@/components/common/Modal";
import { MergeCard } from "./MergeModal";
import { cleanPairs, dropPairsWith, normalizeContact } from "./contactModel";

interface Props {
  onClose: () => void;
  onMerged: (kept: Contact, absorbedId: string) => void;
}

function PairRow({
  a,
  b,
  onMerge,
  onSkip,
}: {
  a: string;
  b: string;
  onMerge: (keep: Contact, absorbed: Contact) => Promise<void>;
  onSkip: () => void;
}) {
  const [ca, setCa] = useState<Contact | null>(null);
  const [cb, setCb] = useState<Contact | null>(null);
  const [keepFirst, setKeepFirst] = useState(true);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let alive = true;
    Promise.all([api.contacts.Get(a), api.contacts.Get(b)])
      .then(([x, y]) => {
        if (!alive) return;
        setCa(normalizeContact(x));
        setCb(normalizeContact(y));
      })
      .catch(() => alive && onSkip());
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [a, b]);

  const merge = async () => {
    if (!ca || !cb || busy) return;
    setBusy(true);
    try {
      await (keepFirst ? onMerge(ca, cb) : onMerge(cb, ca));
    } finally {
      setBusy(false);
    }
  };

  return (
    <li className="rounded-xl border border-line p-3" data-testid="merge-pair">
      <div className="grid grid-cols-[1fr_auto_1fr] items-stretch gap-2">
        <MergeCard contact={ca} keeps={keepFirst} />
        <button
          className="icon-btn self-center"
          onClick={() => setKeepFirst((v) => !v)}
          aria-label="Cambiar cuál se conserva"
          title="Cambiar cuál se conserva"
        >
          <ArrowLeftRight size={18} />
        </button>
        <MergeCard contact={cb} keeps={!keepFirst} />
      </div>
      <div className="mt-3 flex justify-end gap-2">
        <button className="btn btn-sm" onClick={onSkip} disabled={busy}>
          Ignorar
        </button>
        <button className="btn btn-sm btn-primary" onClick={() => void merge()} disabled={!ca || !cb || busy}>
          Unir
        </button>
      </div>
    </li>
  );
}

/** Pares de contactos con el mismo teléfono, lado a lado, para unirlos o ignorarlos. */
export default function SuggestMergesModal({ onClose, onMerged }: Props) {
  const [pairs, setPairs] = useState<Array<[string, string]> | null>(null);

  useEffect(() => {
    let alive = true;
    api.contacts
      .SuggestMerges()
      .then((raw) => alive && setPairs(cleanPairs(raw)))
      .catch(() => alive && setPairs([]));
    return () => {
      alive = false;
    };
  }, []);

  const skip = (a: string, b: string) =>
    setPairs((list) => (list ? list.filter(([x, y]) => !(x === a && y === b)) : list));

  const merge = async (keep: Contact, absorbed: Contact) => {
    try {
      const merged = await api.contacts.Merge(keep.id, absorbed.id);
      setPairs((list) => (list ? dropPairsWith(list, absorbed.id) : list));
      onMerged(normalizeContact(merged), absorbed.id);
    } catch {
      /* el aviso ya salió */
    }
  };

  return (
    <Modal open size="lg" title="Sugerencias de unión" onClose={onClose} footer={<button className="btn" onClick={onClose}>Cerrar</button>}>
      {pairs === null ? (
        <div className="flex justify-center p-8 text-muted">
          <Loader2 className="animate-spin" size={22} />
        </div>
      ) : pairs.length === 0 ? (
        <EmptyState icon={GitMerge} title="Sin sugerencias" />
      ) : (
        <ul className="flex flex-col gap-3">
          {pairs.map(([a, b]) => (
            <PairRow key={`${a}|${b}`} a={a} b={b} onMerge={merge} onSkip={() => skip(a, b)} />
          ))}
        </ul>
      )}
    </Modal>
  );
}
