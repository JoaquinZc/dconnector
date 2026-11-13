import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { mediaUrl } from "@/api/media";
import { createPortal } from "react-dom";
import { ChevronLeft, ChevronRight, Loader2, Pause, Reply, X } from "lucide-react";
import { api } from "@/api";
import type { CapSet, Status, StatusAuthor } from "@/api/types";
import Avatar from "@/components/common/Avatar";
import { channelName } from "@/lib/channels";
import { ChannelGlyph } from "./ChannelGlyph";
import { replyToStatus } from "./replyToStatus";
import {
  advanceProgress,
  authorKey,
  barFill,
  bgOf,
  fontCss,
  formatAgo,
  liveSorted,
  nextCursor,
  prevCursor,
  startItem,
  storyDurationMs,
  type Cursor,
} from "./statusModel";

export const QUICK_REACTIONS = ["💚", "😂", "😮", "😢", "🙏", "🔥"];

/** Milisegundos manteniendo pulsado hasta que cuenta como «mantener» (pausa). */
export const HOLD_MS = 200;
const TICK_MS = 50;

/** Lo que el visor necesita saber de la cuenta de cada autor. */
export interface ViewerAccount {
  label: string;
  channel: string;
  caps?: CapSet | null;
}

interface Props {
  /**
   * Autores en el orden del visor; al terminar uno pasa al siguiente. Cada autor lleva su `accountId`:
   * con ese id se piden sus estados, se marcan vistos y se reacciona.
   */
  authors: StatusAuthor[];
  /** Modo modal: por dónde empezar. */
  startIndex?: number;
  /** Datos de cada cuenta por id (etiqueta, canal y capacidades). */
  accounts?: Record<string, ViewerAccount>;
  /** Capacidades por defecto si `accounts` no trae la cuenta del autor. */
  caps?: CapSet | null;
  /** Modo modal: se llama al terminar el último autor, con Esc, con la X y al responder. */
  onClose?: () => void;
  /** Dentro de la pantalla (sin portal ni fondo negro ni cerrar); al terminar se queda en el último. */
  inline?: boolean;
  /** En línea: autor mostrado (`authorKey`). Si cambia desde fuera, el visor salta a él. */
  selectedKey?: string | null;
  /** En línea: el visor cambió de autor por sí mismo. */
  onSelect?: (key: string) => void;
  /** Se marcó un estado de este autor como visto (para refrescar la lista). */
  onViewed?: (author: StatusAuthor) => void;
  /** Pausa desde fuera (p. ej. hay un diálogo abierto encima). */
  paused?: boolean;
}

interface Pos {
  key: string;
  item: number;
}

/** ¿El foco está en un campo donde las flechas mueven el cursor del texto? */
function isTextTarget(t: EventTarget | null): boolean {
  if (!(t instanceof HTMLElement)) return false;
  return t.isContentEditable || t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.tagName === "SELECT";
}

