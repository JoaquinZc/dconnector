import { useCallback, useEffect, useState } from "react";
import { ArrowLeft, Pause, Pencil, Play, Trash2, XCircle } from "lucide-react";
import { api } from "@/api";
import type { AccountView, Campaign, QueueItem } from "@/api/types";
import AccountPill from "@/components/common/AccountPill";
import { cx } from "@/lib/cx";
import { formatDateTime, truncate } from "@/lib/format";
import { useUI } from "@/store/ui";
import {
  QUEUE_TABS,
  VARIANT_LETTERS,
  campaignActions,
  progressOf,
  queueCount,
  riskInfo,
  stateInfo,
  statusLine,
  type CampaignLive,
  type QueueTab,
} from "./campaignModel";
import Pill, { TONE_COLORS } from "./Pill";
import { ProgressBar } from "./CampaignRow";

interface Props {
  campaign: Campaign;
  live?: CampaignLive;
  account?: AccountView;
  onBack: () => void;
  onEdit: () => void;
  /** Cambió el estado o se borró: la lista debe recargar. */
  onChanged: (deleted?: boolean) => void;
}

const QUEUE_LIMIT = 200;

function Stat({ label, value, testId }: { label: string; value: number; testId: string }) {
  return (
    <div className="card px-4 py-3" data-testid={testId}>
      <p className="text-2xl font-semibold tabular-nums">{value}</p>
      <p className="text-xs text-muted">{label}</p>
    </div>
  );
}

/** Teléfono o id legible de un destinatario de la cola. */
export function recipientLabel(it: QueueItem): string {
  const id = String(it.chatNativeId || it.contactId || "");
  return id.replace(/@.*$/, "") || "—";
}

