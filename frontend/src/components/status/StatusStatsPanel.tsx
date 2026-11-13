import { useEffect, useState } from "react";
import { Loader2 } from "lucide-react";
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { api } from "@/api";
import { cx } from "@/lib/cx";
import { readStatusStats, type CurvePoint, type StatusStatsView } from "./statsShape";

export const PERIODS = [7, 30, 90] as const;
export type Period = (typeof PERIODS)[number];

interface Props {
  accountId: string;
}

const nf = new Intl.NumberFormat("es-EC", { maximumFractionDigits: 1 });

/** «+25 %», «−10 %»; nada si no hay base para comparar. */
export function changeText(pct: number | null): string {
  if (pct === null) return "";
  if (pct === 0) return "0 %";
  const n = nf.format(Math.abs(pct));
  return `${pct > 0 ? "+" : "−"}${n} %`;
}

function Kpi({ label, value, change }: { label: string; value: string; change?: number | null }) {
  const text = changeText(change ?? null);
  return (
    <div className="card flex-1 px-4 py-3">
      <p className="text-xs font-semibold text-muted">{label}</p>
      <p className="mt-1 flex items-baseline gap-2">
        <span className="text-2xl font-semibold" data-testid={`kpi-${label}`}>
          {value}
        </span>
        {text && (
          <span
            data-testid={`change-${label}`}
            className={`text-xs font-medium ${change! > 0 ? "text-ok" : change! < 0 ? "text-danger" : "text-muted"}`}
            title="Frente al periodo anterior"
          >
            {text}
          </span>
        )}
      </p>
    </div>
  );
}

function ViewsChart({ title, data, testId }: { title: string; data: CurvePoint[]; testId: string }) {
  return (
    <div className="card p-4">
      <p className="label">{title}</p>
      {data.some((p) => p.views > 0) ? (
        <div className="h-56 w-full" data-testid={testId}>
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={data} margin={{ top: 8, right: 8, left: -16, bottom: 0 }}>
              <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="var(--border)" />
              <XAxis dataKey="label" tick={{ fontSize: 11, fill: "var(--muted)" }} tickLine={false} axisLine={false} />
              <YAxis allowDecimals={false} tick={{ fontSize: 11, fill: "var(--muted)" }} tickLine={false} axisLine={false} />
              <Tooltip
                contentStyle={{ background: "var(--panel)", border: "1px solid var(--border)", borderRadius: 8 }}
                labelStyle={{ color: "var(--text)" }}
                formatter={(v) => [String(v), "Vistas"]}
              />
              <Area type="monotone" dataKey="views" stroke="var(--accent)" fill="var(--accent)" fillOpacity={0.18} strokeWidth={2} />
            </AreaChart>
          </ResponsiveContainer>
        </div>
      ) : (
        <p className="py-10 text-center text-sm text-muted">Sin datos</p>
      )}
    </div>
  );
}

/** Estadísticas de mis estados: periodo, tres cifras y la curva de vistas. */
export default function StatusStatsPanel({ accountId }: Props) {
  const [days, setDays] = useState<Period>(7);
  const [stats, setStats] = useState<StatusStatsView | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let alive = true;
    setLoading(true);
    api.statuses
      .Stats(accountId, { days, from: "", to: "" })
      .then((raw) => alive && setStats(readStatusStats(raw)))
      .catch(() => alive && setStats(readStatusStats(null)))
      .finally(() => alive && setLoading(false));
    return () => {
      alive = false;
    };
  }, [accountId, days]);

  return (
    <div className="flex flex-col gap-4">
      <div role="radiogroup" aria-label="Periodo" className="inline-flex self-start rounded-lg bg-panel2 p-0.5">
        {PERIODS.map((p) => (
          <button
            key={p}
            role="radio"
            aria-checked={days === p}
            onClick={() => setDays(p)}
            className={cx(
              "rounded-md px-3 py-1 text-sm font-medium",
              days === p ? "bg-panel shadow-sm" : "text-muted hover:text-ink",
            )}
          >
            {p} días
          </button>
        ))}
      </div>

      {loading && !stats ? (
        <div className="flex justify-center p-8 text-muted">
          <Loader2 className="animate-spin" size={22} />
        </div>
      ) : (
        <>
          <div className="flex gap-3">
            <Kpi label="Publicados" value={nf.format(stats?.published ?? 0)} change={stats?.changes.published} />
            <Kpi label="Vistas medias" value={nf.format(stats?.avgViews ?? 0)} change={stats?.changes.avgViews} />
            <Kpi label="Alcance" value={nf.format(stats?.reach ?? 0)} change={stats?.changes.reach} />
          </div>
          <ViewsChart title="Vistas" data={stats?.curve ?? []} testId="views-curve" />
          {(stats?.hourly.some((p) => p.views > 0) ?? false) && (
            <ViewsChart title="Vistas desde la publicación" data={stats?.hourly ?? []} testId="views-hourly" />
          )}
        </>
      )}
    </div>
  );
}
