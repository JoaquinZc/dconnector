// Acciones de Contactos que juntan varias llamadas al servicio (probables sin pantalla).
import { api } from "@/api";
import type { Contact } from "@/api/types";
import { useUI } from "@/store/ui";
import { chatKeyOf, contactFromForm, listChanges, normalizeContact, type ContactForm } from "./contactModel";

/**
 * Guarda el formulario (alta o edición). `Save` lleva los datos del contacto; el cambio de listas va
 * por `SetListMembers` y, si hubo, se vuelve a leer el contacto para tener las listas al día.
 */
export async function saveContactForm(base: Contact | null, form: ContactForm): Promise<Contact> {
  let saved = normalizeContact(await api.contacts.Save(contactFromForm(base, form)));
  const before = base ? normalizeContact(base).listIds ?? [] : [];
  const changes = listChanges(before, form.listIds);
  if (changes.length === 0) return saved;
  for (const ch of changes) {
    await api.contacts.SetListMembers(ch.listId, ch.add ? [saved.id] : null, ch.add ? null : [saved.id]);
  }
  saved = normalizeContact(await api.contacts.Get(saved.id));
  return saved;
}

/** Abre el chat directo de una identidad en la bandeja. */
export function openIdentityChat(i: { accountId: string; peerNativeId: string }): void {
  useUI.getState().openChat(chatKeyOf(i));
}
