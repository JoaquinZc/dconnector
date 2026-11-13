import { useEffect, useId, useMemo, useRef, useState } from "react";
import { api, errorMessage } from "@/api";
import { cx } from "@/lib/cx";
import {
  DAY_LETTERS,
  DAY_NAMES,
  MAX_COUNT,
  MAX_EVERY,
  REPEAT_OPTIONS,
  UNIT_OPTIONS,
  WEEK_ORDER,
  buildRecurrence,
  formatOccurrence,
  summarizeForm,
  validateRecurrenceForm,
  type CustomUnit,
  type EndMode,
  type RecurrenceForm,
  type RepeatKind,
} from "@/lib/recurrence";

interface Props {
  value: RecurrenceForm;
  onChange: (next: RecurrenceForm) => void;
  /** Fecha del primer envío (ms), o `null` si aún no es válida. */
  firstMs: number | null;
  /** Cuántas fechas muestra la vista previa. */
  previewCount?: number;
}

const PREVIEW_DEBOUNCE_MS = 350;

/** Número de un `<input type="number">`; vacío o inválido = 0 (la validación lo avisa). */
function toNum(raw: string): number {
  const n = Number.parseInt(raw, 10);
  return Number.isNaN(n) ? 0 : n;
}

type Preview = { state: "idle" } | { state: "loading" } | { state: "ok"; dates: number[] } | { state: "error"; message: string };

/**
 * Editor de repetición compartido por el diálogo «Programar» del chat y el formulario de la página
 * Programados: frecuencia, días, personalizado, final, resumen y vista previa de las próximas fechas
 * (`PreviewRecurrence`, con debounce).
 */
