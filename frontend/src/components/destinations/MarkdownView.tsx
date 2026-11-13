import { Fragment, useEffect, useRef, useState } from "react";
import { Check, Copy } from "lucide-react";
import { useUI } from "@/store/ui";
import { copyText } from "./clipboard";
import { parseInline, splitByQuery, type InlineNode, type MdBlock } from "./markdownLite";

/** Texto con las apariciones de la búsqueda resaltadas (`<mark data-hit>`). */
export function Highlighted({ text, query }: { text: string; query: string }) {
  if (!query.trim()) return <>{text}</>;
  return (
    <>
      {splitByQuery(text, query).map((p, i) =>
        p.hit ? (
          <mark
            key={i}
            data-hit=""
            className="rounded-sm bg-[color-mix(in_srgb,var(--warn)_45%,transparent)] px-px text-inherit data-[current=true]:bg-accent data-[current=true]:text-white"
          >
            {p.text}
          </mark>
        ) : (
          <Fragment key={i}>{p.text}</Fragment>
        ),
      )}
    </>
  );
}

function Inline({ nodes, query }: { nodes: InlineNode[]; query: string }) {
  return (
    <>
      {nodes.map((n, i) => {
        switch (n.kind) {
          case "code":
            return (
              <code key={i} className="rounded bg-panel2 px-1 py-px font-mono text-[0.9em]">
                <Highlighted text={n.text} query={query} />
              </code>
            );
          case "bold":
            return (
              <strong key={i} className="font-semibold">
                <Inline nodes={n.children} query={query} />
              </strong>
            );
          case "italic":
            return (
              <em key={i}>
                <Inline nodes={n.children} query={query} />
              </em>
            );
          default:
            return <Highlighted key={i} text={n.text} query={query} />;
        }
      })}
    </>
  );
}

/** Texto en línea con formato (`código`, **negrita**, *cursiva*). */
export function InlineText({ text, query }: { text: string; query: string }) {
  return <Inline nodes={parseInline(text)} query={query} />;
}

/** Bloque de código con su botón «Copiar». */
function CodeBlock({ text, lang, query }: { text: string; lang: string; query: string }) {
  const toast = useUI((s) => s.toast);
  const [copied, setCopied] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);

  const copy = async () => {
    if (!(await copyText(text))) {
      toast("error", "No se pudo copiar");
      return;
    }
    setCopied(true);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => setCopied(false), 1500);
  };

  return (
    <div className="overflow-hidden rounded-lg border border-line bg-panel2" data-testid="md-code">
      <div className="flex items-center justify-between border-b border-line px-3 py-1">
        <span className="font-mono text-[11px] uppercase text-muted">{lang || "texto"}</span>
        <button type="button" className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs font-medium text-accent hover:bg-[var(--hover)]" onClick={() => void copy()} title="Copiar este bloque">
          {copied ? <Check size={13} /> : <Copy size={13} />}
          {copied ? "Copiado" : "Copiar"}
        </button>
      </div>
      <pre className="select-text overflow-x-auto whitespace-pre px-3 py-2 font-mono text-xs leading-relaxed">
        <Highlighted text={text} query={query} />
      </pre>
    </div>
  );
}

const HEADING_CLASS: Record<number, string> = {
  1: "mt-2 text-2xl font-bold",
  2: "mt-6 border-b border-line pb-1 text-xl font-semibold",
  3: "mt-4 text-lg font-semibold",
  4: "mt-3 text-base font-semibold",
  5: "mt-3 text-sm font-semibold",
  6: "mt-3 text-sm font-semibold text-muted",
};

/** Pinta los bloques de `parseMarkdown`. El texto es seleccionable. */
export default function MarkdownView({ blocks, query }: { blocks: MdBlock[]; query: string }) {
  return (
    <div className="flex select-text flex-col gap-3 text-sm leading-relaxed" data-testid="md-view">
      {blocks.map((b, i) => {
        switch (b.type) {
          case "heading": {
            const Tag = `h${Math.min(b.level, 6)}` as "h1";
            return (
              <Tag key={i} className={HEADING_CLASS[b.level] ?? HEADING_CLASS[6]}>
                <InlineText text={b.text} query={query} />
              </Tag>
            );
          }
          case "paragraph":
            return (
              <p key={i}>
                <InlineText text={b.text} query={query} />
              </p>
            );
          case "list": {
            const ordered = b.items[0]?.number !== undefined;
            const Tag = ordered ? "ol" : "ul";
            return (
              <Tag key={i} className="flex flex-col gap-1">
                {b.items.map((it, k) => (
                  <li key={k} className="flex gap-2" style={{ marginLeft: it.depth * 18 }}>
                    <span className="w-5 shrink-0 text-right text-muted" aria-hidden>
                      {it.number !== undefined ? `${it.number}.` : "•"}
                    </span>
                    <span className="min-w-0 flex-1">
                      <InlineText text={it.text} query={query} />
                    </span>
                  </li>
                ))}
              </Tag>
            );
          }
          case "table":
            return (
              <div key={i} className="overflow-x-auto rounded-lg border border-line">
                <table className="w-full border-collapse text-left text-[13px]">
                  <thead className="bg-panel2">
                    <tr>
                      {b.header.map((h, k) => (
                        <th key={k} className="border-b border-line px-3 py-1.5 font-semibold">
                          <InlineText text={h} query={query} />
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {b.rows.map((r, k) => (
                      <tr key={k} className="border-b border-line last:border-b-0">
                        {r.map((c, j) => (
                          <td key={j} className="px-3 py-1.5 align-top">
                            <InlineText text={c} query={query} />
                          </td>
                        ))}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            );
          case "code":
            return <CodeBlock key={i} text={b.text} lang={b.lang} query={query} />;
          case "rule":
            return <hr key={i} className="border-line" />;
        }
      })}
    </div>
  );
}
