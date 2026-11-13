import { FileText, FolderOpen } from "lucide-react";
import { api } from "@/api";
import { useConnection } from "@/api/connection";
import { SettingRow, SettingsCard } from "./SettingRow";
import type { SettingsForm } from "./settingsForm";
import Switch from "./Switch";

interface Props {
  form: SettingsForm;
  patch: (p: Partial<SettingsForm>) => void;
  /** Carpeta de datos (`SettingsService.DataDir`). */
  dataDir: string;
}

const run = async (fn: () => Promise<unknown>) => {
  try {
    await fn();
  } catch {
    /* el aviso ya salió */
  }
};

export default function DataSection({ form, patch, dataDir }: Props) {
  // Con un servidor remoto la carpeta es la del servidor: solo se muestra, no se abre.
  const remote = useConnection((s) => s.mode === "remote");
  return (
    <SettingsCard title="Datos">
      <div className="flex items-center gap-3 px-5 py-3">
        <FolderOpen size={18} className="shrink-0 text-muted" />
        <span className="min-w-0 flex-1 truncate font-mono text-sm" title={dataDir} data-testid="data-dir">
          {dataDir || "…"}
        </span>
        {!remote && (
          <>
            <button className="btn btn-sm" onClick={() => void run(() => api.settings.OpenDataDir())}>
              Abrir carpeta
            </button>
            <button className="btn btn-sm" onClick={() => void run(() => api.settings.OpenLogsDir())}>
              <FileText size={14} />
              Abrir registros
            </button>
          </>
        )}
      </div>
      <SettingRow label="Registro detallado">
        <Switch label="Registro detallado" checked={form.verboseLog} onChange={(v) => patch({ verboseLog: v })} />
      </SettingRow>
    </SettingsCard>
  );
}
