import { cx } from "@/lib/cx";

export interface ChipOption {
  id: string;
  label: string;
  color?: string;
  /** No se puede elegir (p. ej. ya está en el otro lado). */
  disabled?: boolean;
  /** Texto de ayuda de la ficha (por defecto, su nombre). */
  hint?: string;
}

interface Props {
  options: ChipOption[];
  /** Vacío = «Todas». */
  value: string[];
  onChange: (v: string[]) => void;
  allLabel: string;
  ariaLabel: string;
  /** Sin la ficha «Todas»: elegir ninguna es «ninguna», no «todas». */
  hideAll?: boolean;
}

/** Selección múltiple con una ficha «Todas» que equivale a no elegir ninguna. */
export default function ChipSelect({ options, value, onChange, allLabel, ariaLabel, hideAll }: Props) {
  const toggle = (id: string) => onChange(value.includes(id) ? value.filter((x) => x !== id) : [...value, id]);
  const base = "inline-flex max-w-[180px] items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium";
  return (
    <div className="flex flex-wrap gap-1.5" role="group" aria-label={ariaLabel}>
      {!hideAll && (
        <button
          type="button"
          aria-pressed={value.length === 0}
          onClick={() => onChange([])}
          className={cx(base, value.length === 0 ? "border-accent bg-accent text-white" : "border-line hover:bg-[var(--hover)]")}
        >
          {allLabel}
        </button>
      )}
      {options.map((o) => {
        const on = value.includes(o.id);
        return (
          <button
            key={o.id}
            type="button"
            aria-pressed={on}
            title={o.hint ?? o.label}
            disabled={o.disabled && !on}
            onClick={() => toggle(o.id)}
            className={cx(
              base,
              on ? "border-accent bg-accent text-white" : "border-line hover:bg-[var(--hover)]",
              o.disabled && !on && "cursor-not-allowed opacity-45 hover:bg-transparent",
            )}
          >
            {o.color && <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: o.color }} />}
            <span className="truncate">{o.label}</span>
          </button>
        );
      })}
    </div>
  );
}
