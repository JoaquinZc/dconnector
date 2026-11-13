// Campañas: cabecera con sus dos botones y la lista de campañas (hasta 1024 px de ancho, centrada).
import { Gauge, Plus } from "lucide-react";
import { PageRoot } from "./parts";
import { CampaignRowsSkeleton } from "./zones";

export default function CampaignsSkeleton() {
  return (
    <PageRoot testId="campaigns-skeleton">
      <header className="flex items-center gap-3 border-b border-line bg-panel px-6 py-3">
        <h1 className="flex-1 text-lg font-semibold">Campañas</h1>
        <span className="btn">
          <Gauge size={16} />
          Ritmo de la cuenta
        </span>
        <span className="btn btn-primary">
          <Plus size={16} />
          Nueva campaña
        </span>
      </header>
      <div className="min-h-0 flex-1 overflow-hidden p-6">
        <CampaignRowsSkeleton bare />
      </div>
    </PageRoot>
  );
}
