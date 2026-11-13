import { channelInfo } from "@/lib/channels";

interface Props {
  channel: string;
  size?: number;
  className?: string;
  /** Anillo del color del fondo (insignia sobre un avatar). */
  ring?: boolean;
}

/** Círculo del color del canal con su letra (W, T, M, I). SVG propio, sin logotipos de marca. */
export default function ChannelIcon({ channel, size = 20, className, ring }: Props) {
  const info = channelInfo(channel);
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      role="img"
      aria-label={info.name}
      className={className}
    >
      <title>{info.name}</title>
      <circle cx="12" cy="12" r={ring ? 11 : 12} fill={info.color} stroke={ring ? "var(--panel)" : "none"} strokeWidth={ring ? 2 : 0} />
      <text
        x="12"
        y="12"
        textAnchor="middle"
        dominantBaseline="central"
        fontFamily="Segoe UI, system-ui, sans-serif"
        fontWeight="700"
        fontSize="13"
        fill="#ffffff"
      >
        {info.letter}
      </text>
    </svg>
  );
}
