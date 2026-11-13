// Formato de texto de WhatsApp: *negrita*, _cursiva_, ~tachado~, `mono` (y ```bloque```), y enlaces
// y correos clicables. Devuelve nodos puros (sin HTML): el componente <WaText> los pinta con React,
// así que nunca hace falta dangerouslySetInnerHTML.
//
// Los marcadores deben pegar al texto (sin espacio por dentro) y no partir palabras. Portado de
// ../dconnector/src/shared/waFormat.ts.

export type WaNode =
  | { type: "text"; text: string }
  | { type: "bold" | "italic" | "strike"; children: WaNode[] }
  | { type: "mono"; text: string }
  | { type: "link"; text: string; href: string };

const LINK_RE =
  /(https?:\/\/[^\s<>"']+[^\s<>"'.,;:!?)\]]|www\.[^\s<>"']+[^\s<>"'.,;:!?)\]]|[\w.+-]+@[\w-]+(?:\.[\w-]+)+)/gi;

const MARKERS: Record<string, "bold" | "italic" | "strike"> = {
  "*": "bold",
  _: "italic",
  "~": "strike",
};

function isBoundary(ch: string | undefined): boolean {
  return ch === undefined || /[\s.,;:!?¡¿()[\]{}"'«»\-*_~`]/.test(ch);
}

/** Formato en línea (sin enlaces). */
function parseInline(text: string): WaNode[] {
  const out: WaNode[] = [];
  let buffer = "";
  let i = 0;
  const flush = () => {
    if (buffer) out.push({ type: "text", text: buffer });
    buffer = "";
  };
  while (i < text.length) {
    // ```bloque monoespaciado```
    if (text.startsWith("```", i)) {
      const end = text.indexOf("```", i + 3);
      if (end > i + 3) {
        flush();
        out.push({ type: "mono", text: text.slice(i + 3, end) });
        i = end + 3;
        continue;
      }
    }
    const ch = text[i];
    // `mono` en una sola línea
    if (ch === "`" && isBoundary(text[i - 1]) && text[i + 1] && !/[\s`]/.test(text[i + 1])) {
      let j = i + 1;
      let close = -1;
      while (j < text.length && text[j] !== "\n") {
        if (text[j] === "`" && !/\s/.test(text[j - 1]) && isBoundary(text[j + 1])) {
          close = j;
          break;
        }
        j++;
      }
      if (close > i + 1) {
        flush();
        out.push({ type: "mono", text: text.slice(i + 1, close) });
        i = close + 1;
        continue;
      }
    }
    const kind = MARKERS[ch];
    if (kind && isBoundary(text[i - 1]) && text[i + 1] && !/\s/.test(text[i + 1])) {
      // Cierre en la misma línea, pegado al texto y seguido de un límite de palabra
      let j = i + 1;
      let close = -1;
      while (j < text.length && text[j] !== "\n") {
        if (text[j] === ch && !/\s/.test(text[j - 1]) && isBoundary(text[j + 1])) {
          close = j;
          break;
        }
        j++;
      }
      if (close > i + 1) {
        flush();
        out.push({ type: kind, children: parseInline(text.slice(i + 1, close)) });
        i = close + 1;
        continue;
      }
    }
    buffer += ch;
    i++;
  }
  flush();
  return out;
}

/** Texto → nodos con formato y enlaces. */
export function parseWhatsApp(input: string): WaNode[] {
  const text = input || "";
  const out: WaNode[] = [];
  let last = 0;
  for (const match of text.matchAll(LINK_RE)) {
    const start = match.index ?? 0;
    if (start > last) out.push(...parseInline(text.slice(last, start)));
    const raw = match[0];
    const href =
      raw.includes("@") && !raw.includes("/")
        ? `mailto:${raw}`
        : raw.startsWith("www.")
          ? `https://${raw}`
          : raw;
    out.push({ type: "link", text: raw, href });
    last = start + raw.length;
  }
  if (last < text.length) out.push(...parseInline(text.slice(last)));
  return out;
}

/** Envuelve la selección con un marcador de formato (los espacios de los extremos quedan fuera). */
export function wrapSelection(
  value: string,
  start: number,
  end: number,
  marker: string,
): { value: string; selectionStart: number; selectionEnd: number } {
  const selected = value.slice(start, end);
  const lead = selected.match(/^\s*/)![0];
  const trail = selected.match(/\s*$/)![0];
  const core = selected.slice(lead.length, selected.length - trail.length);
  const wrapped = `${lead}${marker}${core}${marker}${trail}`;
  const next = value.slice(0, start) + wrapped + value.slice(end);
  const innerStart = start + lead.length + marker.length;
  return { value: next, selectionStart: innerStart, selectionEnd: innerStart + core.length };
}
