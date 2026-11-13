import { cx } from "@/lib/cx";

interface Props {
  checked: boolean;
  onChange: (v: boolean) => void;
  /** Nombre accesible (también el `title`). */
  label: string;
  disabled?: boolean;
}

/** Interruptor accesible (`role="switch"`). */
export default function Switch({ checked, onChange, label, disabled }: Props) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      title={label}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={cx(
        "relative inline-flex h-5 w-9 shrink-0 cursor-pointer items-center rounded-full border border-transparent transition-colors",
        "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--accent)]",
        checked ? "bg-accent" : "bg-[color-mix(in_srgb,var(--muted)_45%,transparent)]",
        disabled && "cursor-not-allowed opacity-50",
      )}
    >
      <span
        className={cx(
          "inline-block h-4 w-4 rounded-full bg-white shadow transition-transform",
          checked ? "translate-x-[18px]" : "translate-x-0.5",
        )}
      />
    </button>
  );
}
