// Ajustes: cabecera, menú de secciones de 224 px (texto real) y la tarjeta de «General» (máx. 672 px).
import { PageRoot } from "./parts";
import { SETTINGS_SECTIONS, SettingsSectionSkeleton } from "./zones";

export default function SettingsSkeleton() {
  return (
    <PageRoot testId="settings-skeleton">
      <header className="border-b border-line bg-panel px-6 py-3">
        <h1 className="text-lg font-semibold">Ajustes</h1>
      </header>
      <div className="flex min-h-0 flex-1">
        <nav className="w-56 shrink-0 border-r border-line bg-panel p-3" aria-hidden="true">
          {SETTINGS_SECTIONS.map((s, i) => (
            <span
              key={s.id}
              className={`block w-full truncate rounded-lg px-3 py-2 text-left text-sm font-medium ${i === 0 ? "bg-[var(--hover)] text-ink" : "text-muted"}`}
            >
              {s.label}
            </span>
          ))}
        </nav>
        <div className="min-w-0 flex-1 overflow-hidden p-6">
          <div className="mx-auto max-w-2xl">
            <SettingsSectionSkeleton bare />
          </div>
        </div>
      </div>
    </PageRoot>
  );
}
