// Pantallas de conexión: arranque, «Sin conexión» con reintento, inicio de sesión y la banda de aviso.
// La app nunca queda en blanco: si el servidor cae, siempre hay algo que leer y un botón que pulsar.
import { useState, type FormEvent, type ReactNode } from "react";
import { Loader2, LogIn, PlugZap, WifiOff } from "lucide-react";
import { errorMessage } from "@/api";
import { useConnection } from "@/api/connection";
import { hasWails } from "@/api/desktop";
import { parseTlsPrompt } from "@/api/tls";
import DesktopStatusLine from "./DesktopStatusLine";
import TlsTrust from "./TlsTrust";

function Frame({ children, label }: { children: ReactNode; label: string }) {
  return (
    <div
      role="region"
      aria-label={label}
      className="flex h-full w-full items-center justify-center overflow-y-auto bg-bg p-4 text-ink"
    >
      <div className="card w-full max-w-sm p-6" style={{ boxShadow: "var(--shadow)" }}>
        {children}
      </div>
    </div>
  );
}

/** Mientras se conecta por primera vez. */
export function ConnectingScreen() {
  return (
    <Frame label="Conectando">
      <div role="status" className="flex items-center gap-3">
        <Loader2 className="shrink-0 animate-spin text-muted" size={22} aria-hidden="true" />
        <span className="min-w-0 break-words">Conectando con el servidor…</span>
      </div>
      <DesktopStatusLine className="mt-2 block break-words text-sm text-muted" />
    </Frame>
  );
}

/** El servidor no responde y aún no hubo conexión buena. */
export function OfflineScreen() {
  const error = useConnection((s) => s.error);
  const baseUrl = useConnection((s) => s.baseUrl);
  const mode = useConnection((s) => s.mode);
  const booting = useConnection((s) => s.booting);
  const retry = useConnection((s) => s.retry);
  const useLocal = useConnection((s) => s.useLocal);
  const tls = !!parseTlsPrompt(error) && hasWails();
  return (
    <Frame label="Sin conexión">
      <div className="flex items-start gap-3">
        <WifiOff className="mt-0.5 shrink-0 text-danger" size={22} aria-hidden="true" />
        <div className="min-w-0 flex-1">
          <h1 className="text-base font-semibold">Sin conexión</h1>
          <p className="mt-1 break-words text-sm text-muted" data-testid="offline-error">
            {tls ? "No se pudo conectar de forma segura." : error || "No se pudo conectar con el servidor."}
          </p>
          <DesktopStatusLine className="mt-1 block break-words text-sm text-muted" />
          {baseUrl && (
            <p className="mt-1 truncate font-mono text-xs text-muted" title={baseUrl}>
              {baseUrl}
            </p>
          )}
        </div>
      </div>
      {tls && (
        <div className="mt-3">
          <TlsTrust message={error} onTrusted={() => retry()} />
        </div>
      )}
      <div className="mt-5 flex flex-wrap gap-2">
        <button className="btn btn-primary" onClick={() => void retry()} disabled={booting}>
          {booting && <Loader2 size={15} className="animate-spin" aria-hidden="true" />}
          Reintentar
        </button>
        {mode === "remote" && hasWails() && (
          <button className="btn" onClick={() => void useLocal().catch(() => undefined)}>
            Usar este equipo
          </button>
        )}
      </div>
    </Frame>
  );
}

/** Inicio de sesión (el servidor devolvió 401 y no hay forma de renovar la sesión sola). */
export function LoginScreen({ overlay = false }: { overlay?: boolean }) {
  const baseUrl = useConnection((s) => s.baseUrl);
  const savedUser = useConnection((s) => s.user);
  const login = useConnection((s) => s.login);
  const mode = useConnection((s) => s.mode);
  const useLocal = useConnection((s) => s.useLocal);
  const [user, setUser] = useState(savedUser);
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy || !user.trim() || !password) return;
    setBusy(true);
    setError("");
    try {
      await login(user, password);
      setPassword("");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const body = (
    <form onSubmit={(e) => void submit(e)} aria-label="Iniciar sesión">
      <div className="flex items-center gap-2">
        <LogIn size={20} className="shrink-0 text-muted" aria-hidden="true" />
        <h1 className="text-base font-semibold">Iniciar sesión</h1>
      </div>
      {baseUrl && (
        <p className="mt-1 truncate font-mono text-xs text-muted" title={baseUrl}>
          {baseUrl}
        </p>
      )}
      <div className="mt-4 flex flex-col gap-3">
        <div>
          <label className="label" htmlFor="login-user">
            Usuario
          </label>
          <input
            id="login-user"
            className="input"
            autoComplete="username"
            autoFocus
            value={user}
            onChange={(e) => setUser(e.target.value)}
          />
        </div>
        <div>
          <label className="label" htmlFor="login-pass">
            Clave
          </label>
          <input
            id="login-pass"
            className="input"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </div>
      </div>
      {error && (
        <p role="alert" className="mt-3 break-words text-sm text-danger" data-testid="login-error">
          {error}
        </p>
      )}
      <div className="mt-5 flex flex-wrap gap-2">
        <button type="submit" className="btn btn-primary" disabled={busy || !user.trim() || !password}>
          {busy && <Loader2 size={15} className="animate-spin" aria-hidden="true" />}
          Entrar
        </button>
        {mode === "remote" && hasWails() && (
          <button type="button" className="btn" onClick={() => void useLocal().catch(() => undefined)}>
            Usar este equipo
          </button>
        )}
      </div>
    </form>
  );

  if (!overlay) return <Frame label="Iniciar sesión">{body}</Frame>;
  return (
    <div className="fixed inset-0 z-[80]">
      <Frame label="Iniciar sesión">{body}</Frame>
    </div>
  );
}

/** Banda sobre la interfaz cuando se pierde el enlace en vivo (los datos que ya hay siguen a la vista). */
export function ConnectionBanner() {
  const link = useConnection((s) => s.link);
  const retry = useConnection((s) => s.retry);
  const d = useConnection((s) => s.desktop);
  const desktopBad = !!d && d.state !== "ready";
  if (!desktopBad && (link === "connected" || link === "connecting")) return null;
  const offline = link === "offline" || (desktopBad && d.state !== "starting" && d.state !== "restarting");
  return (
    <div
      role="status"
      data-testid="conn-banner"
      data-link={link}
      className="fixed inset-x-0 top-0 z-[70] flex items-center justify-center gap-3 px-3 py-1.5 text-sm"
      style={{
        background: offline ? "var(--danger)" : "var(--warn)",
        color: offline ? "#fff" : "#1b1400",
      }}
    >
      {offline ? <WifiOff size={15} aria-hidden="true" /> : <PlugZap size={15} aria-hidden="true" />}
      <span className="min-w-0 break-words text-center" title={d?.error || undefined}>
        {desktopBad ? (
          <DesktopStatusLine />
        ) : offline ? (
          "Sin conexión con el servidor."
        ) : (
          "Reconectando con el servidor…"
        )}
      </span>
      <button
        className="shrink-0 rounded-md border border-current px-2 py-0.5 text-xs font-medium hover:opacity-80"
        onClick={() => void retry()}
      >
        Reintentar
      </button>
    </div>
  );
}
