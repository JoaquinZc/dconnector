// Panel «Programados de este chat»: pendientes y cerrados recientes, con editar, enviar ahora,
// cancelar la ocurrencia o toda la serie, y eliminar.
import { useEffect, useMemo, useRef, useState } from "react";
import { Plus, Repeat, X } from "lucide-react";
import { api } from "@/api";
import type { Scheduled } from "@/api/types";
import Pill from "@/components/campaigns/Pill";
import { contentSummary, scheduledState, sortScheduled } from "@/components/scheduled/scheduledModel";
import { formatDateTime } from "@/lib/format";
import { repeatChipText } from "@/lib/recurrence";
import { useUI } from "@/store/ui";
import { IconButton } from "./parts";

interface Props {
  items: Scheduled[];
  loaded: boolean;
  onClose: () => void;
  /** Abre el diálogo para programar otro. */
  onNew: () => void;
  onEdit: (s: Scheduled) => void;
  /** Se cambió algo: hay que recargar la lista. */
  onChanged: () => void;
}

const LINK = "rounded px-1 py-0.5 text-[12.5px] font-medium text-[var(--accent)] hover:underline focus-visible:outline-2 focus-visible:outline-[var(--accent)] disabled:opacity-40";
const LINK_DANGER = "rounded px-1 py-0.5 text-[12.5px] font-medium text-[var(--danger)] hover:underline focus-visible:outline-2 focus-visible:outline-[var(--danger)] disabled:opacity-40";

