// Esqueletos: el diseño de la pantalla se pinta al instante y solo los datos esperan. Usan los colores del tema
// (`--muted`, `--border`) y respetan «reducir movimiento».
import type { CSSProperties } from "react";

/** Un bloque gris que pulsa. Dale tamaño con `className` (p. ej. `h-4 w-1/2`) o `style`. */
export function Skeleton({ className = "", style }: { className?: string; style?: CSSProperties }) {
  // Si quien llama pide su propio redondeo (`rounded-full`…), el de por defecto no compite con él.
  const rounded = /(^|\s)rounded(-|\s|$)/.test(className) ? "" : "rounded-md";
  return (
    <div
      aria-hidden="true"
      className={`animate-pulse ${rounded} bg-[color-mix(in_srgb,var(--muted)_20%,transparent)] motion-reduce:animate-none ${className}`}
      style={style}
    />
  );
}

/** Burbujas de mensajes alternando lados, para la conversación que se está abriendo. */
export function SkeletonBubbles({ count = 6 }: { count?: number }) {
  return (
    <div role="status" aria-label="Cargando" className="flex h-full flex-col justify-end gap-3 px-6 py-4">
      {Array.from({ length: count }, (_, i) => (
        <Skeleton
          key={i}
          className={`h-10 max-w-[70%] rounded-2xl ${i % 2 ? "self-end" : "self-start"}`}
          style={{ width: `${30 + ((i * 29) % 40)}%` }}
        />
      ))}
    </div>
  );
}
