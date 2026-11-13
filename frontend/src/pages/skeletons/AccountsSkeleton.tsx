// Cuentas: cabecera real y cuadrícula de tarjetas de cuenta.
import { Plus } from "lucide-react";
import { PageRoot } from "./parts";
import { AccountsGridSkeleton } from "./zones";

export default function AccountsSkeleton() {
  return (
    <PageRoot testId="accounts-skeleton">
      <header className="flex items-center gap-3 border-b border-line bg-panel px-6 py-3">
        <h1 className="flex-1 text-lg font-semibold">Cuentas</h1>
        <span className="btn btn-primary">
          <Plus size={16} />
          Añadir cuenta
        </span>
      </header>
      <div className="min-h-0 flex-1 overflow-hidden p-6">
        <AccountsGridSkeleton bare />
      </div>
    </PageRoot>
  );
}
