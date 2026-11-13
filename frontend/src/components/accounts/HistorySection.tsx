// «Historial de mensajes» de la tarjeta de una cuenta: cuánto hay guardado, el avance de la tanda en
// curso y los botones para guardar el historial completo, detenerlo o reiniciarlo.
import { useState } from "react";
import { ChevronDown, ChevronRight, History, Loader2, Play, RotateCcw, Square, TriangleAlert } from "lucide-react";
import type { AccountView, HistoryStatus } from "@/api/types";
import { channelName } from "@/lib/channels";
import { formatDate, formatDateTime } from "@/lib/format";
import {
  emptyStatus,
  formatCount,
  hasProgress,
  isComplete,
  phaseText,
  progressPercent,
  useHistory,
} from "@/store/history";
import { useUI } from "@/store/ui";

/** Quién bloquea la cuenta si se pide demasiado, según el canal. */
export function platformOf(channel: string): string {
  switch (channel) {
    case "whatsapp":
      return "WhatsApp";
    case "telegram":
      return "Telegram";
    case "messenger":
    case "instagram":
      return "Meta";
    default:
      return channelName(channel);
  }
}

export const WHATSAPP_NOTE =
  "Las cuentas ya vinculadas solo reciben lo que el teléfono envíe por cada chat. Para traer todo el historial de golpe hay que vincular la cuenta de nuevo.";

export const UNOFFICIAL_NOTE =
  "Este canal usa una vía no oficial: pedir mucho historial puede poner en riesgo la cuenta. Por eso solo se piden pocos chats cada varias horas.";

/** Resumen de lo guardado: «1.234 mensajes guardados · 45 de 120 chats recorridos · el más antiguo: 12/03/2025». */
export function summaryParts(st: HistoryStatus): string[] {
  const parts: string[] = [];
  parts.push(st.messages === 1 ? "1 mensaje guardado" : `${formatCount(st.messages)} mensajes guardados`);
  if (st.chatsTotal > 0) parts.push(`${formatCount(st.chatsDone)} de ${formatCount(st.chatsTotal)} chats recorridos`);
  if (st.oldestAt > 0) parts.push(`el más antiguo: ${formatDate(st.oldestAt)}`);
  return parts;
}

