// Formato de texto de WhatsApp (*negrita*, _cursiva_, ~tachado~, `mono`, ```bloque```) y enlaces.
// Produce nodos React sin dangerouslySetInnerHTML. Local a la pantalla de chats.
import React from "react";

export type WaNode =
  | { t: "text"; v: string }
  | { t: "b" | "i" | "s"; children: WaNode[] }
  | { t: "code"; v: string }
  | { t: "pre"; v: string }
  | { t: "link"; href: string; v: string };

const MARKS: Record<string, "b" | "i" | "s" | "code"> = { "*": "b", _: "i", "~": "s", "`": "code" };
const LINK_RE = /(?:https?:\/\/|www\.)[^\s<>"']+/y;
const WORD_RE = /[\p{L}\p{N}]/u;

function isWordChar(c: string | undefined): boolean {
  return !!c && WORD_RE.test(c);
}

/** Quita la puntuación final que casi nunca forma parte del enlace. */
function trimLink(raw: string): string {
  let s = raw;
  while (/[.,;:!?)\]}'"»]$/.test(s)) {
    if (s.endsWith(")") && (s.match(/\(/g)?.length ?? 0) >= (s.match(/\)/g)?.length ?? 0)) break;
    s = s.slice(0, -1);
  }
  return s;
}

function findClose(s: string, from: number, mark: string): number {
  for (let j = from; j < s.length; j++) {
    const c = s[j];
    if (c === "\n") return -1;
    if (c !== mark) continue;
    if (/\s/.test(s[j - 1] ?? "")) continue;
    if (isWordChar(s[j + 1])) continue;
    return j;
  }
  return -1;
}

export function parseWaText(input: string): WaNode[] {
  const out: WaNode[] = [];
  let buf = "";
  const flush = () => {
    if (buf) out.push({ t: "text", v: buf });
    buf = "";
  };
  const s = input ?? "";
  let i = 0;
  while (i < s.length) {
    const c = s[i];
    // bloque ```…```
    if (s.startsWith("```", i)) {
      const end = s.indexOf("```", i + 3);
      if (end > i + 3) {
        flush();
        out.push({ t: "pre", v: s.slice(i + 3, end).replace(/^\n|\n$/g, "") });
        i = end + 3;
        continue;
      }
    }
    // enlaces (solo al inicio de palabra)
    if ((c === "h" || c === "w") && !isWordChar(s[i - 1])) {
      LINK_RE.lastIndex = i;
      const m = LINK_RE.exec(s);
      if (m) {
        const raw = trimLink(m[0]);
        if (raw.length > 4) {
          flush();
          out.push({ t: "link", href: raw.startsWith("www.") ? `https://${raw}` : raw, v: raw });
          i += raw.length;
          continue;
        }
      }
    }
    const mark = MARKS[c];
    if (mark && !isWordChar(s[i - 1]) && s[i + 1] !== undefined && !/\s/.test(s[i + 1]) && s[i + 1] !== c) {
      const j = findClose(s, i + 2, c);
      if (j > i + 1) {
        flush();
        const inner = s.slice(i + 1, j);
        out.push(mark === "code" ? { t: "code", v: inner } : { t: mark, children: parseWaText(inner) });
        i = j + 1;
        continue;
      }
    }
    buf += c;
    i++;
  }
  flush();
  return out;
}

function renderNodes(nodes: WaNode[], keyPrefix = ""): React.ReactNode[] {
  return nodes.map((n, idx) => {
    const key = `${keyPrefix}${idx}`;
    switch (n.t) {
      case "text":
        return <React.Fragment key={key}>{n.v}</React.Fragment>;
      case "b":
        return <strong key={key}>{renderNodes(n.children, key + ".")}</strong>;
      case "i":
        return <em key={key}>{renderNodes(n.children, key + ".")}</em>;
      case "s":
        return <s key={key}>{renderNodes(n.children, key + ".")}</s>;
      case "code":
        return (
          <code key={key} className="rounded bg-black/10 px-1 font-mono text-[0.9em] dark:bg-white/10">
            {n.v}
          </code>
        );
      case "pre":
        return (
          <pre key={key} className="my-1 whitespace-pre-wrap rounded bg-black/10 p-2 font-mono text-[0.9em] dark:bg-white/10">
            {n.v}
          </pre>
        );
      case "link":
        return (
          <a
            key={key}
            href={n.href}
            target="_blank"
            rel="noreferrer noopener"
            className="text-[#027eb5] underline-offset-2 hover:underline dark:text-[#53bdeb]"
            onClick={(e) => e.stopPropagation()}
          >
            {n.v}
          </a>
        );
    }
  });
}

/** Texto con formato de WhatsApp, respetando saltos de línea. */
export function WaText({ text, className }: { text: string; className?: string }) {
  return (
    <span className={`whitespace-pre-wrap break-words ${className ?? ""}`}>{renderNodes(parseWaText(text))}</span>
  );
}
