import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Check } from "lucide-react";
import { api } from "@/api";
import type { Campaign, PacingSettings } from "@/api/types";
import Modal from "@/components/common/Modal";
import { clearDraft, loadDraft, saveDraft } from "@/lib/draft";
import { cx } from "@/lib/cx";
import { useAccounts } from "@/store/accounts";
import { useUI } from "@/store/ui";
import {
  STEPS,
  campaignAccounts,
  campaignFromForm,
  emptyForm,
  formFromCampaign,
  launchBlockers,
  validateStep,
  type CampaignForm,
  type Step,
} from "./campaignModel";
import PacingModal from "./PacingModal";
import { DEFAULT_PACING } from "./pacingModel";
import StepAudience from "./StepAudience";
import StepBasics from "./StepBasics";
import StepMessage from "./StepMessage";
import StepSending from "./StepSending";
import StepSummary from "./StepSummary";
import { fetchPacing, useReview } from "./useCampaignApi";

const DRAFT_SCREEN = "campaigns";

interface Props {
  /** Campaña existente que se edita (borrador, programada o en pausa); sin ella, una nueva. */
  campaign?: Campaign | null;
  onClose: () => void;
  /** Se guardó o lanzó algo: la lista debe recargar. */
  onDone: (kind: "saved" | "launched", saved: Campaign) => void;
}

