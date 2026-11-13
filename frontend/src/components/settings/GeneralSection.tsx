import { cx } from "@/lib/cx";
import { SettingRow, SettingsCard } from "./SettingRow";
import { THEMES, type SettingsForm } from "./settingsForm";
import Switch from "./Switch";

export const AUTO_HISTORY_LABEL = "Guardar el historial automáticamente (despacio, en segundo plano)";

interface Props {
  form: SettingsForm;
  patch: (p: Partial<SettingsForm>) => void;
}

export default function GeneralSection({ form, patch }: Props) {
  return (
    <SettingsCard title="General">
      <SettingRow label="Cerrar a la bandeja">
        <Switch label="Cerrar a la bandeja" checked={form.closeToTray} onChange={(v) => patch({ closeToTray: v })} />
      </SettingRow>
      <SettingRow label="Iniciar con Windows">
        <Switch label="Iniciar con Windows" checked={form.startWithWindows} onChange={(v) => patch({ startWithWindows: v })} />
      </SettingRow>
      <SettingRow label="Iniciar minimizado">
        <Switch label="Iniciar minimizado" checked={form.startMinimized} onChange={(v) => patch({ startMinimized: v })} />
      </SettingRow>
      <SettingRow
        label={AUTO_HISTORY_LABEL}
        hint="Pide en segundo plano los mensajes anteriores de tus chats, poco a poco, para no arriesgar bloqueos en tus cuentas."
      >
        <Switch label={AUTO_HISTORY_LABEL} checked={form.autoHistory} onChange={(v) => patch({ autoHistory: v })} />
      </SettingRow>
      <SettingRow label="Tema">
        <div role="radiogroup" aria-label="Tema" className="inline-flex rounded-lg bg-panel2 p-0.5">
          {THEMES.map((t) => (
            <button
              key={t.id}
              type="button"
              role="radio"
              aria-checked={form.theme === t.id}
              onClick={() => patch({ theme: t.id })}
              className={cx(
                "rounded-md px-3 py-1 text-sm font-medium",
                form.theme === t.id ? "bg-panel shadow-sm" : "text-muted hover:text-ink",
              )}
            >
              {t.label}
            </button>
          ))}
        </div>
      </SettingRow>
    </SettingsCard>
  );
}
