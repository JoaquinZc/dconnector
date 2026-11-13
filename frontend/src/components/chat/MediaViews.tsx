// Contenido de las burbujas por tipo: foto, vídeo, voz/audio, documento, sticker, ubicación,
// contacto y encuesta. Los medios que se piden al servidor (/media/…) muestran «Descargando…» mientras
// llegan y «No disponible» con «Reintentar» si fallan (el reintento es solo a mano).
import { useEffect, useRef, useState } from "react";
import {
  Download,
  Loader2,
  FileText,
  MapPin,
  Pause,
  Play,
  RotateCw,
  Square,
  SquareCheck,
  Circle,
  CircleCheck,
  UserRound,
  Video as VideoIcon,
} from "lucide-react";
import type {
  ContactCard,
  Location,
  Media,
  Poll,
} from "../../api/core/model/models.js";
import { formatBytes, formatCoords, formatDuration, osmLink } from "./chatFormat";
import { mediaUrl } from "@/api/media";
import { MediaUnavailable, MediaWait, useMediaLoad } from "./MediaState";

const MAX_W = 320;
const MAX_H = 360;

function fitSize(media: Media): { width: number; height: number } {
  const w = media.width > 0 ? media.width : 240;
  const h = media.height > 0 ? media.height : 240;
  const k = Math.min(1, MAX_W / w, MAX_H / h);
  return { width: Math.max(80, Math.round(w * k)), height: Math.max(80, Math.round(h * k)) };
}

/** Tamaño de un sticker: el natural con tope de 160 px; sin medidas, 128×128. */
export const STICKER_MAX = 160;
export const STICKER_DEFAULT = 128;

export function stickerSize(media: Pick<Media, "width" | "height">): { width: number; height: number } {
  const w = media.width > 0 ? media.width : 0;
  const h = media.height > 0 ? media.height : 0;
  if (!w || !h) return { width: STICKER_DEFAULT, height: STICKER_DEFAULT };
  const k = Math.min(1, STICKER_MAX / Math.max(w, h));
  return { width: Math.max(32, Math.round(w * k)), height: Math.max(32, Math.round(h * k)) };
}

/**
 * Foto: la miniatura (si hay) se queda visible hasta que llega la imagen completa. Mientras llega se
 * dice «Descargando…»; si falla, «No disponible» y «Reintentar». Clic en la foto = visor.
 */
export function ImageView({ media, onOpen }: { media: Media; onOpen: () => void }) {
  const m = useMediaLoad(media.url);
  const { width, height } = fitSize(media);
  const failed = m.phase === "error";
  const loaded = m.phase === "ready";
  return (
    <div
      className="relative block overflow-hidden rounded-lg bg-black/10 dark:bg-white/10"
      style={{ width, height }}
      data-testid="image-view"
      data-phase={m.phase}
    >
      {media.thumb && (
        <img src={mediaUrl(media.thumb)} alt="" className={`absolute inset-0 h-full w-full object-cover ${loaded ? "" : "blur-[2px]"}`} />
      )}
      {!failed && media.url && (
        <img
          key={m.attempt}
          src={m.src}
          alt={media.fileName || "Foto"}
          loading="lazy"
          onLoad={m.onLoaded}
          onError={m.onFailed}
          className={`absolute inset-0 h-full w-full object-cover transition-opacity ${loaded ? "opacity-100" : "opacity-0"}`}
        />
      )}
      {!failed && (
        <button
          type="button"
          onClick={onOpen}
          title="Ver foto"
          aria-label="Ver foto"
          className="absolute inset-0 focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-[var(--accent)]"
        />
      )}
      {m.phase === "loading" && (
        <span className="pointer-events-none absolute inset-0 flex items-center justify-center">
          <MediaWait />
        </span>
      )}
      {failed && <MediaUnavailable onRetry={m.retry} overThumb={!!media.thumb} />}
    </div>
  );
}

/**
 * Vídeo: no se pide hasta que la persona lo reproduce (así un chat con muchos vídeos no dispara
 * decenas de descargas). Mientras llega, «Descargando…»; si falla, «No disponible» y «Reintentar».
 */
