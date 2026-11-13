import { Fragment, useMemo, type ReactNode } from "react";
import { parseWhatsApp, type WaNode } from "@/lib/whatsappFormat";

function renderNodes(nodes: WaNode[], linkClass: string): ReactNode[] {
  return nodes.map((n, i) => {
    switch (n.type) {
      case "text":
        return <Fragment key={i}>{n.text}</Fragment>;
      case "bold":
        return <strong key={i}>{renderNodes(n.children, linkClass)}</strong>;
      case "italic":
        return <em key={i}>{renderNodes(n.children, linkClass)}</em>;
      case "strike":
        return <s key={i}>{renderNodes(n.children, linkClass)}</s>;
      case "mono":
        return (
          <code key={i} className="rounded bg-black/5 px-1 font-mono text-[0.9em] dark:bg-white/10">
            {n.text}
          </code>
        );
      case "link":
        // Se abre en el navegador del sistema.
        return (
          <a
            key={i}
            href={n.href}
            target="_blank"
            rel="noreferrer noopener"
            className={linkClass}
            onClick={(e) => e.stopPropagation()}
          >
            {n.text}
          </a>
        );
    }
  });
}

interface Props {
  text: string | null | undefined;
  className?: string;
  linkClassName?: string;
}

/** Texto con formato de WhatsApp (*negrita*, _cursiva_, ~tachado~, `mono`) y enlaces. Sin HTML crudo. */
export default function WaText({ text, className, linkClassName = "text-[#0a84d0] underline break-all hover:opacity-80" }: Props) {
  const nodes = useMemo(() => parseWhatsApp(text || ""), [text]);
  if (!text) return null;
  return <span className={`whitespace-pre-wrap break-words ${className ?? ""}`}>{renderNodes(nodes, linkClassName)}</span>;
}
