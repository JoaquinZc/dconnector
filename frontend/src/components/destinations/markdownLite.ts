// Lector ligero de Markdown, hecho a mano (sin librerías), para mostrar el documento del protocolo.
// Entiende lo que ese documento usa: títulos, párrafos, listas (con viñetas o numeradas, con
// sangría), tablas simples, bloques de código y, en línea, `código`, **negrita** y *cursiva*.
// No ejecuta HTML: todo se devuelve como datos que la interfaz pinta con React.

export type MdBlock =
  | { type: "heading"; level: number; text: string }
  | { type: "paragraph"; text: string }
  | { type: "list"; items: MdListItem[] }
  | { type: "table"; header: string[]; rows: string[][] }
  | { type: "code"; lang: string; text: string }
  | { type: "rule" };

export interface MdListItem {
  text: string;
  /** Nivel de sangría (0 = el primero). */
  depth: number;
  /** Número de la fuente en listas numeradas; `undefined` en las de viñetas. */
  number?: number;
}

const FENCE = /^\s*```\s*([\w+-]*)\s*$/;
const HEADING = /^(#{1,6})\s+(.*?)\s*#*\s*$/;
const RULE = /^\s*(-{3,}|\*{3,}|_{3,})\s*$/;
const LIST_ITEM = /^(\s*)([-*+]|\d+[.)])\s+(.*)$/;
const TABLE_SEPARATOR = /^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$/;

function isTableStart(lines: string[], i: number): boolean {
  return lines[i].trim().startsWith("|") && i + 1 < lines.length && lines[i + 1].includes("-") && TABLE_SEPARATOR.test(lines[i + 1]);
}

/** Parte una fila de tabla en celdas; un `|` dentro de `código` o escrito `\|` no separa. */
export function splitTableRow(line: string): string[] {
  let s = line.trim();
  if (s.startsWith("|")) s = s.slice(1);
  const cells: string[] = [];
  let cur = "";
  let inCode = false;
  for (let i = 0; i < s.length; i++) {
    const c = s[i];
    if (c === "\\" && s[i + 1] === "|") {
      cur += "|";
      i++;
    } else if (c === "`") {
      inCode = !inCode;
      cur += c;
    } else if (c === "|" && !inCode) {
      cells.push(cur.trim());
      cur = "";
    } else {
      cur += c;
    }
  }
  if (cur.trim() !== "" || s.endsWith("|") === false) cells.push(cur.trim());
  return cells;
}

/** Si la línea empieza un bloque distinto de un párrafo. */
function startsBlock(lines: string[], i: number): boolean {
  const l = lines[i];
  return FENCE.test(l) || HEADING.test(l) || RULE.test(l) || LIST_ITEM.test(l) || isTableStart(lines, i);
}

