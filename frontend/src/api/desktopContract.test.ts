// Contrato entre la interfaz y los servicios de escritorio de Go: cada llamada por nombre (`callWails` / `desktopCall`)
// debe pasar EXACTAMENTE los argumentos que declara el método de Go (sin el `context.Context`, que Wails inyecta).
// Nació de un fallo real: `SaveRemote` recibía un objeto y Go esperaba (url, user, password).
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

const ROOT = join(__dirname, "..", "..", "..");
const GO_DIR = join(ROOT, "internal", "adapter", "ui", "wails");
const TS_DIR = __dirname;

/** Cuenta argumentos de nivel superior de un literal `[a, b, c]` (vacío = 0). */
function countArgs(literal: string | undefined): number {
  if (!literal) return 0;
  const inner = literal.trim().slice(1, -1).trim();
  if (!inner) return 0;
  let depth = 0;
  let n = 1;
  for (const ch of inner) {
    if ("([{".includes(ch)) depth++;
    else if (")]}".includes(ch)) depth--;
    else if (ch === "," && depth === 0) n++;
  }
  return n;
}

/** servicio -> método -> nº de parámetros (sin ctx), leído de los .go reales. */
function goSignatures(): Map<string, Map<string, number>> {
  const out = new Map<string, Map<string, number>>();
  for (const f of readdirSync(GO_DIR)) {
    if (!f.endsWith(".go") || f.endsWith("_test.go")) continue;
    const src = readFileSync(join(GO_DIR, f), "utf8");
    for (const m of src.matchAll(/func \(\w+ \*(\w+)Service\) ([A-Z]\w*)\(([^)]*)\)/g)) {
      const [, svc, method, params] = m;
      const parts = params.split(",").map((p) => p.trim()).filter(Boolean);
      const n = parts.length > 0 && /^ctx\s+context\.Context/.test(parts[0]) ? parts.length - 1 : parts.length;
      if (!out.has(svc)) out.set(svc, new Map());
      out.get(svc)!.set(method, n);
    }
  }
  return out;
}

function tsCallSites(): Array<{ file: string; service: string; method: string; args: number }> {
  const sites: Array<{ file: string; service: string; method: string; args: number }> = [];
  const re = /(?:callWails|desktopCall)(?:<[^>]*>)?\(\s*"(\w+)"\s*,\s*"(\w+)"\s*(?:,\s*(\[[^\]]*\]))?\s*\)/g;
  for (const f of readdirSync(TS_DIR)) {
    if (!f.endsWith(".ts") || f.endsWith(".test.ts")) continue;
    const src = readFileSync(join(TS_DIR, f), "utf8");
    for (const m of src.matchAll(re)) sites.push({ file: f, service: m[1], method: m[2], args: countArgs(m[3]) });
  }
  return sites;
}

describe("contrato interfaz ↔ servicios de escritorio (Go)", () => {
  const sigs = goSignatures();
  const sites = tsCallSites();

  it("lee los servicios de escritorio y encuentra llamadas", () => {
    for (const s of ["Desktop", "Files", "Notifications", "Settings", "Update"]) expect(sigs.has(s), s).toBe(true);
    expect(sites.length).toBeGreaterThan(10);
  });

  it("cada llamada literal existe en Go y pasa los argumentos exactos", () => {
    const problemas: string[] = [];
    for (const s of sites) {
      const n = sigs.get(s.service)?.get(s.method);
      if (n === undefined) problemas.push(`${s.file}: ${s.service}.${s.method} no existe en Go`);
      else if (n !== s.args) problemas.push(`${s.file}: ${s.service}.${s.method} recibe ${n} argumento(s) en Go y la interfaz pasa ${s.args}`);
    }
    expect(problemas).toEqual([]);
  });
});
