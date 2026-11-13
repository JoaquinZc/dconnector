import { useEffect, useState } from "react";
import { BellOff, X } from "lucide-react";
import { api } from "@/api";

/**
 * Aviso discreto del primer arranque: si Windows tiene bloqueadas las notificaciones de la aplicación, lo
 * dice una sola vez y ofrece abrir los ajustes de Windows. La comprobación se hace una vez por instalación
 * (el servicio recuerda que ya se hizo); si todo está bien no se muestra nada.
 */
export default function NotifyNotice() {
  const [reason, setReason] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const st = await api.notifications.Status();
        if (st.asked) return;
        await api.notifications.MarkAsked();
        if (alive && st.state === "denied") {
          setReason(st.reason || "Windows tiene bloqueadas las notificaciones de dConnector.");
        }
      } catch {
        /* sin servicio o sin permiso de lectura: no se molesta */
      }
    })();
    return () => {
      alive = false;
    };
  }, []);

  if (!reason) return null;
  return (
    <div
      role="status"
      data-testid="notify-notice"
      className="anim-pop fixed bottom-4 left-4 z-[65] flex w-[min(380px,calc(100vw-2rem))] items-start gap-2.5 rounded-xl border border-line bg-panel px-3.5 py-3"
      style={{ boxShadow: "var(--shadow)", borderLeft: "4px solid var(--warn)" }}
    >
      <BellOff size={18} className="mt-px shrink-0" style={{ color: "var(--warn)" }} />
      <div className="min-w-0 flex-1 text-sm">
        <p className="break-words">{reason}</p>
        <p className="mt-0.5 text-muted">Por ahora solo sonará un aviso cuando llegue un mensaje.</p>
        <button
          className="btn btn-sm mt-2"
          onClick={() => {
            void api.notifications.OpenSystemSettings().catch(() => undefined);
            setReason(null);
          }}
        >
          Abrir ajustes de Windows
        </button>
      </div>
      <button className="icon-btn !h-6 !w-6 shrink-0" onClick={() => setReason(null)} aria-label="Cerrar aviso" title="Cerrar">
        <X size={14} />
      </button>
    </div>
  );
}