/** Asistente de campañas de 5 pasos (11 §6.4). */
export default function CampaignWizard({ campaign, onClose, onDone }: Props) {
  const toast = useUI((s) => s.toast);
  const views = useAccounts((s) => s.views);
  const order = useAccounts((s) => s.order);
  const accounts = useMemo(() => campaignAccounts(order.map((id) => views[id]).filter(Boolean)), [order, views]);

  const draftId = campaign?.id || "new";
  const restored = useRef(false);
  const [form, setForm] = useState<CampaignForm>(() => {
    const d = loadDraft<CampaignForm>(DRAFT_SCREEN, draftId);
    if (d) {
      restored.current = true;
      return { ...emptyForm(), ...d, id: campaign?.id ?? "" };
    }
    return campaign ? formFromCampaign(campaign) : emptyForm();
  });
  const [hasDraft, setHasDraft] = useState(restored.current);
  const [step, setStep] = useState<Step>(1);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [pacing, setPacing] = useState<PacingSettings | null>(null);
  const [pacingOpen, setPacingOpen] = useState(false);

  // El borrador solo se guarda cuando la persona toca algo (no por abrir y cerrar).
  const touched = useRef(restored.current);
  const patch = useCallback((p: Partial<CampaignForm>) => {
    touched.current = true;
    setForm((f) => ({ ...f, ...p }));
    setError(null);
  }, []);

  // Tras guardar o lanzar ya no hay borrador que conservar.
  const finished = useRef(false);

  useEffect(() => {
    if (touched.current && !finished.current) saveDraft(DRAFT_SCREEN, draftId, form);
  }, [form, draftId]);

  // Una sola cuenta: ya está elegida. Una cuenta que ya no existe: se quita.
  useEffect(() => {
    if (accounts.length === 0) return;
    const known = accounts.some((v) => v.account.id === form.accountId);
    const target = accounts.length === 1 ? accounts[0].account.id : "";
    if (!known && form.accountId !== target) setForm((f) => ({ ...f, accountId: target }));
  }, [accounts, form.accountId]);

  // Ritmo de la cuenta elegida (para el resumen, la revisión y `Save`).
  useEffect(() => {
    if (!form.accountId) {
      setPacing(null);
      return;
    }
    let alive = true;
    setPacing(null);
    void fetchPacing(form.accountId).then((p) => alive && setPacing(p));
    return () => {
      alive = false;
    };
  }, [form.accountId]);

  const { review, loading: reviewing, error: reviewError } = useReview(form, pacing, campaign ?? null, step >= 2);
  const blockers = launchBlockers(form, review, reviewing);
  const account = accounts.find((v) => v.account.id === form.accountId);
  const launchable = !campaign || String(campaign.state) === "draft";

  const canGo = (n: Step) => {
    for (let i = 1; i < n; i++) if (validateStep(i as Step, form, Date.now())) return false;
    return true;
  };

  const next = () => {
    const e = validateStep(step, form, Date.now());
    if (e) return setError(e);
    setError(null);
    setStep((s) => Math.min(5, s + 1) as Step);
  };

  const goTo = (n: Step) => {
    if (n > step && !canGo(n)) {
      const bad = ([1, 2, 3, 4] as Step[]).find((i) => validateStep(i, form, Date.now()));
      if (bad) setError(validateStep(bad, form, Date.now()));
      return;
    }
    setError(null);
    setStep(n);
  };

  const finish = () => {
    finished.current = true;
    clearDraft(DRAFT_SCREEN, draftId);
    clearDraft(DRAFT_SCREEN, "new");
  };

  const buildCampaign = () => campaignFromForm(form, pacing ?? DEFAULT_PACING, campaign ?? null);

  const saveOnly = async () => {
    const e = validateStep(1, form, Date.now());
    if (e) return setError(e);
    setBusy(true);
    try {
      const saved = await api.campaigns.Save(buildCampaign());
      finish();
      toast("success", "Campaña guardada");
      onDone("saved", saved);
      onClose();
    } catch {
      /* el aviso ya salió */
    } finally {
      setBusy(false);
    }
  };

  const launch = async () => {
    if (launchBlockers(form, review, reviewing, Date.now()).length > 0) return;
    setBusy(true);
    try {
      const saved = await api.campaigns.Save(buildCampaign());
      // Si Launch falla, el siguiente intento reutiliza esta campaña y no crea otra.
      setForm((f) => ({ ...f, id: saved.id }));
      await api.campaigns.Launch(saved.id);
      finish();
      toast("success", "Campaña lanzada");
      onDone("launched", saved);
      onClose();
    } catch {
      /* el aviso ya salió */
    } finally {
      setBusy(false);
    }
  };

  const red = review?.level === "red";

  return (
    <>
      <Modal
        open
        onClose={onClose}
        title={campaign ? "Editar campaña" : "Nueva campaña"}
        size="lg"
        closeOnBackdrop={false}
        footer={
          <>
            {hasDraft && step === 1 && (
              <button
                className="btn btn-ghost btn-sm"
                onClick={() => {
                  clearDraft(DRAFT_SCREEN, draftId);
                  touched.current = false;
                  setForm(campaign ? formFromCampaign(campaign) : emptyForm());
                  setHasDraft(false);
                  setError(null);
                }}
              >
                Descartar borrador
              </button>
            )}
            <span className="flex-1" />
            {error && (
              <span className="mr-2 min-w-0 truncate text-sm text-danger" role="alert" title={error}>
                {error}
              </span>
            )}
            <button className="btn" onClick={() => void saveOnly()} disabled={busy}>
              {launchable ? "Guardar borrador" : "Guardar cambios"}
            </button>
            <button className="btn" onClick={() => goTo(Math.max(1, step - 1) as Step)} disabled={busy || step === 1}>
              Atrás
            </button>
            {step < 5 ? (
              <button className="btn btn-primary" onClick={next} disabled={busy}>
                Siguiente
              </button>
            ) : launchable ? (
              <button
                className={red ? "btn btn-danger-solid" : "btn btn-primary"}
                onClick={() => void launch()}
                disabled={busy || blockers.length > 0}
                title={blockers[0]}
                data-testid="launch-button"
              >
                Lanzar
              </button>
            ) : (
              <button className="btn btn-primary" onClick={() => void saveOnly()} disabled={busy}>
                Guardar
              </button>
            )}
          </>
        }
      >
        <ol className="mb-5 flex items-center gap-1" aria-label="Pasos">
          {STEPS.map((s, i) => {
            const done = s.id < step;
            const on = s.id === step;
            return (
              <li key={s.id} className="flex min-w-0 flex-1 items-center gap-1">
                <button
                  type="button"
                  onClick={() => goTo(s.id)}
                  aria-current={on ? "step" : undefined}
                  title={s.label}
                  className={cx(
                    "flex min-w-0 items-center gap-2 rounded-lg px-2 py-1 text-sm",
                    on ? "font-semibold text-ink" : "text-muted hover:text-ink",
                  )}
                >
                  <span
                    className={cx(
                      "flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-xs font-semibold",
                      on || done ? "bg-accent text-white" : "bg-panel2",
                    )}
                  >
                    {done ? <Check size={13} /> : s.id}
                  </span>
                  <span className="truncate">{s.label}</span>
                </button>
                {i < STEPS.length - 1 && <span className="h-px min-w-2 flex-1 bg-line" />}
              </li>
            );
          })}
        </ol>

        <div className="min-h-[320px]">
          {step === 1 && <StepBasics form={form} patch={patch} accounts={accounts} />}
          {step === 2 && <StepAudience form={form} patch={patch} review={review} reviewing={reviewing} />}
          {step === 3 && (
            <StepMessage form={form} patch={patch} review={review} reviewing={reviewing} reviewError={reviewError} />
          )}
          {step === 4 && (
            <StepSending form={form} patch={patch} pacing={pacing} onOpenPacing={() => setPacingOpen(true)} />
          )}
          {step === 5 && (
            <StepSummary
              form={form}
              account={account}
              pacing={pacing}
              review={review}
              reviewing={reviewing}
              reviewError={reviewError}
              blockers={blockers}
            />
          )}
        </div>
      </Modal>

      {pacingOpen && (
        <PacingModal
          accountId={form.accountId}
          onClose={() => setPacingOpen(false)}
          onSaved={(id, p) => {
            if (id === form.accountId) setPacing(p);
          }}
        />
      )}
    </>
  );
}
