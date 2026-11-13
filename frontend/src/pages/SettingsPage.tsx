import { useEffect, useMemo, useRef, useState } from "react";
import { useReloadOnReconnect } from "@/lib/useReconnect";
import { Loader2 } from "lucide-react";
import { api } from "@/api";
import type { AppSettings } from "@/api/types";
import LoadError from "@/components/common/LoadError";
import { SettingsSectionSkeleton } from "@/pages/skeletons/zones";
import AboutSection from "@/components/settings/AboutSection";
import AutoDownloadSection from "@/components/settings/AutoDownloadSection";
import ConnectionSection from "@/components/settings/ConnectionSection";
import CampaignsSection from "@/components/settings/CampaignsSection";
import DataSection from "@/components/settings/DataSection";
import GeneralSection from "@/components/settings/GeneralSection";
import NotificationsSection from "@/components/settings/NotificationsSection";
import {
  formFromDraft,
  isDirty,
  toAppSettings,
  toSettingsForm,
  validateSettings,
  type SettingsForm,
} from "@/components/settings/settingsForm";
import { clearDraft, loadDraft, saveDraft } from "@/lib/draft";
import { cx } from "@/lib/cx";
import { useUI } from "@/store/ui";

type SectionId = "general" | "connection" | "notifications" | "downloads" | "campaigns" | "data" | "about";

const SECTIONS: Array<{ id: SectionId; label: string }> = [
  { id: "general", label: "General" },
  { id: "connection", label: "Conexión" },
  { id: "notifications", label: "Notificaciones" },
  { id: "downloads", label: "Descargas automáticas" },
  { id: "campaigns", label: "Campañas" },
  { id: "data", label: "Datos" },
  { id: "about", label: "Acerca de" },
];

const DRAFT_SCREEN = "settings";
const DRAFT_ID = "main";

/**
 * Ajustes. Un solo formulario para todas las secciones: se guarda al pulsar «Guardar» (barra de
 * abajo, que avisa de los cambios sin guardar). Los cambios pendientes quedan como borrador.
 */
export default function SettingsPage() {
  const toast = useUI((s) => s.toast);
  const [section, setSection] = useState<SectionId>("general");
  const [loaded, setLoaded] = useState(false);
  const [saved, setSaved] = useState<SettingsForm | null>(null);
  const [form, setForm] = useState<SettingsForm | null>(null);
  const base = useRef<AppSettings | null>(null);
  const [version, setVersion] = useState("");
  const [dataDir, setDataDir] = useState("");
  const [busy, setBusy] = useState(false);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const s = await api.settings.Get();
        if (!alive) return;
        base.current = s;
        const loadedForm = toSettingsForm(s);
        const draft = formFromDraft(loadDraft<unknown>(DRAFT_SCREEN, DRAFT_ID));
        setSaved(loadedForm);
        setForm(draft && isDirty(draft, loadedForm) ? draft : loadedForm);
      } catch {
        /* el aviso ya salió */
      } finally {
        if (alive) setLoaded(true);
      }
    })();
    api.settings
      .Version()
      .then((v) => alive && setVersion(v))
      .catch(() => undefined);
    api.settings
      .DataDir()
      .then((d) => alive && setDataDir(d))
      .catch(() => undefined);
    return () => {
      alive = false;
    };
  }, [attempt]);

  // Tras reconectar se vuelve a leer lo guardado; lo que la persona está editando no se pisa.
  useReloadOnReconnect(() => {
    api.settings
      .Get()
      .then((s) => {
        base.current = s;
        const fresh = toSettingsForm(s);
        setSaved(fresh);
        setForm((f) => (f && saved && isDirty(f, saved) ? f : fresh));
      })
      .catch(() => undefined);
    api.settings
      .Version()
      .then(setVersion)
      .catch(() => undefined);
  });

  const dirty = !!form && !!saved && isDirty(form, saved);
  const problem = useMemo(() => (form ? validateSettings(form) : null), [form]);

  // Borrador de lo que falta por guardar.
  useEffect(() => {
    if (!form || !saved) return;
    if (dirty) saveDraft(DRAFT_SCREEN, DRAFT_ID, form);
    else clearDraft(DRAFT_SCREEN, DRAFT_ID);
  }, [form, saved, dirty]);

  const patch = (p: Partial<SettingsForm>) => setForm((f) => (f ? { ...f, ...p } : f));

  const save = async () => {
    if (!form || problem) return;
    setBusy(true);
    try {
      const out = toAppSettings(form, base.current);
      await api.settings.Save(out);
      base.current = out;
      const next = toSettingsForm(out);
      setSaved(next);
      setForm(next);
      useUI.getState().setTheme(next.theme);
      clearDraft(DRAFT_SCREEN, DRAFT_ID);
      toast("success", "Ajustes guardados");
    } catch {
      /* el aviso ya salió */
    } finally {
      setBusy(false);
    }
  };

  const discard = () => {
    if (saved) setForm(saved);
  };

  // El marco (cabecera y secciones) se pinta ya; solo el contenido espera a los ajustes.
  const ready = loaded && !!form;
  const retry = () => {
    setLoaded(false);
    setAttempt((n) => n + 1);
  };

  return (
    <div className="flex h-full flex-col">
      <header className="border-b border-line bg-panel px-6 py-3">
        <h1 className="text-lg font-semibold">Ajustes</h1>
      </header>

      <div className="flex min-h-0 flex-1">
        <nav className="w-56 shrink-0 border-r border-line bg-panel p-3" aria-label="Secciones">
          {SECTIONS.map((s) => (
            <button
              key={s.id}
              onClick={() => setSection(s.id)}
              aria-current={section === s.id ? "page" : undefined}
              className={cx(
                "block w-full truncate rounded-lg px-3 py-2 text-left text-sm font-medium",
                section === s.id ? "bg-[var(--hover)] text-ink" : "text-muted hover:bg-[var(--hover)] hover:text-ink",
              )}
            >
              {s.label}
            </button>
          ))}
        </nav>

        <div className="min-w-0 flex-1 overflow-y-auto p-6">
          <div className="mx-auto max-w-2xl">
            {!loaded && <SettingsSectionSkeleton section={section} />}
            {loaded && !form && <LoadError what="los ajustes" onRetry={retry} />}
            {ready && (
              <>
                {section === "general" && <GeneralSection form={form} patch={patch} />}
                {section === "connection" && <ConnectionSection />}
                {section === "notifications" && <NotificationsSection form={form} patch={patch} />}
                {section === "downloads" && <AutoDownloadSection form={form} patch={patch} />}
                {section === "campaigns" && <CampaignsSection form={form} patch={patch} />}
                {section === "data" && <DataSection form={form} patch={patch} dataDir={dataDir} />}
                {section === "about" && <AboutSection version={version} />}
              </>
            )}
          </div>
        </div>
      </div>

      {dirty && (
        <div
          role="status"
          className="anim-fade flex items-center gap-3 border-t border-line bg-panel px-6 py-3"
          data-testid="save-bar"
        >
          <span className="min-w-0 flex-1 truncate text-sm" title={problem ?? undefined}>
            {problem ? <span className="text-danger">{problem}</span> : "Cambios sin guardar"}
          </span>
          <button className="btn" onClick={discard} disabled={busy}>
            Descartar
          </button>
          <button className="btn btn-primary" onClick={() => void save()} disabled={busy || !!problem}>
            {busy && <Loader2 size={15} className="animate-spin" />}
            Guardar
          </button>
        </div>
      )}
    </div>
  );
}