export default function HistorySection({ view }: { view: AccountView }) {
  const accountId = view.account.id;
  const channel = String(view.account.channel);
  const st = useHistory((s) => s.byAccount[accountId]);
  const start = useHistory((s) => s.start);
  const stop = useHistory((s) => s.stop);
  const reset = useHistory((s) => s.reset);
  const confirm = useUI((s) => s.confirm);
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);

  const status = st ?? emptyStatus(accountId);

  const connected = String(view.status?.state) === "connected";
  const supported = (view.capabilities as { historyOnDemand?: boolean } | undefined)?.historyOnDemand !== false;
  const running = status.running;
  const pct = progressPercent(status);
  const unofficial = channel === "messenger" || channel === "instagram";
  const platform = platformOf(channel);

  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    try {
      await fn();
    } finally {
      setBusy(false);
    }
  };

  const askReset = async () => {
    const ok = await confirm({
      title: "Reiniciar el historial",
      message: `Se borra el avance de «${view.account.label}» para volver a recorrer todos sus chats desde cero. Los mensajes ya guardados se conservan.`,
      confirmLabel: "Reiniciar",
      danger: true,
    });
    if (ok) await run(() => reset(accountId));
  };

  const disabledReason = !supported
    ? "Este canal no permite pedir el historial anterior."
    : !connected
      ? "Conecta la cuenta para guardar su historial."
      : "";

  const bodyId = `history-body-${accountId}`;
  return (
    <section
      aria-label="Historial de mensajes"
      data-testid="history-section"
      data-running={running ? "true" : "false"}
      className="rounded-lg border border-line bg-panel2/40"
    >
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        aria-controls={bodyId}
        className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-sm font-medium hover:bg-[var(--hover)] focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-[var(--accent)]"
      >
        {open ? <ChevronDown size={15} aria-hidden="true" /> : <ChevronRight size={15} aria-hidden="true" />}
        <History size={15} className="text-muted" aria-hidden="true" />
        <span className="flex-1">Historial de mensajes</span>
        {running ? (
          <span className="inline-flex items-center gap-1 text-xs font-normal text-muted" data-testid="history-badge">
            <Loader2 size={12} className="animate-spin" aria-hidden="true" />
            {status.chatsTotal > 0 ? `${pct} %` : "En marcha"}
          </span>
        ) : isComplete(status) ? (
          <span className="text-xs font-normal text-muted" data-testid="history-badge">
            Completo
          </span>
        ) : null}
      </button>

      {open && (
        <div id={bodyId} className="flex flex-col gap-2.5 border-t border-line px-3 py-3">
          <p className="text-sm" data-testid="history-summary">
            {summaryParts(status).join(" · ")}
          </p>

          {running && (
            <div className="flex flex-col gap-1.5" data-testid="history-progress">
              <div
                role="progressbar"
                aria-label="Avance del historial"
                aria-valuemin={0}
                aria-valuemax={100}
                aria-valuenow={pct}
                className="h-2 overflow-hidden rounded-full bg-[color-mix(in_srgb,var(--muted)_25%,transparent)]"
              >
                <div className="h-full rounded-full bg-accent transition-[width]" style={{ width: `${pct}%` }} />
              </div>
              <p className="truncate text-xs text-muted" title={phaseText(status)}>
                {status.auto ? "Tanda automática · " : ""}
                {phaseText(status)}
                {status.current ? (
                  <>
                    {" · "}
                    <span data-testid="history-current">{status.current}</span>
                  </>
                ) : null}
                {status.added > 0 ? ` · +${formatCount(status.added)} mensajes en esta tanda` : ""}
              </p>
            </div>
          )}

          {!running && status.stoppedReason && (
            <p className="flex items-start gap-1.5 text-sm" role="status" data-testid="history-stopped">
              <TriangleAlert size={15} className="mt-0.5 shrink-0 text-warn" aria-hidden="true" />
              <span className="min-w-0">Se detuvo: {status.stoppedReason}</span>
            </p>
          )}

          {!running && !status.stoppedReason && status.nextAutoAt > 0 && (
            <p className="text-xs text-muted" data-testid="history-next">
              Próxima tanda automática: {formatDateTime(status.nextAutoAt)}
            </p>
          )}

          <div className="flex flex-wrap items-center gap-2">
            {running ? (
              <button className="btn btn-sm" disabled={busy} onClick={() => void run(() => stop(accountId))}>
                <Square size={13} aria-hidden="true" />
                Detener
              </button>
            ) : (
              <button
                className="btn btn-primary btn-sm"
                disabled={busy || !!disabledReason}
                title={disabledReason || undefined}
                onClick={() => void run(() => start(accountId))}
              >
                <Play size={13} aria-hidden="true" />
                Guardar historial completo
              </button>
            )}
            {!running && hasProgress(status) && (
              <button className="btn btn-sm" disabled={busy} onClick={() => void askReset()}>
                <RotateCcw size={13} aria-hidden="true" />
                Reiniciar
              </button>
            )}
          </div>

          {disabledReason && !running && (
            <p className="text-xs text-muted" data-testid="history-disabled">
              {disabledReason}
            </p>
          )}

          <p className="text-xs text-muted" data-testid="history-slow">
            Va despacio a propósito: así {platform} no bloquea la cuenta.
          </p>
          {channel === "whatsapp" && (
            <p className="text-xs text-muted" data-testid="history-wa-note">
              {WHATSAPP_NOTE}
            </p>
          )}
          {unofficial && (
            <p className="flex items-start gap-1.5 text-xs text-muted" data-testid="history-unofficial">
              <TriangleAlert size={13} className="mt-0.5 shrink-0 text-warn" aria-hidden="true" />
              <span className="min-w-0">{UNOFFICIAL_NOTE}</span>
            </p>
          )}
        </div>
      )}
    </section>
  );
}
