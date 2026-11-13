// «Añadir cuenta» (11 §6.8): 1) canal 2) etiqueta y color 3) vinculación 4) Conectada.
import { useCallback, useEffect, useMemo, useState } from "react";
import { AlertTriangle, Check, ExternalLink, Loader2 } from "lucide-react";
import { api } from "@/api";
import type { ChannelId } from "@/api/types";
import ChannelIcon from "@/components/common/ChannelIcon";
import Modal from "@/components/common/Modal";
import { CHANNELS, CHANNEL_ORDER, nextAccountColor } from "@/lib/channels";
import { clearDraft, loadDraft, saveDraft } from "@/lib/draft";
import { cx } from "@/lib/cx";
import { useAccounts } from "@/store/accounts";
import { useUI } from "@/store/ui";
import ColorPicker from "./ColorPicker";
import PairingFlow from "./PairingFlow";

interface Draft {
  channel: ChannelId | null;
  label: string;
  color: string;
  apiId: string;
  apiHash: string;
}

const DRAFT_SCREEN = "accounts";
const DRAFT_ID = "new";
const STEPS = ["Canal", "Datos", "Vincular", "Listo"];

function Stepper({ step }: { step: number }) {
  return (
    <ol className="mb-5 flex items-center gap-2 text-xs text-muted" aria-label="Pasos">
      {STEPS.map((s, i) => {
        const n = i + 1;
        const done = step > n;
        const current = step === n;
        return (
          <li key={s} className="flex flex-1 items-center gap-2" aria-current={current ? "step" : undefined}>
            <span
              className={cx(
                "flex h-5 w-5 shrink-0 items-center justify-center rounded-full text-[11px] font-semibold",
                done || current ? "bg-accent text-white" : "bg-panel2",
              )}
            >
              {done ? <Check size={12} strokeWidth={3} /> : n}
            </span>
            <span className={cx("truncate", current && "font-semibold text-ink")}>{s}</span>
            {n < STEPS.length && <span className="h-px flex-1 bg-line" />}
          </li>
        );
      })}
    </ol>
  );
}

interface Props {
  open: boolean;
  onClose: () => void;
}

