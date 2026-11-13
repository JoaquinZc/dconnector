import { useEffect, useRef, useState, type ReactNode } from "react";
import { MoreVertical } from "lucide-react";
import { cx } from "@/lib/cx";

export interface KebabItem {
  label: string;
  icon?: ReactNode;
  onClick: () => void;
  danger?: boolean;
}

interface Props {
  items: KebabItem[];
  label?: string;
  align?: "left" | "right";
}

/** Botón ⋮ con un menú que se cierra al hacer clic fuera o con Esc. */
export default function KebabMenu({ items, label = "Más opciones", align = "right" }: Props) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const down = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    const key = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    window.addEventListener("mousedown", down);
    window.addEventListener("keydown", key);
    return () => {
      window.removeEventListener("mousedown", down);
      window.removeEventListener("keydown", key);
    };
  }, [open]);

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        className="icon-btn"
        aria-label={label}
        title={label}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <MoreVertical size={18} />
      </button>
      {open && (
        <div
          role="menu"
          className={cx(
            "anim-pop absolute top-full z-30 mt-1 min-w-[220px] overflow-hidden rounded-xl border border-line bg-panel py-1.5",
            align === "right" ? "right-0" : "left-0",
          )}
          style={{ boxShadow: "var(--shadow)" }}
        >
          {items.map((it) => (
            <button
              key={it.label}
              type="button"
              role="menuitem"
              className={cx(
                "flex w-full items-center gap-2.5 px-4 py-2 text-left hover:bg-[var(--hover)]",
                it.danger && "text-danger",
              )}
              onClick={() => {
                setOpen(false);
                it.onClick();
              }}
            >
              {it.icon}
              {it.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
