import { useEffect, useMemo, useRef, useState } from "react";
import { Check, ChevronDown, ChevronUp, Copy, Loader2, Search } from "lucide-react";
import { api } from "@/api";
import Modal from "@/components/common/Modal";
import Switch from "@/components/settings/Switch";
import { useUI } from "@/store/ui";
import { copyText } from "./clipboard";
import MarkdownView, { Highlighted } from "./MarkdownView";
import { parseMarkdown } from "./markdownLite";

interface Props {
  open: boolean;
  onClose: () => void;
}

/**
 * «Protocolo v1 — documento completo»: el documento que viaja dentro del programa, legible, con
 * búsqueda, «Copiar todo», copia por bloque de código y una vista de texto sin formato.
 */
export default function ProtocolDocDialog({ open, onClose }: Props) {
  const toast = useUI((s) => s.toast);
  const [doc, setDoc] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);
  const [raw, setRaw] = useState(false);
  const [query, setQuery] = useState("");
  const [count, setCount] = useState(0);
  const [cur, setCur] = useState(0);
  const [copied, setCopied] = useState(false);
  const bodyRef = useRef<HTMLDivElement>(null);
  const copyTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  // El documento se pide la primera vez que se abre.
  useEffect(() => {
    if (!open || doc !== null) return;
    let alive = true;
    setFailed(false);
    api.destinations
      .ProtocolDoc()
      .then((text) => {
        if (!alive) return;
        if (typeof text === "string" && text !== "") setDoc(text);
        else setFailed(true);
      })
      .catch(() => alive && setFailed(true));
    return () => {
      alive = false;
    };
  }, [open, doc]);

  useEffect(() => () => clearTimeout(copyTimer.current), []);

  const blocks = useMemo(() => (doc ? parseMarkdown(doc) : []), [doc]);

  // Las apariciones resaltadas: se cuentan en lo pintado y se salta a la actual.
  useEffect(() => {
    const root = bodyRef.current;
    if (!root) return;
    const marks = Array.from(root.querySelectorAll<HTMLElement>("mark[data-hit]"));
    setCount(marks.length);
    const at = marks.length ? cur % marks.length : 0;
    marks.forEach((m, i) => m.setAttribute("data-current", String(i === at)));
    marks[at]?.scrollIntoView?.({ block: "center" });
  }, [doc, query, raw, cur, blocks]);

  const step = (d: 1 | -1) => {
    if (count === 0) return;
    setCur((c) => (((c + d) % count) + count) % count);
  };

  const copyAll = async () => {
    if (doc === null) return;
    if (!(await copyText(doc))) {
      toast("error", "No se pudo copiar");
      return;
    }
    toast("success", "Copiado");
    setCopied(true);
    clearTimeout(copyTimer.current);
    copyTimer.current = setTimeout(() => setCopied(false), 1500);
  };

  const at = count ? (cur % count) + 1 : 0;

  return (
    <Modal open={open} onClose={onClose} size="full" title="Protocolo v1 — documento completo">
      <div className="flex min-h-full flex-col">
        <div className="sticky -top-4 z-10 -mx-5 -mt-4 mb-4 flex flex-wrap items-center gap-3 border-b border-line bg-panel px-5 py-2.5">
          <div className="relative min-w-[220px] flex-1">
            <Search size={15} className="pointer-events-none absolute left-2.5 top-2.5 text-muted" />
            <input
              type="search"
              className="input !pl-8 !pr-24"
              placeholder="Buscar en el documento"
              aria-label="Buscar en el documento"
              value={query}
              disabled={doc === null}
              onChange={(e) => {
                setQuery(e.target.value);
                setCur(0);
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  step(e.shiftKey ? -1 : 1);
                }
              }}
            />
            {query.trim() !== "" && (
              <span className="absolute right-2 top-2 text-xs text-muted" aria-live="polite" data-testid="md-count">
                {count === 0 ? "Sin resultados" : `${at} de ${count}`}
              </span>
            )}
          </div>
          <button type="button" className="icon-btn" aria-label="Resultado anterior" title="Resultado anterior" disabled={count === 0} onClick={() => step(-1)}>
            <ChevronUp size={16} />
          </button>
          <button type="button" className="icon-btn" aria-label="Resultado siguiente" title="Resultado siguiente" disabled={count === 0} onClick={() => step(1)}>
            <ChevronDown size={16} />
          </button>
          <label className="flex items-center gap-2 text-sm">
            <Switch label="Ver texto sin formato" checked={raw} onChange={setRaw} disabled={doc === null} />
            <span>Ver texto sin formato</span>
          </label>
          <button type="button" className="btn" onClick={() => void copyAll()} disabled={doc === null}>
            {copied ? <Check size={15} /> : <Copy size={15} />}
            {copied ? "Copiado" : "Copiar todo"}
          </button>
        </div>

        <div ref={bodyRef} className="mx-auto w-full max-w-4xl flex-1 pb-6">
          {failed ? (
            <p className="text-sm text-danger" role="alert">
              No se pudo cargar el documento del protocolo.
            </p>
          ) : doc === null ? (
            <p className="flex items-center gap-2 text-sm text-muted">
              <Loader2 size={15} className="animate-spin" />
              Cargando…
            </p>
          ) : raw ? (
            <pre className="select-text whitespace-pre-wrap break-words font-mono text-xs leading-relaxed" data-testid="md-raw">
              <Highlighted text={doc} query={query} />
            </pre>
          ) : (
            <MarkdownView blocks={blocks} query={query} />
          )}
        </div>
      </div>
    </Modal>
  );
}
