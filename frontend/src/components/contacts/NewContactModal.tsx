import { useEffect, useState } from "react";
import type { Contact } from "@/api/types";
import Modal from "@/components/common/Modal";
import { clearDraft, loadDraft, saveDraft } from "@/lib/draft";
import ContactFields from "./ContactFields";
import { saveContactForm } from "./contactActions";
import { EMPTY_FORM, sameForm, validateForm, type ContactForm } from "./contactModel";

interface Props {
  onClose: () => void;
  onCreated: (c: Contact) => void;
}

const fresh = (): ContactForm => ({ ...EMPTY_FORM, tags: [], listIds: [] });

/** Alta manual: nombre, teléfono, etiquetas y notas. Guarda borrador en `draft:contacts:new`. */
export default function NewContactModal({ onClose, onCreated }: Props) {
  const [form, setForm] = useState<ContactForm>(() => {
    const d = loadDraft<Partial<ContactForm>>("contacts", "new");
    return d ? { ...fresh(), ...d, tags: d.tags ?? [], listIds: [] } : fresh();
  });
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (sameForm(form, fresh())) clearDraft("contacts", "new");
    else saveDraft("contacts", "new", form);
  }, [form]);

  const errors = touched ? validateForm(form) : {};

  const submit = async () => {
    setTouched(true);
    if (Object.keys(validateForm(form)).length > 0 || busy) return;
    setBusy(true);
    try {
      const c = await saveContactForm(null, form);
      clearDraft("contacts", "new");
      onCreated(c);
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
      title="Nuevo contacto"
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
      <ContactFields form={form} errors={errors} onChange={setForm} />
    </Modal>
  );
}
