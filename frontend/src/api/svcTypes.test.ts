// Vigila que los tipos de la interfaz no dependan de los enlaces regenerables de Wails (`frontend/bindings`).
// `wails3 generate bindings -clean` borra todo lo que el escritorio no registra: los modelos del núcleo viven en
// `src/api/core` y los servicios del servidor se tipan en `svcTypes.ts`.
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

function walk(dir: string): string[] {
  return readdirSync(dir).flatMap((n) => {
    const p = join(dir, n);
    return statSync(p).isDirectory() ? walk(p) : [p];
  });
}

describe("tipos independientes de los enlaces", () => {
  const files = walk(join(__dirname, "..")).filter((f) => /\.(ts|tsx)$/.test(f));

  it("nada de src importa los modelos del núcleo desde bindings", () => {
    const bad = files.filter((f) => /from\s+"[^"]*bindings\/dconnector\/internal\/core\//.test(readFileSync(f, "utf8")));
    expect(bad).toEqual([]);
  });

  it("solo se importan de bindings los servicios del escritorio (wails)", () => {
    const re = /from\s+"[^"]*bindings\/dconnector\/internal\/adapter\/ui\/wails[^"]*"/;
    const wrongName = /\b(Accounts|Inbox|Messaging|Statuses|Contacts|Campaigns|Scheduled|Stats|Destinations|History)Service\b/;
    for (const f of files) {
      const s = readFileSync(f, "utf8");
      if (re.test(s)) expect(wrongName.test(s), f).toBe(false);
    }
  });

  it("los modelos del núcleo están versionados en src/api/core", () => {
    for (const d of ["model", "service", "campaign", "capability", "stats"]) {
      expect(statSync(join(__dirname, "core", d, "models.ts")).isFile()).toBe(true);
    }
  });
});
