// Compara el catálogo RPC del servidor (fachadas Go) con los tipos de `svcTypes.ts`: mismos servicios, mismos
// nombres de método, misma cantidad de argumentos (sin el `ctx`) y mismo «devuelve algo / no devuelve nada».
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

const FACADE_DIR = join(__dirname, "../../../internal/adapter/api/facade");
const TS_FILE = join(__dirname, "svcTypes.ts");

interface Sig {
  args: number;
  returns: boolean;
}

/** Cuenta los elementos separados por comas del nivel superior. */
function countTop(list: string): number {
  let depth = 0;
  let n = 0;
  let seen = false;
  for (const ch of list) {
    if ("([{<".includes(ch)) depth++;
    else if (")]}>".includes(ch)) depth--;
    if (ch === "," && depth === 0) {
      if (seen) n++;
      seen = false;
    } else if (!/\s/.test(ch)) seen = true;
  }
  return seen ? n + 1 : n;
}

/** Servicios Go: tipo receptor → método → firma. Solo métodos exportados cuyo primer argumento es `ctx`. */
function goCatalog(): Record<string, Record<string, Sig>> {
  const out: Record<string, Record<string, Sig>> = {};
  for (const f of readdirSync(FACADE_DIR).filter((n) => n.endsWith(".go") && !n.endsWith("_test.go"))) {
    const src = readFileSync(join(FACADE_DIR, f), "utf8");
    const re = /func \(\w+ \*(\w+)\) ([A-Z]\w*)\(\s*(?:ctx context\.Context\s*,?)?([^)]*)\)\s*([^{\n]*)\{/g;
    for (const m of src.matchAll(re)) {
      const [, recv, name, params = "", ret] = m;
      const r = ret.trim();
      (out[recv] ??= {})[name] = { args: countTop(params), returns: r !== "error" && r !== "" };
    }
  }
  return out;
}

function tsCatalog(): Record<string, Record<string, Sig>> {
  const src = readFileSync(TS_FILE, "utf8");
  const out: Record<string, Record<string, Sig>> = {};
  for (const blk of src.matchAll(/export interface (\w+)Svc \{([\s\S]*?)\n\}/g)) {
    const methods: Record<string, Sig> = {};
    for (const m of blk[2].matchAll(/^\s+(\w+)\((.*)\): PromiseLike<(.*)>;$/gm)) {
      methods[m[1]] = { args: countTop(m[2]), returns: m[3] !== "void" };
    }
    out[blk[1]] = methods;
  }
  return out;
}

/** Métodos que resuelve el escritorio (no el servidor) y por eso solo existen en TS, o al revés. */
const TS_ONLY: Record<string, string[]> = { Messaging: ["SaveMediaAs"] };

describe("catálogo RPC: svcTypes.ts frente a las fachadas Go", () => {
  const go = goCatalog();
  const ts = tsCatalog();

  it("lee las fachadas (la prueba no se queda vacía)", () => {
    expect(Object.keys(go).length).toBeGreaterThanOrEqual(10);
    expect(Object.keys(ts).length).toBeGreaterThanOrEqual(10);
  });

  it("cada servicio de TS existe en Go y al revés", () => {
    const goServices = Object.keys(go).filter((k) => k !== "Facade");
    for (const s of Object.keys(ts)) expect(go[s], `servicio ${s} sin fachada Go`).toBeDefined();
    for (const s of goServices) expect(ts[s], `fachada Go ${s} sin tipo en svcTypes.ts`).toBeDefined();
  });

  for (const svc of Object.keys(ts)) {
    it(`${svc}: nombres, argumentos y retorno idénticos`, () => {
      const g = go[svc] ?? {};
      const t = ts[svc];
      const onlyTs = TS_ONLY[svc] ?? [];
      for (const [name, sig] of Object.entries(t)) {
        if (onlyTs.includes(name)) continue;
        expect(g[name], `${svc}.${name} no existe en Go`).toBeDefined();
        expect(sig.args, `${svc}.${name}: número de argumentos`).toBe(g[name].args);
        expect(sig.returns, `${svc}.${name}: devuelve valor`).toBe(g[name].returns);
      }
      for (const name of Object.keys(g)) expect(t[name] ?? (onlyTs.includes(name) ? 1 : undefined), `${svc}.${name} falta en svcTypes.ts`).toBeDefined();
    });
  }
});
