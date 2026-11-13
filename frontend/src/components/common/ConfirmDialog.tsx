import { useEffect, useState } from "react";
import Modal from "./Modal";

interface Props {
  open: boolean;
  title: string;
  message?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  /** Botón rojo (acciones que borran). */
  danger?: boolean;
  /** Si se indica, hay que escribir exactamente este texto para confirmar. */
  requireText?: string;
  onConfirm: () => void;
  onCancel: () => void;
}

export default function ConfirmDialog({
  open,
  title,
  message,
  confirmLabel = "Aceptar",
  cancelLabel = "Cancelar",
  danger,
  requireText,
  onConfirm,
  onCancel,
}: Props) {
  const [typed, setTyped] = useState("");
  useEffect(() => {
    if (open) setTyped("");
  }, [open]);

  const ok = !requireText || typed.trim() === requireText.trim();

  return (
    <Modal
      open={open}
      onClose={onCancel}
      title={title}
      size="sm"
      footer={
        <>
          <button className="btn" onClick={onCancel}>
            {cancelLabel}
          </button>
          <button
            className={`btn ${danger ? "btn-danger-solid" : "btn-primary"}`}
            disabled={!ok}
            onClick={onConfirm}
            data-testid="confirm-ok"
          >
            {confirmLabel}
          </button>
        </>
      }
    >
      {message && <p className="mb-3 whitespace-pre-line break-words text-sm">{message}</p>}
      {requireText && (
        <div>
          <label className="label" htmlFor="confirm-text">
            Escribe <strong className="text-ink">{requireText}</strong> para confirmar
          </label>
          <input
            id="confirm-text"
            className="input"
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && ok) onConfirm();
            }}
            autoComplete="off"
            data-autofocus
          />
        </div>
      )}
    </Modal>
  );
}
