import { useEffect } from "react";
import { Check } from "lucide-react";
import { api } from "@/api";
import type { AccountView } from "@/api/types";
import Modal from "@/components/common/Modal";
import { useAccounts } from "@/store/accounts";
import PairingFlow from "./PairingFlow";

interface Props {
  view: AccountView | null;
  onClose: () => void;
}

/** «Volver a vincular»: el mismo paso de vinculación del asistente, sobre una cuenta que ya existe. */
export default function RelinkModal({ view, onClose }: Props) {
  const id = view?.account.id;
  const live = useAccounts((s) => (id ? s.views[id] : undefined)) ?? view;
  const connected = String(live?.status?.state) === "connected";

  // Si el adaptador está parado (no espera vinculación), se arranca para que pida lo que falte.
  useEffect(() => {
    if (!view) return;
    const state = String(view.status?.state);
    if (state === "disconnected" || state === "error") void api.accounts.Connect(view.account.id).catch(() => undefined);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view?.account.id]);

  if (!view) return null;
  return (
    <Modal open onClose={onClose} title={`Volver a vincular · ${view.account.label}`} size="md" closeOnBackdrop={false}>
      {connected ? (
        <div className="flex flex-col items-center gap-3 py-4 text-center">
          <div className="flex h-14 w-14 items-center justify-center rounded-full bg-accent text-white">
            <Check size={30} strokeWidth={3} />
          </div>
          <h3 className="text-lg font-semibold">Conectada ✓</h3>
          <button className="btn btn-primary mt-2" onClick={onClose}>
            Listo
          </button>
        </div>
      ) : (
        <PairingFlow accountId={view.account.id} channel={String(view.account.channel)} />
      )}
    </Modal>
  );
}
