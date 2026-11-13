// Contactos: columna de listas (260 px), cabecera con buscador y filtros, y tabla de cinco columnas.
import { ChevronDown, MoreVertical, Plus, RefreshCw, Search, Users } from "lucide-react";
import { Skeleton } from "@/components/common/Skeleton";
import { PageRoot } from "./parts";
import { ContactRowsSkeleton, ContactsTableHead, ListRowsSkeleton } from "./zones";

/** Columna izquierda: título, «Nueva lista», «Todos» (elegida) y las listas a la espera. */
export function ListsSidebarSkeleton() {
  return (
    <aside className="flex w-[260px] shrink-0 flex-col border-r border-line bg-panel" data-testid="lists-sidebar-skeleton">
      <div className="flex items-center gap-2 px-4 py-3">
        <h2 className="flex-1 text-sm font-semibold text-muted">Listas</h2>
        <span className="icon-btn">
          <Plus size={18} />
        </span>
      </div>
      <div className="min-h-0 flex-1 overflow-hidden px-2 pb-3">
        <div className="flex w-full items-center gap-2.5 rounded-lg bg-panel2 px-3 py-2 text-left font-semibold">
          <Users size={16} className="text-muted" />
          Todos
        </div>
        <ListRowsSkeleton bare />
      </div>
    </aside>
  );
}

/** Cabecera de la tabla: título real, total pendiente, buscador, filtros y botones. */
export function ContactsHeaderSkeleton() {
  return (
    <header className="flex flex-wrap items-center gap-x-3 gap-y-2 border-b border-line px-6 py-3">
      <h1 className="text-lg font-semibold">Contactos</h1>
      <Skeleton className="h-4 w-20" />
      <div className="relative ml-4 min-w-[180px] max-w-sm flex-1">
        <Search size={16} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-muted" />
        <div className="input flex items-center !pl-9 text-muted">Buscar</div>
      </div>
      <div className="w-44">
        <div className="input flex items-center text-muted">Etiqueta</div>
      </div>
      <div className="input flex w-40 items-center justify-between">
        Origen: todos
        <ChevronDown size={14} className="text-muted" />
      </div>
      <span className="flex-1" />
      <span className="btn">
        <RefreshCw size={16} />
        Actualizar desde las cuentas
      </span>
      <span className="btn btn-primary">
        <Plus size={16} />
        Nuevo contacto
      </span>
      <span className="icon-btn">
        <MoreVertical size={18} />
      </span>
    </header>
  );
}

export default function ContactsSkeleton() {
  return (
    <PageRoot className="flex h-full" testId="contacts-skeleton">
      <ListsSidebarSkeleton />
      <main className="flex min-w-0 flex-1 flex-col bg-panel">
        <ContactsHeaderSkeleton />
        <div className="flex min-h-0 flex-1 flex-col">
          <ContactsTableHead />
          <div className="min-h-0 flex-1 overflow-hidden bg-panel">
            <ContactRowsSkeleton bare />
          </div>
        </div>
      </main>
    </PageRoot>
  );
}
