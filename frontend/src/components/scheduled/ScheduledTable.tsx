import { Ban, CalendarX2, Pencil, Repeat, Send, Trash2 } from "lucide-react";
import type { AccountView, Scheduled } from "@/api/types";
import AccountPill from "@/components/common/AccountPill";
import Pill from "@/components/campaigns/Pill";
import { formatDateTime } from "@/lib/format";
import { repeatChipText } from "@/lib/recurrence";
import { contentSummary, destinationLabel, kindLabel, rowActions, scheduledState } from "./scheduledModel";

interface Props {
  items: Scheduled[];
  views: Record<string, AccountView>;
  busyId?: string;
  onEdit: (s: Scheduled) => void;
  onSendNow: (s: Scheduled) => void;
  onCancel: (s: Scheduled) => void;
  /** Cancela todas las ocurrencias pendientes de la serie. */
  onCancelSeries?: (s: Scheduled) => void;
  onDelete: (s: Scheduled) => void;
}

const COLS = "grid-cols-[150px_130px_minmax(0,1fr)_90px_minmax(0,1.6fr)_110px_168px]";
/** Las mismas columnas como `grid-template-columns` (para el esqueleto de la primera carga). */
export const SCHEDULED_COLS = "150px 130px minmax(0,1fr) 90px minmax(0,1.6fr) 110px 168px";

/** Tabla de programados: fecha, cuenta, destino, tipo, contenido, estado y acciones. */
export default function ScheduledTable({ items, views, busyId, onEdit, onSendNow, onCancel, onCancelSeries, onDelete }: Props) {
  return (
    <div className="card overflow-hidden" role="table" aria-label="Programados">
      <div role="row" className={`grid ${COLS} items-center gap-3 border-b border-line bg-panel2 px-4 py-2 text-xs font-semibold text-muted`}>
        <span role="columnheader">Fecha</span>
        <span role="columnheader">Cuenta</span>
        <span role="columnheader">Destino</span>
        <span role="columnheader">Tipo</span>
        <span role="columnheader">Contenido</span>
        <span role="columnheader">Estado</span>
        <span role="columnheader" className="text-right">
          <span className="sr-only">Acciones</span>
        </span>
      </div>
      {items.map((s) => {
        const st = scheduledState(s.state);
        const a = rowActions(s);
        const view = views[String(s.accountId)];
        const content = contentSummary(s);
        const busy = busyId === s.id;
        const chip = repeatChipText(s.recurrence, s.sendAt);
        const inSeries = !!s.seriesId;
        return (
          <div
            key={s.id}
            role="row"
            data-testid="scheduled-row"
            data-state={s.state}
            data-series={s.seriesId || undefined}
            className={`grid ${COLS} items-center gap-3 border-b border-line px-4 py-2 text-sm last:border-b-0`}
          >
            <span role="cell" className="tabular-nums">
              {formatDateTime(s.sendAt)}
            </span>
            <span role="cell" className="min-w-0">
              {view ? <AccountPill label={view.account.label} color={view.account.color} /> : <span className="text-xs text-muted">—</span>}
            </span>
            <span role="cell" className="truncate" title={destinationLabel(s)}>
              {destinationLabel(s)}
            </span>
            <span role="cell">{kindLabel(s)}</span>
            <span role="cell" className="min-w-0 text-muted">
              <span className="block truncate" title={content}>
                {content || "—"}
              </span>
              {(chip || inSeries) && (
                <span
                  className="mt-0.5 inline-flex max-w-full items-center gap-1 rounded-full bg-panel2 px-2 py-px text-xs text-muted"
                  data-testid="recurrence-chip"
                  title={chip || "Ocurrencia de una serie"}
                >
                  <Repeat size={11} className="shrink-0" aria-hidden />
                  <span className="truncate">{chip || "Ocurrencia de una serie"}</span>
                </span>
              )}
            </span>
            <span role="cell">
              <Pill tone={st.tone} title={s.error || undefined}>
                {st.label}
              </Pill>
            </span>
            <span role="cell" className="flex justify-end gap-0.5">
              {a.edit && (
                <button className="icon-btn" onClick={() => onEdit(s)} disabled={busy} aria-label="Editar" title="Editar">
                  <Pencil size={16} />
                </button>
              )}
              {a.sendNow && (
                <button className="icon-btn" onClick={() => onSendNow(s)} disabled={busy} aria-label="Enviar ahora" title="Enviar ahora">
                  <Send size={16} />
                </button>
              )}
              {a.cancel && (
                <button className="icon-btn" onClick={() => onCancel(s)} disabled={busy} aria-label="Cancelar" title="Cancelar">
                  <Ban size={16} />
                </button>
              )}
              {a.cancelSeries && onCancelSeries && (
                <button className="icon-btn" onClick={() => onCancelSeries(s)} disabled={busy} aria-label="Cancelar serie" title="Cancelar serie">
                  <CalendarX2 size={16} />
                </button>
              )}
              {a.remove && (
                <button className="icon-btn" onClick={() => onDelete(s)} disabled={busy} aria-label="Borrar" title="Borrar">
                  <Trash2 size={16} />
                </button>
              )}
            </span>
          </div>
        );
      })}
    </div>
  );
}
