import { useEffect } from "react";
import { AlertTriangle, CheckCircle2, Download, FolderOpen, Loader2, RefreshCw } from "lucide-react";
import { UNAVAILABLE, formatMB, isBusy, phaseText, updateApi, useUpdate, type UpdateStatus } from "@/api/update";
import { SettingRow, SettingsCard } from "./SettingRow";
import Switch from "./Switch";

const NOTE_LINES = 8;
const NOTE_CHARS = 700;

/** Primeras líneas de las notas, como texto plano (nunca se interpreta como HTML). */
export function noteExcerpt(notes: string): string {
  const lines = notes.split("\n").map((l) => l.trimEnd());
  let out = lines.slice(0, NOTE_LINES).join("\n").trim();
  const cut = lines.length > NOTE_LINES || out.length > NOTE_CHARS;
  if (out.length > NOTE_CHARS) out = out.slice(0, NOTE_CHARS);
  return cut ? out + "…" : out;
}

function lastCheckText(ms: number): string {
  if (!ms) return "Todavía no se ha buscado.";
  return "Última búsqueda: " + new Date(ms).toLocaleString("es-EC", { dateStyle: "medium", timeStyle: "short" });
}

/** Sección «Actualizaciones» de Ajustes: versión, búsqueda, notas y botón «Actualizar». */
export default function UpdatesSection() {
  const { available, loaded, status, error } = useUpdate();
  const init = useUpdate((s) => s.init);
  const check = useUpdate((s) => s.check);
  const apply = useUpdate((s) => s.apply);
  const setAuto = useUpdate((s) => s.setAutoCheck);

  useEffect(() => init(), [init]);

  if (!available) {
    return (
      <SettingsCard title="Actualizaciones">
        <p className="px-5 py-3 text-sm text-muted" data-testid="upd-unavailable">
          {UNAVAILABLE}
        </p>
      </SettingsCard>
    );
  }
  if (!loaded || !status) {
    return (
      <SettingsCard title="Actualizaciones">
        <p className="px-5 py-3 text-sm text-muted" role="status">
          {error || "Cargando…"}
        </p>
      </SettingsCard>
    );
  }
  return <Body status={status} error={error} check={check} apply={apply} setAuto={setAuto} />;
}

