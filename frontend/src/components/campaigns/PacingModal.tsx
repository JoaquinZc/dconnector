import { useEffect, useMemo, useState } from "react";
import type { PacingSettings } from "@/api/types";
import Modal from "@/components/common/Modal";
import { useAccounts } from "@/store/accounts";
import { campaignAccounts } from "./campaignModel";
import PacingEditor from "./PacingEditor";

interface Props {
  /** Cuenta que se abre primero (vacío = la primera). */
  accountId?: string;
  onClose: () => void;
  onSaved?: (accountId: string, p: PacingSettings) => void;
}

/** «Ritmo de la cuenta»: elegir cuenta y editar su ritmo anti-bloqueo. */
export default function PacingModal({ accountId, onClose, onSaved }: Props) {
  const views = useAccounts((s) => s.views);
  const order = useAccounts((s) => s.order);
  const accounts = useMemo(() => campaignAccounts(order.map((id) => views[id]).filter(Boolean)), [order, views]);
  const [current, setCurrent] = useState(accountId ?? "");

  useEffect(() => {
    if (!accounts.some((v) => v.account.id === current)) setCurrent(accounts[0]?.account.id ?? "");
  }, [accounts, current]);

  return (
    <Modal open onClose={onClose} title="Ritmo de la cuenta" size="lg" closeOnBackdrop={false}>
      {accounts.length === 0 ? (
        <p className="py-6 text-center text-muted">Sin cuentas de WhatsApp o Telegram</p>
      ) : (
        <div className="flex flex-col gap-4">
          <div className="max-w-xs">
            <label className="label" htmlFor="pacing-account">
              Cuenta
            </label>
            <select id="pacing-account" className="input" value={current} onChange={(e) => setCurrent(e.target.value)}>
              {accounts.map((v) => (
                <option key={v.account.id} value={v.account.id}>
                  {v.account.label}
                </option>
              ))}
            </select>
          </div>
          {current && (
            <PacingEditor
              key={current}
              accountId={current}
              onSaved={(p) => onSaved?.(current, p)}
              onCancel={onClose}
            />
          )}
        </div>
      )}
    </Modal>
  );
}
