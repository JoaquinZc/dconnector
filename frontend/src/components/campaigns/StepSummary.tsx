import type { ReactNode } from "react";
import type { AccountView, CampaignReview, PacingSettings } from "@/api/types";
import AccountPill from "@/components/common/AccountPill";
import { parseLocalInput } from "@/components/status/statusForm";
import { formatDateTime, truncate } from "@/lib/format";
import {
  filledVariants,
  formatEstimate,
  riskCounts,
  skippedTotal,
  weekdaysLabel,
  type CampaignForm,
} from "./campaignModel";
import { pacingSummary } from "./pacingModel";
import ReviewPanel from "./ReviewPanel";

interface Props {
  form: CampaignForm;
  account?: AccountView;
  pacing: PacingSettings | null;
  review: CampaignReview | null;
  reviewing: boolean;
  reviewError: string;
  blockers: string[];
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid grid-cols-[120px_1fr] gap-3 py-1.5 text-sm">
      <dt className="text-muted">{label}</dt>
      <dd className="min-w-0 break-words">{children}</dd>
    </div>
  );
}

/** Paso 5: resumen, estimación y lo que impide lanzar. */
export default function StepSummary({ form, account, pacing, review, reviewing, reviewError, blockers }: Props) {
  const variants = filledVariants(form);
  const when = form.startMode === "later" ? formatDateTime(parseLocalInput(form.startAt) ?? 0) : "Ahora";
  return (
    <div className="grid grid-cols-1 gap-5 md:grid-cols-[1fr_280px]">
      <dl className="min-w-0 divide-y divide-line">
        <Row label="Nombre">{form.name}</Row>
        <Row label="Cuenta">
          {account ? <AccountPill label={account.account.label} color={account.account.color} /> : "—"}
        </Row>
        <Row label="Destinatarios">
          {review ? (
            <span data-testid="summary-recipients">
              {review.recipients}
              <span className="ml-2 text-xs text-muted">
                {riskCounts(review)
                  .map((r) => `${r.label} ${r.count}`)
                  .join(" · ")}
                {skippedTotal(review) > 0 ? ` · Omitidos ${skippedTotal(review)}` : ""}
              </span>
            </span>
          ) : (
            "—"
          )}
        </Row>
        <Row label="Mensaje">
          <span title={form.text}>{truncate(form.text, 140) || "—"}</span>
          {variants.length > 0 && <span className="ml-2 text-xs text-muted">+ {variants.length} variantes</span>}
          {form.filePath && <span className="ml-2 text-xs text-muted">· {form.fileName || "Adjunto"}</span>}
        </Row>
        <Row label="Inicio">{when}</Row>
        <Row label="Ventana">
          {form.fromTime}–{form.toTime} · {weekdaysLabel(form.weekdays)}
        </Row>
        <Row label="Prueba A/B">{form.abEnabled ? "Sí" : "No"}</Row>
        <Row label="Ritmo">{pacing ? pacingSummary(pacing) : "—"}</Row>
        <Row label="Duración">
          <span data-testid="summary-estimate">{review ? formatEstimate(review.estimate) || "—" : "—"}</span>
        </Row>
      </dl>

      <div className="flex flex-col gap-3">
        <ReviewPanel review={review} loading={reviewing} error={reviewError} />
        {blockers.length > 0 && (
          <ul className="rounded-xl border border-danger/40 bg-[color-mix(in_srgb,var(--danger)_8%,transparent)] p-3 text-sm text-danger" data-testid="launch-blockers">
            {blockers.map((b, i) => (
              <li key={i}>{b}</li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
