import { ChevronRight } from "lucide-react";
import type { AccountView, Campaign } from "@/api/types";
import AccountPill from "@/components/common/AccountPill";
import { cx } from "@/lib/cx";
import { progressOf, stateInfo, statusLine, type CampaignLive } from "./campaignModel";
import Pill, { TONE_COLORS } from "./Pill";

interface Props {
  campaign: Campaign;
  live?: CampaignLive;
  account?: AccountView;
  onOpen: () => void;
}

/** Barra de progreso de una campaña. */
export function ProgressBar({ pct, tone = "ok" }: { pct: number; tone?: "ok" | "danger" | "warn" }) {
  return (
    <div className="h-1.5 w-full overflow-hidden rounded-full bg-panel2" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
      <div className="h-full rounded-full transition-[width]" style={{ width: `${pct}%`, background: TONE_COLORS[tone] }} />
    </div>
  );
}

/** Una fila de la lista: nombre, cuenta, estado, progreso y qué está pasando. */
export default function CampaignRow({ campaign: c, live, account, onOpen }: Props) {
  const st = stateInfo(c.state);
  const p = progressOf(c.totals);
  const line = statusLine(c, live);
  return (
    <button
      type="button"
      onClick={onOpen}
      data-testid="campaign-row"
      className="card grid w-full grid-cols-[minmax(0,1.4fr)_auto_minmax(120px,1fr)_minmax(0,1.2fr)_auto] items-center gap-4 px-4 py-3 text-left hover:bg-[var(--hover)]"
    >
      <span className="min-w-0">
        <span className="block truncate font-medium" title={c.name}>
          {c.name}
        </span>
        <span className="mt-0.5 block">
          {account ? (
            <AccountPill label={account.account.label} color={account.account.color} />
          ) : (
            <span className="text-xs text-muted">Cuenta eliminada</span>
          )}
        </span>
      </span>

      <Pill tone={st.tone}>{st.label}</Pill>

      <span className="min-w-0">
        <span className="mb-1 block text-sm tabular-nums" data-testid="campaign-progress">
          {p.sent}/{p.total}
        </span>
        <ProgressBar pct={p.pct} tone={c.state === "braked" ? "danger" : "ok"} />
      </span>

      <span
        className={cx("min-w-0 truncate text-sm", !line.text && "text-muted")}
        style={line.text ? { color: line.tone === "muted" ? "var(--muted)" : TONE_COLORS[line.tone] } : undefined}
        title={line.text}
        data-testid="campaign-line"
      >
        {line.text || "—"}
      </span>

      <ChevronRight size={16} className="text-muted" />
    </button>
  );
}
