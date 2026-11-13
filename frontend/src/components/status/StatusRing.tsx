import Avatar from "@/components/common/Avatar";
import { ringSegments } from "./statusModel";

interface Props {
  /** Estados vigentes (un segmento por estado). 0 = sin anillo. */
  count: number;
  /** Cuántos no se han visto. */
  unviewed: number;
  name: string;
  src?: string | null;
  colorKey?: string;
  size?: number;
  /** Todo el anillo con el color de acento (mis estados). */
  allActive?: boolean;
}

/** Avatar con anillo segmentado: acento = no visto, gris = visto. */
export default function StatusRing({ count, unviewed, name, src, colorKey, size = 50, allActive }: Props) {
  const stroke = 3;
  const r = (size - stroke) / 2;
  const c = 2 * Math.PI * r;
  const segs = ringSegments(count, allActive ? count : unviewed);
  const inner = size - stroke * 2 - 4;

  return (
    <span className="relative inline-flex shrink-0 items-center justify-center" style={{ width: size, height: size }}>
      {segs.length > 0 && (
        <svg
          width={size}
          height={size}
          viewBox={`0 0 ${size} ${size}`}
          className="absolute inset-0"
          aria-hidden
          data-testid="status-ring"
        >
          {segs.map((s, i) => (
            <circle
              key={i}
              cx={size / 2}
              cy={size / 2}
              r={r}
              fill="none"
              strokeWidth={stroke}
              strokeLinecap="round"
              stroke={s.viewed ? "var(--seen)" : "var(--accent)"}
              strokeDasharray={`${(s.end - s.start) * c} ${c}`}
              strokeDashoffset={-s.start * c}
              transform={`rotate(-90 ${size / 2} ${size / 2})`}
              data-viewed={s.viewed ? "1" : "0"}
            />
          ))}
        </svg>
      )}
      <Avatar src={src} name={name} size={inner} colorKey={colorKey} />
    </span>
  );
}
