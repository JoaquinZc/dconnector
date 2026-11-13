// Menú de un mensaje (clic derecho o flecha): barra de reacciones y acciones permitidas.
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { Copy, CornerUpLeft, Forward, Info, Pencil, Plus, Trash2 } from "lucide-react";
import type { Message } from "../../api/core/model/models.js";
import { hasAnyOption, messageMenuOptions, type CapsLike } from "./chatCaps";
import LazyEmojiPicker from "./LazyEmojiPicker";

export const QUICK_EMOJIS = ["👍", "❤️", "😂", "😮", "😢", "🙏"] as const;

export interface MessageMenuProps {
  msg: Message;
  at: { x: number; y: number };
  caps: CapsLike | null | undefined;
  /** emoji con el que ya reaccioné a este mensaje, si lo hay */
  myReaction?: string;
  now?: number;
  onClose: () => void;
  onReply: () => void;
  onReact: (emoji: string) => void;
  onForward: () => void;
  onCopy: () => void;
  onEdit: () => void;
  onDelete: () => void;
  onInfo: () => void;
}

function Item({
  icon,
  label,
  onClick,
  danger,
}: {
  icon: React.ReactNode;
  label: string;
  onClick: () => void;
  danger?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`flex w-full items-center gap-3 px-4 py-2 text-left text-[14px] hover:bg-black/5 dark:hover:bg-white/10 ${
        danger ? "text-[var(--danger)]" : ""
      }`}
    >
      <span className="flex w-5 justify-center text-[var(--muted)]">{icon}</span>
      {label}
    </button>
  );
}

export default function MessageMenu(p: MessageMenuProps) {
  const opts = messageMenuOptions(p.msg, p.caps, p.now);
  const ref = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState({ left: p.at.x, top: p.at.y });
  const [picker, setPicker] = useState(false);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    const left = Math.max(8, Math.min(p.at.x, window.innerWidth - r.width - 8));
    const top = Math.max(8, Math.min(p.at.y, window.innerHeight - r.height - 8));
    setPos({ left, top });
  }, [p.at.x, p.at.y, picker]);

  useEffect(() => {
    const down = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) p.onClose();
    };
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") p.onClose();
    };
    window.addEventListener("mousedown", down);
    window.addEventListener("keydown", key);
    window.addEventListener("blur", p.onClose);
    return () => {
      window.removeEventListener("mousedown", down);
      window.removeEventListener("keydown", key);
      window.removeEventListener("blur", p.onClose);
    };
  }, [p]);

  const run = (fn: () => void) => () => {
    p.onClose();
    fn();
  };

  return (
    <div
      ref={ref}
      role="menu"
      className="fixed z-50 flex flex-col gap-1"
      style={{ left: pos.left, top: pos.top }}
      onContextMenu={(e) => e.preventDefault()}
    >
      {opts.react && (
        <div className="flex items-center gap-1 self-start rounded-full bg-[var(--panel)] px-2 py-1.5 shadow-xl ring-1 ring-black/5">
          {QUICK_EMOJIS.map((e) => (
            <button
              key={e}
              type="button"
              onClick={run(() => p.onReact(e))}
              title={p.myReaction === e ? "Quitar reacción" : e}
              className={`h-9 w-9 rounded-full text-[22px] transition-transform hover:scale-125 ${
                p.myReaction === e ? "bg-black/10 dark:bg-white/15" : ""
              }`}
            >
              {e}
            </button>
          ))}
          <button
            type="button"
            onClick={() => setPicker((v) => !v)}
            title="Más emojis"
            className="flex h-9 w-9 items-center justify-center rounded-full bg-black/5 text-[var(--muted)] hover:bg-black/10 dark:bg-white/10"
          >
            <Plus size={18} />
          </button>
        </div>
      )}
      {picker ? (
        <div className="overflow-hidden rounded-xl shadow-xl">
          <LazyEmojiPicker onPick={(e) => run(() => p.onReact(e))()} />
        </div>
      ) : (
        hasAnyOption(opts) || opts.deleteForMe ? (
          <div className="min-w-[210px] overflow-hidden rounded-xl bg-[var(--panel)] py-1.5 shadow-xl ring-1 ring-black/5">
            {opts.reply && <Item icon={<CornerUpLeft size={16} />} label="Responder" onClick={run(p.onReply)} />}
            {opts.copy && <Item icon={<Copy size={16} />} label="Copiar" onClick={run(p.onCopy)} />}
            {opts.forward && <Item icon={<Forward size={16} />} label="Reenviar" onClick={run(p.onForward)} />}
            {opts.edit && <Item icon={<Pencil size={16} />} label="Editar" onClick={run(p.onEdit)} />}
            {opts.info && <Item icon={<Info size={16} />} label="Info." onClick={run(p.onInfo)} />}
            {(opts.deleteForMe || opts.deleteForAll) && (
              <Item icon={<Trash2 size={16} />} label="Eliminar" danger onClick={run(p.onDelete)} />
            )}
          </div>
        ) : null
      )}
    </div>
  );
}
