import { SettingRow, SettingsCard } from "./SettingRow";

/** Licencias de terceros (lista estática, 11 §6.10). */
export const THIRD_PARTY_LICENSES: Array<{ name: string; license: string }> = [
  { name: "whatsmeow", license: "MPL-2.0" },
  { name: "gotd", license: "MIT" },
  { name: "messagix", license: "AGPL-3.0" },
  { name: "Wails", license: "MIT" },
  { name: "modernc sqlite", license: "BSD-3-Clause" },
];

export default function AboutSection({ version }: { version: string }) {
  return (
    <SettingsCard title="Acerca de">
      <SettingRow label="Versión">
        <span className="font-mono text-sm" data-testid="app-version">
          {version || "…"}
        </span>
      </SettingRow>
      <SettingRow label="Protocolo">
        <span className="font-mono text-sm">v1</span>
      </SettingRow>
      <div className="px-5 py-3">
        <span className="label">Licencias de terceros</span>
        <ul className="divide-y divide-line rounded-lg border border-line">
          {THIRD_PARTY_LICENSES.map((l) => (
            <li key={l.name} className="flex items-center justify-between px-3 py-2 text-sm">
              <span>{l.name}</span>
              <span className="font-mono text-xs text-muted">{l.license}</span>
            </li>
          ))}
        </ul>
      </div>
    </SettingsCard>
  );
}
