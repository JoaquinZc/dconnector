// Error de la primera carga de una pantalla: dice qué pasó y ofrece volver a intentarlo (nunca un esqueleto eterno).
import { RefreshCw, TriangleAlert } from "lucide-react";

interface Props {
  /** Qué no se pudo cargar: «las campañas». */
  what: string;
  onRetry: () => void;
  className?: string;
}

export default function LoadError({ what, onRetry, className }: Props) {
  return (
    <div role="alert" className={`flex flex-col items-center justify-center gap-3 p-8 text-center text-muted ${className ?? ""}`}>
      <TriangleAlert size={40} strokeWidth={1.4} aria-hidden="true" />
      <p className="text-base">{`No se pudieron cargar ${what}`}</p>
      <p className="max-w-md text-sm">Comprueba la conexión con el servidor y vuelve a intentarlo.</p>
      <button type="button" className="btn" onClick={onRetry}>
        <RefreshCw size={16} aria-hidden="true" />
        Reintentar
      </button>
    </div>
  );
}
