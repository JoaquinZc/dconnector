// Estadísticas: cabecera con pestañas y periodo, (si hay varias cuentas) la fila de cuentas, y el panel de la pestaña.
import { Skeleton } from "@/components/common/Skeleton";
import { useAccounts } from "@/store/accounts";
import { PageRoot } from "./parts";
import { ConversationsStatsSkeleton } from "./zones";

export const STATS_TABS = ["Conversaciones", "Grupos"] as const;
export const STATS_PERIODS = ["7 días", "30 días", "90 días", "Personalizado"] as const;

export default function StatsSkeleton() {
  // La fila de cuentas solo existe con más de una cuenta activa (lo mismo que decide la página real).
  const several = useAccounts((s) => s.order.filter((id) => s.views[id]?.account.enabled).length > 1);
  return (
    <PageRoot testId="stats-skeleton">
      <header className="flex flex-wrap items-center gap-x-6 gap-y-2 border-b border-line bg-panel px-6 py-3">
        <h1 className="text-lg font-semibold">Estadísticas</h1>
        <div className="inline-flex rounded-lg bg-panel2 p-0.5">
          {STATS_TABS.map((label, i) => (
            <span key={label} className={`inline-block rounded-md px-4 py-1.5 text-sm font-medium ${i === 0 ? "bg-panel shadow-sm" : "text-muted"}`}>
              {label}
            </span>
          ))}
        </div>
        <span className="flex-1" />
        <div className="flex flex-wrap items-center gap-2">
          <div className="inline-flex rounded-lg bg-panel2 p-0.5">
            {STATS_PERIODS.map((label, i) => (
              <span key={label} className={`inline-block rounded-md px-3 py-1 text-sm font-medium ${i === 1 ? "bg-panel shadow-sm" : "text-muted"}`}>
                {label}
              </span>
            ))}
          </div>
        </div>
      </header>
      {several && (
        <div className="border-b border-line bg-panel px-6 py-2" data-testid="stats-accounts-skeleton">
          <div className="flex flex-wrap items-center gap-1.5">
            <Skeleton className="h-[26px] w-20 rounded-full" />
            <Skeleton className="h-[26px] w-24 rounded-full" />
          </div>
        </div>
      )}
      <div className="min-h-0 flex-1 overflow-hidden bg-bg p-6">
        <ConversationsStatsSkeleton bare />
      </div>
    </PageRoot>
  );
}
