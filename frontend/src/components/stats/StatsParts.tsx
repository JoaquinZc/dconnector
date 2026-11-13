import type { ReactNode } from "react";
import { ArrowDownRight, ArrowUpRight, Minus } from "lucide-react";
import { cx } from "@/lib/cx";
import {
  HEAT_DAYS,
  HEAT_HOURS,
  changeText,
  changeTone,
  heatLevel,
  heatmapPeak,
  type MixView,
} from "./statsShape";

export const nf = new Intl.NumberFormat("es-EC", { maximumFractionDigits: 1 });
export const n = (v: number) => nf.format(v);

/** Tarjeta con título. */
export function Card({ title, children, className }: { title?: string; children: ReactNode; className?: string }) {
  return (
    <section className={cx("card p-4", className)}>
      {title && <h3 className="label !mb-2">{title}</h3>}
      {children}
    </section>
  );
}

/** Un «Sin datos» corto. */
export function NoData({ className }: { className?: string }) {
  return <p className={cx("py-8 text-center text-sm text-muted", className)}>Sin datos</p>;
}

interface KpiProps {
  label: string;
  value: string;
  /** Variación frente al periodo anterior (fracción); `null`/`undefined` = sin base. */
  change?: number | null;
  /** Bajar es bueno (tiempos de respuesta). */
  lowerIsBetter?: boolean;
  hint?: string;
}

/** Cifra del periodo con su variación frente al anterior. */
export function Kpi({ label, value, change, lowerIsBetter, hint }: KpiProps) {
  const text = changeText(change);
  const tone = changeTone(change, lowerIsBetter);
  const Arrow = tone === "flat" ? Minus : change! > 0 ? ArrowUpRight : ArrowDownRight;
  return (
    <div className="card px-4 py-3" title={hint}>
      <p className="truncate text-xs font-semibold text-muted">{label}</p>
      <p className="mt-1 flex items-baseline gap-2">
        <span className="text-2xl font-semibold tabular-nums" data-testid={`kpi-${label}`}>
          {value}
        </span>
        {text && (
          <span
            data-testid={`change-${label}`}
            data-tone={tone}
            title="Frente al periodo anterior"
            className={cx(
              "inline-flex items-center gap-0.5 text-xs font-medium",
              tone === "good" && "text-ok",
              tone === "bad" && "text-danger",
              tone === "flat" && "text-muted",
            )}
          >
            <Arrow size={13} />
            {text}
          </span>
        )}
      </p>
    </div>
  );
}

/** Mapa de calor día × hora (filas lunes…domingo, columnas 0–23 h). `grid` ya viene 7 × 24. */
export function Heatmap({ grid, unit = "mensajes" }: { grid: number[][]; unit?: string }) {
  const max = Math.max(0, ...grid.flat());
  const peak = heatmapPeak(grid);
  return (
    <div className="flex flex-col gap-2" data-testid="heatmap">
      <div className="overflow-x-auto">
        <table className="border-separate" style={{ borderSpacing: 2 }}>
          <tbody>
            {grid.map((row, d) => (
              <tr key={d}>
                <th scope="row" className="pr-1 text-left text-[10px] font-semibold text-muted">
                  {HEAT_DAYS[d]}
                </th>
                {row.map((v, h) => (
                  <td
                    key={h}
                    data-testid="heat-cell"
                    title={`${HEAT_DAYS[d]} ${h}:00 · ${v} ${unit}`}
                    className="h-4 w-4 rounded-sm"
                    style={{
                      background: v
                        ? `color-mix(in srgb, var(--accent) ${Math.round(heatLevel(v, max) * 100)}%, transparent)`
                        : "var(--panel-2)",
                    }}
                  />
                ))}
              </tr>
            ))}
            <tr>
              <td />
              {Array.from({ length: HEAT_HOURS }, (_, h) => (
                <td key={h} className="text-center text-[9px] text-muted">
                  {h % 3 === 0 ? h : ""}
                </td>
              ))}
            </tr>
          </tbody>
        </table>
      </div>
      {peak && (
        <p className="text-xs text-muted" data-testid="heat-peak">
          Más actividad: <b className="text-ink">{HEAT_DAYS[peak.day]}</b> de <b className="text-ink">{peak.hour}:00</b> a{" "}
          <b className="text-ink">{peak.hour + 1}:00</b>
        </p>
      )}
    </div>
  );
}

const MIX: Array<[keyof MixView, string, string]> = [
  ["text", "Texto", "#25D366"],
  ["image", "Fotos", "#229ED9"],
  ["voice", "Voz y audio", "#F59E0B"],
  ["video", "Vídeos", "#8B5CF6"],
  ["document", "Documentos", "#64748B"],
  ["sticker", "Stickers", "#EC4899"],
  ["other", "Otros", "#CBD5E1"],
];

/** Barra con la mezcla de contenido y su leyenda. */
export function MixBar({ mix }: { mix: MixView }) {
  const total = MIX.reduce((a, [k]) => a + mix[k], 0);
  if (!total) return <NoData className="!py-4" />;
  return (
    <div className="flex flex-col gap-2">
      <div className="flex h-3 overflow-hidden rounded-full bg-panel2">
        {MIX.map(([k, label, color]) =>
          mix[k] ? <div key={k} title={label} style={{ width: `${(mix[k] / total) * 100}%`, background: color }} /> : null,
        )}
      </div>
      <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted">
        {MIX.map(([k, label, color]) =>
          mix[k] ? (
            <span key={k} className="flex items-center gap-1">
              <span className="h-2 w-2 rounded-full" style={{ background: color }} />
              {label} <b className="tabular-nums text-ink">{n(mix[k])}</b>
            </span>
          ) : null,
        )}
      </div>
    </div>
  );
}

/** Barra de ranking (parte 0..1 del máximo). */
export function RankBar({ share }: { share: number }) {
  return (
    <span className="block h-1.5 overflow-hidden rounded-full bg-panel2">
      <span className="block h-full rounded-full bg-accent" style={{ width: `${Math.round(share * 100)}%` }} />
    </span>
  );
}

export const CHART_TOOLTIP = {
  contentStyle: { background: "var(--panel)", border: "1px solid var(--border)", borderRadius: 8 },
  labelStyle: { color: "var(--text)" },
} as const;

export const AXIS_TICK = { fontSize: 11, fill: "var(--muted)" } as const;

/** Colores de las series: enviados / recibidos. */
export const SENT_COLOR = "#00a884";
export const RECEIVED_COLOR = "#229ED9";
