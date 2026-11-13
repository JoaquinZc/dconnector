import { useCallback, useEffect, useMemo, useState } from "react";
import { useReloadOnReconnect } from "@/lib/useReconnect";
import { CalendarClock, Plus } from "lucide-react";
import { api } from "@/api";
import type { Scheduled } from "@/api/types";
import EmptyState from "@/components/common/EmptyState";
import LoadError from "@/components/common/LoadError";
import { Skeleton } from "@/components/common/Skeleton";
import { ScheduledTableSkeleton } from "@/pages/skeletons/zones";
import ScheduledForm from "@/components/scheduled/ScheduledForm";
import ScheduledTable from "@/components/scheduled/ScheduledTable";
import {
  FILTERS,
  countByFilter,
  filterScheduled,
  sortScheduled,
  type ScheduledFilter,
} from "@/components/scheduled/scheduledModel";
import { cx } from "@/lib/cx";
import { useAccounts } from "@/store/accounts";
import { useUI } from "@/store/ui";

/** El planificador revisa cada 15 s: la tabla también. */
const POLL_MS = 15_000;
const TEN_YEARS = 10 * 365 * 24 * 60 * 60 * 1000;

export default function ScheduledPage() {
  const views = useAccounts((s) => s.views);
  const confirm = useUI((s) => s.confirm);
  const toast = useUI((s) => s.toast);

  const [items, setItems] = useState<Scheduled[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [failed, setFailed] = useState(false);
  const [filter, setFilter] = useState<ScheduledFilter>("all");
  const [busyId, setBusyId] = useState("");
  // `undefined` = cerrado, `null` = nuevo, otro = editar.
  const [editing, setEditing] = useState<Scheduled | null | undefined>(undefined);

  const load = useCallback(async () => {
    try {
      setItems(await api.scheduled.List(0, Date.now() + TEN_YEARS));
      setFailed(false);
    } catch {
      setFailed(true); // el aviso ya salió; se conserva lo que hubiera
    } finally {
      setLoaded(true);
    }
  }, []);

  useEffect(() => {
    void load();
    const t = setInterval(() => void load(), POLL_MS);
    return () => clearInterval(t);
  }, [load]);

  useReloadOnReconnect(() => void load());

  const counts = useMemo(() => countByFilter(items), [items]);
  const shown = useMemo(() => sortScheduled(filterScheduled(items, filter)), [items, filter]);

  const act = async (s: Scheduled, fn: () => Promise<void>, done: string) => {
    setBusyId(s.id);
    try {
      await fn();
      toast("success", done);
    } catch {
      /* el aviso ya salió */
    } finally {
      setBusyId("");
      await load();
    }
  };

  const sendNow = (s: Scheduled) => void act(s, () => api.scheduled.SendNow(s.id), "Enviado");

  const cancel = async (s: Scheduled) => {
    const ok = await confirm({
      title: "Cancelar programado",
      message: "No se enviará.",
      confirmLabel: "Cancelar programado",
      danger: true,
    });
    if (ok) await act(s, () => api.scheduled.Cancel(s.id), "Programado cancelado");
  };

  const cancelSeries = async (s: Scheduled) => {
    const ok = await confirm({
      title: "Cancelar serie",
      message: "No se enviará esta ni las siguientes repeticiones.",
      confirmLabel: "Cancelar serie",
      danger: true,
    });
    if (ok) await act(s, () => api.scheduled.CancelSeries(s.seriesId), "Serie cancelada");
  };

  const remove = async (s: Scheduled) => {
    const ok = await confirm({
      title: "Borrar programado",
      message:
        s.state === "pending"
          ? s.recurrence && s.seriesId
            ? "Se salta esta repetición; la serie continúa con la siguiente."
            : "Aún no se envió y no se enviará."
          : undefined,
      confirmLabel: "Borrar",
      danger: true,
    });
    if (ok) await act(s, () => api.scheduled.Delete(s.id), "Programado borrado");
  };

  return (
    <div className="flex h-full flex-col">
      <header className="flex flex-wrap items-center gap-3 border-b border-line bg-panel px-6 py-3">
        <h1 className="text-lg font-semibold">Programados</h1>
        <div role="tablist" aria-label="Filtro por estado" className="ml-2 inline-flex rounded-lg bg-panel2 p-0.5">
          {FILTERS.map((f) => (
            <button
              key={f.id}
              role="tab"
              aria-selected={filter === f.id}
              onClick={() => setFilter(f.id)}
              className={cx(
                "rounded-md px-3 py-1.5 text-sm font-medium",
                filter === f.id ? "bg-panel shadow-sm" : "text-muted hover:text-ink",
              )}
            >
              {f.label}
              {loaded ? (
                <span className="ml-1.5 text-xs text-muted" data-testid={`count-${f.id}`}>
                  {counts[f.id]}
                </span>
              ) : (
                <Skeleton className="ml-1.5 inline-block h-3 w-3 align-middle" />
              )}
            </button>
          ))}
        </div>
        <span className="flex-1" />
        <button className="btn btn-primary" onClick={() => setEditing(null)}>
          <Plus size={16} />
          Nuevo
        </button>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto p-6">
        {!loaded ? (
          <ScheduledTableSkeleton />
        ) : failed && items.length === 0 ? (
          <LoadError what="los programados" onRetry={() => void load()} />
        ) : shown.length === 0 ? (
          <EmptyState
            icon={CalendarClock}
            title={items.length === 0 ? "Sin programados" : "Nada con este estado"}
            action={
              items.length === 0 ? (
                <button className="btn btn-primary" onClick={() => setEditing(null)}>
                  <Plus size={16} />
                  Nuevo
                </button>
              ) : undefined
            }
          />
        ) : (
          <ScheduledTable
            items={shown}
            views={views}
            busyId={busyId}
            onEdit={(s) => setEditing(s)}
            onSendNow={sendNow}
            onCancel={(s) => void cancel(s)}
            onCancelSeries={(s) => void cancelSeries(s)}
            onDelete={(s) => void remove(s)}
          />
        )}
      </div>

      {editing !== undefined && (
        <ScheduledForm
          key={editing?.id ?? "new"}
          item={editing}
          onClose={() => setEditing(undefined)}
          onSaved={() => void load()}
        />
      )}
    </div>
  );
}
