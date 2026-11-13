import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { findMatches, parseInline, parseMarkdown, splitByQuery, splitTableRow } from "./markdownLite";

describe("parseMarkdown", () => {
  it("títulos, párrafos (las líneas seguidas se unen) y regla", () => {
    expect(parseMarkdown("# Uno\n\nlínea a\nlínea b\n\n---\n\n### Tres")).toEqual([
      { type: "heading", level: 1, text: "Uno" },
      { type: "paragraph", text: "línea a línea b" },
      { type: "rule" },
      { type: "heading", level: 3, text: "Tres" },
    ]);
  });

  it("bloques de código conservan el texto y el lenguaje; sin cerrar llegan al final", () => {
    expect(parseMarkdown("```json\n{ \"a\": 1 }\n\n  x\n```\nfin")).toEqual([
      { type: "code", lang: "json", text: '{ "a": 1 }\n\n  x' },
      { type: "paragraph", text: "fin" },
    ]);
    expect(parseMarkdown("```\nsin cierre\nmás")).toEqual([{ type: "code", lang: "", text: "sin cierre\nmás" }]);
    // Un título dentro del código no es un título.
    expect(parseMarkdown("```\n# no soy título\n```")).toEqual([{ type: "code", lang: "", text: "# no soy título" }]);
  });

  it("listas con viñetas, sangría y continuación", () => {
    const [list] = parseMarkdown("- uno\n  sigue\n- dos\n  - hijo\n- tres");
    expect(list).toEqual({
      type: "list",
      items: [
        { text: "uno sigue", depth: 0, number: undefined },
        { text: "dos", depth: 0, number: undefined },
        { text: "hijo", depth: 1, number: undefined },
        { text: "tres", depth: 0, number: undefined },
      ],
    });
  });

  it("listas numeradas conservan los números de la fuente, también tras un bloque", () => {
    const blocks = parseMarkdown("1. a\n2. b:\n\n```\nx\n```\n\n   trozo\n3. c");
    expect(blocks.map((b) => b.type)).toEqual(["list", "code", "paragraph", "list"]);
    expect(blocks[0]).toMatchObject({ items: [{ number: 1 }, { number: 2 }] });
    expect(blocks[3]).toMatchObject({ items: [{ text: "c", number: 3 }] });
  });

  it("una línea en blanco entre elementos no corta la lista", () => {
    const blocks = parseMarkdown("- a\n\n- b");
    expect(blocks).toHaveLength(1);
    expect(blocks[0]).toMatchObject({ type: "list", items: [{ text: "a" }, { text: "b" }] });
  });

  it("tablas simples", () => {
    const [t] = parseMarkdown("| Campo | Significado |\n| --- | --- |\n| `v` | versión |\n| `id` | el id |\n\ntexto");
    expect(t).toEqual({
      type: "table",
      header: ["Campo", "Significado"],
      rows: [
        ["`v`", "versión"],
        ["`id`", "el id"],
      ],
    });
  });

  it("splitTableRow no parte por un | dentro de `código` ni por \\|", () => {
    expect(splitTableRow("| `a|b` | c \\| d | e |")).toEqual(["`a|b`", "c | d", "e"]);
    expect(splitTableRow("a | b")).toEqual(["a", "b"]);
  });

  it("un texto vacío no da bloques", () => {
    expect(parseMarkdown("")).toEqual([]);
    expect(parseMarkdown("\n\n  \n")).toEqual([]);
  });

  it("entiende los saltos de línea de Windows", () => {
    expect(parseMarkdown("# A\r\n\r\ntexto\r\n")).toEqual([
      { type: "heading", level: 1, text: "A" },
      { type: "paragraph", text: "texto" },
    ]);
  });
});

describe("parseInline", () => {
  it("código, negrita y cursiva", () => {
    expect(parseInline("a `b` **c** *d* e")).toEqual([
      { kind: "text", text: "a " },
      { kind: "code", text: "b" },
      { kind: "text", text: " " },
      { kind: "bold", children: [{ kind: "text", text: "c" }] },
      { kind: "text", text: " " },
      { kind: "italic", children: [{ kind: "text", text: "d" }] },
      { kind: "text", text: " e" },
    ]);
  });

  it("lo que no cierra queda como texto", () => {
    expect(parseInline("2 * 3 y `sin cierre")).toEqual([{ kind: "text", text: "2 * 3 y `sin cierre" }]);
  });

  it("el código no interpreta lo de dentro", () => {
    expect(parseInline("`**x**`")).toEqual([{ kind: "code", text: "**x**" }]);
  });
});

describe("búsqueda", () => {
  it("no distingue mayúsculas ni tildes", () => {
    expect(findMatches("Conexión y conexion", "CONEXION")).toEqual([
      [0, 8],
      [11, 19],
    ]);
    expect(findMatches("abc", "")).toEqual([]);
    expect(findMatches("abc", "   ")).toEqual([]);
    expect(findMatches("abc", "z")).toEqual([]);
  });

  it("splitByQuery parte el texto en piezas", () => {
    expect(splitByQuery("a-ack-b-ack", "ack")).toEqual([
      { text: "a-", hit: false },
      { text: "ack", hit: true },
      { text: "-b-", hit: false },
      { text: "ack", hit: true },
    ]);
    expect(splitByQuery("nada", "x")).toEqual([{ text: "nada", hit: false }]);
  });
});

describe("el documento real del protocolo", () => {
  const doc = readFileSync(resolve(__dirname, "../../../../docs/PROTOCOLO-v1.md"), "utf8");
  const blocks = parseMarkdown(doc);

  it("se entiende entero: título, diez secciones, tablas y código", () => {
    expect(blocks[0]).toEqual({ type: "heading", level: 1, text: "Protocolo dConnector v1" });
    expect(blocks.filter((b) => b.type === "heading" && b.level === 2)).toHaveLength(10);
    expect(blocks.filter((b) => b.type === "table").length).toBeGreaterThanOrEqual(5);
    expect(blocks.filter((b) => b.type === "code").length).toBeGreaterThanOrEqual(8);
  });

  it("las filas de las tablas tienen tantas celdas como su cabecera", () => {
    for (const b of blocks) {
      if (b.type !== "table") continue;
      for (const r of b.rows) expect(r.length, r.join(" | ")).toBe(b.header.length);
    }
  });

  it("los bloques de código no pierden ni una línea", () => {
    const fences = doc.split("\n").filter((l) => l.trim().startsWith("```")).length;
    expect(fences % 2).toBe(0);
    expect(blocks.filter((b) => b.type === "code")).toHaveLength(fences / 2);
  });

  it("documenta contactScope", () => {
    expect(doc).toContain("contactScope");
  });
});
