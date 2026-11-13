import { useCallback, useEffect, useMemo, useState } from "react";
import { useReloadOnReconnect } from "@/lib/useReconnect";
import { Plus, Unplug } from "lucide-react";
import { api } from "@/api";
import type { ContactList, Destination } from "@/api/types";
import EmptyState from "@/components/common/EmptyState";
import DestinationCard from "@/components/destinations/DestinationCard";
import DestinationForm from "@/components/destinations/DestinationForm";
import { indexListNames } from "@/components/destinations/destinationModel";
import { useDestinationStates } from "@/components/destinations/useDestinationStates";
import { useUI } from "@/store/ui";

export default function DestinationsPage() {
  const confirm = useUI((s) => s.confirm);
  const toast = useUI((s) => s.toast);
  const states = useDestinationStates();

  const [items, setItems] = useState<Destination[]>([]);
  const [loaded, setLoaded] = useState(false);
  // Listas de contactos, para nombrar el alcance de cada destino (`null` mientras no cargan).
  const [lists, setLists] = useState<ContactList[] | null>(null);
  // `undefined` = cerrado, `null` = nuevo, otro = editar.
  const [editing, setEditing] = useState<Destination | null | undefined>(undefined);

  const load = useCallback(async () => {
    try {
      setItems(await api.destinations.List());
    } catch {
      /* el aviso ya salió */
    } finally {
      setLoaded(true);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const loadLists = useCallback(() => {
    let alive = true;
    api.contacts
      .Lists()
      .then((all) => alive && setLists(all))
      .catch(() => {
        /* sin nombres, las tarjetas muestran un resumen genérico */
      });
    return () => {
      alive = false;
    };
  }, []);
  useEffect(() => loadLists(), [loadLists]);
  useReloadOnReconnect(() => {
    void load();
    loadLists();
  });
  const listNames = useMemo(() => (lists ? indexListNames(lists) : null), [lists]);

  const toggle = async (d: Destination, enabled: boolean) => {
    setItems((list) => list.map((x) => (x.id === d.id ? { ...x, enabled } : x)));
    try {
      await api.destinations.Save({ ...d, enabled });
    } catch {
      await load();
    }
  };

  const remove = async (d: Destination) => {
    const ok = await confirm({
      title: "Eliminar destino",
      message: `Se borra «${d.name}» y los eventos que tenía pendientes.`,
      confirmLabel: "Eliminar",
      danger: true,
    });
    if (!ok) return;
    try {
      await api.destinations.Delete(d.id);
      toast("success", "Destino eliminado");
      await load();
    } catch {
      /* el aviso ya salió */
    }
  };

  return (
    <div className="flex h-full flex-col">
      <header className="flex items-center gap-3 border-b border-line bg-panel px-6 py-3">
        <h1 className="flex-1 text-lg font-semibold">Destinos</h1>
        <button className="btn btn-primary" onClick={() => setEditing(null)}>
          <Plus size={16} />
          Nuevo destino
        </button>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto p-6">
        {loaded && items.length === 0 ? (
          <EmptyState
            icon={Unplug}
            title="Sin destinos"
            action={
              <button className="btn btn-primary" onClick={() => setEditing(null)}>
                <Plus size={16} />
                Nuevo destino
              </button>
            }
          />
        ) : (
          <div className="mx-auto flex max-w-3xl flex-col gap-3">
            {items.map((d) => (
              <DestinationCard
                key={d.id}
                destination={d}
                state={states[d.id]}
                listNames={listNames}
                onToggle={(v) => void toggle(d, v)}
                onEdit={() => setEditing(d)}
                onDelete={() => void remove(d)}
              />
            ))}
          </div>
        )}
      </div>

      {editing !== undefined && (
        <DestinationForm
          key={editing?.id ?? "new"}
          destination={editing}
          onClose={() => setEditing(undefined)}
          onSaved={() => void load()}
        />
      )}
    </div>
  );
}
