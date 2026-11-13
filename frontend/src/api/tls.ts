// Certificados TLS autofirmados: el escritorio no se conecta a un servidor remoto con un certificado sin confirmar y
// devuelve un error en español que termina en «Huella SHA-256: AB:CD:…». Aquí se extrae esa huella.

export interface TlsPrompt {
  /** Huella normalizada: mayúsculas, pares hexadecimales separados por «:». */
  fingerprint: string;
  /** `true` si ya había una huella fijada y el certificado ahora es otro («La huella del certificado de … cambió»). */
  changed: boolean;
}

// 32 bytes (SHA-256) = 64 hexadecimales; se admiten separadores «:», «-» o espacios, o ninguno.
const FP = /huella\s+sha[\s-]?256\s*[:：]?\s*((?:[0-9a-f]{2}[\s:-]?){31}[0-9a-f]{2})/i;

/** Pasa «ab:cd…», «AB CD…» o «abcd…» a «AB:CD:…». Devuelve "" si no son 64 hexadecimales. */
export function normalizeFingerprint(raw: string): string {
  const hex = raw.replace(/[^0-9a-f]/gi, "").toUpperCase();
  if (hex.length !== 64 || /[^0-9A-F]/.test(hex)) return "";
  return hex.match(/.{2}/g)!.join(":");
}

/** Si el mensaje trae una huella de certificado por confirmar, la devuelve; si no, `null`. */
export function parseTlsPrompt(message: string): TlsPrompt | null {
  const m = FP.exec(message);
  if (!m) return null;
  const fingerprint = normalizeFingerprint(m[1]);
  if (!fingerprint) return null;
  return { fingerprint, changed: /cambi[óo]/i.test(message) };
}
