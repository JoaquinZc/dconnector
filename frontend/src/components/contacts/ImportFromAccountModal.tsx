import { useState } from "react";
import { api } from "@/api";
import type { ImportResult } from "@/api/types";
import ChannelIcon from "@/components/common/ChannelIcon";
import Modal from "@/components/common/Modal";
import { cx } from "@/lib/cx";
import { useAccounts } from "@/store/accounts";
import { importSummary } from "./contactModel";

interface Props {
  onClose: () => void;
  /** Se llama al terminar con lo que se importó, para recargar la tabla. */
  onDone: (r: ImportResult) => void;
}

/** Trae los contactos de una cuenta conectada. */
export default function ImportFromAccountModal({ onClose, onDone }: Props) {
  const views = useAccounts((s) => s.views);
  const order = useAccounts((s) => s.order);
  const accounts = order.map((id) => views[id]).filter((v) => v && v.account.enabled);
  const [accountId, setAccountId] = useState("");
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<ImportResult | null>(null);

  const run = async () => {
    if (!accountId || busy) return;
    setBusy(true);
    try {
      const r = await api.contacts.ImportFromAccount(accountId);
      setResult(r);
      onDone(r);
    } catch {
      /* el aviso ya salió */
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal
      open
      size="sm"
      title="Importar de una cuenta"
      onClose={onClose}
      footer={
        result ? (
          <button className="btn btn-primary" onClick={onClose}>
            Listo
          </button>
        ) : (
          <>
            <button className="btn" onClick={onClose}>
              Cancelar
            </button>
            <button className="btn btn-primary" disabled={!accountId || busy} onClick={() => void run()}>
              {busy ? "Importando…" : "Importar"}
            </button>
          </>
        )
      }
    >
      {result ? (
        <p className="text-base" data-testid="import-result">
          {importSummary(result)}
        </p>
      ) : accounts.length === 0 ? (
        <p className="text-muted">Sin cuentas</p>
      ) : (
        <div role="radiogroup" aria-label="Cuenta" className="flex flex-col gap-2">
          {accounts.map((v) => {
            const connected = String(v.status?.state) === "connected";
            const on = accountId === v.account.id;
            return (
              <button
                key={v.account.id}
                type="button"
                role="radio"
                aria-checked={on}
                disabled={!connected || busy}
                title={connected ? v.account.label : "La cuenta no está conectada"}
                onClick={() => setAccountId(v.account.id)}
                className={cx(
                  "flex items-center gap-3 rounded-lg border px-3 py-2 text-left",
                  on ? "border-[var(--accent)] bg-panel2" : "border-line hover:bg-[var(--hover)]",
                  !connected && "cursor-not-allowed opacity-50",
                )}
              >
                <ChannelIcon channel={String(v.account.channel)} size={22} />
                <span className="min-w-0 flex-1 truncate font-medium">{v.account.label}</span>
                <span className="h-2.5 w-2.5 shrink-0 rounded-full" style={{ background: v.account.color }} />
              </button>
            );
          })}
        </div>
      )}
    </Modal>
  );
}
