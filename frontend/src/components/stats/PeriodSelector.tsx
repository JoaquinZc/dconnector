import { PRESET_DAYS, localDay, presetLabel, validateCustom, type PeriodPreset, type PeriodState } from "./periodModel";
import { cx } from "@/lib/cx";

interface Props {
  value: PeriodState;
  onChange: (p: PeriodState) => void;
}

const PRESETS: PeriodPreset[] = [...PRESET_DAYS, "custom"];

/** 7 / 30 / 90 días o personalizado (dos fechas locales). */
export default function PeriodSelector({ value, onChange }: Props) {
  const today = new Date();
  const error = value.preset === "custom" ? validateCustom(value.from, value.to, today) : null;
  const max = localDay(today);

  return (
    <div className="flex flex-wrap items-center gap-2">
      <div role="radiogroup" aria-label="Periodo" className="inline-flex rounded-lg bg-panel2 p-0.5">
        {PRESETS.map((p) => (
          <button
            key={p}
            type="button"
            role="radio"
            aria-checked={value.preset === p}
            onClick={() => onChange({ ...value, preset: p })}
            className={cx(
              "rounded-md px-3 py-1 text-sm font-medium",
              value.preset === p ? "bg-panel shadow-sm" : "text-muted hover:text-ink",
            )}
          >
            {presetLabel(p)}
          </button>
        ))}
      </div>
      {value.preset === "custom" && (
        <div className="flex items-center gap-2">
          <input
            type="date"
            className="input !h-8 !w-auto"
            aria-label="Desde"
            value={value.from}
            max={max}
            onChange={(e) => onChange({ ...value, from: e.target.value })}
          />
          <span className="text-muted">—</span>
          <input
            type="date"
            className="input !h-8 !w-auto"
            aria-label="Hasta"
            value={value.to}
            max={max}
            onChange={(e) => onChange({ ...value, to: e.target.value })}
          />
          {error && (
            <span className="text-xs text-danger" role="alert">
              {error}
            </span>
          )}
        </div>
      )}
    </div>
  );
}
