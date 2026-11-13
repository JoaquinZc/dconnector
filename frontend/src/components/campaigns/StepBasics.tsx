import type { AccountView } from "@/api/types";
import ChannelIcon from "@/components/common/ChannelIcon";
import { stateLabel, stateTone, TONE_COLOR } from "@/lib/accountStatus";
import { channelName } from "@/lib/channels";
import { cx } from "@/lib/cx";
import type { CampaignForm } from "./campaignModel";

interface Props {
  form: CampaignForm;
  patch: (p: Partial<CampaignForm>) => void;
  accounts: AccountView[];
}

/** Paso 1: nombre y cuenta (solo WhatsApp o Telegram). */
export default function StepBasics({ form, patch, accounts }: Props) {
  return (
    <div className="flex flex-col gap-5">
      <div>
        <label className="label" htmlFor="campaign-name">
          Nombre
        </label>
        <input
          id="campaign-name"
          className="input"
          value={form.name}
          maxLength={80}
          onChange={(e) => patch({ name: e.target.value })}
          data-autofocus
          autoComplete="off"
        />
      </div>

      <div>
        <span className="label">Cuenta</span>
        {accounts.length === 0 ? (
          <p className="rounded-lg border border-line bg-panel2 p-4 text-center text-sm text-muted">
            Sin cuentas de WhatsApp o Telegram
          </p>
        ) : (
          <div role="radiogroup" aria-label="Cuenta" className="grid grid-cols-1 gap-2 sm:grid-cols-2">
            {accounts.map((v) => {
              const on = form.accountId === v.account.id;
              const tone = stateTone(v.status?.state);
              return (
                <button
                  key={v.account.id}
                  type="button"
                  role="radio"
                  aria-checked={on}
                  onClick={() => patch({ accountId: v.account.id })}
                  className={cx(
                    "flex items-center gap-3 rounded-xl border p-3 text-left",
                    on ? "border-accent bg-[color-mix(in_srgb,var(--accent)_10%,transparent)]" : "border-line bg-panel hover:bg-[var(--hover)]",
                  )}
                >
                  <ChannelIcon channel={String(v.account.channel)} size={28} />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate font-medium" title={v.account.label}>
                      {v.account.label}
                    </span>
                    <span className="block truncate text-xs text-muted">
                      {channelName(String(v.account.channel))}
                      {v.account.selfHandle ? ` · ${v.account.selfHandle}` : ""}
                    </span>
                  </span>
                  <span className="flex items-center gap-1 text-xs" style={{ color: TONE_COLOR[tone] }}>
                    <span className="h-2 w-2 rounded-full" style={{ background: TONE_COLOR[tone] }} />
                    {stateLabel(v.status?.state)}
                  </span>
                </button>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
