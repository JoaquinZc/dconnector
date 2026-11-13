import { SettingsCard } from "./SettingRow";
import { AUTO_KINDS, MAX_MB_LIMIT, type AutoKind, type AutoRule, type SettingsForm } from "./settingsForm";
import Switch from "./Switch";

interface Props {
  form: SettingsForm;
  patch: (p: Partial<SettingsForm>) => void;
}

export default function AutoDownloadSection({ form, patch }: Props) {
  const set = (kind: AutoKind, rule: Partial<AutoRule>) =>
    patch({ autoDownload: { ...form.autoDownload, [kind]: { ...form.autoDownload[kind], ...rule } } });

  return (
    <SettingsCard title="Descargas automáticas">
      {AUTO_KINDS.map(({ id, label }) => {
        const rule = form.autoDownload[id];
        const bad = !Number.isFinite(rule.maxMB) || rule.maxMB < 1 || rule.maxMB > MAX_MB_LIMIT;
        return (
          <div key={id} className="flex items-center gap-4 px-5 py-3">
            <span className="min-w-0 flex-1">{label}</span>
            <label className="flex items-center gap-2 text-sm text-muted">
              <span>Máx.</span>
              <input
                type="number"
                min={1}
                max={MAX_MB_LIMIT}
                step={1}
                className="input !h-8 !w-20 text-right"
                aria-label={`Tamaño máximo de ${label} en MB`}
                aria-invalid={bad}
                value={Number.isFinite(rule.maxMB) ? rule.maxMB : ""}
                disabled={!rule.enabled}
                onChange={(e) => set(id, { maxMB: e.target.value === "" ? NaN : Number(e.target.value) })}
              />
              <span>MB</span>
            </label>
            <Switch label={`Descargar ${label.toLowerCase()}`} checked={rule.enabled} onChange={(v) => set(id, { enabled: v })} />
          </div>
        );
      })}
    </SettingsCard>
  );
}
