import { truncate } from "@/lib/format";

interface Props {
  label: string;
  color: string;
  className?: string;
}

/** Píldora de cuenta: punto de su color + etiqueta (máx. 12 caracteres; el resto con «…»). */
export default function AccountPill({ label, color, className }: Props) {
  return (
    <span
      title={label}
      className={`inline-flex max-w-full items-center gap-1 whitespace-nowrap rounded-full px-1.5 py-px text-[11px] font-medium leading-4 ${className ?? ""}`}
      style={{ background: `color-mix(in srgb, ${color} 18%, transparent)`, color }}
    >
      <span className="inline-block h-1.5 w-1.5 shrink-0 rounded-full" style={{ background: color }} />
      <span className="min-w-0 overflow-hidden text-ellipsis">{truncate(label, 12)}</span>
    </span>
  );
}