export function VideoView({ media }: { media: Media }) {
  const { width, height } = fitSize(media);
  const m = useMediaLoad(media.url, "idle");
  const failed = m.phase === "error";
  return (
    <div
      className="relative block overflow-hidden rounded-lg bg-black"
      style={{ width, height }}
      data-testid="video-view"
      data-phase={m.phase}
    >
      {failed ? (
        <>
          {media.thumb && <img src={mediaUrl(media.thumb)} alt="" className="absolute inset-0 h-full w-full object-cover" />}
          <MediaUnavailable onRetry={m.retry} overThumb={!!media.thumb} showIcon={!media.thumb} />
        </>
      ) : (
        <video
          key={m.attempt}
          controls
          preload="none"
          poster={mediaUrl(media.thumb) || undefined}
          src={m.src}
          className="block h-full w-full object-contain"
          onPlay={() => m.setPhase((p) => (p === "ready" ? p : "loading"))}
          onWaiting={() => m.setPhase("loading")}
          onLoadedData={m.onLoaded}
          onCanPlay={m.onLoaded}
          onPlaying={m.onLoaded}
          onError={m.onFailed}
        >
          <track kind="captions" />
        </video>
      )}
      {m.phase === "loading" && !failed && (
        <span className="pointer-events-none absolute left-1/2 top-3 -translate-x-1/2">
          <MediaWait />
        </span>
      )}
    </div>
  );
}

export const RATES = [1, 1.5, 2] as const;

export function rateLabel(r: number): string {
  return `${String(r).replace(".", ",")}×`;
}

/** Reproductor de nota de voz o audio, con velocidades 1× / 1,5× / 2×. El archivo se pide al reproducir. */
export function AudioPlayer({ media, voice }: { media: Media; voice: boolean }) {
  const ref = useRef<HTMLAudioElement>(null);
  const m = useMediaLoad(media.url, "idle");
  const [playing, setPlaying] = useState(false);
  const [current, setCurrent] = useState(0);
  const [duration, setDuration] = useState(media.duration || 0);
  const [rateIdx, setRateIdx] = useState(0);

  useEffect(() => {
    if (ref.current) ref.current.playbackRate = RATES[rateIdx];
  }, [rateIdx]);

  // «Reintentar» vuelve a pedir el archivo y lo reproduce.
  const { attempt, onFailed } = m;
  useEffect(() => {
    if (attempt > 0) ref.current?.play().catch((e: unknown) => failFrom(e, onFailed));
  }, [attempt, onFailed]);

  const toggle = () => {
    const a = ref.current;
    if (!a) return;
    if (a.paused) {
      m.setPhase((p) => (p === "ready" ? p : "loading"));
      void a.play().catch((e: unknown) => failFrom(e, m.onFailed));
    } else a.pause();
  };

  const pct = duration > 0 ? Math.min(100, (current / duration) * 100) : 0;
  return (
    <div className="flex w-[260px] max-w-full items-center gap-2 py-1" data-testid="audio-player" data-phase={m.phase}>
      <audio
        key={m.attempt}
        ref={ref}
        src={m.src}
        preload="none"
        onPlay={() => setPlaying(true)}
        onPause={() => {
          setPlaying(false);
          m.setPhase((p) => (p === "loading" ? "idle" : p));
        }}
        onEnded={() => {
          setPlaying(false);
          setCurrent(0);
        }}
        onTimeUpdate={(e) => setCurrent(e.currentTarget.currentTime)}
        onLoadedMetadata={(e) => {
          const d = e.currentTarget.duration;
          if (Number.isFinite(d) && d > 0) setDuration(d);
        }}
        onCanPlay={m.onLoaded}
        onPlaying={m.onLoaded}
        onWaiting={() => m.setPhase("loading")}
        onError={m.onFailed}
      />
      <button
        type="button"
        onClick={toggle}
        title={playing ? "Pausar" : "Reproducir"}
        aria-label={playing ? "Pausar" : "Reproducir"}
        className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-[var(--accent)] text-white"
      >
        {playing ? <Pause size={16} /> : <Play size={16} className="ml-0.5" />}
      </button>
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <input
          type="range"
          min={0}
          max={duration || 1}
          step={0.1}
          value={Math.min(current, duration || 1)}
          aria-label="Posición"
          onChange={(e) => {
            const v = Number(e.target.value);
            if (ref.current) ref.current.currentTime = v;
            setCurrent(v);
          }}
          className="h-1 w-full cursor-pointer accent-[var(--accent)]"
          style={{ background: `linear-gradient(to right, var(--accent) ${pct}%, rgba(128,128,128,.35) ${pct}%)` }}
        />
        <span className="flex min-w-0 items-center gap-1.5 text-[11px] text-[var(--muted)]">
          {m.phase === "loading" ? (
            <span role="status" className="anim-wait inline-flex items-center gap-1">
              <Loader2 size={11} className="animate-spin" aria-hidden="true" />
              Descargando…
            </span>
          ) : m.phase === "error" ? (
            <span role="status" className="inline-flex items-center gap-1.5">
              No disponible
              <button
                type="button"
                onClick={m.retry}
                className="inline-flex items-center gap-1 rounded-full bg-black/10 px-1.5 py-0.5 font-medium text-[var(--text)] hover:bg-black/20 dark:bg-white/15 dark:hover:bg-white/25"
              >
                <RotateCw size={10} aria-hidden="true" />
                Reintentar
              </button>
            </span>
          ) : (
            <span>{formatDuration(playing || current > 0 ? current : duration)}</span>
          )}
          {!voice && media.fileName && m.phase !== "error" ? <span className="truncate">· {media.fileName}</span> : null}
        </span>
      </div>
      <button
        type="button"
        onClick={() => setRateIdx((i) => (i + 1) % RATES.length)}
        title="Velocidad de reproducción"
        className="h-6 min-w-9 shrink-0 rounded-full bg-black/10 px-1.5 text-[11px] font-semibold dark:bg-white/15"
      >
        {rateLabel(RATES[rateIdx])}
      </button>
    </div>
  );
}

