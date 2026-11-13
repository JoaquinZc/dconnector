import { useEffect, useRef, useState } from "react";
import { CalendarClock, Film, Image as ImageIcon, Loader2, Send, X } from "lucide-react";
import { api } from "@/api";
import type { CapSet } from "@/api/types";
import Modal from "@/components/common/Modal";
import { clearDraft, loadDraft, saveDraft } from "@/lib/draft";
import { channelInfo } from "@/lib/channels";
import { cx } from "@/lib/cx";
import { formatFileSize } from "@/lib/format";
import { useUI } from "@/store/ui";
import AudiencePicker from "./AudiencePicker";
import {
  DEFAULT_MAX_TEXT,
  buildOutgoing,
  defaultScheduleMs,
  draftOf,
  formFromDraft,
  isFormBlank,
  mediaKindOf,
  parseLocalInput,
  scheduleError,
  toLocalInput,
  validateForm,
  type StatusDraft,
  type StatusForm,
} from "./statusForm";
import { STATUS_COLORS, STATUS_FONTS, bgOf, fontCss } from "./statusModel";

interface Props {
  accountId: string;
  caps?: CapSet | null;
  /** Cuentas donde se puede publicar (para elegir el canal); si hay una sola, solo se indica. */
  targets?: { id: string; label: string; channel: string }[];
  onTarget?: (id: string) => void;
  onClose: () => void;
  /** Tras publicar o programar. */
  onDone?: (kind: "posted" | "scheduled") => void;
}

const DRAFT_SCREEN = "statuses";

