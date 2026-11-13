import { describe, expect, it } from "vitest";
import { normalizeFingerprint, parseTlsPrompt } from "./tls";

const HEX = "0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF";
const COLON = HEX.match(/.{2}/g)!.join(":");

describe("parseTlsPrompt", () => {
  it("extrae la huella del error de certificado sin confiar", () => {
    const msg =
      "El servidor usa un certificado que no es de confianza todavía. Compara la huella con la que imprime el servidor al arrancar y, si coincide, confía en ella. Huella SHA-256: " +
      COLON;
    expect(parseTlsPrompt(msg)).toEqual({ fingerprint: COLON, changed: false });
  });

  it("detecta que la huella cambió", () => {
    const msg = `La huella del certificado de srv:8443 cambió: puede que el servidor renovara su certificado. Por seguridad no se conectó. Huella SHA-256: ${COLON}`;
    expect(parseTlsPrompt(msg)).toEqual({ fingerprint: COLON, changed: true });
  });

  it("acepta minúsculas, sin dos puntos, con guiones o espacios y envuelta en JSON de Wails", () => {
    expect(parseTlsPrompt(`huella sha-256: ${COLON.toLowerCase()}`)?.fingerprint).toBe(COLON);
    expect(parseTlsPrompt(`Huella SHA256 ${HEX.toLowerCase()}`)?.fingerprint).toBe(COLON);
    expect(parseTlsPrompt(`Huella SHA-256: ${COLON.replace(/:/g, "-")}.`)?.fingerprint).toBe(COLON);
    expect(parseTlsPrompt(`Huella SHA-256: ${COLON.replace(/:/g, " ")}`)?.fingerprint).toBe(COLON);
  });

  it("sin huella o con una huella incompleta no hay aviso", () => {
    expect(parseTlsPrompt("No se pudo conectar con el servidor.")).toBeNull();
    expect(parseTlsPrompt("Huella SHA-256: AB:CD:EF")).toBeNull();
    expect(parseTlsPrompt("")).toBeNull();
  });
});

describe("normalizeFingerprint", () => {
  it("normaliza o rechaza", () => {
    expect(normalizeFingerprint(HEX.toLowerCase())).toBe(COLON);
    expect(normalizeFingerprint("AB:CD")).toBe("");
    expect(normalizeFingerprint("zz".repeat(32))).toBe("");
  });
});
