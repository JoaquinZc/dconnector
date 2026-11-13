import { useEffect, useState } from "react";
import { Loader2, ShieldCheck } from "lucide-react";
import { errorMessage } from "@/api";
import { useConnection, type Link, type ProbeResult } from "@/api/connection";
import { desktop, hasWails } from "@/api/desktop";
import { INSECURE_HTTP_WARNING, isInsecureRemote, parseServerUrl } from "@/api/session";
import { parseTlsPrompt } from "@/api/tls";
import TlsTrust from "@/components/connection/TlsTrust";
import { cx } from "@/lib/cx";
import { useUI } from "@/store/ui";
import { SettingRow, SettingsCard } from "./SettingRow";
import Switch from "./Switch";

const LINK_TEXT: Record<Link, { label: string; color: string }> = {
  connected: { label: "Conectado", color: "var(--ok)" },
  connecting: { label: "Reconectando", color: "var(--warn)" },
  reconnecting: { label: "Reconectando", color: "var(--warn)" },
  offline: { label: "Sin conexión", color: "var(--danger)" },
};

/** Texto del estado del enlace. */
export function linkLabel(link: Link): string {
  return LINK_TEXT[link].label;
}

/** Sección «Conexión» de Ajustes: modo Local o Remoto, «Probar» y estado del enlace. */
export default function ConnectionSection() {
  const toast = useUI((s) => s.toast);
  const { mode, baseUrl, user: savedUser, link, serverVersion, apiVersion, warning, tlsFingerprint } = useConnection();
  const probe = useConnection((s) => s.probe);
  const saveRemote = useConnection((s) => s.saveRemote);
  const useLocal = useConnection((s) => s.useLocal);
  const logout = useConnection((s) => s.logout);

  const [choice, setChoice] = useState<"local" | "remote">(mode);
  const [url, setUrl] = useState(mode === "remote" ? baseUrl : "");
  const [user, setUser] = useState(mode === "remote" ? savedUser : "");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState<"" | "probe" | "save">("");
  const [result, setResult] = useState<ProbeResult | null>(null);
  const [error, setError] = useState("");
  const [keepBg, setKeepBg] = useState<boolean | null>(null);
  const desktopApp = hasWails();

  useEffect(() => setChoice(mode), [mode]);
  useEffect(() => {
    if (!desktopApp) return;
    let alive = true;
    desktop
      .keepRunningInBackground()
      .then((v) => alive && setKeepBg(v))
      .catch(() => undefined);
    return () => {
      alive = false;
    };
  }, [desktopApp]);

  const tls = desktopApp && !!parseTlsPrompt(error);
  const canTry = !!url.trim() && !!user.trim() && !!password && !busy;
  const st = LINK_TEXT[link];

  const doProbe = async () => {
    setBusy("probe");
    setError("");
    setResult(null);
    try {
      setResult(await probe(url, user, password));
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy("");
    }
  };

  const doSave = async () => {
    setBusy("save");
    setError("");
    try {
      // En el escritorio la prueba y el login los hace el programa (es quien verifica la huella del certificado).
      if (desktopApp) await saveRemote(url, user, password);
      else {
        const r = result ?? (await probe(url, user, password));
        await saveRemote(r.base, user, password, r.token);
      }
    } catch (e) {
      setError(errorMessage(e));
      setBusy("");
    }
  };

  const doLocal = async () => {
    setBusy("save");
    try {
      await useLocal();
    } catch (e) {
      toast("error", errorMessage(e));
      setBusy("");
    }
  };

  const toggleBg = async (v: boolean) => {
    setKeepBg(v);
    try {
      await desktop.setKeepRunningInBackground(v);
    } catch (e) {
      setKeepBg(!v);
      toast("error", errorMessage(e));
    }
  };

  return (
    <SettingsCard title="Conexión">
      <SettingRow label="Estado">
        <span className="inline-flex items-center gap-2 text-sm" role="status" data-testid="conn-state" data-link={link}>
          <i className="h-2 w-2 rounded-full" style={{ background: st.color }} aria-hidden="true" />
          {st.label}
        </span>
      </SettingRow>
      {serverVersion && (
        <SettingRow label="Servidor">
          <span className="font-mono text-sm" data-testid="conn-version">
            {serverVersion}
            {apiVersion ? ` · API ${apiVersion}` : ""}
          </span>
        </SettingRow>
      )}
      {mode === "remote" && tlsFingerprint && (
        <div className="px-5 py-3 text-sm">
          <span className="label">Huella TLS</span>
          <span className="block break-all font-mono text-xs text-muted" data-testid="conn-saved-fp">
            {tlsFingerprint}
          </span>
        </div>
      )}
      {warning && (
        <p role="alert" className="px-5 py-3 text-sm" style={{ color: "var(--warn)" }} data-testid="conn-warning">
          {warning}
        </p>
      )}
      <SettingRow label="Modo">
        <div role="radiogroup" aria-label="Modo de conexión" className="inline-flex rounded-lg bg-panel2 p-0.5">
          {(
            [
              ["local", "Local"],
              ["remote", "Remoto"],
            ] as const
          ).map(([id, label]) => (
            <button
              key={id}
              type="button"
              role="radio"
              aria-checked={choice === id}
              onClick={() => setChoice(id)}
              className={cx(
                "rounded-md px-3 py-1 text-sm font-medium",
                choice === id ? "bg-panel shadow-sm" : "text-muted hover:text-ink",
              )}
            >
              {label}
            </button>
          ))}
        </div>
      </SettingRow>

      {choice === "remote" ? (
        <div className="flex flex-col gap-3 px-5 py-4" data-testid="conn-remote">
          <div>
            <label className="label" htmlFor="conn-url">
              Dirección del servidor
            </label>
            <input
              id="conn-url"
              className="input"
              placeholder="https://servidor:8443"
              autoComplete="off"
              value={url}
              onChange={(e) => {
                setUrl(e.target.value);
                setResult(null);
              }}
            />
            {isInsecureRemote(parseServerUrl(url) ?? "") && (
              <p role="alert" data-testid="conn-http-warning" className="mt-1 break-words text-xs" style={{ color: "var(--warn)" }}>
                {INSECURE_HTTP_WARNING}
              </p>
            )}
          </div>
          <div className="grid min-w-0 grid-cols-1 gap-3 min-[420px]:grid-cols-2">
            <div className="min-w-0">
              <label className="label" htmlFor="conn-user">
                Usuario
              </label>
              <input
                id="conn-user"
                className="input"
                autoComplete="username"
                value={user}
                onChange={(e) => {
                  setUser(e.target.value);
                  setResult(null);
                }}
              />
            </div>
            <div className="min-w-0">
              <label className="label" htmlFor="conn-pass">
                Clave
              </label>
              <input
                id="conn-pass"
                className="input"
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(e) => {
                  setPassword(e.target.value);
                  setResult(null);
                }}
              />
            </div>
          </div>

          {error && !tls && (
            <p role="alert" className="break-words text-sm text-danger" data-testid="conn-error">
              {error}
            </p>
          )}
          {tls && (
            <TlsTrust
              message={error}
              onTrusted={async () => {
                toast("success", "Huella guardada");
                await doSave();
              }}
            />
          )}

          {result && (
            <div className="min-w-0 rounded-lg border border-line px-3 py-2 text-sm" data-testid="conn-probe-ok">
              <p className="flex items-center gap-1.5 font-medium" style={{ color: "var(--ok)" }}>
                <ShieldCheck size={15} aria-hidden="true" />
                Conexión correcta
              </p>
              <p className="mt-1 break-words font-mono text-xs text-muted">
                Versión {result.version} · API {result.apiVersion} · {result.tls ? "TLS" : "sin TLS"}
              </p>
              {result.tlsFingerprint && (
                <p className="mt-1 break-all font-mono text-xs text-muted" title={result.tlsFingerprint} data-testid="conn-fp">
                  Huella {result.tlsFingerprint}
                </p>
              )}
              {result.warning && (
                <p className="mt-1" style={{ color: "var(--warn)" }}>
                  {result.warning}
                </p>
              )}
            </div>
          )}

          <div className="flex flex-wrap gap-2">
            {!desktopApp && (
              <button className="btn" onClick={() => void doProbe()} disabled={!canTry}>
                {busy === "probe" && <Loader2 size={15} className="animate-spin" aria-hidden="true" />}
                Probar
              </button>
            )}
            <button className="btn btn-primary" onClick={() => void doSave()} disabled={!canTry}>
              {busy === "save" && <Loader2 size={15} className="animate-spin" aria-hidden="true" />}
              Guardar y conectar
            </button>
            {mode === "remote" && (
              <button className="btn" onClick={() => void logout()} disabled={!!busy}>
                Cerrar sesión
              </button>
            )}
          </div>
        </div>
      ) : (
        mode === "remote" && (
          <div className="flex items-center justify-between gap-3 px-5 py-3">
            <span className="min-w-0 flex-1 truncate text-sm text-muted" title={baseUrl}>
              Ahora usas {baseUrl}
            </span>
            <button className="btn shrink-0" onClick={() => void doLocal()} disabled={!!busy}>
              Usar este equipo
            </button>
          </div>
        )
      )}

      {desktopApp && keepBg !== null && (
        <SettingRow label="Seguir en segundo plano">
          <Switch label="Seguir en segundo plano" checked={keepBg} onChange={(v) => void toggleBg(v)} />
        </SettingRow>
      )}
    </SettingsCard>
  );
}
