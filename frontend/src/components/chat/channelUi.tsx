// Iconos y colores de canal de la columna de chats: chips del filtro (colores del tema) e
// insignia de la fila (relleno fijo por canal, igual en claro y oscuro).
import type { CSSProperties } from "react";
import { MessageCircle, Send, Zap } from "lucide-react";

type IconProps = { size?: number; strokeWidth?: number; className?: string };

/** Lucide ya no incluye logotipos de marca: el glifo de Instagram se dibuja con el mismo trazo. */
function InstagramGlyph({ size = 16, strokeWidth = 1.8, className }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={strokeWidth}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      className={className}
    >
      <rect x="2" y="2" width="20" height="20" rx="5" />
      <circle cx="12" cy="12" r="4" />
      <path d="M17.5 6.5h.01" />
    </svg>
  );
}

interface ChannelUi {
  /** variable del tema para chips y puntos (cambia entre claro y oscuro) */
  cssVar: string;
  /** relleno fijo de la insignia de la fila */
  fill: string;
  Icon: (p: IconProps) => React.ReactElement;
}

const UI: Record<string, ChannelUi> = {
  whatsapp: { cssVar: "--ch-wa", fill: "#1a8f4f", Icon: (p) => <MessageCircle {...p} aria-hidden="true" /> },
  instagram: { cssVar: "--ch-ig", fill: "#c13584", Icon: InstagramGlyph },
  telegram: { cssVar: "--ch-tg", fill: "#1e88c9", Icon: (p) => <Send {...p} aria-hidden="true" /> },
  messenger: { cssVar: "--ch-ms", fill: "#0b6fe0", Icon: (p) => <Zap {...p} aria-hidden="true" /> },
};

const FALLBACK: ChannelUi = { cssVar: "--muted", fill: "#667781", Icon: (p) => <MessageCircle {...p} aria-hidden="true" /> };

export function channelUi(channel: string): ChannelUi {
  return UI[channel] ?? FALLBACK;
}

/** Icono del canal. El color lo pone quien lo usa (`currentColor`). */
export function ChannelGlyph({ channel, size = 16, strokeWidth }: { channel: string; size?: number; strokeWidth?: number }) {
  const { Icon } = channelUi(channel);
  return <Icon size={size} strokeWidth={strokeWidth} />;
}

/** Variable CSS local `--c` con el color del canal, para chips activos (`text-[var(--c)]`). */
export function channelColorStyle(channel: string): CSSProperties {
  return { ["--c" as string]: `var(${channelUi(channel).cssVar})` };
}
