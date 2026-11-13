import { useEffect, useState } from "react";
import { api } from "@/api";
import type { AccountView } from "@/api/types";
import Modal from "@/components/common/Modal";
import { channelName } from "@/lib/channels";
import { useAccounts } from "@/store/accounts";
import ColorPicker from "./ColorPicker";

interface Props {
  view: AccountView | null;
  onClose: () => void;
}

export default function EditAccountModal({ view, onClose }: Props) {
  const acc = view?.account;
  const [label, setLabel] = useState("");
  const [color, setColor] = useState("");
  const [enabled, setEnabled] = useState(true);
  const [apiId, setApiId] = useState("");
  const [apiHash, setApiHash] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!acc) return;
    setLabel(acc.label);
    setColor(acc.color);
    setEnabled(acc.enabled);
    setApiId(acc.config?.api_id ?? "");
    setApiHash(acc.config?.api_hash ?? "");
  }, [acc?.id]); // eslint-disable-line react-hooks/exhaustive-deps

  if (!acc) return null;
  const telegram = String(acc.channel) === "telegram";
  const valid = label.trim().length > 0 && (!telegram || (/^\d+$/.test(apiId.trim()) && apiHash.trim().length > 0));

  const save = async () => {
    setBusy(true);
    try {
      await api.accounts.Update(acc.id, label.trim(), color, enabled);
      if (telegram && (apiId.trim() !== (acc.config?.api_id ?? "") || apiHash.trim() !== (acc.config?.api_hash ?? ""))) {
        await api.accounts.SetConfig(acc.id, { ...(acc.config ?? {}), api_id: apiId.trim(), api_hash: apiHash.trim() });
      }
      await useAccounts.getState().load();
      onClose();
    } catch {
      /* el aviso ya salió */
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal
      open
      onClose={onClose}
      title={`Editar · ${channelName(acc.channel)}`}
      footer={
        <>
          <button className="btn" onClick={onClose}>
            Cancelar
          </button>
          <button className="btn btn-primary" disabled={!valid || busy} onClick={save}>
            Guardar
          </button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <div>
          <label className="label" htmlFor="edit-label">
            Etiqueta
          </label>
          <input id="edit-label" className="input" value={label} maxLength={40} onChange={(e) => setLabel(e.target.value)} />
        </div>
        <div>
          <span className="label">Color</span>
          <ColorPicker value={color} onChange={setColor} />
        </div>
        {telegram && (
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="label" htmlFor="edit-api-id">
                api_id
              </label>
              <input id="edit-api-id" className="input" inputMode="numeric" value={apiId} onChange={(e) => setApiId(e.target.value)} />
            </div>
            <div>
              <label className="label" htmlFor="edit-api-hash">
                api_hash
              </label>
              <input id="edit-api-hash" className="input" value={apiHash} onChange={(e) => setApiHash(e.target.value)} />
            </div>
          </div>
        )}
        <label className="flex cursor-pointer items-center gap-2">
          <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} className="h-4 w-4 accent-[var(--accent)]" />
          <span>Cuenta activa</span>
        </label>
      </div>
    </Modal>
  );
}