function Body({
  status: s,
  error,
  check,
  apply,
  setAuto,
}: {
  status: UpdateStatus;
  error: string;
  check: () => Promise<void>;
  apply: () => Promise<void>;
  setAuto: (on: boolean) => Promise<void>;
}) {
  const latest = s.latest ?? null;
  const busy = isBusy(s.state);
  const failed = s.state === "error" || !!error;
  const msg = s.state === "error" ? s.error || error : error;
  const pct = s.state === "downloading" && s.progress.total > 0 ? Math.min(100, Math.round((s.progress.bytes / s.progress.total) * 100)) : null;
  const dev = s.currentVersion === "dev";
  const showFolder = failed && !!latest;

  return (
    <SettingsCard title="Actualizaciones">
      <SettingRow label="Versión instalada">
        <span className="font-mono text-sm" data-testid="upd-current">
          {s.currentVersion}
        </span>
      </SettingRow>

      <div className="flex flex-col gap-3 px-5 py-4" data-testid="upd-body" data-state={s.state}>
        {busy && (
          <div className="min-w-0" role="status">
            <p className="flex items-center gap-2 text-sm" data-testid="upd-phase">
              <Loader2 size={15} className="shrink-0 animate-spin" aria-hidden="true" />
              <span className="min-w-0 break-words">{phaseText(s)}</span>
            </p>
            {s.state === "downloading" && (
              <div
                className="mt-2 h-2 overflow-hidden rounded-full bg-panel2"
                role="progressbar"
                aria-label="Progreso de la descarga"
                aria-valuemin={0}
                aria-valuemax={100}
                aria-valuenow={pct ?? undefined}
              >
                <div className="h-full rounded-full bg-accent transition-[width]" style={{ width: `${pct ?? 5}%` }} />
              </div>
            )}
          </div>
        )}

        {!busy && !failed && s.state === "uptodate" && (
          <p className="flex items-center gap-2 text-sm" data-testid="upd-uptodate" style={{ color: "var(--ok)" }}>
            <CheckCircle2 size={16} aria-hidden="true" />
            Estás al día
          </p>
        )}

        {!busy && latest && (
          <div className="min-w-0" data-testid="upd-latest">
            <p className="text-sm font-medium">
              Hay una versión nueva: <span className="font-mono">{latest.version}</span>
              {latest.sizeBytes > 0 && <span className="font-normal text-muted"> · {formatMB(latest.sizeBytes)}</span>}
            </p>
            {latest.notes && (
              <pre
                className="mt-2 max-h-40 overflow-y-auto whitespace-pre-wrap break-words rounded-lg border border-line bg-panel2 px-3 py-2 font-sans text-xs text-muted"
                data-testid="upd-notes"
              >
                {noteExcerpt(latest.notes)}
              </pre>
            )}
            {!s.canInstall && (
              <p className="mt-2 break-words text-sm text-muted" data-testid="upd-cannot">
                {dev
                  ? "Esta es una compilación de desarrollo: no se actualiza sola."
                  : "Esta copia no se actualiza desde aquí. Descarga la versión nueva desde su página."}
              </p>
            )}
          </div>
        )}

        {!busy && !latest && !failed && s.state !== "uptodate" && (
          <p className="text-sm text-muted">{lastCheckText(s.lastCheck)}</p>
        )}

        {failed && (
          <div className="min-w-0 rounded-lg border px-3 py-2 text-sm" role="alert" data-testid="upd-error" style={{ borderColor: "var(--danger)" }}>
            <p className="flex items-start gap-2 break-words text-danger">
              <AlertTriangle size={16} className="mt-0.5 shrink-0" aria-hidden="true" />
              <span className="min-w-0">{msg}</span>
            </p>
            {showFolder && (
              <p className="mt-1 break-all text-xs text-muted" data-testid="upd-folder">
                {s.folder}
              </p>
            )}
          </div>
        )}

        <div className="flex flex-wrap gap-2">
          {latest && s.canInstall && !busy && !failed && (
            <button className="btn btn-primary" onClick={() => void apply()} data-testid="upd-apply">
              <Download size={15} aria-hidden="true" />
              Actualizar a {latest.version}
            </button>
          )}
          {latest && !s.canInstall && !busy && (
            <button className="btn btn-primary" onClick={() => void updateApi.openReleasePage().catch(() => undefined)} data-testid="upd-page">
              Abrir la página de la versión
            </button>
          )}
          {failed && !busy && (
            <button className="btn btn-primary" onClick={() => void (latest && s.canInstall ? apply() : check())} data-testid="upd-retry">
              Reintentar
            </button>
          )}
          {showFolder && (
            <button className="btn" onClick={() => void updateApi.openUpdatesFolder().catch(() => undefined)} data-testid="upd-open-folder">
              <FolderOpen size={15} aria-hidden="true" />
              Abrir carpeta
            </button>
          )}
          <button className="btn" onClick={() => void check()} disabled={busy} data-testid="upd-check">
            <RefreshCw size={15} aria-hidden="true" />
            Buscar actualizaciones
          </button>
        </div>
        {s.lastCheck > 0 && !busy && (
          <p className="text-xs text-muted" data-testid="upd-last">
            {lastCheckText(s.lastCheck)}
          </p>
        )}
      </div>

      <SettingRow label="Buscar al abrir">
        <Switch label="Buscar al abrir" checked={s.autoCheck} onChange={(v) => void setAuto(v)} />
      </SettingRow>
    </SettingsCard>
  );
}