export function parseMarkdown(src: string): MdBlock[] {
  const lines = src.replace(/\r\n?/g, "\n").split("\n");
  const out: MdBlock[] = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    if (line.trim() === "") {
      i++;
      continue;
    }
    const fence = FENCE.exec(line);
    if (fence) {
      const body: string[] = [];
      i++;
      while (i < lines.length && !/^\s*```\s*$/.test(lines[i])) body.push(lines[i++]);
      i++; // la cerca de cierre (si falta, se acabó el documento)
      out.push({ type: "code", lang: fence[1] ?? "", text: body.join("\n") });
      continue;
    }
    const h = HEADING.exec(line);
    if (h) {
      out.push({ type: "heading", level: h[1].length, text: h[2] });
      i++;
      continue;
    }
    if (RULE.test(line) && !LIST_ITEM.test(line)) {
      out.push({ type: "rule" });
      i++;
      continue;
    }
    if (isTableStart(lines, i)) {
      const header = splitTableRow(lines[i]);
      i += 2;
      const rows: string[][] = [];
      while (i < lines.length && lines[i].trim().startsWith("|")) rows.push(splitTableRow(lines[i++]));
      out.push({ type: "table", header, rows });
      continue;
    }
    if (LIST_ITEM.test(line)) {
      const items: MdListItem[] = [];
      while (i < lines.length) {
        const m = LIST_ITEM.exec(lines[i]);
        if (m) {
          const num = /^\d+/.exec(m[2]);
          items.push({ text: m[3].trim(), depth: Math.floor(m[1].replace(/\t/g, "  ").length / 2), number: num ? Number(num[0]) : undefined });
          i++;
          continue;
        }
        // Línea sangrada que sigue a un elemento: es continuación de su texto.
        if (lines[i].trim() !== "" && /^\s{2,}\S/.test(lines[i]) && !startsBlock(lines, i) && items.length > 0) {
          items[items.length - 1].text += " " + lines[i].trim();
          i++;
          continue;
        }
        // Línea en blanco entre elementos: la lista sigue si lo siguiente es otro elemento.
        if (lines[i].trim() === "") {
          let j = i;
          while (j < lines.length && lines[j].trim() === "") j++;
          if (j < lines.length && LIST_ITEM.test(lines[j])) {
            i = j;
            continue;
          }
        }
        break;
      }
      out.push({ type: "list", items });
      continue;
    }
    // Párrafo: hasta una línea en blanco o el inicio de otro bloque.
    const para: string[] = [line.trim()];
    i++;
    while (i < lines.length && lines[i].trim() !== "" && !startsBlock(lines, i)) para.push(lines[i++].trim());
    out.push({ type: "paragraph", text: para.join(" ") });
  }
  return out;
}

// ---------------------------------------------------------------- en línea

export type InlineNode =
  | { kind: "text"; text: string }
  | { kind: "code"; text: string }
  | { kind: "bold"; children: InlineNode[] }
  | { kind: "italic"; children: InlineNode[] };

export function parseInline(s: string): InlineNode[] {
  const out: InlineNode[] = [];
  let buf = "";
  const flush = () => {
    if (buf) out.push({ kind: "text", text: buf });
    buf = "";
  };
  let i = 0;
  while (i < s.length) {
    const c = s[i];
    if (c === "`") {
      const end = s.indexOf("`", i + 1);
      if (end > i) {
        flush();
        out.push({ kind: "code", text: s.slice(i + 1, end) });
        i = end + 1;
        continue;
      }
    } else if (c === "*" && s[i + 1] === "*") {
      const end = s.indexOf("**", i + 2);
      if (end > i + 2) {
        flush();
        out.push({ kind: "bold", children: parseInline(s.slice(i + 2, end)) });
        i = end + 2;
        continue;
      }
    } else if (c === "*" && s[i + 1] !== undefined && !/\s/.test(s[i + 1])) {
      let end = s.indexOf("*", i + 1);
      while (end > 0 && /\s/.test(s[end - 1])) end = s.indexOf("*", end + 1);
      if (end > i + 1) {
        flush();
        out.push({ kind: "italic", children: parseInline(s.slice(i + 1, end)) });
        i = end + 1;
        continue;
      }
    }
    buf += c;
    i++;
  }
  flush();
  return out;
}

// ---------------------------------------------------------------- búsqueda

/** Una letra sin tilde ni mayúsculas (para buscar «conexion» y encontrar «Conexión»). */
function fold(ch: string): string {
  return ch.normalize("NFD").replace(/\p{M}/gu, "").toLowerCase();
}

/**
 * Posiciones `[inicio, fin)` de cada aparición de `query` en `text`, sin distinguir mayúsculas ni
 * tildes. Las apariciones no se solapan. Una búsqueda vacía no encuentra nada.
 */
export function findMatches(text: string, query: string): Array<[number, number]> {
  const q = [...query.trim()].map(fold).join("");
  if (!q) return [];
  let folded = "";
  const map: number[] = []; // índice en `folded` → índice en `text`
  for (let i = 0; i < text.length; i++) {
    const f = fold(text[i]);
    for (let k = 0; k < f.length; k++) map.push(i);
    folded += f;
  }
  const hits: Array<[number, number]> = [];
  let from = 0;
  for (;;) {
    const at = folded.indexOf(q, from);
    if (at < 0) break;
    const start = map[at];
    const end = map[at + q.length - 1] + 1;
    hits.push([start, end]);
    from = at + q.length;
  }
  return hits;
}

/** Texto en piezas, marcando cuáles son aparición de la búsqueda. */
export function splitByQuery(text: string, query: string): Array<{ text: string; hit: boolean }> {
  const hits = findMatches(text, query);
  if (hits.length === 0) return [{ text, hit: false }];
  const out: Array<{ text: string; hit: boolean }> = [];
  let pos = 0;
  for (const [a, b] of hits) {
    if (a > pos) out.push({ text: text.slice(pos, a), hit: false });
    out.push({ text: text.slice(a, b), hit: true });
    pos = b;
  }
  if (pos < text.length) out.push({ text: text.slice(pos), hit: false });
  return out;
}
