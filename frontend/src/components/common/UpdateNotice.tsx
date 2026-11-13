import { useEffect } from "react";
import { Download, Loader2, X } from "lucide-react";
import { isBusy, phaseText, updateApi, useUpdate } from "@/api/update";

/**
 * Aviso discreto (no modal) cuando hay una versión nueva: «Actualizar» o «Más tarde» (no vuelve a salir para
 * esa versión en esta sesión). Si la copia no se puede actualizar desde aquí, ofrece abrir la página.
 */
export default function UpdateNotice() {
  const available = useUpdate((s) => s.available);
  const status = useUpdate((s) => s.status);
  const dismissed = useUpdate((s) => s.dismissed);
  const init = useUpdate((s) => s.init);
  const apply = useUpdate((s) => s.apply);
  const dismiss = useUpdate((s) => s.dismiss);

  useEffect(() => init(), [init]);

  const latest = status?.latest;
  if (!available || !status || !latest || dismissed.includes(latest.version)) return null;
  const showing = status.state === "available" || status.state === "error" || isBusy(status.state);
  if (!showing || status.state === "checking") return null;

  const busy = isBusy(status.state);
  const pct = status.state === "downloading" && status.progress.total > 0 ? Math.min(100, Math.round((status.progress.bytes / status.progress.total) * 100)) : null;
  return (
    <div
      role="status"
      data-testid="update-notice"
      className="anim-pop fixed bottom-4 right-4 z-[65] flex w-[min(360px,calc(100vw-2rem))] items-start gap-2.5 rounded-xl border border-line bg-panel px-3.5 py-3"
      style={{ boxShadow: "var(--shadow)", borderLeft: "4px solid var(--accent)" }}
    >
      <Download size={18} className="mt-px shrink-0" style={{ color: "var(--accent)" }} aria-hidden="true" />
      <div className="min-w-0 flex-1 text-sm">
        <p className="break-words font-medium">Hay una versión nueva ({latest.version})</p>
        {busy && (
          <>
            <p className="mt-1 flex items-center gap-1.5 break-words text-muted" data-testid="update-notice-phase">
              <Loader2 size={13} className="shrink-0 animate-spin" aria-hidden="true" />
              {phaseText(status)}
            </p>
            {pct !== null && (
              <div className="mt-1.5 h-1.5 overflow-hidden rounded-full bg-panel2" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={pct} aria-label="Progreso de la descarga">
                <div className="h-full rounded-full bg-accent" style={{ width: `${pct}%` }} />
              </div>
            )}
          </>
        )}
        {status.state === "error" && (
          <p className="mt-1 break-words text-danger" data-testid="update-notice-error">
            {status.error || "No se pudo actualizar."}
          </p>
        )}
        {!busy && (
          <div className="mt-2 flex flex-wrap gap-2">
            {status.canInstall ? (
              <button className="btn btn-primary btn-sm" onClick={() => void apply()}>
                {status.state === "error" ? "Reintentar" : "Actualizar"}
              </button>
            ) : (
              <button className="btn btn-primary btn-sm" onClick={() => void updateApi.openReleasePage().catch(() => undefined)}>
                Ver versión
              </button>
            )}
            <button className="btn btn-sm" onClick={() => dismiss(latest.version)}>
              Más tarde
            </button>
          </div>
        )}
      </div>
      {!busy && (
        <button className="icon-btn !h-6 !w-6 shrink-0" onClick={() => dismiss(latest.version)} aria-label="Cerrar aviso" title="Cerrar">
          <X size={14} />
        </button>
      )}
    </div>
  );
}
