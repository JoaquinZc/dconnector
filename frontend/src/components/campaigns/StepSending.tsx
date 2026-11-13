import { Gauge } from "lucide-react";
import type { PacingSettings } from "@/api/types";
import Switch from "@/components/settings/Switch";
import { cx } from "@/lib/cx";
import { WEEKDAYS, toggleWeekday, type CampaignForm } from "./campaignModel";
import { pacingSummary } from "./pacingModel";

interface Props {
  form: CampaignForm;
  patch: (p: Partial<CampaignForm>) => void;
  pacing: PacingSettings | null;
  onOpenPacing: () => void;
}

/** Paso 4: inicio, ventana horaria y días, A/B y el ritmo de la cuenta. */
export default function StepSending({ form, patch, pacing, onOpenPacing }: Props) {
  return (
    <div className="flex flex-col gap-5">
      <div>
        <span className="label">Inicio</span>
        <div className="flex flex-wrap items-center gap-3">
          <div role="radiogroup" aria-label="Inicio" className="inline-flex rounded-lg bg-panel2 p-0.5">
            {(
              [
                ["now", "Ahora"],
                ["later", "Fecha y hora"],
              ] as const
            ).map(([id, label]) => (
              <button
                key={id}
                role="radio"
                aria-checked={form.startMode === id}
                onClick={() => patch({ startMode: id })}
                className={cx(
                  "rounded-md px-4 py-1.5 text-sm font-medium",
                  form.startMode === id ? "bg-panel shadow-sm" : "text-muted hover:text-ink",
                )}
              >
                {label}
              </button>
            ))}
          </div>
          {form.startMode === "later" && (
            <input
              type="datetime-local"
              className="input !w-auto"
              value={form.startAt}
              onChange={(e) => patch({ startAt: e.target.value })}
              aria-label="Fecha y hora de inicio"
            />
          )}
        </div>
      </div>

      <div>
        <span className="label">Ventana horaria</span>
        <div className="flex flex-wrap items-center gap-3">
          <input
            type="time"
            className="input !w-auto"
            value={form.fromTime}
            onChange={(e) => patch({ fromTime: e.target.value })}
            aria-label="Desde"
          />
          <span className="text-muted">a</span>
          <input
            type="time"
            className="input !w-auto"
            value={form.toTime}
            onChange={(e) => patch({ toTime: e.target.value })}
            aria-label="Hasta"
          />
        </div>
        <div role="group" aria-label="Días" className="mt-3 flex flex-wrap gap-1.5">
          {WEEKDAYS.map((d) => {
            const on = form.weekdays.includes(d.id);
            return (
              <button
                key={d.id}
                type="button"
                aria-pressed={on}
                title={d.label}
                onClick={() => patch({ weekdays: toggleWeekday(form.weekdays, d.id) })}
                className={cx(
                  "h-9 w-11 rounded-lg border text-sm font-medium",
                  on ? "border-accent bg-accent text-white" : "border-line bg-panel hover:bg-[var(--hover)]",
                )}
              >
                {d.label}
              </button>
            );
          })}
        </div>
      </div>

      <div className="flex items-center gap-3">
        <Switch checked={form.abEnabled} onChange={(abEnabled) => patch({ abEnabled })} label="Prueba A/B" />
        <span className="text-sm" title="Envía primero una muestra y reparte el resto con la variante que mejor responda">
          Prueba A/B
        </span>
      </div>

      <div className="flex items-center gap-3 rounded-xl border border-line bg-panel2 p-3">
        <Gauge size={18} className="shrink-0 text-muted" />
        <div className="min-w-0 flex-1">
          <p className="text-sm font-medium">Ritmo de la cuenta</p>
          <p className="truncate text-xs text-muted" title={pacing ? pacingSummary(pacing) : undefined}>
            {pacing ? pacingSummary(pacing) : "…"}
          </p>
        </div>
        <button className="btn btn-sm" onClick={onOpenPacing}>
          Ajustar
        </button>
      </div>
    </div>
  );
}
