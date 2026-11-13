import { useEffect, useRef, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { X } from "lucide-react";

export type ModalSize = "sm" | "md" | "lg" | "full";

const WIDTHS: Record<ModalSize, string> = {
  sm: "w-[400px]",
  md: "w-[560px]",
  lg: "w-[820px]",
  full: "w-[calc(100vw-48px)] h-[calc(100vh-48px)]",
};

// Pila de modales abiertos: `Esc` (App.tsx) cierra el de arriba.
const stack: Array<{ close: () => void }> = [];

/** Cierra el modal de más arriba. Devuelve `false` si no había ninguno. */
export function closeTopModal(): boolean {
  const top = stack[stack.length - 1];
  if (!top) return false;
  top.close();
  return true;
}

interface Props {
  open: boolean;
  onClose: () => void;
  title?: ReactNode;
  size?: ModalSize;
  children: ReactNode;
  /** Pie con los botones. */
  footer?: ReactNode;
  /** Cerrar al pulsar fuera (por defecto sí). */
  closeOnBackdrop?: boolean;
  /** Sin la X de la cabecera. */
  hideClose?: boolean;
}

export default function Modal({
  open,
  onClose,
  title,
  size = "md",
  children,
  footer,
  closeOnBackdrop = true,
  hideClose,
}: Props) {
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  const panelRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const entry = { close: () => closeRef.current() };
    stack.push(entry);
    const previous = document.activeElement as HTMLElement | null;
    // Enfoca el primer campo; si no hay, el propio panel.
    const t = setTimeout(() => {
      const el = panelRef.current?.querySelector<HTMLElement>("[data-autofocus], input:not([type=hidden]), textarea, select");
      (el ?? panelRef.current)?.focus();
    }, 0);
    return () => {
      clearTimeout(t);
      const i = stack.indexOf(entry);
      if (i >= 0) stack.splice(i, 1);
      previous?.focus?.();
    };
  }, [open]);

  if (!open) return null;

  return createPortal(
    <div
      className="anim-fade fixed inset-0 z-50 flex items-center justify-center p-6"
      style={{ background: "var(--overlay)" }}
      onMouseDown={(e) => {
        if (closeOnBackdrop && e.target === e.currentTarget) onClose();
      }}
    >
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        tabIndex={-1}
        className={`anim-pop flex max-h-full max-w-full flex-col overflow-hidden rounded-2xl bg-panel text-ink outline-none ${WIDTHS[size]}`}
        style={{ boxShadow: "var(--shadow)" }}
      >
        {(title || !hideClose) && (
          <div className="flex items-center gap-2 border-b border-line px-5 py-3">
            <h2 className="min-w-0 flex-1 truncate text-base font-semibold">{title}</h2>
            {!hideClose && (
              <button className="icon-btn" onClick={onClose} aria-label="Cerrar" title="Cerrar">
                <X size={18} />
              </button>
            )}
          </div>
        )}
        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">{children}</div>
        {footer && <div className="flex items-center justify-end gap-2 border-t border-line px-5 py-3">{footer}</div>}
      </div>
    </div>,
    document.body,
  );
}
