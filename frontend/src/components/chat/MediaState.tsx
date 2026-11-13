// Estados de carga de los medios de una burbuja: «Descargando…», «No disponible» y «Reintentar».
// El servidor sirve /media/<cuenta>/<id>; si el archivo caducó en WhatsApp, el backend le pide al teléfono
// que lo suba otra vez (puede tardar hasta ~30 s) y responde 404 si no se logra.
import { useCallback, useEffect, useState } from "react";
import { ImageOff, Loader2, RotateCw } from "lucide-react";
import { mediaUrl } from "@/api/media";

export type MediaPhase = "idle" | "loading" | "ready" | "error";

/** URL con el número de intento (`?r=N`) para saltar la caché tras un fallo; el intento 0 no la cambia. */
export function retryUrl(url: string, attempt: number): string {
  if (!url || attempt <= 0) return url;
  return `${url}${url.includes("?") ? "&" : "?"}r=${attempt}`;
}

/**
 * Estado de carga de un medio. `initial` es `loading` para lo que se pide al dibujarse (fotos, stickers) e
 * `idle` para lo que solo se pide al usarlo (audio, vídeo). Reintentar es siempre cosa de la persona:
 * nunca hay reintentos automáticos.
 */
export function useMediaLoad(url: string, initial: MediaPhase = "loading") {
  const [phase, setPhase] = useState<MediaPhase>(initial);
  const [attempt, setAttempt] = useState(0);

  // Otro medio en el mismo lugar: empieza de cero.
  useEffect(() => {
    setPhase(initial);
    setAttempt(0);
  }, [url, initial]);

  const retry = useCallback(() => {
    setAttempt((a) => a + 1);
    setPhase("loading");
  }, []);

  return {
    phase,
    setPhase,
    attempt,
    src: retryUrl(mediaUrl(url), attempt),
    retry,
    onLoaded: useCallback(() => setPhase("ready"), []),
    onFailed: useCallback(() => setPhase("error"), []),
  };
}

/** Píldora «Descargando…» con un giro discreto; aparece tras una pausa corta (no parpadea). */
export function MediaWait({ label = "Descargando…" }: { label?: string }) {
  return (
    <span
      role="status"
      className="anim-wait pointer-events-none inline-flex items-center gap-1.5 rounded-full bg-black/55 px-2.5 py-1 text-[11.5px] text-white"
    >
      <Loader2 size={13} className="animate-spin" aria-hidden="true" />
      {label}
    </span>
  );
}

/**
 * «No disponible» con el botón «Reintentar». `overThumb` es para cuando hay miniatura debajo: texto blanco
 * sobre un velo oscuro en lugar del gris del marcador.
 */
export function MediaUnavailable({
  onRetry,
  overThumb = false,
  compact = false,
  showIcon = true,
}: {
  onRetry: () => void;
  overThumb?: boolean;
  compact?: boolean;
  showIcon?: boolean;
}) {
  return (
    <div
      role="status"
      className={`flex flex-col items-center justify-center gap-1.5 text-center text-xs ${
        overThumb ? "absolute inset-0 bg-black/45 text-white" : "h-full w-full text-[var(--muted)]"
      }`}
    >
      {showIcon && !compact && <ImageOff size={24} aria-hidden="true" />}
      <span>No disponible</span>
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          onRetry();
        }}
        className="inline-flex items-center gap-1 rounded-full bg-black/15 px-2.5 py-1 text-[11.5px] font-medium hover:bg-black/25 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--accent)] dark:bg-white/15 dark:hover:bg-white/25"
      >
        <RotateCw size={12} aria-hidden="true" />
        Reintentar
      </button>
    </div>
  );
}
