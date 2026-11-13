// Diálogo «Programar» del chat: mensaje, fecha y hora con atajos, y repetición. También edita un
// programado del chat (Update, o UpdateSeries si es una serie).
import { useMemo, useState } from "react";
import { Paperclip, X } from "lucide-react";
import { api, errorMessage } from "@/api";
import type { Outgoing, Scheduled } from "@/api/types";
import Modal from "@/components/common/Modal";
import RecurrenceEditor from "@/components/scheduled/RecurrenceEditor";
import { formatFileSize } from "@/lib/format";
import {
  defaultFirstMs,
  emptyRecurrenceForm,
  parseLocalInput,
  quickShortcuts,
  recurrenceFormFromRule,
  toLocalInput,
} from "@/lib/recurrence";
import { useUI } from "@/store/ui";
import { type Attachment } from "./composerReducer";
import {
  buildChatScheduled,
  outgoingsFor,
  savedToast,
  validateScheduleDraft,
  type ScheduleTarget,
} from "./scheduleModel";

interface Props {
  target: ScheduleTarget;
  /** Texto del compositor al abrir. */
  initialText?: string;
  /** Adjuntos ya preparados en el compositor. */
  files?: Attachment[];
  /** Programado que se edita; sin él, uno nuevo. */
  item?: Scheduled | null;
  /** Tope de texto del canal (0 o ausente = sin dato). */
  maxTextLen?: number;
  onClose: () => void;
  /** Todo quedó programado (el diálogo se cierra a continuación). */
  onSaved: (created: Scheduled[]) => void;
  /** Se guardó al menos uno (también si otro falló después). */
  onChanged?: () => void;
}

