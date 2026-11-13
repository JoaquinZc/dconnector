// Estados: columna de 360 px (filtros por canal, «Mis estados», lista por canal) y visor con sus pestañas.
import { EyeOff, Plus } from "lucide-react";
import { Skeleton } from "@/components/common/Skeleton";
import { useAccounts } from "@/store/accounts";
import { Line, PageRoot } from "./parts";
import { StatusListSkeleton, StoryViewerSkeleton } from "./zones";

export const STATUS_TABS = ["Ver estados", "Mis estados", "Estadísticas"] as const;

/** Columna izquierda de Estados con el marco real; la lista espera. */
export function StatusSidebarSkeleton() {
  // Un círculo por cuenta (hasta cuatro), como el real; sin cuentas conocidas, uno.
  const avatars = useAccounts((st) => Math.max(1, Math.min(4, st.order.length)));
  return (
    <aside className="flex h-full w-[360px] shrink-0 flex-col border-r border-line bg-panel" data-testid="status-sidebar-skeleton">
      <header className="flex h-[68px] shrink-0 items-center justify-between pl-5 pr-4">
        <h1 className="m-0 text-[22px] font-semibold tracking-[-0.2px]">Estados</h1>
        <span className="flex h-[38px] w-[38px] items-center justify-center rounded-xl bg-accent text-[#04201a]">
          <Plus size={20} strokeWidth={1.8} aria-hidden />
        </span>
      </header>
      <div className="flex shrink-0 flex-wrap items-center gap-x-1 gap-y-1.5 px-4 pb-3">
        <span className="inline-flex h-[30px] items-center justify-center rounded-[15px] border-[1.5px] border-transparent bg-[var(--badge-bg)] px-3 text-[12.5px] font-semibold text-[var(--badge-fg)]">
          Todos
        </span>
        <Skeleton className="h-[30px] w-24 rounded-[15px]" />
        <Skeleton className="h-[30px] w-20 rounded-[15px]" />
        <span className="ml-auto inline-flex h-[30px] items-center gap-1.5 rounded-[15px] border border-line bg-panel px-[11px] text-[12.5px] font-medium text-ink">
          <EyeOff size={16} aria-hidden />
          Ocultar vistos
        </span>
      </div>
      <div className="flex shrink-0 items-center gap-3 border-y border-line bg-panel px-4 py-2.5">
        <div className="flex min-w-0 flex-1 items-center gap-3 text-left">
          <span className="flex shrink-0" aria-hidden>
            {Array.from({ length: avatars }, (_, i) => (
              <Skeleton key={i} className={`h-[34px] w-[34px] rounded-full border-2 border-panel ${i > 0 ? "-ml-3" : ""}`} />
            ))}
          </span>
          <span className="min-w-0 flex-1">
            <b className="block text-[15px] font-semibold">Mis estados</b>
            <Line lh={19} bar={11} w={112} />
          </span>
        </div>
        <span className="flex h-[34px] w-[34px] shrink-0 items-center justify-center rounded-full bg-accent text-[#04201a]">
          <Plus size={16} strokeWidth={1.8} aria-hidden />
        </span>
      </div>
      <div className="relative min-h-0 flex-1 overflow-hidden">
        <StatusListSkeleton bare />
      </div>
    </aside>
  );
}

export default function StatusesSkeleton() {
  return (
    <PageRoot className="flex h-full" testId="statuses-skeleton">
      <StatusSidebarSkeleton />
      <main className="flex min-w-0 flex-1 flex-col bg-bg">
        <header className="flex h-[68px] shrink-0 items-center gap-3 border-b border-line bg-panel px-5">
          <div className="inline-flex rounded-[10px] bg-panel2 p-[3px]">
            {STATUS_TABS.map((label, i) => (
              <span
                key={label}
                className={`inline-flex h-8 items-center rounded-lg px-4 text-[13.5px] ${
                  i === 0 ? "bg-panel font-semibold text-ink shadow-[0_1px_2px_rgba(11,20,26,0.12)]" : "font-medium text-muted"
                }`}
              >
                {label}
              </span>
            ))}
          </div>
          <span className="flex-1" />
          <span className="btn btn-primary !h-[38px] !rounded-xl !px-4 !font-semibold">
            <Plus size={16} aria-hidden />
            Nuevo estado
          </span>
        </header>
        <div className="min-h-0 flex-1 bg-chat">
          <StoryViewerSkeleton bare />
        </div>
      </main>
    </PageRoot>
  );
}
