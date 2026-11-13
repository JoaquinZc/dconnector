import { SettingsCard } from "./SettingRow";
import type { SettingsForm } from "./settingsForm";

interface Props {
  form: SettingsForm;
  patch: (p: Partial<SettingsForm>) => void;
}

export default function CampaignsSection({ form, patch }: Props) {
  return (
    <SettingsCard title="Campañas">
      <div className="px-5 py-3">
        <label className="label" htmlFor="optout-keywords">
          Palabras de baja
        </label>
        <textarea
          id="optout-keywords"
          className="input"
          rows={3}
          placeholder="baja, stop, cancelar"
          value={form.optOutKeywords}
          onChange={(e) => patch({ optOutKeywords: e.target.value })}
        />
      </div>
      <div className="px-5 py-3">
        <label className="label" htmlFor="optout-footer">
          Texto del pie
        </label>
        <input
          id="optout-footer"
          className="input"
          value={form.optOutFooterText}
          onChange={(e) => patch({ optOutFooterText: e.target.value })}
        />
      </div>
    </SettingsCard>
  );
}