export default function StoryViewer({
  authors,
  startIndex = 0,
  accounts,
  caps,
  onClose,
  inline = false,
  selectedKey,
  onSelect,
  onViewed,
  paused: pausedOutside = false,
}: Props) {
  const keys = useMemo(() => authors.map(authorKey), [authors]);
  const [pos, setPos] = useState<Pos>(() => {
    const byKey = selectedKey ? keys.indexOf(selectedKey) : -1;
    const i = byKey >= 0 ? byKey : Math.max(0, Math.min(startIndex, authors.length - 1));
    return { key: keys[i] ?? "", item: 0 };
  });
  const [groups, setGroups] = useState<Record<string, Status[]>>({});
  const [progress, setProgress] = useState(0);
  const [held, setHeld] = useState(false);
  const [footerHover, setFooterHover] = useState(false);
  const [readyKey, setReadyKey] = useState<string | null>(null);
  const [reacted, setReacted] = useState<{ key: string; emoji: string } | null>(null);

  const requested = useRef(new Map<string, string>());
  const positioned = useRef(new Set<string>());
  const viewed = useRef(new Set<string>());
  const holdTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const holdEngaged = useRef(false);
  const videoRef = useRef<HTMLVideoElement>(null);
  const lastGroup = useRef(0);
  const prevSelected = useRef(selectedKey);
  const pendingSelect = useRef<string | null>(null);

  // Posición en la lista actual: se busca por clave, no por índice, para que recargar la lista no mueva el visor.
  let group = keys.indexOf(pos.key);
  if (group < 0) group = Math.min(lastGroup.current, keys.length - 1);
  lastGroup.current = Math.max(0, group);
  const author: StatusAuthor | undefined = authors[group];
  const curKey = author ? keys[group] : "";
  const list = curKey ? groups[curKey] : undefined;
  const item = curKey === pos.key ? pos.item : 0;
  const current: Status | undefined = list?.[item];
  const isVideo = String(current?.kind) === "video";
  const paused = held || footerHover || pausedOutside;

  const sizes = useMemo(() => keys.map((k) => groups[k]?.length), [keys, groups]);
  const sizesRef = useRef(sizes);
  sizesRef.current = sizes;
  const keysRef = useRef(keys);
  keysRef.current = keys;
  const cursorRef = useRef<Cursor>({ group, item });
  cursorRef.current = { group, item };
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  const selectRef = useRef(onSelect);
  selectRef.current = onSelect;
  const viewedRef = useRef(onViewed);
  viewedRef.current = onViewed;
  const selectedRef = useRef(selectedKey);
  selectedRef.current = selectedKey;
  const curKeyRef = useRef(curKey);
  curKeyRef.current = curKey;

  const go = useCallback((c: Cursor) => {
    const key = keysRef.current[c.group];
    if (key === undefined) return;
    setPos({ key, item: c.item });
    setProgress(0);
  }, []);

  const next = useCallback(() => {
    const n = nextCursor(cursorRef.current, sizesRef.current);
    if (n) go(n);
    else if (!inline) closeRef.current?.();
  }, [go, inline]);

  const prev = useCallback(() => {
    go(prevCursor(cursorRef.current));
  }, [go]);

  const jumpAuthor = (d: number) => {
    const g = cursorRef.current.group + d;
    if (g >= 0 && g < keysRef.current.length) go({ group: g, item: 0 });
  };

  // La clave del visor se corrige si su autor desapareció de la lista.
  useEffect(() => {
    if (curKey && pos.key !== curKey) setPos({ key: curKey, item: 0 });
  }, [curKey, pos.key]);

  // En línea: elegir a alguien en la lista salta a él y vuelve a pedir sus estados.
  // Se recuerda la petición hasta que el autor esté en la lista del visor (la lista puede llegar un render después).
  useEffect(() => {
    if (selectedKey !== prevSelected.current) {
      prevSelected.current = selectedKey;
      pendingSelect.current = selectedKey ?? null;
    }
    const want = pendingSelect.current;
    if (!inline || !want) return;
    if (curKeyRef.current === want) {
      pendingSelect.current = null;
      return;
    }
    if (!keysRef.current.includes(want)) return;
    pendingSelect.current = null;
    requested.current.delete(want);
    positioned.current.delete(want);
    setGroups((p) => {
      const n = { ...p };
      delete n[want];
      return n;
    });
    setPos({ key: want, item: 0 });
    setProgress(0);
  }, [inline, selectedKey, keys]);

  // En línea: avisa de que el visor pasó a otro autor (flechas, fin de un autor).
  useEffect(() => {
    if (!inline || !curKey || curKey === selectedRef.current) return;
    selectRef.current?.(curKey);
  }, [inline, curKey]);

  // Carga los estados del autor actual y precarga el siguiente; si cambió su número de estados, los vuelve a pedir.
  useEffect(() => {
    for (const g of [group, group + 1]) {
      const a = authors[g];
      if (!a) continue;
      const key = keys[g];
      const fp = `${a.count}:${a.lastAt}`;
      if (requested.current.get(key) === fp) continue;
      requested.current.set(key, fp);
      api.statuses
        .ByAuthor(a.accountId, a.authorId)
        .then((raw) => setGroups((p) => ({ ...p, [key]: liveSorted(raw) })))
        .catch(() => setGroups((p) => ({ ...p, [key]: [] })));
    }
  }, [authors, keys, group]);

  // Al llegar un grupo: empieza por el primer estado no visto; si está vacío, en el modal pasa al siguiente.
  useEffect(() => {
    if (!list || !curKey) return;
    if (list.length === 0) {
      if (!inline) next();
      return;
    }
    if (!positioned.current.has(curKey)) {
      positioned.current.add(curKey);
      const i = startItem(list);
      if (i !== item) go({ group, item: i });
    }
  }, [list, curKey, item, group, inline, go, next]);

  // «Visto» al mostrarlo (los míos no), con la cuenta del autor.
  useEffect(() => {
    if (!current || !author || current.fromMe || viewed.current.has(current.key)) return;
    viewed.current.add(current.key);
    const a = author;
    void api.statuses
      .MarkViewed(a.accountId, current.key)
      .then(() => viewedRef.current?.(a))
      .catch(() => undefined);
  }, [author, current]);

  const mediaReady =
    !current || (String(current.kind) === "text" ? true : !current.media?.url || readyKey === current.key);

  // Reloj de fotos y textos (el vídeo avanza con su propia reproducción).
  useEffect(() => {
    if (!current || isVideo || paused || !mediaReady) return;
    const duration = storyDurationMs(current);
    let last = Date.now();
    const t = setInterval(() => {
      const now = Date.now();
      const dt = now - last;
      last = now;
      setProgress((p) => advanceProgress(p, dt, duration, false).value);
    }, TICK_MS);
    return () => clearInterval(t);
  }, [current, isVideo, paused, mediaReady]);

  useEffect(() => {
    if (progress >= 1 && !isVideo) next();
  }, [progress, isVideo, next]);

  // Pausa del vídeo con la pausa del visor.
  useEffect(() => {
    const v = videoRef.current;
    if (!v) return;
    if (paused) v.pause();
    else void v.play?.()?.catch?.(() => undefined);
  }, [paused, current?.key]);

  // Teclado. En línea, las flechas no actúan si se está escribiendo ni si hay un diálogo abierto.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (inline) {
        if (e.defaultPrevented || e.ctrlKey || e.metaKey || e.altKey) return;
        if (isTextTarget(e.target)) return;
        if (document.querySelector('[role="dialog"][aria-modal="true"]')) return;
      }
      if (e.key === "ArrowRight") next();
      else if (e.key === "ArrowLeft") prev();
      else if (e.key === "Escape" && !inline) closeRef.current?.();
      else return;
      e.preventDefault();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [inline, next, prev]);

  useEffect(
    () => () => {
      if (holdTimer.current) clearTimeout(holdTimer.current);
    },
    [],
  );

  // Mantener pulsado = pausa; un toque corto = ir al lado pulsado.
  const pressStart = () => {
    holdEngaged.current = false;
    if (holdTimer.current) clearTimeout(holdTimer.current);
    holdTimer.current = setTimeout(() => {
      holdEngaged.current = true;
      setHeld(true);
    }, HOLD_MS);
  };
  const pressEnd = (e?: { clientX: number; currentTarget: HTMLElement }) => {
    if (holdTimer.current) clearTimeout(holdTimer.current);
    holdTimer.current = null;
    if (holdEngaged.current) {
      holdEngaged.current = false;
      setHeld(false);
      return;
    }
    if (!e) return;
    const rect = e.currentTarget.getBoundingClientRect();
    const x = rect.width > 0 ? (e.clientX - rect.left) / rect.width : 1;
    if (x < 0.3) prev();
    else next();
  };
  const pressCancel = () => {
    if (holdTimer.current) clearTimeout(holdTimer.current);
    holdTimer.current = null;
    if (holdEngaged.current) {
      holdEngaged.current = false;
      setHeld(false);
    }
  };

  const react = async (emoji: string) => {
    if (!current || !author) return;
    try {
      await api.statuses.React(author.accountId, current.key, emoji);
      setReacted({ key: current.key, emoji });
    } catch {
      /* el aviso ya salió */
    }
  };

  const reply = () => {
    if (!current) return;
    const st = current;
    onClose?.();
    replyToStatus(st);
  };

  const count = list?.length ?? 0;
  const acc = author ? accounts?.[String(author.accountId)] : undefined;
  const accCaps = acc?.caps ?? caps;
  const canReact = !!accCaps?.statusReact && !!current && !current.fromMe;

  const card = (
    <div
      className={
        inline
          ? "relative flex h-full max-h-[604px] flex-col overflow-hidden rounded-[20px] bg-neutral-900 text-white shadow-[0_16px_40px_rgba(11,20,26,0.28)]"
          : "relative overflow-hidden rounded-xl bg-neutral-900"
      }
      style={
        inline
          ? { aspectRatio: "9 / 16", maxWidth: "100%" }
          : { height: "min(calc(100vh - 32px), 860px)", aspectRatio: "9 / 16", maxWidth: "calc(100vw - 120px)" }
      }
    >
      {/* Barras de progreso */}
      <div className="absolute inset-x-0 top-0 z-20 flex gap-1 px-3.5 pb-0 pt-3">
        {Array.from({ length: Math.max(count, 1) }, (_, i) => (
          <div key={i} className="h-[3px] flex-1 overflow-hidden rounded-full bg-white/30">
            <div
              data-testid="story-bar"
              data-fill={barFill(i, item, progress).toFixed(2)}
              className="h-full bg-white"
              style={{ width: `${barFill(i, item, progress) * 100}%` }}
            />
          </div>
        ))}
      </div>

      {/* Cabecera */}
      <div className="pointer-events-none absolute inset-x-0 top-3 z-20 flex items-center gap-2.5 bg-gradient-to-b from-black/60 to-transparent px-3.5 pb-6 pt-3">
        <Avatar src={author?.avatarUrl} name={author?.authorName ?? "?"} size={38} colorKey={author?.authorId} className="ring-2 ring-white/90" />
        <div className="min-w-0 flex-1">
          <p className="truncate text-[14.5px] font-semibold" title={author?.authorName}>
            {author?.authorName}
          </p>
          {current && <p className="text-xs text-white/90">{formatAgo(current.postedAt)}</p>}
        </div>
        {paused && <Pause size={16} aria-label="En pausa" />}
        {acc && (
          <span
            data-testid="story-account"
            className="inline-flex h-[22px] max-w-[45%] items-center gap-[5px] rounded-[11px] bg-black/30 px-2 text-[11.5px] font-semibold"
          >
            <ChannelGlyph channel={acc.channel} size={12} />
            <span className="truncate" title={`${channelName(acc.channel)} · ${acc.label}`}>
              {acc.label}
            </span>
          </span>
        )}
      </div>

      {/* Escenario: tocar a los lados navega; mantener pausa */}
      <div
        data-testid="story-stage"
        className="absolute inset-0 z-10 select-none"
        onMouseDown={pressStart}
        onMouseUp={pressEnd}
        onMouseLeave={pressCancel}
      >
        {!current ? (
          <div className="flex h-full items-center justify-center px-6 text-center text-white/70">
            {list && list.length === 0 ? "Sin estados vigentes" : <Loader2 className="animate-spin" size={28} />}
          </div>
        ) : (
          <StoryContent
            status={current}
            videoRef={videoRef}
            onReady={() => setReadyKey(current.key)}
            onVideoProgress={setProgress}
            onVideoEnd={next}
          />
        )}
      </div>

      {/* Pie: reaccionar y responder */}
      {current && !current.fromMe && (
        <div
          className="absolute inset-x-0 bottom-0 z-20 flex flex-col items-center gap-2 bg-gradient-to-t from-black/70 to-transparent px-3 pb-3 pt-10"
          onMouseEnter={() => setFooterHover(true)}
          onMouseLeave={() => setFooterHover(false)}
          onFocus={() => setFooterHover(true)}
          onBlur={() => setFooterHover(false)}
        >
          {canReact && (
            <div className="flex items-center gap-1" role="group" aria-label="Reaccionar">
              {QUICK_REACTIONS.map((e) => (
                <button
                  key={e}
                  type="button"
                  aria-label={`Reaccionar ${e}`}
                  onClick={() => void react(e)}
                  className={`flex h-9 w-9 items-center justify-center rounded-full text-xl transition-transform hover:scale-125 ${
                    reacted?.key === current.key && reacted.emoji === e ? "bg-white/25" : ""
                  }`}
                >
                  {e}
                </button>
              ))}
            </div>
          )}
          <button type="button" className="btn btn-sm !border-white/30 !bg-white/10 !text-white hover:!bg-white/20" onClick={reply}>
            <Reply size={14} />
            Responder
          </button>
        </div>
      )}
    </div>
  );

  const rootData = {
    "data-testid": "story-viewer",
    "data-group": group,
    "data-item": item,
    "data-paused": paused ? "1" : "0",
  } as const;

  if (inline) {
    return (
      <div
        role="region"
        aria-label={`Estados de ${author?.authorName ?? ""}`}
        {...rootData}
        className="flex h-full min-h-0 items-center justify-center gap-7 bg-chat px-4 py-4"
      >
        <button
          type="button"
          className="flex h-11 w-11 shrink-0 items-center justify-center rounded-full border border-line bg-panel text-ink hover:bg-[var(--hover)] disabled:cursor-not-allowed disabled:opacity-40"
          aria-label="Autor anterior"
          title="Autor anterior"
          disabled={group <= 0}
          onClick={() => jumpAuthor(-1)}
        >
          <ChevronLeft size={20} aria-hidden />
        </button>
        <div className="flex h-full min-w-0 items-center justify-center">{card}</div>
        <button
          type="button"
          className="flex h-11 w-11 shrink-0 items-center justify-center rounded-full border border-line bg-panel text-ink hover:bg-[var(--hover)] disabled:cursor-not-allowed disabled:opacity-40"
          aria-label="Autor siguiente"
          title="Autor siguiente"
          disabled={group >= keys.length - 1}
          onClick={() => jumpAuthor(1)}
        >
          <ChevronRight size={20} aria-hidden />
        </button>
      </div>
    );
  }

  return createPortal(
    <div
      role="dialog"
      aria-modal="true"
      aria-label={`Estados de ${author?.authorName ?? ""}`}
      {...rootData}
      className="anim-fade fixed inset-0 z-[60] flex items-center justify-center bg-black/90 text-white"
    >
      <button
        className="icon-btn absolute left-4 top-1/2 -translate-y-1/2 !text-white/80 hover:!text-white"
        aria-label="Anterior"
        title="Anterior"
        onClick={prev}
      >
        <ChevronLeft size={26} />
      </button>
      <button
        className="icon-btn absolute right-4 top-1/2 -translate-y-1/2 !text-white/80 hover:!text-white"
        aria-label="Siguiente"
        title="Siguiente"
        onClick={next}
      >
        <ChevronRight size={26} />
      </button>
      <button
        className="icon-btn absolute right-4 top-4 !text-white/80 hover:!text-white"
        aria-label="Cerrar"
        title="Cerrar"
        onClick={onClose}
      >
        <X size={22} />
      </button>
      {card}
    </div>,
    document.body,
  );
}