/** `play()` también rechaza al pausar a medias (AbortError) o si el navegador lo impide: solo un origen roto es un fallo. */
function failFrom(e: unknown, fail: () => void): void {
  if ((e as { name?: string } | null)?.name === "NotSupportedError") fail();
}

/**
 * Documento: «Guardar como» pide el archivo al servidor. `onSaveAs` puede devolver una promesa con `false`
 * si no se pudo; entonces se dice «No disponible» y se ofrece «Reintentar».
 */
export function DocumentView({ media, onSaveAs }: { media: Media; onSaveAs: () => Promise<boolean | void> | void }) {
  const [phase, setPhase] = useState<"idle" | "loading" | "error">("idle");
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  const save = async () => {
    if (phase === "loading") return;
    setPhase("loading");
    let ok = true;
    try {
      ok = (await onSaveAs()) !== false;
    } catch {
      ok = false;
    }
    if (alive.current) setPhase(ok ? "idle" : "error");
  };

  return (
    <div
      className="flex w-[280px] max-w-full items-center gap-3 rounded-lg bg-black/5 p-2.5 dark:bg-white/10"
      data-testid="document-view"
      data-phase={phase}
    >
      <FileText size={32} className="shrink-0 text-[var(--muted)]" />
      <div className="min-w-0 flex-1">
        <div className="truncate text-[13.5px]" title={media.fileName}>
          {media.fileName || "Documento"}
        </div>
        <div className="truncate text-[11px] text-[var(--muted)]">
          {phase === "loading" ? (
            <span role="status" className="inline-flex items-center gap-1">
              <Loader2 size={11} className="animate-spin" aria-hidden="true" />
              Descargando…
            </span>
          ) : phase === "error" ? (
            <span role="status">No disponible</span>
          ) : (
            formatBytes(media.size)
          )}
        </div>
      </div>
      <button
        type="button"
        onClick={() => void save()}
        disabled={phase === "loading"}
        title={phase === "error" ? "Reintentar" : "Guardar como"}
        className="flex shrink-0 items-center gap-1 rounded-full bg-black/10 px-2.5 py-1 text-[11px] font-medium hover:bg-black/15 disabled:opacity-60 dark:bg-white/15 dark:hover:bg-white/20"
      >
        {phase === "error" ? <RotateCw size={12} aria-hidden="true" /> : <Download size={12} aria-hidden="true" />}
        {phase === "error" ? "Reintentar" : "Guardar como"}
      </button>
    </div>
  );
}

/**
 * Sticker: sin burbuja y al tamaño natural (máx. 160 px; 128 si no se conocen las medidas). El marcador
 * ocupa el mismo lugar mientras llega, y «No disponible» ofrece «Reintentar».
 */
export function StickerView({ media }: { media: Media }) {
  const m = useMediaLoad(media.url);
  const { width, height } = stickerSize(media);
  if (!media.url) {
    return (
      <div className="flex items-center justify-center text-[var(--muted)]" style={{ width, height }}>
        <VideoIcon size={28} />
      </div>
    );
  }
  const failed = m.phase === "error";
  return (
    <div className="relative" style={{ width, height }} data-testid="sticker-view" data-phase={m.phase}>
      {media.thumb && !failed && m.phase !== "ready" && (
        <img src={mediaUrl(media.thumb)} alt="" className="absolute inset-0 h-full w-full object-contain blur-[2px]" />
      )}
      {!failed && (
        <img
          key={m.attempt}
          src={m.src}
          alt="Sticker"
          loading="lazy"
          onLoad={m.onLoaded}
          onError={m.onFailed}
          className={`h-full w-full object-contain transition-opacity ${m.phase === "ready" ? "opacity-100" : "opacity-0"}`}
        />
      )}
      {m.phase === "loading" && (
        <span className="pointer-events-none absolute inset-0 flex items-center justify-center rounded-xl bg-black/5 dark:bg-white/10">
          <MediaWait />
        </span>
      )}
      {failed && (
        <div className="absolute inset-0 rounded-xl bg-black/5 dark:bg-white/10">
          <MediaUnavailable onRetry={m.retry} compact />
        </div>
      )}
    </div>
  );
}