export default function RecurrenceEditor({ value, onChange, firstMs, previewCount = 5 }: Props) {
  const uid = useId();
  const id = (s: string) => `${uid}-${s}`;
  const patch = (p: Partial<RecurrenceForm>) => onChange({ ...value, ...p });

  const localError = useMemo(() => validateRecurrenceForm(value, firstMs), [value, firstMs]);
  const summary = useMemo(() => summarizeForm(value, firstMs), [value, firstMs]);

  const setRepeat = (repeat: RepeatKind) => {
    const next: Partial<RecurrenceForm> = { repeat };
    // Al elegir «Cada semana» se marca de entrada el día de la fecha elegida.
    if (repeat === "weekly" && value.weekdays.length === 0 && firstMs !== null) next.weekdays = [new Date(firstMs).getDay()];
    patch(next);
  };

  const toggleDay = (d: number) => {
    const has = value.weekdays.includes(d);
    patch({ weekdays: has ? value.weekdays.filter((x) => x !== d) : [...value.weekdays, d] });
  };

  // --- vista previa (con debounce y descartando respuestas viejas)
  const [preview, setPreview] = useState<Preview>({ state: "idle" });
  const seq = useRef(0);
  const ruleKey = JSON.stringify([value, firstMs]);
  useEffect(() => {
    const n = ++seq.current;
    if (value.repeat === "none" || firstMs === null || localError) {
      setPreview({ state: "idle" });
      return;
    }
    const rule = buildRecurrence(value);
    if (!rule) {
      setPreview({ state: "idle" });
      return;
    }
    setPreview({ state: "loading" });
    const t = setTimeout(() => {
      api.scheduled
        .PreviewRecurrence(rule, firstMs, previewCount)
        .then((dates) => {
          if (n === seq.current) setPreview({ state: "ok", dates });
        })
        .catch((e: unknown) => {
          if (n === seq.current) setPreview({ state: "error", message: errorMessage(e) });
        });
    }, PREVIEW_DEBOUNCE_MS);
    return () => clearTimeout(t);
    // `ruleKey` resume value y firstMs.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ruleKey, localError, previewCount]);

  const repeating = value.repeat !== "none";

  return (
    <div className="flex flex-col gap-3" data-testid="recurrence-editor">
      <div>
        <label className="label" htmlFor={id("repeat")}>
          Repetir
        </label>
        <select id={id("repeat")} className="input" value={value.repeat} onChange={(e) => setRepeat(e.target.value as RepeatKind)}>
          {REPEAT_OPTIONS.map((o) => (
            <option key={o.id} value={o.id}>
              {o.label}
            </option>
          ))}
        </select>
      </div>

      {value.repeat === "weekly" && (
        <div className="flex flex-col gap-2">
          <div role="group" aria-label="Días de la semana" className="flex flex-wrap gap-1.5">
            {WEEK_ORDER.map((d) => {
              const on = value.weekdays.includes(d);
              return (
                <button
                  key={d}
                  type="button"
                  aria-pressed={on}
                  aria-label={DAY_NAMES[d]}
                  title={DAY_NAMES[d]}
                  onClick={() => toggleDay(d)}
                  className={cx(
                    "h-8 w-8 rounded-full border text-sm font-medium transition-colors",
                    "focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-[var(--accent)]",
                    on ? "border-accent bg-accent text-[var(--accent-contrast)]" : "border-line bg-panel2 text-muted hover:text-ink",
                  )}
                >
                  {DAY_LETTERS[d]}
                </button>
              );
            })}
          </div>
          <div className="flex items-center gap-2 text-sm">
            <label htmlFor={id("weekly-every")}>Cada</label>
            <input
              id={id("weekly-every")}
              type="number"
              min={1}
              max={MAX_EVERY}
              className="input !w-20"
              value={value.weeklyEvery || ""}
              onChange={(e) => patch({ weeklyEvery: toNum(e.target.value) })}
            />
            <span>{value.weeklyEvery === 1 ? "semana" : "semanas"}</span>
          </div>
        </div>
      )}

      {value.repeat === "custom" && (
        <div className="flex items-center gap-2 text-sm">
          <label htmlFor={id("custom-every")}>Cada</label>
          <input
            id={id("custom-every")}
            type="number"
            min={value.customUnit === "minutes" ? 5 : 1}
            max={MAX_EVERY}
            className="input !w-24"
            value={value.customEvery || ""}
            onChange={(e) => patch({ customEvery: toNum(e.target.value) })}
          />
          <select
            aria-label="Unidad"
            className="input !w-32"
            value={value.customUnit}
            onChange={(e) => patch({ customUnit: e.target.value as CustomUnit })}
          >
            {UNIT_OPTIONS.map((o) => (
              <option key={o.id} value={o.id}>
                {o.label}
              </option>
            ))}
          </select>
        </div>
      )}

      {repeating && (
        <div>
          <span className="label">Termina</span>
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <select
              aria-label="Termina"
              className="input !w-40"
              value={value.endMode}
              onChange={(e) => patch({ endMode: e.target.value as EndMode })}
            >
              <option value="never">Nunca</option>
              <option value="date">El día…</option>
              <option value="count">Después de…</option>
            </select>
            {value.endMode === "date" && (
              <input
                type="date"
                aria-label="Día en que termina"
                className="input !w-44"
                value={value.endDate}
                onChange={(e) => patch({ endDate: e.target.value })}
              />
            )}
            {value.endMode === "count" && (
              <>
                <input
                  type="number"
                  min={1}
                  max={MAX_COUNT}
                  aria-label="Número de veces"
                  className="input !w-24"
                  value={value.maxCount || ""}
                  onChange={(e) => patch({ maxCount: toNum(e.target.value) })}
                />
                <span>veces</span>
              </>
            )}
          </div>
        </div>
      )}

      {repeating && (
        <div className="rounded-lg border border-line bg-panel2 px-3 py-2 text-sm" aria-live="polite">
          {localError ? (
            <p className="text-danger" data-testid="recurrence-error">
              {localError}
            </p>
          ) : (
            <>
              {summary && (
                <p className="font-medium" data-testid="recurrence-summary">
                  {summary}
                </p>
              )}
              {preview.state === "loading" && <p className="mt-1 text-muted">Calculando próximas fechas…</p>}
              {preview.state === "error" && (
                <p className="mt-1 text-danger" data-testid="recurrence-error">
                  {preview.message}
                </p>
              )}
              {preview.state === "ok" && (
                <div className="mt-1">
                  <p className="text-xs text-muted">Próximas fechas</p>
                  {preview.dates.length === 0 ? (
                    <p className="text-muted">No hay fechas con esta regla.</p>
                  ) : (
                    <ol aria-label="Próximas fechas" className="mt-0.5 list-none space-y-0.5 tabular-nums" data-testid="recurrence-preview">
                      {preview.dates.map((ms, i) => (
                        <li key={`${ms}-${i}`}>{formatOccurrence(ms)}</li>
                      ))}
                    </ol>
                  )}
                </div>
              )}
            </>
          )}
        </div>
      )}
    </div>
  );
}
