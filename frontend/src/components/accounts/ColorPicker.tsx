import { Check } from "lucide-react";
import { ACCOUNT_PALETTE } from "@/lib/channels";

interface Props {
  value: string;
  onChange: (color: string) => void;
}

/** Los 10 colores de la paleta de cuentas. */
export default function ColorPicker({ value, onChange }: Props) {
  return (
    <div className="flex flex-wrap gap-2" role="radiogroup" aria-label="Color de la cuenta">
      {ACCOUNT_PALETTE.map((c) => {
        const selected = c.toLowerCase() === value.toLowerCase();
        return (
          <button
            key={c}
            type="button"
            role="radio"
            aria-checked={selected}
            aria-label={c}
            onClick={() => onChange(c)}
            className="flex h-7 w-7 items-center justify-center rounded-full text-white transition-transform hover:scale-110"
            style={{ background: c, outline: selected ? `2px solid ${c}` : "none", outlineOffset: 2 }}
          >
            {selected && <Check size={15} strokeWidth={3} />}
          </button>
        );
      })}
    </div>
  );
}
