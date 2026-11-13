// Piezas visuales pequeñas de la pantalla de chats: avatar con insignia de canal, píldora de
// cuenta, superposición (modal) y botón de icono. Locales a esta carpeta.
import React, { useEffect, useState } from "react";
import { mediaUrl } from "@/api/media";
import { X } from "lucide-react";
import { initials, senderColor, truncate } from "./chatFormat";

export const CHANNELS: Record<string, { name: string; color: string; letter: string }> = {
  whatsapp: { name: "WhatsApp", color: "#25D366", letter: "W" },
  telegram: { name: "Telegram", color: "#229ED9", letter: "T" },
  messenger: { name: "Messenger", color: "#0084FF", letter: "M" },
  instagram: { name: "Instagram", color: "#E1306C", letter: "I" },
};

export function channelInfo(channel: string) {
  return CHANNELS[channel] ?? { name: channel, color: "#8696a0", letter: (channel[0] ?? "?").toUpperCase() };
}

/** Círculo de color con la letra del canal (sin logotipos de marca). */
export function ChannelBadge({ channel, size = 16 }: { channel: string; size?: number }) {
  const c = channelInfo(channel);
  return (
    <span
      title={c.name}
      className="inline-flex shrink-0 items-center justify-center rounded-full font-bold leading-none text-white ring-2 ring-[var(--panel)]"
      style={{ width: size, height: size, background: c.color, fontSize: Math.round(size * 0.58) }}
    >
      {c.letter}
    </span>
  );
}

/** Avatar redondo (foto o iniciales) con la insignia del canal en la esquina inferior derecha. */
export function ChatAvatar({
  title,
  avatarUrl,
  channel,
  size = 48,
  badge = true,
}: {
  title: string;
  avatarUrl?: string;
  channel?: string;
  size?: number;
  badge?: boolean;
}) {
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [avatarUrl]);
  const showImg = !!avatarUrl && !failed;
  return (
    <span className="relative inline-block shrink-0" style={{ width: size, height: size }}>
      {showImg ? (
        <img
          src={mediaUrl(avatarUrl)}
          alt=""
          className="h-full w-full rounded-full object-cover"
          onError={() => setFailed(true)}
        />
      ) : (
        <span
          className="flex h-full w-full items-center justify-center rounded-full font-semibold text-white"
          style={{ background: senderColor(title || "?"), fontSize: Math.round(size * 0.36) }}
        >
          {initials(title)}
        </span>
      )}
      {badge && channel && (
        <span className="absolute -bottom-0.5 -right-0.5">
          <ChannelBadge channel={channel} size={size >= 40 ? 16 : 12} />
        </span>
      )}
    </span>
  );
}

/** Píldora con el color y la etiqueta de la cuenta (máx. 12 caracteres). */
export function AccountChip({ label, color }: { label: string; color: string }) {
  return (
    <span
      title={label}
      className="inline-block max-w-[110px] shrink-0 truncate rounded-full px-1.5 py-px align-middle text-[10px] font-medium leading-4 text-white"
      style={{ background: color || "#8696a0" }}
    >
      {truncate(label, 12)}
    </span>
  );
}

/** Superposición centrada con cierre por Esc y clic fuera. */
export function Overlay({
  onClose,
  children,
  width = 440,
  dark = false,
  label,
}: {
  onClose: () => void;
  children: React.ReactNode;
  width?: number;
  dark?: boolean;
  label?: string;
}) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onClose();
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [onClose]);
  return (
    <div
      role="dialog"
      aria-label={label}
      className={`fixed inset-0 z-50 flex items-center justify-center p-4 ${dark ? "bg-black/90" : "bg-black/50"}`}
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      {dark ? (
        children
      ) : (
        <div
          className="flex max-h-[85vh] w-full flex-col overflow-hidden rounded-xl bg-[var(--panel)] text-[var(--text)] shadow-2xl"
          style={{ maxWidth: width }}
        >
          {children}
        </div>
      )}
    </div>
  );
}

export interface PopItem {
  label: string;
  onClick: () => void;
  danger?: boolean;
  /** Se ve pero no se puede elegir (el motivo va en la propia etiqueta). */
  disabled?: boolean;
}

/** Menú flotante simple en una posición de pantalla (clic derecho). Se cierra al hacer clic fuera. */
export function PopMenu({ at, items, onClose }: { at: { x: number; y: number }; items: PopItem[]; onClose: () => void }) {
  const ref = React.useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState({ left: at.x, top: at.y });
  React.useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    setPos({
      left: Math.max(8, Math.min(at.x, window.innerWidth - r.width - 8)),
      top: Math.max(8, Math.min(at.y, window.innerHeight - r.height - 8)),
    });
  }, [at.x, at.y]);
  useEffect(() => {
    const down = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose();
    };
    const key = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("mousedown", down);
    window.addEventListener("keydown", key);
    return () => {
      window.removeEventListener("mousedown", down);
      window.removeEventListener("keydown", key);
    };
  }, [onClose]);
  return (
    <div
      ref={ref}
      role="menu"
      className="fixed z-50 min-w-[190px] overflow-hidden rounded-xl bg-[var(--panel)] py-1.5 text-[var(--text)] shadow-xl ring-1 ring-black/5"
      style={{ left: pos.left, top: pos.top }}
      onContextMenu={(e) => e.preventDefault()}
    >
      {items.map((it) => (
        <button
          key={it.label}
          type="button"
          role="menuitem"
          disabled={it.disabled}
          onClick={() => {
            onClose();
            it.onClick();
          }}
          className={`block w-full px-4 py-2 text-left text-[14px] hover:bg-black/5 disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent dark:hover:bg-white/10 ${
            it.danger ? "text-[var(--danger)]" : ""
          }`}
        >
          {it.label}
        </button>
      ))}
    </div>
  );
}

export function OverlayHeader({ title, onClose }: { title: string; onClose: () => void }) {
  return (
    <div className="flex items-center gap-2 border-b border-[var(--border)] px-4 py-3">
      <h2 className="min-w-0 flex-1 truncate text-base font-semibold" title={title}>
        {title}
      </h2>
      <IconButton title="Cerrar" onClick={onClose}>
        <X size={18} />
      </IconButton>
    </div>
  );
}

export function IconButton({
  title,
  onClick,
  children,
  active,
  disabled,
  className = "",
}: {
  title: string;
  onClick?: (e: React.MouseEvent<HTMLButtonElement>) => void;
  children: React.ReactNode;
  active?: boolean;
  disabled?: boolean;
  className?: string;
}) {
  return (
    <button
      type="button"
      title={title}
      aria-label={title}
      disabled={disabled}
      onClick={onClick}
      className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-full text-[var(--muted)] transition-colors hover:bg-black/5 hover:text-[var(--text)] disabled:opacity-40 dark:hover:bg-white/10 ${
        active ? "bg-black/5 text-[var(--text)] dark:bg-white/10" : ""
      } ${className}`}
    >
      {children}
    </button>
  );
}