/** Detalle de una campaña: totales, cola por estado y acciones. */
export default function CampaignDetail({ campaign: c, live, account, onBack, onEdit, onChanged }: Props) {
  const confirm = useUI((s) => s.confirm);
  const toast = useUI((s) => s.toast);
  const [tab, setTab] = useState<QueueTab>("pending");
  const [items, setItems] = useState<QueueItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);

  const st = stateInfo(c.state);
  const p = progressOf(c.totals);
  const line = statusLine(c, live);
  const actions = campaignActions(String(c.state));
  const t = c.totals;

  // La cola se vuelve a pedir al cambiar de pestaña y cuando cambian los totales.
  const totalsKey = `${t.queued}|${t.sent}|${t.failed}|${t.skipped}`;
  useEffect(() => {
    let alive = true;
    const states = QUEUE_TABS.find((x) => x.id === tab)?.states ?? [];
    setLoading(true);
    api.campaigns
      .Queue(c.id, states, QUEUE_LIMIT)
      .then((list) => alive && setItems(list))
      .catch(() => alive && setItems([]))
      .finally(() => alive && setLoading(false));
    return () => {
      alive = false;
    };
  }, [c.id, tab, totalsKey]);

  const run = useCallback(
    async (fn: () => Promise<void>, done?: string, deleted?: boolean) => {
      setBusy(true);
      try {
        await fn();
        if (done) toast("success", done);
        onChanged(deleted);
      } catch {
        /* el aviso ya salió */
      } finally {
        setBusy(false);
      }
    },
    [onChanged, toast],
  );

  const cancel = async () => {
    const ok = await confirm({
      title: "Cancelar campaña",
      message: `Se dejan de enviar los mensajes que faltan de «${c.name}».`,
      confirmLabel: "Cancelar campaña",
      danger: true,
    });
    if (ok) await run(() => api.campaigns.Cancel(c.id), "Campaña cancelada");
  };

  const remove = async () => {
    const ok = await confirm({
      title: "Eliminar campaña",
      message: `Se borra «${c.name}» con su cola.`,
      confirmLabel: "Eliminar",
      danger: true,
    });
    if (ok) await run(() => api.campaigns.Delete(c.id), "Campaña eliminada", true);
  };

  return (
    <div className="flex h-full flex-col">
      <header className="flex flex-wrap items-center gap-3 border-b border-line bg-panel px-6 py-3">
        <button className="icon-btn" onClick={onBack} aria-label="Volver" title="Volver">
          <ArrowLeft size={18} />
        </button>
        <h1 className="min-w-0 max-w-[40%] truncate text-lg font-semibold" title={c.name}>
          {c.name}
        </h1>
        {account && <AccountPill label={account.account.label} color={account.account.color} />}
        <Pill tone={st.tone}>{st.label}</Pill>
        {line.text && (
          <span className="min-w-0 truncate text-sm" style={{ color: TONE_COLORS[line.tone] }} title={line.text}>
            {line.text}
          </span>
        )}
        <span className="flex-1" />
        {actions.edit && (
          <button className="btn" onClick={onEdit} disabled={busy}>
            <Pencil size={15} />
            Editar
          </button>
        )}
        {actions.pause && (
          <button className="btn" onClick={() => void run(() => api.campaigns.Pause(c.id))} disabled={busy}>
            <Pause size={15} />
            Pausar
          </button>
        )}
        {actions.resume && (
          <button className="btn btn-primary" onClick={() => void run(() => api.campaigns.Resume(c.id))} disabled={busy}>
            <Play size={15} />
            Reanudar
          </button>
        )}
        {actions.cancel && (
          <button className="btn btn-danger" onClick={() => void cancel()} disabled={busy}>
            <XCircle size={15} />
            Cancelar
          </button>
        )}
        <button
          className="btn btn-danger"
          onClick={() => void remove()}
          disabled={busy || !actions.remove}
          title={actions.remove ? undefined : "Pausa o cancela la campaña primero"}
        >
          <Trash2 size={15} />
          Eliminar
        </button>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto p-6">
        <div className="mx-auto flex max-w-5xl flex-col gap-5">
          <div className="card p-4">
            <div className="mb-2 flex items-baseline gap-2">
              <span className="text-2xl font-semibold tabular-nums" data-testid="detail-progress">
                {p.sent}/{p.total}
              </span>
              <span className="text-sm text-muted">enviados</span>
            </div>
            <ProgressBar pct={p.pct} tone={c.state === "braked" ? "danger" : "ok"} />
          </div>

          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
            <Stat label="Pendientes" value={t.queued} testId="stat-queued" />
            <Stat label="Enviados" value={t.sent} testId="stat-sent" />
            <Stat label="Fallidos" value={t.failed} testId="stat-failed" />
            <Stat label="Omitidos" value={t.skipped} testId="stat-skipped" />
            <Stat label="Respuestas" value={t.replied} testId="stat-replied" />
            <Stat label="Bajas" value={t.optOut} testId="stat-optout" />
          </div>

          <div className="card p-4">
            <h2 className="label">Mensaje</h2>
            <p className="whitespace-pre-wrap break-words text-sm" title={c.text}>
              {truncate(c.text, 400) || "—"}
            </p>
            {(c.variants ?? []).length > 0 && (
              <p className="mt-2 text-xs text-muted">+ {(c.variants ?? []).length} variantes</p>
            )}
          </div>

          <div className="card overflow-hidden">
            <div role="tablist" className="flex gap-1 border-b border-line px-3 pt-2">
              {QUEUE_TABS.map((x) => (
                <button
                  key={x.id}
                  role="tab"
                  aria-selected={tab === x.id}
                  onClick={() => setTab(x.id)}
                  className={cx(
                    "-mb-px border-b-2 px-3 py-2 text-sm font-medium",
                    tab === x.id ? "border-accent text-ink" : "border-transparent text-muted hover:text-ink",
                  )}
                >
                  {x.label}
                  <span className="ml-1.5 text-xs text-muted">{queueCount(x.id, t)}</span>
                </button>
              ))}
            </div>

            {items.length === 0 ? (
              <p className="p-6 text-center text-sm text-muted">{loading ? "…" : "Vacío"}</p>
            ) : (
              <ul className="divide-y divide-line" data-testid="queue-list">
                {items.map((it) => {
                  const risk = riskInfo(it.risk);
                  const when = it.sentAt || it.sendAfter;
                  return (
                    <li key={it.id} className="grid grid-cols-[minmax(0,1fr)_minmax(0,2fr)_auto] items-center gap-3 px-4 py-2 text-sm" data-testid="queue-item">
                      <span className="flex min-w-0 items-center gap-2">
                        <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: risk.color }} title={risk.label} />
                        <span className="truncate" title={String(it.chatNativeId)}>
                          {recipientLabel(it)}
                        </span>
                        <span className="shrink-0 text-xs text-muted">{VARIANT_LETTERS[it.variant] ?? ""}</span>
                      </span>
                      <span className="min-w-0 truncate text-muted" title={it.error || it.text}>
                        {it.error ? <span className="text-danger">{it.error}</span> : truncate(it.text, 120)}
                      </span>
                      <span className="text-xs text-muted tabular-nums">{when ? formatDateTime(when) : ""}</span>
                    </li>
                  );
                })}
              </ul>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
