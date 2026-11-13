import { useEffect, useRef } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { BellOff } from "lucide-react";
import type { Contact, ContactList } from "@/api/types";
import Avatar from "@/components/common/Avatar";
import ChannelIcon from "@/components/common/ChannelIcon";
import { channelName } from "@/lib/channels";
import { cx } from "@/lib/cx";
import { ContactRowsSkeleton } from "@/pages/skeletons/zones";
import { channelsOf, contactTitle, sourceOf } from "./contactModel";

export const ROW_HEIGHT = 56;
export const CONTACT_COLUMNS = "minmax(200px,2.2fr) 150px 110px minmax(120px,1.6fr) minmax(120px,1.6fr)";
const COLUMNS = CONTACT_COLUMNS;

interface Props {
  rows: Contact[];
  lists: ContactList[];
  selectedId: string | null;
  loading: boolean;
  /** Primera carga en curso: la cabecera de la tabla se pinta y las filas son esqueletos. */
  initialLoading?: boolean;
  more: boolean;
  onLoadMore: () => void;
  onOpen: (c: Contact) => void;
  onTag: (tag: string) => void;
}

function Chips({ children, empty }: { children: React.ReactNode[]; empty?: boolean }) {
  if (empty) return null;
  return <div className="flex min-w-0 flex-wrap gap-1 overflow-hidden" style={{ maxHeight: ROW_HEIGHT - 14 }}>{children}</div>;
}

/** Tabla virtualizada de contactos con carga de la página siguiente al acercarse al final. */
export default function ContactsTable({ rows, lists, selectedId, loading, initialLoading = false, more, onLoadMore, onOpen, onTag }: Props) {
  const parentRef = useRef<HTMLDivElement>(null);
  const byId = new Map(lists.map((l) => [l.id, l]));

  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 8,
    getItemKey: (i) => rows[i]?.id ?? i,
  });
  const items = virtualizer.getVirtualItems();
  const lastIndex = items.length ? items[items.length - 1].index : -1;

  useEffect(() => {
    if (lastIndex >= 0 && lastIndex >= rows.length - 15 && more && !loading) onLoadMore();
  }, [lastIndex, rows.length, more, loading, onLoadMore]);

  return (
    <div role="table" aria-label="Contactos" aria-rowcount={rows.length} className="flex min-h-0 flex-1 flex-col">
      <div
        role="row"
        className="grid items-center gap-3 border-b border-line bg-panel px-4 py-2 text-xs font-semibold text-muted"
        style={{ gridTemplateColumns: COLUMNS }}
      >
        <span role="columnheader">Nombre</span>
        <span role="columnheader">Teléfono</span>
        <span role="columnheader">Canales</span>
        <span role="columnheader">Listas</span>
        <span role="columnheader">Etiquetas</span>
      </div>
      <div ref={parentRef} className="min-h-0 flex-1 overflow-y-auto bg-panel" data-testid="contacts-scroll">
        {initialLoading && <ContactRowsSkeleton />}
        <div style={{ height: virtualizer.getTotalSize(), position: "relative", width: "100%" }}>
          {items.map((vi) => {
            const c = rows[vi.index];
            if (!c) return null;
            const title = contactTitle(c);
            const tags = c.tags ?? [];
            const listIds = (c.listIds ?? []).filter((id) => byId.has(id));
            return (
              <div
                key={vi.key}
                role="row"
                tabIndex={0}
                aria-selected={selectedId === c.id}
                data-testid="contact-row"
                data-contact-id={c.id}
                onClick={() => onOpen(c)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    onOpen(c);
                  }
                }}
                className={cx(
                  "absolute left-0 top-0 grid w-full cursor-pointer items-center gap-3 border-b border-line px-4",
                  selectedId === c.id ? "bg-panel2" : "hover:bg-[var(--hover)]",
                )}
                style={{ height: vi.size, transform: `translateY(${vi.start}px)`, gridTemplateColumns: COLUMNS }}
              >
                <div role="cell" className="flex min-w-0 items-center gap-3">
                  <Avatar name={title} colorKey={c.id} size={36} />
                  <span className="min-w-0 truncate font-medium" title={title}>
                    {title}
                  </span>
                  {sourceOf(c) === "auto" && (
                    <span
                      className="shrink-0 rounded-full bg-panel2 px-1.5 py-px text-[10.5px] font-medium text-muted"
                      title="Se agregó sola al vincular una cuenta"
                      data-testid="auto-badge"
                    >
                      Automático
                    </span>
                  )}
                  {c.optedOut && <BellOff size={14} className="shrink-0 text-muted" aria-label="No recibe campañas" />}
                </div>
                <span role="cell" className="truncate text-muted" title={c.phone}>
                  {c.phone}
                </span>
                <div role="cell" className="flex items-center gap-1">
                  {channelsOf(c).map((ch) => (
                    <span key={ch} title={channelName(ch)}>
                      <ChannelIcon channel={ch} size={18} />
                    </span>
                  ))}
                </div>
                <div role="cell" className="min-w-0">
                  <Chips empty={listIds.length === 0}>
                    {listIds.map((id) => (
                      <span
                        key={id}
                        className="inline-flex max-w-full items-center gap-1 rounded-full bg-panel2 px-2 py-0.5 text-xs"
                        title={byId.get(id)?.name}
                      >
                        <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: byId.get(id)?.color }} />
                        <span className="truncate">{byId.get(id)?.name}</span>
                      </span>
                    ))}
                  </Chips>
                </div>
                <div role="cell" className="min-w-0">
                  <Chips empty={tags.length === 0}>
                    {tags.map((t) => (
                      <button
                        key={t}
                        type="button"
                        className="max-w-full truncate rounded-full border border-line px-2 py-0.5 text-xs text-muted hover:text-ink"
                        title={`Filtrar por «${t}»`}
                        onClick={(e) => {
                          e.stopPropagation();
                          onTag(t);
                        }}
                      >
                        {t}
                      </button>
                    ))}
                  </Chips>
                </div>
              </div>
            );
          })}
        </div>
        {loading && !initialLoading && <div className="py-3 text-center text-xs text-muted">Cargando…</div>}
      </div>
    </div>
  );
}
