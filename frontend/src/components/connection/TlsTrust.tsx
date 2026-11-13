import { useState } from "react";
import { Loader2, ShieldAlert } from "lucide-react";
import { errorMessage } from "@/api";
import { desktop, hasWails } from "@/api/desktop";
import { parseTlsPrompt } from "@/api/tls";

/**
 * Aviso del certificado TLS por confirmar. Lee la huella del error que devuelve el escritorio («… Huella SHA-256:
 * AB:CD:…») y deja confiar en ella; `onTrusted` reintenta la conexión. No pinta nada si el error no trae huella o
 * si no hay escritorio (el navegador gestiona sus propios certificados).
 */
export default function TlsTrust({ message, onTrusted }: { message: string; onTrusted: () => void | Promise<void> }) {
  const prompt = parseTlsPrompt(message);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  if (!prompt || !hasWails()) return null;

  const trust = async () => {
    setBusy(true);
    setError("");
    try {
      await desktop.tlsTrust(prompt.fingerprint);
      await onTrusted();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div
      role="alert"
      data-testid="tls-trust"
      data-changed={prompt.changed ? "1" : "0"}
      className="min-w-0 rounded-lg border px-3 py-2 text-sm"
      style={{ borderColor: prompt.changed ? "var(--danger)" : "var(--warn)" }}
    >
      <p className="flex items-start gap-1.5 font-medium">
        <ShieldAlert size={16} className="mt-0.5 shrink-0" aria-hidden="true" />
        <span className="min-w-0 break-words">
          {prompt.changed ? "El certificado del servidor cambió" : "Certificado sin confirmar"}
        </span>
      </p>
      <p className="mt-1 break-words text-muted">
        {prompt.changed
          ? "Si lo renovaste tú, confía en la huella nueva. Si no, no continúes."
          : "Compárala con la que muestra el servidor al arrancar."}
      </p>
      <p className="mt-1 break-all font-mono text-xs" data-testid="tls-fingerprint" title={prompt.fingerprint}>
        {prompt.fingerprint}
      </p>
      {error && (
        <p className="mt-1 break-words text-danger" data-testid="tls-error">
          {error}
        </p>
      )}
      <button className="btn mt-2" onClick={() => void trust()} disabled={busy}>
        {busy && <Loader2 size={15} className="animate-spin" aria-hidden="true" />}
        Confiar en este certificado
      </button>
    </div>
  );
}
