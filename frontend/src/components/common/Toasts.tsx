import { AlertCircle, CheckCircle2, Info, X } from "lucide-react";
import { useUI, type ToastKind } from "@/store/ui";

const KIND: Record<ToastKind, { color: string; Icon: typeof Info }> = {
  error: { color: "var(--danger)", Icon: AlertCircle },
  success: { color: "var(--ok)", Icon: CheckCircle2 },
  info: { color: "#229ED9", Icon: Info },
};

/** Avisos (errores, confirmaciones) apilados abajo a la derecha. */
export default function Toasts() {
  const toasts = useUI((s) => s.toasts);
  const dismiss = useUI((s) => s.dismissToast);
  return (
    <div
      className="pointer-events-none fixed bottom-4 right-4 z-[70] flex w-[min(380px,calc(100vw-2rem))] flex-col gap-2"
      role="status"
      aria-live="polite"
    >
      {toasts.map((t) => {
        const { color, Icon } = KIND[t.kind];
        return (
          <div
            key={t.id}
            className="anim-pop pointer-events-auto flex items-start gap-2.5 rounded-xl border border-line bg-panel px-3.5 py-3"
            style={{ boxShadow: "var(--shadow)", borderLeft: `4px solid ${color}` }}
          >
            <Icon size={18} className="mt-px shrink-0" style={{ color }} />
            <p className="min-w-0 flex-1 break-words text-sm" title={t.message}>
              {t.message}
            </p>
            <button className="icon-btn !h-6 !w-6 shrink-0" onClick={() => dismiss(t.id)} aria-label="Cerrar aviso" title="Cerrar">
              <X size={14} />
            </button>
          </div>
        );
      })}
    </div>
  );
}
