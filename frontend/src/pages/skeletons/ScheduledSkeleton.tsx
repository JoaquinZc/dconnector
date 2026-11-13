// Programados: cabecera con las pestañas de estado y la tabla de siete columnas.
import { Plus } from "lucide-react";
import { Skeleton } from "@/components/common/Skeleton";
import { PageRoot } from "./parts";
import { ScheduledTableSkeleton } from "./zones";

export const SCHEDULED_FILTER_LABELS = ["Todos", "Pendientes", "Enviados", "Fallidos", "Cancelados"] as const;

export default function ScheduledSkeleton() {
  return (
    <PageRoot testId="scheduled-skeleton">
      <header className="flex flex-wrap items-center gap-3 border-b border-line bg-panel px-6 py-3">
        <h1 className="text-lg font-semibold">Programados</h1>
        <div className="ml-2 inline-flex rounded-lg bg-panel2 p-0.5">
          {SCHEDULED_FILTER_LABELS.map((label, i) => (
            <span
              key={label}
              className={`inline-flex items-center rounded-md px-3 py-1.5 text-sm font-medium ${i === 0 ? "bg-panel shadow-sm" : "text-muted"}`}
            >
              {label}
              <Skeleton className="ml-1.5 h-3 w-3" />
            </span>
          ))}
        </div>
        <span className="flex-1" />
        <span className="btn btn-primary">
          <Plus size={16} />
          Nuevo
        </span>
      </header>
      <div className="min-h-0 flex-1 overflow-hidden p-6">
        <ScheduledTableSkeleton bare />
      </div>
    </PageRoot>
  );
}