export default function PostStatusModal({ accountId, caps, targets, onTarget, onClose, onDone }: Props) {
  const toast = useUI((s) => s.toast);
  const [form, setForm] = useState<StatusForm>(() => formFromDraft(loadDraft<StatusDraft>(DRAFT_SCREEN, accountId)));
  const [scheduling, setScheduling] = useState(false);
  const [when, setWhen] = useState(() => toLocalInput(defaultScheduleMs()));
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const first = useRef(true);

  const audienceChoice = !!caps?.statusAudience;
  const opts = { audienceChoice, caps };
  const maxText = caps?.maxTextLen && caps.maxTextLen > 0 ? caps.maxTextLen : DEFAULT_MAX_TEXT;

  const patch = (p: Partial<StatusForm>) => {
    setForm((f) => ({ ...f, ...p }));
    setError(null);
  };

  // Borrador (sin el archivo).
  useEffect(() => {
    if (first.current) {
      first.current = false;
      return;
    }
    if (isFormBlank(form)) clearDraft(DRAFT_SCREEN, accountId);
    else saveDraft(DRAFT_SCREEN, accountId, draftOf(form));
  }, [form, accountId]);

  const pick = async () => {
    try {
      const files = await api.files.PickFiles("media");
      if (files[0]) patch({ file: files[0] });
    } catch {
      /* el aviso ya salió */
    }
  };

  const check = (): boolean => {
    const e = validateForm(form, opts);
    setError(e);
    return !e;
  };

  const publish = async () => {
    if (!check()) return;
    setBusy(true);
    try {
      await api.statuses.Post(accountId, buildOutgoing(form, opts));
      clearDraft(DRAFT_SCREEN, accountId);
      toast("success", "Estado publicado");
      onDone?.("posted");
      onClose();
    } catch {
      /* el aviso ya salió */
    } finally {
      setBusy(false);
    }
  };

  const schedule = async () => {
    if (!check()) return;
    const se = scheduleError(when);
    if (se) return setError(se);
    const sendAt = parseLocalInput(when);
    if (sendAt === null) return;
    setBusy(true);
    try {
      await api.scheduled.Save({
        id: "",
        accountId,
        kind: "status",
        chatNativeId: "",
        chatTitle: "",
        status: buildOutgoing(form, opts),
        sendAt,
        state: "pending",
        sentAt: 0,
        error: "",
        createdAt: 0,
        seriesId: "",
      });
      clearDraft(DRAFT_SCREEN, accountId);
      toast("success", "Estado programado");
      onDone?.("scheduled");
      onClose();
    } catch {
      /* el aviso ya salió */
    } finally {
      setBusy(false);
    }
  };

  const isVideo = form.file ? mediaKindOf(form.file.mime, form.file.name) === "video" : false;

  return (
    <Modal
      open
      onClose={onClose}
      size="lg"
      title="Nuevo estado"
      footer={
        <>
          {error && (
            <span role="alert" className="mr-auto min-w-0 truncate text-sm text-danger" title={error}>
              {error}
            </span>
          )}
          <button className="btn" onClick={onClose} disabled={busy}>
            Cancelar
          </button>
          <button className="btn" aria-pressed={scheduling} onClick={() => setScheduling((v) => !v)} disabled={busy}>
            <CalendarClock size={15} />
            Programar
          </button>
          <button className="btn btn-primary" onClick={() => void publish()} disabled={busy}>
            {busy ? <Loader2 size={15} className="animate-spin" /> : <Send size={15} />}
            Publicar
          </button>
        </>
      }
    >
      <div className="flex flex-col gap-5">
        {targets && targets.length > 0 && (
          <div role="radiogroup" aria-label="Publicar en" className="flex flex-wrap items-center gap-2">
            <span className="text-sm text-muted">Se publicará en</span>
            {targets.map((t) => {
              const on = t.id === accountId;
              const info = channelInfo(t.channel);
              return (
                <button
                  key={t.id}
                  type="button"
                  role="radio"
                  aria-checked={on}
                  disabled={targets.length === 1 || busy}
                  onClick={() => !on && onTarget?.(t.id)}
                  className={cx(
                    "inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-sm",
                    on ? "border-accent bg-accent/10 font-medium" : "border-line text-muted hover:text-ink",
                  )}
                >
                  <span className="inline-block h-2 w-2 rounded-full" style={{ background: info.color }} />
                  {info.name} · {t.label}
                </button>
              );
            })}
          </div>
        )}
        <div role="tablist" className="inline-flex self-start rounded-lg bg-panel2 p-0.5">
          {(
            [
              ["text", "Texto"],
              ["media", "Foto o vídeo"],
            ] as const
          ).map(([id, label]) => (
            <button
              key={id}
              role="tab"
              aria-selected={form.mode === id}
              onClick={() => patch({ mode: id })}
              className={cx(
                "rounded-md px-4 py-1.5 text-sm font-medium",
                form.mode === id ? "bg-panel shadow-sm" : "text-muted hover:text-ink",
              )}
            >
              {label}
            </button>
          ))}
        </div>

        {form.mode === "text" ? (
          <div className="flex gap-5">
            <div className="flex min-w-0 flex-1 flex-col gap-4">
              <div>
                <label className="label" htmlFor="status-text">
                  Texto
                </label>
                <textarea
                  id="status-text"
                  data-autofocus
                  className="input"
                  rows={5}
                  maxLength={maxText}
                  value={form.text}
                  onChange={(e) => patch({ text: e.target.value })}
                  style={{ fontFamily: fontCss(form.font) }}
                />
                <p className="mt-1 text-right text-xs text-muted">
                  {form.text.length}/{maxText}
                </p>
              </div>
              <div>
                <span className="label">Color</span>
                <div className="grid grid-cols-6 gap-2" role="radiogroup" aria-label="Color de fondo">
                  {STATUS_COLORS.map((c) => (
                    <button
                      key={c}
                      type="button"
                      role="radio"
                      aria-checked={form.color === c}
                      aria-label={`Color ${c}`}
                      onClick={() => patch({ color: c })}
                      className={cx(
                        "h-8 rounded-lg border-2",
                        form.color === c ? "border-ink" : "border-transparent hover:border-line",
                      )}
                      style={{ background: c }}
                    />
                  ))}
                </div>
              </div>
              <div>
                <span className="label">Letra</span>
                <div className="grid grid-cols-4 gap-2" role="radiogroup" aria-label="Letra">
                  {STATUS_FONTS.map((f) => (
                    <button
                      key={f.value}
                      type="button"
                      role="radio"
                      aria-checked={form.font === f.value}
                      aria-label={f.label}
                      title={f.label}
                      onClick={() => patch({ font: f.value })}
                      className={cx(
                        "h-9 rounded-lg border text-lg",
                        form.font === f.value ? "border-accent bg-[var(--hover)]" : "border-line hover:bg-[var(--hover)]",
                      )}
                      style={{ fontFamily: f.css }}
                    >
                      Aa
                    </button>
                  ))}
                </div>
              </div>
            </div>

            <div
              data-testid="status-preview"
              className="flex h-[300px] w-[170px] shrink-0 items-center justify-center overflow-hidden rounded-xl p-4 text-center font-semibold text-white"
              style={{
                background: bgOf(form.color),
                fontFamily: fontCss(form.font),
                fontSize: form.text.length < 40 ? 20 : form.text.length < 120 ? 16 : 13,
              }}
            >
              <span className="whitespace-pre-wrap break-words">{form.text || " "}</span>
            </div>
          </div>
        ) : (
          <div className="flex flex-col gap-4">
            {form.file ? (
              <div className="flex items-center gap-3 rounded-lg border border-line bg-panel2 px-3 py-2">
                {isVideo ? <Film size={22} className="text-muted" /> : <ImageIcon size={22} className="text-muted" />}
                <div className="min-w-0 flex-1">
                  <p className="truncate font-medium" title={form.file.name}>
                    {form.file.name}
                  </p>
                  <p className="text-xs text-muted">{formatFileSize(form.file.size)}</p>
                </div>
                <button className="icon-btn" aria-label="Quitar archivo" title="Quitar archivo" onClick={() => patch({ file: null })}>
                  <X size={16} />
                </button>
              </div>
            ) : (
              <button className="btn self-start" onClick={() => void pick()}>
                <ImageIcon size={15} />
                Elegir foto o vídeo
              </button>
            )}
            <div>
              <label className="label" htmlFor="status-caption">
                Pie
              </label>
              <input
                id="status-caption"
                className="input"
                maxLength={maxText}
                value={form.text}
                onChange={(e) => patch({ text: e.target.value })}
              />
            </div>
          </div>
        )}

        <div>
          <span className="label">Audiencia</span>
          <AudiencePicker
            accountId={accountId}
            choice={audienceChoice}
            mode={form.audienceMode}
            listIds={form.listIds}
            peerIds={form.peerIds}
            onChange={(p) => patch(p)}
          />
        </div>

        {scheduling && (
          <div className="flex items-end gap-3 rounded-lg border border-line bg-panel2 p-3">
            <div className="flex-1">
              <label className="label" htmlFor="status-when">
                Fecha y hora
              </label>
              <input
                id="status-when"
                type="datetime-local"
                className="input"
                value={when}
                onChange={(e) => {
                  setWhen(e.target.value);
                  setError(null);
                }}
              />
            </div>
            <button className="btn btn-primary" onClick={() => void schedule()} disabled={busy}>
              <CalendarClock size={15} />
              Confirmar
            </button>
          </div>
        )}
      </div>
    </Modal>
  );
}
