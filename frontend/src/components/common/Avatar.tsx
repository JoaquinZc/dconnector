import { useEffect, useState } from "react";
import { mediaUrl } from "@/api/media";
import { colorForId, initialsOf } from "@/lib/channels";

interface Props {
  /** URL del avatar ("/avatar/<cuenta>/<archivo>") o vacío. */
  src?: string | null;
  /** Nombre para las iniciales. */
  name: string;
  size?: number;
  /** Id para el color estable; por defecto el nombre. */
  colorKey?: string;
  className?: string;
}

/** Imagen de perfil o, si no hay (o falla), iniciales con color estable. */
export default function Avatar({ src, name, size = 40, colorKey, className }: Props) {
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [src]);

  const style = { width: size, height: size, minWidth: size } as const;
  if (src && !failed) {
    return (
      <img
        src={mediaUrl(src)}
        alt=""
        draggable={false}
        onError={() => setFailed(true)}
        className={`rounded-full object-cover bg-panel2 ${className ?? ""}`}
        style={style}
      />
    );
  }
  return (
    <div
      aria-hidden
      className={`flex select-none items-center justify-center rounded-full font-semibold text-white ${className ?? ""}`}
      style={{ ...style, background: colorForId(colorKey ?? name), fontSize: Math.max(11, Math.round(size * 0.38)) }}
    >
      {initialsOf(name)}
    </div>
  );
}