export default function ScheduleDialog({ target, initialText = "", files: initialFiles, item, maxTextLen, onClose, onSaved, onChanged }: Props) {
  const toast = useUI((s) => s.toast);
  const editing = !!item;
  const failedItem = item?.state === "failed";

  const [text, setText] = useState(item?.message?.text ?? initialText);
  const [files, setFiles] = useState<Attachment[]>(initialFiles ?? []);
  const [when, setWhen] = useState(() => toLocalInput(item ? item.sendAt : defaultFirstMs()));
  const [recurrence, setRecurrence] = useState(() =>
    item && !failedItem ? recurrenceFormFromRule(item.recurrence, item.sendAt) : emptyRecurrenceForm(),
  );
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const shortcuts = useMemo(() => quickShortcuts(), []);

  const keptFile = item?.message?.filePath ? { name: item.message.fileName || item.message.filePath } : null;
  const firstMs = parseLocalInput(when);

  const clearError = () => setError(null);

  const save = async () => {
    const e = validateScheduleDraft({ text, fileCount: files.length, keepsFile: !!keptFile, when, recurrence, maxTextLen });
    if (e) return setError(e);
    setBusy(true);
    setError(null);
    const created: Scheduled[] = [];
    try {
      if (item) {
        const message = { ...(item.message ?? { kind: "text" }), text: text.trim() } as Outgoing;
        const built = buildChatScheduled(target, message, when, recurrence, item);
        const isSeries = !!item.recurrence && !!item.seriesId && item.state === "pending";
        created.push(isSeries ? await api.scheduled.UpdateSeriesQuiet(item.seriesId, built) : await api.scheduled.UpdateQuiet(built));
      } else {
        const outs = outgoingsFor(text, files);
        const hasText = text.trim() !== "";
        for (let i = 0; i < outs.length; i++) {
          created.push(await api.scheduled.SaveQuiet(buildChatScheduled(target, outs[i], when, recurrence)));
          // Lo ya programado sale del diálogo: si algo falla después, reintentar no lo duplica.
          if (hasText && i === 0) setText("");
          else {
            const done = files[i - (hasText ? 1 : 0)];
            setFiles((prev) => prev.filter((x) => x.id !== done.id));
          }
        }
      }
      toast("success", savedToast(created.length, editing));
      onChanged?.();
      onSaved(created);
      onClose();
    } catch (err) {
      setError(errorMessage(err));
      if (created.length) onChanged?.();
    } finally {
      setBusy(false);
    }
  };

  const totalMessages = editing ? 1 : outgoingsFor(text, files).length;

  return (
    <Modal
      open
      onClose={onClose}
      title={editing ? "Editar programado" : "Programar mensaje"}
      size="md"
      closeOnBackdrop={false}
      footer={
        <>
          <button className="btn" onClick={onClose} disabled={busy}>
            Cancelar
          </button>
          <button className="btn btn-primary" onClick={() => void save()} disabled={busy}>
            {editing ? "Guardar" : "Programar"}
          </button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <p className="text-sm text-muted">
          Para <span className="font-medium text-ink">{target.chatTitle || target.chatNativeId}</span>
        </p>

        <div>
          <label className="label" htmlFor="schedule-text">
            Mensaje
          </label>
          <textarea
            id="schedule-text"
            data-autofocus
            className="input min-h-[96px]"
            value={text}
            maxLength={4096}
            onChange={(e) => {
              setText(e.target.value);
              clearError();
            }}
          />
          {(files.length > 0 || keptFile) && (
            <ul className="mt-2 flex flex-wrap gap-2" aria-label="Adjuntos">
              {keptFile && (
                <li className="inline-flex max-w-full items-center gap-1.5 rounded-lg border border-line bg-panel2 px-2.5 py-1.5 text-sm">
                  <Paperclip size={14} className="shrink-0 text-muted" />
                  <span className="max-w-[260px] truncate" title={keptFile.name}>
                    {keptFile.name}
                  </span>
                </li>
              )}
              {files.map((f) => (
                <li key={f.id} className="inline-flex max-w-full items-center gap-1.5 rounded-lg border border-line bg-panel2 px-2.5 py-1.5 text-sm">
                  <Paperclip size={14} className="shrink-0 text-muted" />
                  <span className="max-w-[220px] truncate" title={f.path}>
                    {f.name}
                  </span>
                  {f.size > 0 && <span className="text-xs text-muted">{formatFileSize(f.size)}</span>}
                  <button
                    type="button"
                    className="text-muted hover:text-ink"
                    aria-label={`Quitar ${f.name}`}
                    onClick={() => {
                      setFiles((prev) => prev.filter((x) => x.id !== f.id));
                      clearError();
                    }}
                  >
                    <X size={14} />
                  </button>
                </li>
              ))}
            </ul>
          )}
          {!editing && totalMessages > 1 && (
            <p className="mt-1.5 text-xs text-muted" data-testid="schedule-count">
              Se programarán {totalMessages} mensajes a la misma hora: el texto y cada adjunto por separado.
            </p>
          )}
        </div>

        <div>
          <label className="label" htmlFor="schedule-when">
            Fecha y hora
          </label>
          <input
            id="schedule-when"
            type="datetime-local"
            className="input"
            value={when}
            onChange={(e) => {
              setWhen(e.target.value);
              clearError();
            }}
          />
          <div className="mt-2 flex flex-wrap gap-1.5" role="group" aria-label="Atajos de fecha">
            {shortcuts.map((s) => (
              <button
                key={s.id}
                type="button"
                className="btn btn-sm"
                aria-pressed={firstMs === s.ms}
                onClick={() => {
                  setWhen(toLocalInput(s.ms));
                  clearError();
                }}
              >
                {s.label}
              </button>
            ))}
          </div>
        </div>

        {!failedItem && (
          <RecurrenceEditor
            value={recurrence}
            onChange={(r) => {
              setRecurrence(r);
              clearError();
            }}
            firstMs={firstMs}
          />
        )}
        {editing && item?.recurrence && item.seriesId && item.state === "pending" && (
          <p className="text-xs text-muted">Los cambios valen para esta repetición y las siguientes.</p>
        )}

        {error && (
          <p className="rounded-lg border border-[color-mix(in_srgb,var(--danger)_45%,var(--border))] px-3 py-2 text-sm text-danger" role="alert">
            {error}
          </p>
        )}
      </div>
    </Modal>
  );
}
