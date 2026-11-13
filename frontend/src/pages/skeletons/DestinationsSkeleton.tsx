// Destinos: cabecera y tarjetas de destino (hasta 768 px de ancho, centradas).
import { Plus } from "lucide-react";
import { PageRoot } from "./parts";
import { DestinationCardsSkeleton } from "./zones";

export default function DestinationsSkeleton() {
  return (
    <PageRoot testId="destinations-skeleton">
      <header className="flex items-center gap-3 border-b border-line bg-panel px-6 py-3">
        <h1 className="flex-1 text-lg font-semibold">Destinos</h1>
        <span className="btn btn-primary">
          <Plus size={16} />
          Nuevo destino
        </span>
      </header>
      <div className="min-h-0 flex-1 overflow-hidden p-6">
        <DestinationCardsSkeleton bare />
      </div>
    </PageRoot>
  );
}