interface ContentProps {
  status: Status;
  videoRef: React.RefObject<HTMLVideoElement | null>;
  onReady: () => void;
  onVideoProgress: (p: number) => void;
  onVideoEnd: () => void;
}

function StoryContent({ status, videoRef, onReady, onVideoProgress, onVideoEnd }: ContentProps) {
  const kind = String(status.kind);
  const url = mediaUrl(status.media?.url ?? "");

  if (kind === "text") {
    const len = status.text.length;
    const size = len < 40 ? 30 : len < 120 ? 24 : len < 300 ? 19 : 15;
    return (
      <div
        className="flex h-full w-full items-center justify-center overflow-hidden p-8 text-center font-semibold"
        style={{ background: bgOf(status.bgColor), fontFamily: fontCss(status.font), fontSize: size, lineHeight: 1.3 }}
      >
        <span className="whitespace-pre-wrap break-words">{status.text}</span>
      </div>
    );
  }

  return (
    <div className="relative flex h-full w-full items-center justify-center bg-black">
      {kind === "video" ? (
        <video
          key={status.key}
          ref={videoRef}
          src={url}
          poster={mediaUrl(status.media?.thumb) || undefined}
          autoPlay
          playsInline
          className="h-full w-full object-contain"
          onLoadedData={onReady}
          onTimeUpdate={(e) => {
            const v = e.currentTarget;
            if (v.duration > 0) onVideoProgress(Math.min(1, v.currentTime / v.duration));
          }}
          onEnded={onVideoEnd}
          onError={onVideoEnd}
        />
      ) : (
        <img
          key={status.key}
          src={url}
          alt=""
          draggable={false}
          className="h-full w-full object-contain"
          onLoad={onReady}
          onError={onReady}
        />
      )}
      {status.text && (
        <p className="absolute inset-x-0 bottom-24 mx-auto max-w-[90%] rounded-lg bg-black/50 px-3 py-2 text-center text-sm">
          {status.text}
        </p>
      )}
    </div>
  );
}