export default function AddAccountWizard({ open, onClose }: Props) {
  const views = useAccounts((s) => s.views);
  const usedColors = useMemo(() => Object.values(views).map((v) => v.account.color), [views]);

  const [step, setStep] = useState(1);
  const [channel, setChannel] = useState<ChannelId | null>(null);
  const [label, setLabel] = useState("");
  const [color, setColor] = useState("");
  const [apiId, setApiId] = useState("");
  const [apiHash, setApiHash] = useState("");
  const [understood, setUnderstood] = useState(false);
  const [creating, setCreating] = useState(false);
  const [accountId, setAccountId] = useState<string | null>(null);

  // `ready`: el borrador ya se restauró (si no, el primer guardado lo pisaría con valores vacíos).
  const [ready, setReady] = useState(false);

  // Al abrir: parte del borrador (canal, etiqueta, color y claves de Telegram).
  useEffect(() => {
    if (!open) {
      setReady(false);
      return;
    }
    const d = loadDraft<Draft>(DRAFT_SCREEN, DRAFT_ID);
    setReady(true);
    setUnderstood(false);
    setAccountId(null);
    setCreating(false);
    setLabel(d?.label ?? "");
    setApiId(d?.apiId ?? "");
    setApiHash(d?.apiHash ?? "");
    setColor(d?.color || nextAccountColor(usedColors));
    setChannel(d?.channel ?? null);
    setStep(d?.channel ? 2 : 1);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  // Guarda el borrador mientras se escribe (antes de crear la cuenta).
  useEffect(() => {
    if (!open || !ready || accountId) return;
    saveDraft<Draft>(DRAFT_SCREEN, DRAFT_ID, { channel, label, color, apiId, apiHash });
  }, [open, ready, accountId, channel, label, color, apiId, apiHash]);

  const isMeta = channel === "messenger" || channel === "instagram";
  const isTelegram = channel === "telegram";

  const detailsValid =
    label.trim().length > 0 &&
    (!isTelegram || (/^\d+$/.test(apiId.trim()) && apiHash.trim().length > 0)) &&
    (!isMeta || understood);

  const create = async () => {
    if (!channel || !detailsValid || creating) return;
    setCreating(true);
    try {
      const config = isTelegram ? { api_id: apiId.trim(), api_hash: apiHash.trim() } : null;
      const acc = await api.accounts.Create(channel, label.trim(), color, config);
      clearDraft(DRAFT_SCREEN, DRAFT_ID);
      setAccountId(acc.id);
      await useAccounts.getState().load();
      setStep(3);
    } catch {
      /* el aviso ya salió */
    } finally {
      setCreating(false);
    }
  };

  const onConnected = useCallback(() => setStep(4), []);

  const connectedView = accountId ? views[accountId] : undefined;

  const pickChannel = (c: ChannelId) => {
    setChannel(c);
    setUnderstood(false);
    setStep(2);
  };

  const title = channel && step > 1 ? `Añadir cuenta · ${CHANNELS[channel].name}` : "Añadir cuenta";

  return (
    <Modal open={open} onClose={onClose} title={title} size="md" closeOnBackdrop={step !== 3}>
      <Stepper step={step} />

      {step === 1 && (
        <div className="grid grid-cols-2 gap-3" data-testid="channel-grid">
          {CHANNEL_ORDER.map((c) => (
            <button
              key={c}
              type="button"
              onClick={() => pickChannel(c)}
              className="flex flex-col items-center gap-2 rounded-xl border border-line bg-panel2 px-3 py-5 transition-colors hover:border-accent hover:bg-[var(--hover)]"
            >
              <ChannelIcon channel={c} size={44} />
              <span className="font-semibold">{CHANNELS[c].name}</span>
              <span className="h-4 text-xs text-muted">{c === "messenger" || c === "instagram" ? "Vía no oficial" : ""}</span>
            </button>
          ))}
        </div>
      )}

      {step === 2 && channel && (
        <div className="flex flex-col gap-4">
          <div>
            <label className="label" htmlFor="new-label">
              Etiqueta
            </label>
            <input
              id="new-label"
              className="input"
              value={label}
              maxLength={40}
              placeholder="Ventas, Personal…"
              onChange={(e) => setLabel(e.target.value)}
              data-autofocus
            />
          </div>
          <div>
            <span className="label">Color</span>
            <ColorPicker value={color} onChange={setColor} />
          </div>

          {isTelegram && (
            <div>
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="label" htmlFor="new-api-id">
                    api_id
                  </label>
                  <input
                    id="new-api-id"
                    className="input"
                    inputMode="numeric"
                    value={apiId}
                    onChange={(e) => setApiId(e.target.value)}
                    autoComplete="off"
                  />
                </div>
                <div>
                  <label className="label" htmlFor="new-api-hash">
                    api_hash
                  </label>
                  <input id="new-api-hash" className="input" value={apiHash} onChange={(e) => setApiHash(e.target.value)} autoComplete="off" />
                </div>
              </div>
              <a
                href="https://my.telegram.org"
                target="_blank"
                rel="noreferrer noopener"
                className="mt-2 inline-flex items-center gap-1 text-sm text-[#0a84d0] hover:underline"
              >
                my.telegram.org
                <ExternalLink size={13} />
              </a>
            </div>
          )}

          {isMeta && (
            <div className="rounded-lg border p-3" style={{ borderColor: "var(--warn)", background: "color-mix(in srgb, var(--warn) 10%, transparent)" }}>
              <p className="flex items-start gap-2 text-sm font-medium">
                <AlertTriangle size={18} className="mt-px shrink-0" style={{ color: "var(--warn)" }} />
                Vía no oficial: Meta puede bloquear la cuenta
              </p>
              <label className="mt-2.5 flex cursor-pointer items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={understood}
                  onChange={(e) => setUnderstood(e.target.checked)}
                  className="h-4 w-4 accent-[var(--accent)]"
                />
                Entiendo
              </label>
            </div>
          )}

          <div className="flex justify-between pt-1">
            <button className="btn" onClick={() => setStep(1)}>
              Atrás
            </button>
            <button className="btn btn-primary" disabled={!detailsValid || creating} onClick={() => void create()}>
              {creating && <Loader2 size={15} className="animate-spin" />}
              Continuar
            </button>
          </div>
        </div>
      )}

      {step === 3 && accountId && channel && <PairingFlow accountId={accountId} channel={channel} onConnected={onConnected} />}

      {step === 4 && (
        <div className="flex flex-col items-center gap-3 py-4 text-center" data-testid="connected-step">
          <div className="flex h-16 w-16 items-center justify-center rounded-full bg-accent text-white">
            <Check size={34} strokeWidth={3} />
          </div>
          <h3 className="text-xl font-semibold">Conectada ✓</h3>
          <p className="max-w-full truncate text-muted" title={connectedView?.account.selfName}>
            {connectedView?.account.selfName || connectedView?.account.label}
            {connectedView?.account.selfHandle ? ` · ${connectedView.account.selfHandle}` : ""}
          </p>
          <div className="mt-3 flex gap-2">
            <button className="btn" onClick={onClose}>
              Cerrar
            </button>
            <button
              className="btn btn-primary"
              onClick={() => {
                onClose();
                useUI.getState().setPage("chats");
              }}
            >
              Ir a chats
            </button>
          </div>
        </div>
      )}
    </Modal>
  );
}
