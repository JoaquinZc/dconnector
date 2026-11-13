import { useEffect, useRef, useState } from "react";
import { GitMerge, MessageSquare, MessageSquarePlus, Split, Trash2, X } from "lucide-react";
import { api } from "@/api";
import type { Contact, ContactList, Identity } from "@/api/types";
import AccountPill from "@/components/common/AccountPill";
import Avatar from "@/components/common/Avatar";
import ChannelIcon from "@/components/common/ChannelIcon";
import { contactTargets, openChatWith, type ChatTarget } from "@/components/chat/newChat";
import { PopMenu, type PopItem } from "@/components/chat/parts";
import { clearDraft, loadDraft, saveDraft } from "@/lib/draft";
import { useAccounts } from "@/store/accounts";
import { useUI } from "@/store/ui";
import ContactFields from "./ContactFields";
import { openIdentityChat, saveContactForm } from "./contactActions";
import { canSplit, contactTitle, formFromContact, normalizeContact, sameForm, validateForm, type ContactForm } from "./contactModel";

interface Props {
  contact: Contact;
  lists: ContactList[];
  onClose: () => void;
  /** Guardado o separado: el contacto al día. */
  onChanged: (c: Contact) => void;
  onDeleted: (id: string) => void;
  onMerge: (c: Contact) => void;
  /** Se separó una identidad: nació un contacto nuevo y la tabla debe recargarse. */
  onSplit?: () => void;
}

function startForm(c: Contact): ContactForm {
  const base = formFromContact(c);
  const d = loadDraft<Partial<ContactForm>>("contacts", c.id);
  if (!d) return base;
  return { ...base, ...d, tags: d.tags ?? base.tags, listIds: d.listIds ?? base.listIds };
}

