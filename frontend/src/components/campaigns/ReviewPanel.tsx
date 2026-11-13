import { Loader2 } from "lucide-react";
import type { CampaignReview } from "@/api/types";
import { LEVEL_LABEL, formatEstimate, levelTone } from "./campaignModel";
import Pill, { TONE_COLORS } from "./Pill";

interface Props {
  review: CampaignReview | null;
  loading: boolean;
  error?: string;
  /** Muestra también la estimación de duración. */
  showEstimate?: boolean;
}

/** Resultado de `Review` en vivo: nivel verde/amarillo/rojo y los hallazgos. */
export default function ReviewPanel({ review, loading, error, showEstimate }: Props) {
  const items = review?.items ?? [];
  const warnings = review?.warnings ?? [];
  return (
    <div className="rounded-xl border border-line bg-panel2 p-3" data-testid="review-panel" aria-live="polite">
      <div className="mb-2 flex items-center gap-2">
        <h3 className="flex-1 text-sm font-semibold">Revisión</h3>
        {loading && <Loader2 size={14} className="animate-spin text-muted" aria-label="Revisando" />}
        {review && (
          <Pill tone={levelTone(review.level)} title={review.level}>
            {LEVEL_LABEL[review.level] ?? review.level}
          </Pill>
        )}
      </div>

      {error && <p className="text-sm text-danger">{error}</p>}
      {!review && !error && !loading && <p className="text-sm text-muted">Sin datos</p>}

      {review && (
        <ul className="flex flex-col gap-1.5">
          {items.map((it, i) => (
            <li key={`${it.code}-${i}`} className="flex items-start gap-2 text-sm" data-level={it.level}>
              <span
                className="mt-1.5 h-2 w-2 shrink-0 rounded-full"
                style={{ background: TONE_COLORS[levelTone(it.level)] }}
                aria-hidden
              />
              <span className="min-w-0 break-words">{it.message}</span>
            </li>
          ))}
          {warnings.map((w, i) => (
            <li key={`w-${i}`} className="flex items-start gap-2 text-sm text-muted">
              <span className="mt-1.5 h-2 w-2 shrink-0 rounded-full" style={{ background: TONE_COLORS.warn }} aria-hidden />
              <span className="min-w-0 break-words">{w}</span>
            </li>
          ))}
          {items.length === 0 && warnings.length === 0 && <li className="text-sm text-muted">Sin avisos</li>}
        </ul>
      )}

      {review && showEstimate && (
        <p className="mt-3 border-t border-line pt-2 text-sm">
          <span className="text-muted">Duración </span>
          {formatEstimate(review.estimate) || "—"}
        </p>
      )}
    </div>
  );
}
