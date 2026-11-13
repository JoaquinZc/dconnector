import { useEffect, useMemo, useRef, useState } from "react";
import { Paperclip, X } from "lucide-react";
import { api } from "@/api";
import type { CapSet, Scheduled } from "@/api/types";
import Modal from "@/components/common/Modal";
import { clearDraft, loadDraft, saveDraft } from "@/lib/draft";
import { formatFileSize } from "@/lib/format";
import { useAccounts } from "@/store/accounts";
import { useUI } from "@/store/ui";
import { normalizeRecurrenceForm, parseLocalInput } from "@/lib/recurrence";
import ChatPicker from "./ChatPicker";
import RecurrenceEditor from "./RecurrenceEditor";
import {
  buildScheduled,
  emptyScheduledForm,
  formFromScheduled,
  validateScheduledForm,
  type ScheduledForm as FormState,
} from "./scheduledModel";

const DRAFT_SCREEN = "scheduled";

interface Props {
  /** Programado que se edita; sin él, uno nuevo. */
  item?: Scheduled | null;
  onClose: () => void;
  onSaved: () => void;
}

/** «Nuevo» / «Editar»: cuenta → chat → mensaje (texto + un archivo) → fecha y hora. */
export default function ScheduledForm({ item, onClose, onSaved }: Props) {
  const toast = useUI((s) => s.toast);
  const views = useAccounts((s) => s.views);
  const order = useAccounts((s) => s.order);
  const accounts = useMemo(
    () => order.map((id) => views[id]).filter((v) => v && v.account.enabled),
    [order, views],
  );

  const draftId = item?.id || "new";
  const [form, setForm] = useState<FormState>(() => {
    const d = loadDraft<FormState>(DRAFT_SCREEN, draftId);
    if (d) return { ...emptyScheduledForm(), ...d, id: item?.id ?? "", recurrence: normalizeRecurrenceForm(d.recurrence) };
    return item ? formFromScheduled(item) : emptyScheduledForm();
  });
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  // El borrador solo se guarda cuando la persona toca algo (no por abrir y cerrar).
  const touched = useRef(!!loadDraft(DRAFT_SCREEN, draftId));

  useEffect(() => {
    if (touched.current) saveDraft(DRAFT_SCREEN, draftId, form);
  }, [form, draftId]);

  // Una sola cuenta: ya está elegida.
  useEffect(() => {
    if (!form.accountId && accounts.length === 1) setForm((f) => ({ ...f, accountId: accounts[0].account.id }));
  }, [accounts, form.accountId]);

  const patch = (p: Partial<FormState>) => {
    touched.current = true;
    setForm((f) => ({ ...f, ...p }));
    setError(null);
  };

  const caps = (views[form.accountId]?.capabilities as CapSet | undefined) ?? null;
  const hasCaps = !!caps && Object.keys(caps).length > 0;

  const attach = async () => {
    try {
      const files = await api.files.PickFiles("any");
      const f = files[0];
      if (f) patch({ filePath: f.path, fileName: f.name, fileMime: f.mime, fileSize: f.size });
    } catch {
      /* el aviso ya salió */
    }
  };

  const save = async () => {
    const e = validateScheduledForm(form, hasCaps ? caps : null);
    if (e) return setError(e);
    setBusy(true);
    try {
      const built = buildScheduled(form, item);
      // La ocurrencia pendiente de una serie se cambia con UpdateSeries: lo cambiado vale también para las siguientes.
      if (item?.recurrence && item.seriesId && item.state === "pending") await api.scheduled.UpdateSeries(item.seriesId, built);
      else await api.scheduled.Save(built);
      clearDraft(DRAFT_SCREEN, draftId);
      toast("success", item ? "Cambios guardados" : "Mensaje programado");
      onSaved();
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
      title={item ? "Editar programado" : "Nuevo programado"}
      size="md"
      closeOnBackdrop={false}
      footer={
        <>
          {error && (
            <span className="mr-auto min-w-0 truncate text-sm text-danger" role="alert" title={error}>
              {error}
            </span>
          )}
          <button className="btn" onClick={onClose} disabled={busy}>
            Cancelar
          </button>
          <button className="btn btn-primary" onClick={() => void save()} disabled={busy}>
            {item ? "Guardar" : "Programar"}
          </button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <div>
          <label className="label" htmlFor="scheduled-account">
            Cuenta
          </label>
          <select
            id="scheduled-account"
            className="input"
            value={form.accountId}
            onChange={(e) => patch({ accountId: e.target.value, chatNativeId: "", chatTitle: "" })}
          >
            <option value="">Elige una cuenta</option>
            {accounts.map((v) => (
              <option key={v.account.id} value={v.account.id}>
                {v.account.label}
              </option>
            ))}
          </select>
        </div>

        <div>
          <span className="label">Chat</span>
          <ChatPicker
            accountId={form.accountId}
            value={form.chatNativeId}
            title={form.chatTitle}
            onChange={(c) => patch({ chatNativeId: c.nativeId, chatTitle: c.title })}
          />
        </div>

        <div>
          <label className="label" htmlFor="scheduled-text">
            Mensaje
          </label>
          <textarea
            id="scheduled-text"
            className="input min-h-[96px]"
            value={form.text}
            onChange={(e) => patch({ text: e.target.value })}
            maxLength={4096}
          />
          <div className="mt-2 flex items-center gap-2">
            {form.filePath ? (
              <span className="inline-flex max-w-full items-center gap-1.5 rounded-lg border border-line bg-panel2 px-2.5 py-1.5 text-sm">
                <Paperclip size={14} className="shrink-0 text-muted" />
                <span className="max-w-[240px] truncate" title={form.filePath}>
                  {form.fileName}
                </span>
                {form.fileSize > 0 && <span className="text-xs text-muted">{formatFileSize(form.fileSize)}</span>}
                <button
                  type="button"
                  className="text-muted hover:text-ink"
                  aria-label="Quitar archivo"
                  onClick={() => patch({ filePath: "", fileName: "", fileMime: "", fileSize: 0 })}
                >
                  <X size={14} />
                </button>
              </span>
            ) : (
              <button className="btn btn-sm" onClick={() => void attach()}>
                <Paperclip size={14} />
                Adjuntar
              </button>
            )}
          </div>
        </div>

        <div>
          <label className="label" htmlFor="scheduled-when">
            Fecha y hora
          </label>
          <input
            id="scheduled-when"
            type="datetime-local"
            className="input"
            value={form.when}
            onChange={(e) => patch({ when: e.target.value })}
          />
        </div>

        {item?.state !== "failed" && (
          <RecurrenceEditor value={form.recurrence} onChange={(recurrence) => patch({ recurrence })} firstMs={parseLocalInput(form.when)} />
        )}
      </div>
    </Modal>
  );
}
