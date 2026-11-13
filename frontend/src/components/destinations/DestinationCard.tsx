import { Pencil, Terminal, Trash2 } from "lucide-react";
import type { Destination, DestinationState } from "@/api/types";
import Switch from "@/components/settings/Switch";
import { TONE_COLOR } from "@/lib/accountStatus";
import { contactScopeSummary, pendingLabel, stateView, toForm, type ListNames } from "./destinationModel";

interface Props {
  destination: Destination;
  state: DestinationState | undefined;
  /** Nombres de las listas de contactos (`null` si aún no se cargan). */
  listNames?: ListNames;
  onToggle: (enabled: boolean) => void;
  onEdit: () => void;
  onDelete: () => void;
}

/** Fila de la lista de destinos con su estado en vivo. */
export default function DestinationCard({ destination: d, state, listNames = null, onToggle, onEdit, onDelete }: Props) {
  const v = stateView(d.enabled, state);
  const scope = contactScopeSummary(toForm(d), listNames);
  const color = TONE_COLOR[v.tone];

  return (
    <div className="card flex items-center gap-4 px-4 py-3" data-testid="destination-card">
      <span
        className="h-2.5 w-2.5 shrink-0 rounded-full"
        style={{ background: color }}
        data-tone={v.tone}
        aria-hidden
        data-testid="destination-dot"
      />
      <div className="min-w-0 flex-1">
        <p className="flex items-center gap-2">
          <span className="truncate font-semibold" title={d.name}>
            {d.name}
          </span>
          {d.allowCommands && (
            <span title="Permite comandos" aria-label="Permite comandos" className="text-muted">
              <Terminal size={14} />
            </span>
          )}
        </p>
        <p className="truncate font-mono text-xs text-muted" title={d.url}>
          {d.url}
        </p>
        <p className="truncate text-xs text-muted" title={scope} data-testid="destination-scope">
          {scope}
        </p>
      </div>

      <div className="flex min-w-0 max-w-[260px] flex-col items-end text-right">
        <span className="text-sm font-medium" style={{ color }}>
          {v.label}
        </span>
        {v.pending > 0 && <span className="text-xs text-muted">{pendingLabel(v.pending)}</span>}
        {v.error && v.tone !== "ok" && (
          <span className="max-w-full truncate text-xs text-danger" title={v.error}>
            {v.error}
          </span>
        )}
      </div>

      <Switch label={d.enabled ? "Desactivar" : "Activar"} checked={d.enabled} onChange={onToggle} />
      <button className="icon-btn" aria-label="Editar" title="Editar" onClick={onEdit}>
        <Pencil size={16} />
      </button>
      <button className="icon-btn" aria-label="Eliminar" title="Eliminar" onClick={onDelete}>
        <Trash2 size={16} />
      </button>
    </div>
  );
}