/** Tarjeta de ubicación: coordenadas y enlace a OpenStreetMap (sin mapa embebido). */
export function LocationView({ loc }: { loc: Location }) {
  return (
    <a
      href={osmLink(loc.lat, loc.lng)}
      target="_blank"
      rel="noreferrer noopener"
      className="flex w-[260px] max-w-full items-center gap-3 rounded-lg bg-black/5 p-2.5 hover:bg-black/10 dark:bg-white/10 dark:hover:bg-white/15"
      title="Abrir en OpenStreetMap"
      onClick={(e) => e.stopPropagation()}
    >
      <MapPin size={30} className="shrink-0 text-[#ea4335]" />
      <div className="min-w-0 flex-1">
        <div className="truncate text-[13.5px] font-medium">{loc.name || (loc.live ? "Ubicación en tiempo real" : "Ubicación")}</div>
        {loc.address && <div className="truncate text-[12px] text-[var(--muted)]" title={loc.address}>{loc.address}</div>}
        <div className="text-[11px] text-[var(--muted)]">{formatCoords(loc.lat, loc.lng)}</div>
        <div className="text-[11px] text-[#027eb5] dark:text-[#53bdeb]">Ver en OpenStreetMap</div>
      </div>
    </a>
  );
}

export function ContactView({
  card,
  onOpenChat,
}: {
  card: ContactCard;
  onOpenChat?: () => void;
}) {
  return (
    <div className="w-[260px] max-w-full rounded-lg bg-black/5 p-2.5 dark:bg-white/10">
      <div className="flex items-center gap-3">
        <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-[var(--muted)]/30 text-[var(--muted)]">
          <UserRound size={20} />
        </span>
        <div className="min-w-0 flex-1">
          <div className="truncate text-[13.5px] font-medium" title={card.name}>{card.name || "Contacto"}</div>
          {card.phone && <div className="truncate text-[12px] text-[var(--muted)]">{card.phone}</div>}
        </div>
      </div>
      {onOpenChat && (
        <button
          type="button"
          onClick={onOpenChat}
          className="mt-2 w-full rounded-full border border-[var(--border)] py-1 text-[12px] font-medium text-[var(--accent)] hover:bg-black/5 dark:hover:bg-white/10"
        >
          Abrir chat
        </button>
      )}
    </div>
  );
}

/** Nuevo conjunto de opciones elegidas al pulsar una (selección única o múltiple). */
export function nextVote(poll: Poll, selfId: string, option: string): string[] {
  const mine = (poll.options ?? []).filter((o) => (o.voters ?? []).includes(selfId)).map((o) => o.name);
  if (!poll.multi) return mine.length === 1 && mine[0] === option ? [] : [option];
  return mine.includes(option) ? mine.filter((n) => n !== option) : [...mine, option];
}

export function PollView({
  poll,
  selfId,
  canVote,
  onVote,
}: {
  poll: Poll;
  selfId: string;
  canVote: boolean;
  onVote: (options: string[]) => void;
}) {
  const total = (poll.options ?? []).reduce((n, o) => n + (o.voters?.length ?? 0), 0);
  return (
    <div className="w-[280px] max-w-full py-0.5">
      <div className="text-[14px] font-medium break-words">{poll.question}</div>
      <div className="mb-1.5 text-[11px] text-[var(--muted)]">{poll.multi ? "Selecciona una o más opciones" : "Selecciona una opción"}</div>
      <div className="flex flex-col gap-2">
        {(poll.options ?? []).map((o) => {
          const n = o.voters?.length ?? 0;
          const mine = (o.voters ?? []).includes(selfId);
          const pct = total > 0 ? (n / total) * 100 : 0;
          const Icon = poll.multi ? (mine ? SquareCheck : Square) : mine ? CircleCheck : Circle;
          return (
            <button
              key={o.name}
              type="button"
              disabled={!canVote}
              onClick={() => onVote(nextVote(poll, selfId, o.name))}
              className="text-left disabled:cursor-default"
              title={canVote ? "Votar" : undefined}
            >
              <div className="flex items-center gap-2 text-[13px]">
                <Icon size={16} className={mine ? "text-[var(--accent)]" : "text-[var(--muted)]"} />
                <span className="min-w-0 flex-1 truncate">{o.name}</span>
                <span className="text-[12px] text-[var(--muted)]">{n}</span>
              </div>
              <div className="ml-6 mt-1 h-1 overflow-hidden rounded-full bg-black/10 dark:bg-white/15">
                <div className="h-full rounded-full bg-[var(--accent)]" style={{ width: `${pct}%` }} />
              </div>
            </button>
          );
        })}
      </div>
      <div className="mt-1.5 text-[11px] text-[var(--muted)]">{total === 1 ? "1 voto" : `${total} votos`}</div>
    </div>
  );
}
