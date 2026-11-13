import type { ReactNode } from "react";

/** Tarjeta de una sección de Ajustes. */
export function SettingsCard({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="card divide-y divide-line" aria-label={title}>
      <h2 className="px-5 py-3 text-base font-semibold">{title}</h2>
      {children}
    </section>
  );
}

/** Etiqueta a la izquierda y control a la derecha. */
export function SettingRow({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-6 px-5 py-3">
      <span className="min-w-0 flex-1">
        {label}
        {hint && <span className="mt-0.5 block text-sm text-muted">{hint}</span>}
      </span>
      <div className="shrink-0">{children}</div>
    </div>
  );
}
