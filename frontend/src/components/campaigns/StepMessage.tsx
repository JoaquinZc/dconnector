import { useEffect, useState } from "react";
import { Paperclip, X } from "lucide-react";
import { api } from "@/api";
import type { CampaignReview } from "@/api/types";
import WaText from "@/components/common/WaText";
import Switch from "@/components/settings/Switch";
import { baseName, previewText, type CampaignForm } from "./campaignModel";
import ReviewPanel from "./ReviewPanel";
import { fetchFooterText } from "./useCampaignApi";
import VariantsEditor from "./VariantsEditor";

interface Props {
  form: CampaignForm;
  patch: (p: Partial<CampaignForm>) => void;
  review: CampaignReview | null;
  reviewing: boolean;
  reviewError: string;
}

/** Paso 3: mensaje con variantes, adjunto, pie de baja y revisión en vivo. */
export default function StepMessage({ form, patch, review, reviewing, reviewError }: Props) {
  const [footer, setFooter] = useState("");
  useEffect(() => {
    let alive = true;
    void fetchFooterText().then((t) => alive && setFooter(t));
    return () => {
      alive = false;
    };
  }, []);

  const attach = async () => {
    try {
      const files = await api.files.PickFiles("any");
      const f = files[0];
      if (f) patch({ filePath: f.path, fileName: f.name || baseName(f.path) });
    } catch {
      /* el aviso ya salió */
    }
  };

  const shown = previewText(form.text);

  return (
    <div className="grid grid-cols-1 gap-5 md:grid-cols-[1fr_280px]">
      <div className="flex min-w-0 flex-col gap-4">
        <VariantsEditor
          text={form.text}
          variants={form.variants}
          onText={(text) => patch({ text })}
          onVariants={(variants) => patch({ variants })}
          required={review?.requiredVariants ?? 0}
        />

        <div className="flex flex-wrap items-center gap-3">
          {form.filePath ? (
            <span className="inline-flex max-w-full items-center gap-1.5 rounded-lg border border-line bg-panel2 px-2.5 py-1.5 text-sm">
              <Paperclip size={14} className="shrink-0 text-muted" />
              <span className="max-w-[260px] truncate" title={form.filePath}>
                {form.fileName || baseName(form.filePath)}
              </span>
              <button
                type="button"
                className="text-muted hover:text-ink"
                aria-label="Quitar adjunto"
                onClick={() => patch({ filePath: "", fileName: "" })}
              >
                <X size={14} />
              </button>
            </span>
          ) : (
            <button className="btn" onClick={() => void attach()}>
              <Paperclip size={15} />
              Adjuntar
            </button>
          )}
          <span className="flex-1" />
          <label className="flex items-center gap-2 text-sm">
            <Switch
              checked={form.optOutFooter}
              onChange={(optOutFooter) => patch({ optOutFooter })}
              label="Pie de baja"
            />
            Pie de baja
          </label>
        </div>

        {(shown.trim() || form.optOutFooter) && (
          <div className="rounded-xl bg-chat p-3" data-testid="message-preview">
            <div className="ml-auto max-w-[85%] rounded-lg bg-bubble-out px-3 py-2 text-sm shadow-sm">
              <WaText text={shown} />
              {form.optOutFooter && footer && (
                <p className="mt-2 text-xs text-muted" title={footer}>
                  <WaText text={footer} />
                </p>
              )}
            </div>
          </div>
        )}
      </div>

      <ReviewPanel review={review} loading={reviewing} error={reviewError} />
    </div>
  );
}
