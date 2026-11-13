// Lista virtualizada de la conversación: se pega al final, carga mensajes antiguos al subir
// (conservando la posición) y puede saltar a un mensaje.
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { ChevronDown } from "lucide-react";
import type { TimelineItem } from "./timeline";

export interface ScrollTarget {
  index: number;
  nonce: number;
}

const NEAR_BOTTOM = 80;
const NEAR_TOP = 300;

export default function MessageList({
  chatKey,
  items,
  hasMore,
  loading,
  onLoadOlder,
  renderItem,
  scrollTarget,
  topSlot,
}: {
  /** Algo discreto pegado arriba (p. ej. «Cargar mensajes anteriores»); no mueve el contenido. */
  topSlot?: React.ReactNode;
  chatKey: string;
  items: TimelineItem[];
  hasMore: boolean;
  loading: boolean;
  onLoadOlder: () => void;
  renderItem: (item: TimelineItem) => React.ReactNode;
  scrollTarget: ScrollTarget | null;
}) {
  const parentRef = useRef<HTMLDivElement>(null);
  const stick = useRef(true);
  const lastScrollTop = useRef(0);
  const nearTop = useRef(false);
  const prev = useRef<{ firstMsg: string | null; len: number; lastKey: string | null; anchorStart: number }>({
    firstMsg: null,
    len: 0,
    lastKey: null,
    anchorStart: 0,
  });
  const [atBottom, setAtBottom] = useState(true);

  const v = useVirtualizer({
    count: items.length,
    getScrollElement: () => parentRef.current,
    estimateSize: (i) => (items[i]?.type === "day" ? 44 : 76),
    overscan: 10,
    getItemKey: (i) => items[i]?.key ?? i,
  });
  const total = v.getTotalSize();

  // Cambio de chat: vuelve a pegarse al final.
  useLayoutEffect(() => {
    stick.current = true;
    nearTop.current = false;
    prev.current = { firstMsg: null, len: 0, lastKey: null, anchorStart: 0 };
    setAtBottom(true);
  }, [chatKey]);

  // Cambios en la lista: mensajes antiguos arriba (conserva posición) o nuevos abajo (pega al final).
  useLayoutEffect(() => {
    const el = parentRef.current;
    const p = prev.current;
    if (!el || items.length === 0) {
      prev.current = { firstMsg: null, len: 0, lastKey: null, anchorStart: 0 };
      return;
    }
    const firstMsgIdx = items.findIndex((i) => i.type === "msg");
    const firstMsg = firstMsgIdx >= 0 ? items[firstMsgIdx].key : null;
    const lastItem = items[items.length - 1];
    if (p.firstMsg !== null && firstMsg !== p.firstMsg && items.length > p.len) {
      const k = items.findIndex((i) => i.key === p.firstMsg);
      const start = k > 0 ? v.measurementsCache[k]?.start : undefined;
      if (start !== undefined) {
        const next = start - p.anchorStart + lastScrollTop.current;
        el.scrollTop = next;
        lastScrollTop.current = next;
      }
    } else if (p.lastKey !== null && lastItem.key !== p.lastKey && lastItem.type === "msg" && lastItem.message.fromMe) {
      stick.current = true; // lo que envío siempre se ve
    }
    if (stick.current) el.scrollTop = el.scrollHeight;
    prev.current = {
      firstMsg,
      len: items.length,
      lastKey: lastItem.key,
      anchorStart: (firstMsgIdx >= 0 ? v.measurementsCache[firstMsgIdx]?.start : 0) ?? 0,
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [items]);

  // Medidas tardías (fotos que cargan, etc.): sigue pegado al final.
  useLayoutEffect(() => {
    const el = parentRef.current;
    if (el && stick.current) el.scrollTop = el.scrollHeight;
  }, [total]);

  // Si lo cargado no llena la ventana y hay más, pide más.
  useEffect(() => {
    const el = parentRef.current;
    if (!el || loading || !hasMore) return;
    if (el.scrollHeight <= el.clientHeight + 20 || el.scrollTop < NEAR_TOP) onLoadOlder();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loading, hasMore, items.length]);

  useEffect(() => {
    if (!scrollTarget) return;
    stick.current = false;
    v.scrollToIndex(scrollTarget.index, { align: "center" });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [scrollTarget?.nonce]);

  const onScroll = (e: React.UIEvent<HTMLDivElement>) => {
    const el = e.currentTarget;
    lastScrollTop.current = el.scrollTop;
    const bottom = el.scrollHeight - el.scrollTop - el.clientHeight < NEAR_BOTTOM;
    stick.current = bottom;
    setAtBottom((b) => (b === bottom ? b : bottom));
    if (el.scrollTop < NEAR_TOP) {
      if (!nearTop.current) {
        nearTop.current = true;
        onLoadOlder();
      }
    } else if (el.scrollTop > NEAR_TOP * 2) {
      nearTop.current = false;
    }
  };

  const toBottom = () => {
    const el = parentRef.current;
    if (!el) return;
    stick.current = true;
    el.scrollTo({ top: el.scrollHeight, behavior: "smooth" });
  };

  return (
    <div className="relative min-h-0 flex-1">
      <div ref={parentRef} onScroll={onScroll} className="h-full overflow-y-auto overflow-x-hidden py-2" data-testid="message-list">
        {items.length === 0 ? (
          <div className="flex h-full items-center justify-center text-[13px] text-[var(--muted)]">
            {loading ? "Cargando…" : ""}
          </div>
        ) : (
          <div style={{ height: total, position: "relative", width: "100%" }}>
            {v.getVirtualItems().map((vi) => (
              <div
                key={vi.key}
                data-index={vi.index}
                ref={v.measureElement}
                style={{ position: "absolute", top: 0, left: 0, width: "100%", transform: `translateY(${vi.start}px)` }}
              >
                {items[vi.index] && renderItem(items[vi.index])}
              </div>
            ))}
          </div>
        )}
      </div>
      {topSlot && <div className="pointer-events-none absolute inset-x-0 top-2 z-10 flex justify-center">{topSlot}</div>}
      {!atBottom && (
        <button
          type="button"
          onClick={toBottom}
          title="Ir al final"
          className="absolute bottom-4 right-5 flex h-10 w-10 items-center justify-center rounded-full bg-[var(--panel)] text-[var(--muted)] shadow-lg hover:text-[var(--text)]"
        >
          <ChevronDown size={22} />
        </button>
      )}
    </div>
  );
}
