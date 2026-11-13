import { useEffect, useMemo, useState } from "react";
import { RotateCcw } from "lucide-react";
import { api } from "@/api";
import type { PacingSettings } from "@/api/types";
import Switch from "@/components/settings/Switch";
import { useUI } from "@/store/ui";
import {
  PACING_GROUPS,
  DEFAULT_PACING,
  pacingToText,
  parsePacing,
  safePacingText,
  samePacing,
  type PacingErrors,
  type PacingField,
  type PacingText,
} from "./pacingModel";

interface FormProps {
  text: PacingText;
  errors: PacingErrors;
  onField: (key: PacingField["key"], value: string) => void;
  disabled?: boolean;
}

/** Los seis grupos de campos de `PacingSettings`. */
export function PacingForm({ text, errors, onField, disabled }: FormProps) {
  return (
    <div className="flex flex-col gap-4">
      {PACING_GROUPS.map((g) => (
        <section key={g.id} aria-label={g.title} data-testid={`pacing-group-${g.id}`} className="rounded-xl border border-line bg-panel2 p-4">
          <h3 className="mb-3 text-sm font-semibold">{g.title}</h3>
          <div className="grid grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-3">
            {g.fields.map((f) =>
              f.type === "bool" ? (
                <div key={f.key} className="flex items-center gap-2 self-end pb-1.5">
                  <Switch
                    checked={text[f.key] === "true"}
                    onChange={(v) => onField(f.key, v ? "true" : "false")}
                    label={`${g.title}: ${f.label}`}
                    disabled={disabled}
                  />
                  <span className="text-sm">{f.label}</span>
                </div>
              ) : (
                <div key={f.key} title={f.hint}>
                  <label className="label" htmlFor={`pacing-${f.key}`}>
                    {f.label}
                  </label>
                  <div className="flex items-center gap-1.5">
                    <input
                      id={`pacing-${f.key}`}
                      className="input"
                      inputMode="decimal"
                      value={text[f.key]}
                      disabled={disabled}
                      aria-invalid={!!errors[f.key]}
                      onChange={(e) => onField(f.key, e.target.value)}
                    />
                    {f.unit && <span className="shrink-0 text-xs text-muted">{f.unit}</span>}
                  </div>
                  {errors[f.key] && <p className="mt-1 text-xs text-danger">{errors[f.key]}</p>}
                </div>
              ),
            )}
          </div>
        </section>
      ))}
    </div>
  );
}

interface EditorProps {
  accountId: string;
  /** Se llama con el ritmo guardado. */
  onSaved?: (p: PacingSettings) => void;
  onCancel?: () => void;
}

/** Carga el ritmo de la cuenta, lo edita y lo guarda; «Restablecer valores seguros» rellena los de fábrica. */
export default function PacingEditor({ accountId, onSaved, onCancel }: EditorProps) {
  const toast = useUI((s) => s.toast);
  const [saved, setSaved] = useState<PacingSettings | null>(null);
  const [text, setText] = useState<PacingText>(() => safePacingText());
  const [loaded, setLoaded] = useState(false);
  const [busy, setBusy] = useState(false);
  const [showErrors, setShowErrors] = useState(false);

  useEffect(() => {
    let alive = true;
    setLoaded(false);
    setSaved(null);
    setShowErrors(false);
    api.campaigns
      .GetPacing(accountId)
      .then((p) => {
        if (!alive) return;
        setSaved(p);
        setText(pacingToText(p));
      })
      .catch(() => alive && setText(safePacingText()))
      .finally(() => alive && setLoaded(true));
    return () => {
      alive = false;
    };
  }, [accountId]);

  const parsed = useMemo(() => parsePacing(text), [text]);
  const dirty = !saved || !parsed.pacing || !samePacing(saved, parsed.pacing);
  const isSafe = parsed.pacing ? samePacing(parsed.pacing, DEFAULT_PACING) : false;

  const save = async () => {
    if (!parsed.pacing) return setShowErrors(true);
    setBusy(true);
    try {
      await api.campaigns.SetPacing(accountId, parsed.pacing);
      setSaved(parsed.pacing);
      toast("success", "Ritmo guardado");
      onSaved?.(parsed.pacing);
    } catch {
      /* el aviso ya salió */
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex flex-col gap-4">
      <PacingForm
        text={text}
        errors={showErrors ? parsed.errors : {}}
        onField={(k, v) => setText((t) => ({ ...t, [k]: v }))}
        disabled={!loaded || busy}
      />
      <div className="flex items-center gap-2">
        <button className="btn" onClick={() => setText(safePacingText())} disabled={!loaded || busy || isSafe}>
          <RotateCcw size={15} />
          Restablecer valores seguros
        </button>
        <span className="flex-1" />
        {onCancel && (
          <button className="btn" onClick={onCancel} disabled={busy}>
            Cerrar
          </button>
        )}
        <button className="btn btn-primary" onClick={() => void save()} disabled={!loaded || busy || !dirty}>
          Guardar
        </button>
      </div>
    </div>
  );
}
