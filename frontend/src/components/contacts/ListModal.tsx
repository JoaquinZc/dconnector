import { useEffect, useState } from "react";
import { Check } from "lucide-react";
import type { ContactList } from "@/api/types";
import Modal from "@/components/common/Modal";
import { ACCOUNT_PALETTE } from "@/lib/channels";
import { DEFAULT_LIST_COLOR, validateListName } from "./contactModel";

interface Props {
  /** `null` = lista nueva. */
  list: ContactList | null;
  lists: ContactList[];
  onClose: () => void;
  onSave: (l: ContactList) => Promise<void>;
}

/** Crear o editar una lista: nombre y color. */
export default function ListModal({ list, lists, onClose, onSave }: Props) {
  const [name, setName] = useState(list?.name ?? "");
  const [color, setColor] = useState(list?.color || DEFAULT_LIST_COLOR);
  const [busy, setBusy] = useState(false);
  const [touched, setTouched] = useState(false);

  useEffect(() => {
    setName(list?.name ?? "");
    setColor(list?.color || DEFAULT_LIST_COLOR);
    setTouched(false);
  }, [list]);

  const error = validateListName(name, lists, list?.id ?? "");

  const submit = async () => {
    setTouched(true);
    if (error || busy) return;
    setBusy(true);
    try {
      await onSave({ id: list?.id ?? "", name: name.trim(), color, memberCount: list?.memberCount ?? 0 });
    } catch {
      /* el aviso ya salió */
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal
      open
      size="sm"
      title={list ? "Editar lista" : "Nueva lista"}
      onClose={onClose}
      footer={
        <>
          <button className="btn" onClick={onClose}>
            Cancelar
          </button>
          <button className="btn btn-primary" disabled={busy} onClick={() => void submit()}>
            Guardar
          </button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <div>
          <label className="label" htmlFor="list-name">
            Nombre
          </label>
          <input
            id="list-name"
            className="input"
            value={name}
            maxLength={60}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && void submit()}
            data-autofocus
          />
          {touched && error && <p className="mt-1 text-xs text-danger">{error}</p>}
        </div>
        <div>
          <span className="label">Color</span>
          <div className="flex flex-wrap gap-2" role="radiogroup" aria-label="Color de la lista">
            {ACCOUNT_PALETTE.map((c) => {
              const selected = c.toLowerCase() === color.toLowerCase();
              return (
                <button
                  key={c}
                  type="button"
                  role="radio"
                  aria-checked={selected}
                  aria-label={`Color ${c}`}
                  onClick={() => setColor(c)}
                  className="flex h-7 w-7 items-center justify-center rounded-full text-white transition-transform hover:scale-110"
                  style={{ background: c, outline: selected ? `2px solid ${c}` : "none", outlineOffset: 2 }}
                >
                  {selected && <Check size={15} strokeWidth={3} />}
                </button>
              );
            })}
          </div>
        </div>
      </div>
    </Modal>
  );
}
