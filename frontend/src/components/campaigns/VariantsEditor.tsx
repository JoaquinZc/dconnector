import { useRef, useState } from "react";
import { Plus, Shuffle, Tag, Trash2 } from "lucide-react";
import { cx } from "@/lib/cx";
import { MAX_EXTRA_VARIANTS, VARIANT_LETTERS } from "./campaignModel";

interface Props {
  /** Mensaje A. */
  text: string;
  /** Variantes B…E. */
  variants: string[];
  onText: (t: string) => void;
  onVariants: (v: string[]) => void;
  /** Mensajes distintos que pide la revisión (0 = ninguno). */
  required?: number;
}

/** Mensaje A y variantes B…E: cada destinatario recibe una; `{nombre}` y `{a|b}` se rellenan por persona. */
export default function VariantsEditor({ text, variants, onText, onVariants, required = 0 }: Props) {
  const [active, setActive] = useState(0);
  const ref = useRef<HTMLTextAreaElement>(null);
  const texts = [text, ...variants];
  const current = Math.min(active, texts.length - 1);
  const value = texts[current] ?? "";
  const filled = texts.filter((t) => t.trim()).length;

  const setText = (i: number, t: string) => {
    if (i === 0) onText(t);
    else onVariants(variants.map((v, j) => (j === i - 1 ? t : v)));
  };
  const add = () => {
    if (variants.length >= MAX_EXTRA_VARIANTS) return;
    onVariants([...variants, ""]);
    setActive(texts.length);
  };
  const remove = (i: number) => {
    onVariants(variants.filter((_, j) => j !== i - 1));
    setActive(Math.max(0, i - 1));
  };

  const insert = (snippet: string, selectInside?: [number, number]) => {
    const el = ref.current;
    const focused = !!el && document.activeElement === el;
    const start = focused ? el!.selectionStart : value.length;
    const end = focused ? el!.selectionEnd : value.length;
    setText(current, value.slice(0, start) + snippet + value.slice(end));
    requestAnimationFrame(() => {
      el?.focus();
      const a = selectInside ? start + selectInside[0] : start + snippet.length;
      const b = selectInside ? start + selectInside[1] : start + snippet.length;
      el?.setSelectionRange(a, b);
    });
  };

  return (
    <div className="flex flex-col gap-3">
      <div role="tablist" aria-label="Mensajes" className="flex flex-wrap items-center gap-1">
        {texts.map((t, i) => (
          <button
            key={i}
            role="tab"
            aria-selected={current === i}
            onClick={() => setActive(i)}
            title={i === 0 ? "Mensaje A" : `Variante ${VARIANT_LETTERS[i]}`}
            className={cx(
              "flex h-8 min-w-8 items-center justify-center gap-1 rounded-lg border px-2.5 text-xs font-bold",
              current === i
                ? "border-accent bg-accent text-white"
                : t.trim()
                  ? "border-line bg-panel hover:bg-[var(--hover)]"
                  : "border-dashed border-line text-muted hover:bg-[var(--hover)]",
            )}
          >
            {VARIANT_LETTERS[i]}
          </button>
        ))}
        {variants.length < MAX_EXTRA_VARIANTS && (
          <button className="icon-btn" onClick={add} aria-label="Añadir variante" title="Añadir variante">
            <Plus size={16} />
          </button>
        )}
        <span className="flex-1" />
        {required > 0 && (
          <span className={cx("text-xs", filled >= required ? "text-ok" : "text-warn")}>
            {filled} de {required} mensajes
          </span>
        )}
      </div>

      <textarea
        ref={ref}
        className="input min-h-[140px]"
        value={value}
        onChange={(e) => setText(current, e.target.value)}
        placeholder={current === 0 ? "Mensaje" : `Variante ${VARIANT_LETTERS[current]}`}
        aria-label={current === 0 ? "Mensaje A" : `Variante ${VARIANT_LETTERS[current]}`}
        maxLength={4096}
      />

      <div className="flex flex-wrap items-center gap-2">
        <button className="btn btn-sm" onClick={() => insert("{nombre}")} title="Primer nombre del contacto">
          <Tag size={13} />
          {"{nombre}"}
        </button>
        <button className="btn btn-sm" onClick={() => insert("{hola|buenas}", [1, 11])} title="Elige una opción al azar por persona">
          <Shuffle size={13} />
          {"{a|b}"}
        </button>
        <span className="flex-1" />
        {current > 0 && (
          <button className="btn btn-sm btn-danger" onClick={() => remove(current)}>
            <Trash2 size={13} />
            Quitar variante
          </button>
        )}
      </div>
    </div>
  );
}
