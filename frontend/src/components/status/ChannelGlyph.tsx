// Glifos genéricos de canal (trazo, sin logotipos de marca) e insignia redonda para la pantalla de Estados.
import type { CSSProperties } from "react";

const PATHS: Record<string, React.ReactNode> = {
  whatsapp: <path d="M7.9 20A9 9 0 1 0 4 16.1L2 22Z" />,
  telegram: <path d="M22 2 11 13M22 2l-7 20-4-9-9-4z" />,
  instagram: (
    <>
      <rect x="2" y="2" width="20" height="20" rx="5" />
      <circle cx="12" cy="12" r="4" />
      <path d="M17.5 6.5h.01" />
    </>
  ),
  messenger: <path d="M13 2 3 14h9l-1 8 10-12h-9z" />,
};

/** Variable de color del canal (cambia con el tema). */
export function channelVar(channel: string): string {
  switch (channel) {
    case "telegram":
      return "var(--ch-tg)";
    case "instagram":
      return "var(--ch-ig)";
    case "messenger":
      return "var(--ch-ms)";
    default:
      return "var(--ch-wa)";
  }
}

/** Fondo de la insignia: los mismos tonos medios en claro y oscuro, para que el glifo blanco se lea siempre. */
const BADGE_BG: Record<string, string> = {
  whatsapp: "#1a8f4f",
  telegram: "#1e88c9",
  instagram: "#c13584",
  messenger: "#0b6fe0",
};

export function ChannelGlyph({ channel, size = 16, className }: { channel: string; size?: number; className?: string }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={size <= 12 ? 2.2 : 1.8}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
      className={className}
      style={{ flex: "none" } as CSSProperties}
    >
      {PATHS[channel] ?? PATHS.whatsapp}
    </svg>
  );
}

/** Círculo con el color del canal y su glifo en blanco. */
export function ChannelBadge({
  channel,
  size = 24,
  className,
  style,
}: {
  channel: string;
  size?: number;
  className?: string;
  style?: CSSProperties;
}) {
  return (
    <span
      aria-hidden
      data-channel={channel}
      className={`inline-flex shrink-0 items-center justify-center rounded-full text-white ${className ?? ""}`}
      style={{ width: size, height: size, background: BADGE_BG[channel] ?? BADGE_BG.whatsapp, ...style }}
    >
      <ChannelGlyph channel={channel} size={12} />
    </span>
  );
}