/** Ficha del contacto: datos, identidades por cuenta, listas, etiquetas, notas y baja de campañas. */
export default function ContactPanel({ contact, lists, onClose, onChanged, onDeleted, onMerge, onSplit }: Props) {
  const views = useAccounts((s) => s.views);
  const confirm = useUI((s) => s.confirm);
  const toast = useUI((s) => s.toast);

  const [form, setForm] = useState<ContactForm>(() => startForm(contact));
  const [touched, setTouched] = useState(false);
  const [busy, setBusy] = useState(false);
  const [writeMenu, setWriteMenu] = useState<{ x: number; y: number } | null>(null);
  const baseRef = useRef<ContactForm>(formFromContact(contact));
  const writing = useRef(false);

  // Si el contacto cambia por fuera (guardado, separado) y no hay cambios sin guardar, se refresca el formulario.
  useEffect(() => {
    const next = formFromContact(contact);
    setForm((f) => (sameForm(f, baseRef.current) ? next : f));
    baseRef.current = next;
  }, [contact]);

  const dirty = !sameForm(form, baseRef.current);

  useEffect(() => {
    if (dirty) saveDraft("contacts", contact.id, form);
    else clearDraft("contacts", contact.id);
  }, [form, dirty, contact.id]);

  const errors = touched ? validateForm(form) : {};

  const save = async () => {
    setTouched(true);
    if (Object.keys(validateForm(form)).length > 0 || busy) return;
    setBusy(true);
    try {
      const saved = await saveContactForm(contact, form);
      clearDraft("contacts", contact.id);
      baseRef.current = formFromContact(saved);
      setForm(baseRef.current);
      setTouched(false);
      onChanged(saved);
      toast("success", "Contacto guardado");
    } catch {
      /* el aviso ya salió */
    } finally {
      setBusy(false);
    }
  };

  const discard = () => {
    clearDraft("contacts", contact.id);
    setForm(baseRef.current);
    setTouched(false);
  };

  const split = async (i: Identity) => {
    const label = i.displayName || i.phone || i.peerNativeId;
    const ok = await confirm({
      title: "Separar identidad",
      message: `«${label}» pasa a ser un contacto aparte.`,
      confirmLabel: "Separar",
    });
    if (!ok) return;
    try {
      await api.contacts.SplitIdentity(contact.id, String(i.accountId), i.peerNativeId);
      // Se vuelve a leer el contacto abierto (no se da por hecho cuál de los dos devuelve el servicio).
      onChanged(normalizeContact(await api.contacts.Get(contact.id)));
      onSplit?.();
      toast("success", "Identidad separada");
    } catch {
      /* el aviso ya salió */
    }
  };

  const remove = async () => {
    const ok = await confirm({
      title: "Eliminar contacto",
      message: `Se borra «${contactTitle(contact)}». Sus chats no se tocan.`,
      confirmLabel: "Eliminar",
      danger: true,
    });
    if (!ok) return;
    try {
      await api.contacts.Delete(contact.id);
      clearDraft("contacts", contact.id);
      onDeleted(contact.id);
    } catch {
      /* el aviso ya salió */
    }
  };

  // «Escribir»: abre (o crea vacío) el chat con la persona y va a Chats con el chat abierto.
  const targets = contactTargets(contact, views);
  const write = async (t: ChatTarget) => {
    if (writing.current) return;
    writing.current = true;
    try {
      const chat = await openChatWith(t);
      if (chat) useUI.getState().openChat(chat.key);
    } finally {
      writing.current = false;
    }
  };
  const writeItems: PopItem[] = targets.map((t) => ({
    label: `${t.accountLabel}${t.detail ? ` · ${t.detail}` : ""}${t.enabled ? "" : ` — ${t.reason}`}`,
    disabled: !t.enabled,
    onClick: () => void write(t),
  }));
  const onlyTarget = targets.length === 1 ? targets[0] : null;
  const writeBlocked = targets.length === 0 ? "Sin cuenta ni teléfono para escribir" : onlyTarget && !onlyTarget.enabled ? onlyTarget.reason : "";

  const title = contactTitle(contact);
  const identities = contact.identities ?? [];
  const splittable = canSplit(contact);

  return (
    <aside className="flex w-[380px] shrink-0 flex-col border-l border-line bg-panel" aria-label="Ficha del contacto">
      <header className="flex items-center gap-3 border-b border-line px-4 py-3">
        <Avatar name={title} colorKey={contact.id} size={44} />
        <div className="min-w-0 flex-1">
          <h2 className="truncate text-base font-semibold" title={title}>
            {title}
          </h2>
          {dirty && <p className="text-xs text-warn">Cambios sin guardar</p>}
        </div>
        <button
          className="btn btn-primary"
          disabled={writeBlocked !== ""}
          title={writeBlocked || "Escribir a este contacto"}
          onClick={(e) => {
            if (onlyTarget) return void write(onlyTarget);
            const r = e.currentTarget.getBoundingClientRect();
            setWriteMenu({ x: r.left, y: r.bottom + 4 });
          }}
        >
          <MessageSquarePlus size={16} />
          Escribir
        </button>
        <button className="icon-btn" onClick={onClose} aria-label="Cerrar ficha" title="Cerrar">
          <X size={18} />
        </button>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto px-4 py-4">
        <div className="flex flex-col gap-6">
          <section aria-label="Identidades">
            <h3 className="label">Identidades</h3>
            {identities.length === 0 ? (
              <p className="text-sm text-muted">Sin identidades</p>
            ) : (
              <ul className="flex flex-col gap-2">
                {identities.map((i) => {
                  const acc = views[String(i.accountId)]?.account;
                  const text = i.displayName || i.phone || i.peerNativeId;
                  return (
                    <li
                      key={`${i.accountId}|${i.peerNativeId}`}
                      className="flex items-center gap-2 rounded-lg border border-line px-3 py-2"
                      data-testid="identity"
                    >
                      <ChannelIcon channel={String(i.channel)} size={22} />
                      <div className="min-w-0 flex-1">
                        <p className="truncate" title={text}>
                          {text}
                        </p>
                        {acc && <AccountPill label={acc.label} color={acc.color} />}
                      </div>
                      <button
                        className="icon-btn"
                        onClick={() => openIdentityChat(i)}
                        aria-label={`Abrir chat con ${text}`}
                        title="Abrir chat"
                      >
                        <MessageSquare size={17} />
                      </button>
                      {splittable && (
                        <button
                          className="icon-btn"
                          onClick={() => void split(i)}
                          aria-label={`Separar ${text}`}
                          title="Separar"
                        >
                          <Split size={17} />
                        </button>
                      )}
                    </li>
                  );
                })}
              </ul>
            )}
          </section>

          <ContactFields form={form} errors={errors} lists={lists} advanced onChange={setForm} />

          <div className="flex gap-2">
            <button className="btn flex-1" onClick={() => onMerge(contact)}>
              <GitMerge size={16} />
              Unir con otro
            </button>
            <button className="btn btn-danger" onClick={() => void remove()}>
              <Trash2 size={16} />
              Eliminar
            </button>
          </div>
        </div>
      </div>

      <footer className="flex items-center justify-end gap-2 border-t border-line px-4 py-3">
        <button className="btn" disabled={!dirty || busy} onClick={discard}>
          Descartar
        </button>
        <button className="btn btn-primary" disabled={!dirty || busy} onClick={() => void save()}>
          Guardar
        </button>
      </footer>
      {writeMenu && <PopMenu at={writeMenu} items={writeItems} onClose={() => setWriteMenu(null)} />}
    </aside>
  );
}
