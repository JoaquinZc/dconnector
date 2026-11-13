import { cx } from "@/lib/cx";
import type { AccountView } from "@/api/types";

interface Props {
  accounts: AccountView[];
  /** Ids elegidos; vacío = todas. */
  selected: string[];
  onChange: (ids: string[]) => void;
}

/** Chips «Todas» + una por cuenta (selección múltiple). Vacío = todas. */
export default function StatsAccountPicker({ accounts, selected, onChange }: Props) {
  if (accounts.length < 2) return null;
  const toggle = (id: string) =>
    onChange(selected.includes(id) ? selected.filter((x) => x !== id) : [...selected, id]);
  const allOn = selected.length === 0;

  return (
    <div className="flex flex-wrap items-center gap-1.5" role="group" aria-label="Cuentas">
      <button
        type="button"
        aria-pressed={allOn}
        onClick={() => onChange([])}
        className={cx(
          "rounded-full border px-3 py-1 text-sm",
          allOn ? "border-transparent bg-accent text-white" : "border-line text-muted hover:text-ink",
        )}
      >
        Todas
      </button>
      {accounts.map((v) => {
        const on = selected.includes(v.account.id);
        return (
          <button
            key={v.account.id}
            type="button"
            aria-pressed={on}
            onClick={() => toggle(v.account.id)}
            title={v.account.label}
            className={cx(
              "inline-flex max-w-[180px] items-center gap-1.5 rounded-full border px-3 py-1 text-sm",
              on ? "border-transparent text-white" : "border-line text-muted hover:text-ink",
            )}
            style={on ? { background: v.account.color } : undefined}
          >
            {!on && <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: v.account.color }} />}
            <span className="truncate">{v.account.label}</span>
          </button>
        );
      })}
    </div>
  );
}
