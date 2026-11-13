import type { Contact } from "@/api/types";
import AccountPill from "@/components/common/AccountPill";
import Avatar from "@/components/common/Avatar";
import ChannelIcon from "@/components/common/ChannelIcon";
import { Skeleton } from "@/components/common/Skeleton";
import { useAccounts } from "@/store/accounts";
import { contactTitle } from "./contactModel";

/** Resumen de un contacto (para comparar al unir): nombre, teléfono, identidades y etiquetas. */
export default function ContactPreview({ contact }: { contact: Contact | null }) {
  const views = useAccounts((s) => s.views);
  if (!contact) {
    return (
      <div role="status" aria-label="Cargando" className="flex min-w-0 flex-col gap-2 p-3" data-testid="contact-preview-skeleton">
        <div className="flex items-center gap-3">
          <Skeleton className="h-10 w-10 shrink-0 rounded-full" />
          <div className="flex min-w-0 flex-1 flex-col gap-2">
            <Skeleton className="h-4 w-2/3" />
            <Skeleton className="h-3.5 w-1/2" />
          </div>
        </div>
        <Skeleton className="h-3.5 w-full" />
        <Skeleton className="h-3.5 w-4/5" />
      </div>
    );
  }
  const title = contactTitle(contact);
  const identities = contact.identities ?? [];
  const tags = contact.tags ?? [];
  return (
    <div className="flex min-w-0 flex-col gap-2 p-3" data-testid="contact-preview">
      <div className="flex items-center gap-3">
        <Avatar name={title} colorKey={contact.id} size={40} />
        <div className="min-w-0">
          <p className="truncate font-semibold" title={title}>
            {title}
          </p>
          <p className="truncate text-sm text-muted">{contact.phone || "Sin teléfono"}</p>
        </div>
      </div>
      <ul className="flex flex-col gap-1">
        {identities.map((i) => {
          const acc = views[String(i.accountId)]?.account;
          const text = i.displayName || i.phone || i.peerNativeId;
          return (
            <li key={`${i.accountId}|${i.peerNativeId}`} className="flex min-w-0 items-center gap-2 text-sm">
              <ChannelIcon channel={String(i.channel)} size={16} />
              <span className="min-w-0 flex-1 truncate" title={text}>
                {text}
              </span>
              {acc && <AccountPill label={acc.label} color={acc.color} />}
            </li>
          );
        })}
        {identities.length === 0 && <li className="text-sm text-muted">Sin identidades</li>}
      </ul>
      {tags.length > 0 && (
        <p className="truncate text-xs text-muted" title={tags.join(", ")}>
          {tags.join(" · ")}
        </p>
      )}
    </div>
  );
}