export default function ChatScheduledPanel({ items, loaded, onClose, onNew, onEdit, onChanged }: Props) {
  const confirm = useUI((s) => s.confirm);
  const toast = useUI((s) => s.toast);
  const [busyId, setBusyId] = useState("");
  const panelRef = useRef<HTMLDivElement>(null);

  // Foco al abrir y de vuelta al cerrar; Escape cierra (salvo que haya un diálogo encima).
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    panelRef.current?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      if (document.querySelector("[role=dialog][aria-modal=true]")) return;
      e.stopPropagation();
      onClose();
    };
    window.addEventListener("keydown", onKey, true);
    return () => {
      window.removeEventListener("keydown", onKey, true);
      previous?.focus?.();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const { open, closed } = useMemo(() => {
    const sorted = sortScheduled(items);
    return { open: sorted.filter((s) => s.state === "pending"), closed: sorted.filter((s) => s.state !== "pending") };
  }, [items]);

  const run = async (s: Scheduled, fn: () => Promise<void>, done: string) => {
    setBusyId(s.id);
    try {
      await fn();
      toast("success", done);
    } catch {
      /* el aviso ya salió */
    } finally {
      setBusyId("");
      onChanged();
    }
  };

  const sendNow = (s: Scheduled) => void run(s, () => api.scheduled.SendNow(s.id), "Enviado");

  const cancel = async (s: Scheduled) => {
    const ok = await confirm({
      title: "Cancelar esta repetición",
      message: s.recurrence ? "Esta vez no se enviará; la serie sigue con la siguiente." : "No se enviará.",
      confirmLabel: "Cancelar",
      danger: true,
    });
    if (ok) await run(s, () => api.scheduled.Cancel(s.id), "Programado cancelado");
  };

  const cancelSeries = async (s: Scheduled) => {
    const ok = await confirm({
      title: "Cancelar serie",
      message: "No se enviará esta ni las siguientes repeticiones.",
      confirmLabel: "Cancelar serie",
      danger: true,
    });
    if (ok) await run(s, () => api.scheduled.CancelSeries(s.seriesId), "Serie cancelada");
  };

  const remove = async (s: Scheduled) => {
    const ok = await confirm({
      title: "Eliminar programado",
      message: s.state === "pending" ? (s.recurrence ? "Se salta esta repetición; la serie continúa." : "Aún no se envió y no se enviará.") : undefined,
      confirmLabel: "Eliminar",
      danger: true,
    });
    if (ok) await run(s, () => api.scheduled.Delete(s.id), "Programado eliminado");
  };

  const removeSeries = async (s: Scheduled) => {
    const ok = await confirm({
      title: "Eliminar serie",
      message: "Se borran todas las repeticiones, también las ya enviadas.",
      confirmLabel: "Eliminar serie",
      danger: true,
    });
    if (ok) await run(s, () => api.scheduled.DeleteSeries(s.seriesId), "Serie eliminada");
  };

  const row = (s: Scheduled) => {
    const st = scheduledState(s.state);
    const busy = busyId === s.id;
    const isPending = s.state === "pending";
    const isSeries = !!s.recurrence && !!s.seriesId;
    const chip = isPending ? repeatChipText(s.recurrence, s.sendAt) : "";
    const content = contentSummary(s);
    return (
      <li key={s.id} data-testid="chat-scheduled-item" data-state={s.state} className="border-b border-[var(--border)] px-4 py-2.5 last:border-b-0">
        <div className="flex items-start gap-2">
          <div className="min-w-0 flex-1">
            <div className="line-clamp-2 break-words text-[14px]" title={content}>
              {content || "—"}
            </div>
            <div className="mt-0.5 text-[12.5px] tabular-nums text-[var(--muted)]">{formatDateTime(s.sendAt)}</div>
          </div>
          <Pill tone={st.tone} title={s.error || undefined}>
            {st.label}
          </Pill>
        </div>
        {chip && (
          <div className="mt-1 inline-flex max-w-full items-center gap-1 rounded-full bg-black/5 px-2 py-px text-[12px] text-[var(--muted)] dark:bg-white/10" data-testid="chat-scheduled-chip" title={chip}>
            <Repeat size={11} className="shrink-0" aria-hidden />
            <span className="truncate">{chip}</span>
          </div>
        )}
        {!isPending && s.seriesId && <div className="mt-1 text-[12px] text-[var(--muted)]">Ocurrencia de una serie</div>}
        {s.error && !isPending && <div className="mt-1 text-[12px] text-[var(--muted)]">{s.error}</div>}
        <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5">
          {(isPending || s.state === "failed") && s.kind === "message" && (
            <button type="button" className={LINK} disabled={busy} onClick={() => onEdit(s)}>
              Editar
            </button>
          )}
          {(isPending || s.state === "failed") && (
            <button type="button" className={LINK} disabled={busy} onClick={() => sendNow(s)}>
              Enviar ahora
            </button>
          )}
          {isPending && (
            <button type="button" className={LINK} disabled={busy} onClick={() => void cancel(s)}>
              {isSeries ? "Cancelar esta" : "Cancelar"}
            </button>
          )}
          {isPending && isSeries && (
            <button type="button" className={LINK_DANGER} disabled={busy} onClick={() => void cancelSeries(s)}>
              Cancelar serie
            </button>
          )}
          <button type="button" className={LINK_DANGER} disabled={busy} onClick={() => void remove(s)}>
            Eliminar
          </button>
          {s.seriesId && (
            <button type="button" className={LINK_DANGER} disabled={busy} onClick={() => void removeSeries(s)}>
              Eliminar serie
            </button>
          )}
        </div>
      </li>
    );
  };

  return (
    <>
      <div className="fixed inset-0 z-30" onMouseDown={onClose} />
      <div
        ref={panelRef}
        role="dialog"
        aria-label="Programados de este chat"
        tabIndex={-1}
        className="absolute bottom-full left-2 z-40 mb-1 flex max-h-[min(460px,60vh)] w-[min(400px,calc(100%-16px))] flex-col overflow-hidden rounded-xl bg-[var(--panel)] text-[var(--text)] shadow-2xl ring-1 ring-black/5 outline-none"
      >
        <div className="flex items-center gap-2 border-b border-[var(--border)] px-4 py-2.5">
          <h2 className="min-w-0 flex-1 truncate text-[15px] font-semibold">Programados de este chat</h2>
          <button type="button" className="btn btn-sm" onClick={onNew}>
            <Plus size={14} />
            Programar
          </button>
          <IconButton title="Cerrar" onClick={onClose}>
            <X size={18} />
          </IconButton>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto">
          {loaded && items.length === 0 && <p className="px-4 py-6 text-center text-[13.5px] text-[var(--muted)]">Nada programado para este chat.</p>}
          {open.length > 0 && (
            <>
              <h3 className="px-4 pt-2 text-[11.5px] font-semibold uppercase tracking-wide text-[var(--muted)]">Pendientes</h3>
              <ul aria-label="Pendientes">{open.map(row)}</ul>
            </>
          )}
          {closed.length > 0 && (
            <>
              <h3 className="px-4 pt-2 text-[11.5px] font-semibold uppercase tracking-wide text-[var(--muted)]">Recientes</h3>
              <ul aria-label="Recientes">{closed.map(row)}</ul>
            </>
          )}
        </div>
      </div>
    </>
  );
}
