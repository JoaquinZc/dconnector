import { Pencil, Plus, Trash2, Users } from "lucide-react";
import type { ContactList } from "@/api/types";
import { ListRowsSkeleton } from "@/pages/skeletons/zones";
import { cx } from "@/lib/cx";
import { sortLists } from "./contactModel";

interface Props {
  lists: ContactList[];
  /** Id de la lista elegida; "" = todos. */
  selected: string;
  /** Las listas aún no llegaron: se pintan esqueletos en vez de «Sin listas». */
  loading?: boolean;
  onSelect: (id: string) => void;
  onCreate: () => void;
  onEdit: (l: ContactList) => void;
  onDelete: (l: ContactList) => void;
}

/** Columna izquierda: «Todos» y las listas, con crear, renombrar/color y borrar. */
export default function ListsSidebar({ lists, selected, loading = false, onSelect, onCreate, onEdit, onDelete }: Props) {
  return (
    <aside className="flex w-[260px] shrink-0 flex-col border-r border-line bg-panel" aria-label="Listas">
      <div className="flex items-center gap-2 px-4 py-3">
        <h2 className="flex-1 text-sm font-semibold text-muted">Listas</h2>
        <button className="icon-btn" onClick={onCreate} aria-label="Nueva lista" title="Nueva lista">
          <Plus size={18} />
        </button>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-3">
        <button
          type="button"
          onClick={() => onSelect("")}
          aria-current={selected === "" ? "true" : undefined}
          className={cx(
            "flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-left",
            selected === "" ? "bg-panel2 font-semibold" : "hover:bg-[var(--hover)]",
          )}
        >
          <Users size={16} className="text-muted" />
          Todos
        </button>
        {sortLists(lists).map((l) => (
          <div
            key={l.id}
            className={cx(
              "group flex items-center rounded-lg pr-1",
              selected === l.id ? "bg-panel2" : "hover:bg-[var(--hover)]",
            )}
          >
            <button
              type="button"
              onClick={() => onSelect(l.id)}
              aria-current={selected === l.id ? "true" : undefined}
              className={cx("flex min-w-0 flex-1 items-center gap-2.5 px-3 py-2 text-left", selected === l.id && "font-semibold")}
              title={l.name}
            >
              <span className="h-3 w-3 shrink-0 rounded-full" style={{ background: l.color }} />
              <span className="min-w-0 flex-1 truncate">{l.name}</span>
              <span className="text-xs font-normal text-muted">{l.memberCount}</span>
            </button>
            <button
              className="icon-btn !h-7 !w-7 opacity-0 focus:opacity-100 group-hover:opacity-100"
              onClick={() => onEdit(l)}
              aria-label={`Editar ${l.name}`}
              title="Editar"
            >
              <Pencil size={14} />
            </button>
            <button
              className="icon-btn !h-7 !w-7 opacity-0 focus:opacity-100 group-hover:opacity-100"
              onClick={() => onDelete(l)}
              aria-label={`Eliminar ${l.name}`}
              title="Eliminar"
            >
              <Trash2 size={14} />
            </button>
          </div>
        ))}
        {loading && lists.length === 0 && <ListRowsSkeleton />}
        {!loading && lists.length === 0 && <p className="px-3 py-4 text-sm text-muted">Sin listas</p>}
      </div>
    </aside>
  );
}
