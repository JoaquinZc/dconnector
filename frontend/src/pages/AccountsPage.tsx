import { useState } from "react";
import { Plus, Smartphone } from "lucide-react";
import { api } from "@/api";
import type { AccountView } from "@/api/types";
import AccountCard from "@/components/accounts/AccountCard";
import AddAccountWizard from "@/components/accounts/AddAccountWizard";
import EditAccountModal from "@/components/accounts/EditAccountModal";
import RelinkModal from "@/components/accounts/RelinkModal";
import EmptyState from "@/components/common/EmptyState";
import { AccountsGridSkeleton } from "@/pages/skeletons/zones";
import { moveBefore, useAccounts } from "@/store/accounts";
import { useUI } from "@/store/ui";

export default function AccountsPage() {
  const views = useAccounts((s) => s.views);
  const order = useAccounts((s) => s.order);
  const reorder = useAccounts((s) => s.reorder);
  const load = useAccounts((s) => s.load);
  const loaded = useAccounts((s) => s.loaded);
  const confirm = useUI((s) => s.confirm);
  const toast = useUI((s) => s.toast);

  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<string | null>(null);
  const [relinking, setRelinking] = useState<string | null>(null);

  // Orden mientras se arrastra (se confirma al soltar).
  const [dragId, setDragId] = useState<string | null>(null);
  const [preview, setPreview] = useState<string[] | null>(null);
  const shown = preview ?? order;

  const endDrag = () => {
    if (preview && preview.join() !== order.join()) void reorder(preview);
    setDragId(null);
    setPreview(null);
  };

  const guard = async (fn: () => Promise<unknown>) => {
    try {
      await fn();
    } catch {
      /* el aviso ya salió */
    }
  };

  const actions = {
    onConnect: (v: AccountView) => guard(() => api.accounts.Connect(v.account.id)),
    onDisconnect: (v: AccountView) => guard(() => api.accounts.Disconnect(v.account.id)),
    onEdit: (v: AccountView) => setEditing(v.account.id),
    onRelink: (v: AccountView) => setRelinking(v.account.id),
    onLogout: async (v: AccountView) => {
      const ok = await confirm({
        title: "Cerrar sesión",
        message: `Se cerrará la sesión de «${v.account.label}» y habrá que volver a vincularla.`,
        confirmLabel: "Cerrar sesión",
        danger: true,
      });
      if (ok) await guard(() => api.accounts.Logout(v.account.id));
    },
    onDelete: async (v: AccountView) => {
      const ok = await confirm({
        title: "Eliminar cuenta",
        message: "Se borran sus chats y mensajes guardados en este equipo.",
        confirmLabel: "Eliminar",
        danger: true,
        requireText: v.account.label,
      });
      if (!ok) return;
      try {
        await api.accounts.Delete(v.account.id);
        toast("success", `Cuenta «${v.account.label}» eliminada`);
        await load();
      } catch {
        /* el aviso ya salió */
      }
    },
  };

  const move = (id: string, delta: -1 | 1) => {
    const i = order.indexOf(id);
    const j = i + delta;
    if (i < 0 || j < 0 || j >= order.length) return;
    void reorder(moveBefore(order, id, order[j]));
  };

  return (
    <div className="flex h-full flex-col">
      <header className="flex items-center gap-3 border-b border-line bg-panel px-6 py-3">
        <h1 className="flex-1 text-lg font-semibold">Cuentas</h1>
        <button className="btn btn-primary" onClick={() => setAdding(true)}>
          <Plus size={16} />
          Añadir cuenta
        </button>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto p-6">
        {shown.length === 0 && !loaded ? (
          <AccountsGridSkeleton />
        ) : shown.length === 0 ? (
          <EmptyState
            icon={Smartphone}
            title="Sin cuentas"
            action={
              <button className="btn btn-primary" onClick={() => setAdding(true)}>
                <Plus size={16} />
                Añadir cuenta
              </button>
            }
          />
        ) : (
          <div className="grid gap-4" style={{ gridTemplateColumns: "repeat(auto-fill, minmax(min(340px, 100%), 1fr))" }}>
            {shown.map((id) => {
              const view = views[id];
              if (!view) return null;
              return (
                <AccountCard
                  key={id}
                  view={view}
                  dragging={dragId === id}
                  onDragStart={(d) => {
                    setDragId(d);
                    setPreview(order);
                  }}
                  onDragEnter={(over) => {
                    if (dragId && over !== dragId) setPreview((p) => moveBefore(p ?? order, dragId, over));
                  }}
                  onDragEnd={endDrag}
                  onMove={move}
                  {...actions}
                />
              );
            })}
          </div>
        )}
      </div>

      <AddAccountWizard open={adding} onClose={() => setAdding(false)} />
      <EditAccountModal view={editing ? (views[editing] ?? null) : null} onClose={() => setEditing(null)} />
      <RelinkModal view={relinking ? (views[relinking] ?? null) : null} onClose={() => setRelinking(null)} />
    </div>
  );
}
