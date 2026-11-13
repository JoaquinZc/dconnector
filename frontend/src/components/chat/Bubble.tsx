// Burbuja de un mensaje, para todos los tipos de contenido.
import React from "react";
import { Ban, Check, CheckCheck, ChevronDown, Clock, CircleAlert, Forward, TriangleAlert } from "lucide-react";
import type {
  ContactCard,
  Message,
} from "../../api/core/model/models.js";
import { deliveryTick, formatClock, senderColor, TICK_LABEL, unsupportedLabel, type Tick } from "./chatFormat";
import { WaText } from "./waText";
import {
  AudioPlayer,
  ContactView,
  DocumentView,
  ImageView,
  LocationView,
  PollView,
  StickerView,
  VideoView,
} from "./MediaViews";

export interface BubbleProps {
  msg: Message;
  showSender: boolean;
  first?: boolean;
  /** id nativo propio en la cuenta (para marcar mis votos) */
  selfId: string;
  canVote: boolean;
  highlighted?: boolean;
  onMenu: (msg: Message, at: { x: number; y: number }) => void;
  onOpenImage: (msg: Message) => void;
  onGoToQuoted: (nativeId: string) => void;
  /** Puede devolver una promesa con `false` si no se pudo guardar (el documento dice «No disponible»). */
  onSaveAs: (msg: Message) => Promise<boolean | void> | void;
  onVote: (msg: Message, options: string[]) => void;
  /** clave del chat directo de una tarjeta de contacto, si existe */
  findContactChat?: (card: ContactCard) => string | undefined;
  onOpenChat?: (chatKey: string) => void;
}

export function TickIcon({ tick }: { tick: Tick }) {
  const label = TICK_LABEL[tick];
  switch (tick) {
    case "pending":
      return <Clock size={13} aria-label={label} className="text-[var(--muted)]" />;
    case "failed":
      return <TriangleAlert size={13} aria-label={label} className="text-[var(--danger)]" />;
    case "sent":
      return <Check size={15} aria-label={label} className="text-[var(--muted)]" />;
    case "delivered":
      return <CheckCheck size={15} aria-label={label} className="text-[var(--muted)]" />;
    case "read":
      return <CheckCheck size={15} aria-label={label} className="text-[#53bdeb]" />;
  }
}

export function Footer({ msg, overlay = false }: { msg: Message; overlay?: boolean }) {
  const tick = deliveryTick(msg);
  return (
    <span
      className={`inline-flex items-center gap-1 whitespace-nowrap text-[11px] leading-none text-[var(--muted)] ${
        overlay ? "rounded-full bg-black/45 px-1.5 py-0.5 !text-white" : ""
      }`}
    >
      {msg.editedAt > 0 && <span>editado</span>}
      <span>{formatClock(msg.timestamp)}</span>
      {tick && <TickIcon tick={tick} />}
    </span>
  );
}

function Reactions({ msg }: { msg: Message }) {
  const groups = new Map<string, number>();
  for (const r of msg.reactions ?? []) {
    if (!r.emoji) continue;
    groups.set(r.emoji, (groups.get(r.emoji) ?? 0) + 1);
  }
  if (groups.size === 0) return null;
  const total = Array.from(groups.values()).reduce((a, b) => a + b, 0);
  const title = (msg.reactions ?? [])
    .filter((r) => r.emoji)
    .map((r) => `${r.senderName || r.senderNativeId} ${r.emoji}`)
    .join("\n");
  return (
    <div
      title={title}
      className={`-mt-1.5 mb-1 flex ${msg.fromMe ? "justify-end" : "justify-start"} px-2`}
    >
      <span className="inline-flex items-center gap-0.5 rounded-full border border-[var(--border)] bg-[var(--panel)] px-1.5 py-0.5 text-[13px] shadow-sm">
        {Array.from(groups.keys()).map((e) => (
          <span key={e}>{e}</span>
        ))}
        {total > 1 && <span className="ml-0.5 text-[11px] text-[var(--muted)]">{total}</span>}
      </span>
    </div>
  );
}

function Quote({ msg, onGo }: { msg: Message; onGo: () => void }) {
  if (!msg.quotedNativeId && !msg.quotedPreview) return null;
  return (
    <button
      type="button"
      onClick={onGo}
      className="mb-1 block w-full overflow-hidden rounded-md border-l-4 border-[var(--accent)] bg-black/5 px-2 py-1 text-left dark:bg-black/20"
      title={msg.quotedPreview}
    >
      {msg.quotedSender && (
        <div className="truncate text-[12px] font-semibold" style={{ color: senderColor(msg.quotedSender) }}>
          {msg.quotedSender}
        </div>
      )}
      <div className="line-clamp-2 break-words text-[12.5px] text-[var(--muted)]">{msg.quotedPreview || "Mensaje"}</div>
    </button>
  );
}

/**
 * Mensaje que el canal no sabe traducir. Si trae un texto («Mensaje de una sola vez: ábrelo en tu
 * teléfono», «Esperando este mensaje…») se muestra ese; solo si está vacío sale «Mensaje no compatible».
 */
function Unsupported({ text }: { text?: string }) {
  return (
    <div
      className="flex items-start gap-1.5 break-words text-[13px] italic text-[var(--muted)]"
      data-testid="unsupported"
    >
      <CircleAlert size={14} className="mt-[3px] shrink-0" aria-hidden="true" />
      <span className="min-w-0">{unsupportedLabel({ text })}</span>
    </div>
  );
}

function Content({ p }: { p: BubbleProps }) {
  const { msg } = p;
  const kind = msg.kind as string;
  const text = msg.text ?? "";
  const caption = text ? (
    <div className="mt-1 text-[14px] leading-[19px]">
      <WaText text={text} />
    </div>
  ) : null;
  switch (kind) {
    case "image":
      return msg.media ? (
        <>
          <ImageView media={msg.media} onOpen={() => p.onOpenImage(msg)} />
          {caption}
        </>
      ) : (
        <Unsupported text={text} />
      );
    case "video":
      return msg.media ? (
        <>
          <VideoView media={msg.media} />
          {caption}
        </>
      ) : (
        <Unsupported text={text} />
      );
    case "voice":
    case "audio":
      return msg.media ? (
        <>
          <AudioPlayer media={msg.media} voice={kind === "voice"} />
          {caption}
        </>
      ) : (
        <Unsupported text={text} />
      );
    case "document":
      return msg.media ? (
        <>
          <DocumentView media={msg.media} onSaveAs={() => p.onSaveAs(msg)} />
          {caption}
        </>
      ) : (
        <Unsupported text={text} />
      );
    case "sticker":
      return msg.media ? <StickerView media={msg.media} /> : <Unsupported text={text} />;
    case "location":
      return msg.location ? <LocationView loc={msg.location} /> : <Unsupported text={text} />;
    case "contact":
      return (
        <div className="flex flex-col gap-1.5">
          {(msg.contacts ?? []).map((c, i) => {
            const key = p.findContactChat?.(c);
            return (
              <ContactView
                key={`${c.phone}-${i}`}
                card={c}
                onOpenChat={key && p.onOpenChat ? () => p.onOpenChat!(key) : undefined}
              />
            );
          })}
        </div>
      );
    case "poll":
      return msg.poll ? (
        <PollView
          poll={msg.poll}
          selfId={p.selfId}
          canVote={p.canVote}
          onVote={(opts) => p.onVote(msg, opts)}
        />
      ) : (
        <Unsupported text={text} />
      );
    case "unsupported":
      return <Unsupported text={text} />;
    case "text":
    default:
      return (
        <div className="text-[14px] leading-[19px]">
          <WaText text={text} />
        </div>
      );
  }
}

/** Mensaje de sistema (altas, bajas, cambios de título): centrado y gris. */
function SystemMessage({ msg }: { msg: Message }) {
  return (
    <div className="my-1 flex justify-center px-6">
      <span className="max-w-[80%] rounded-lg bg-[var(--panel)]/80 px-3 py-1 text-center text-[12.5px] text-[var(--muted)] shadow-sm">
        {msg.text}
      </span>
    </div>
  );
}

function BubbleImpl(p: BubbleProps) {
  const { msg } = p;
  const kind = msg.kind as string;
  if (kind === "system") return <SystemMessage msg={msg} />;

  const deleted = msg.deletedAt > 0;
  const sticker = kind === "sticker" && !!msg.media && !deleted;
  const mediaOnly =
    !deleted && (kind === "image" || kind === "video") && !msg.text && !msg.quotedNativeId && !msg.quotedPreview;
  const openMenu = (e: React.MouseEvent) => {
    e.preventDefault();
    p.onMenu(msg, { x: e.clientX, y: e.clientY });
  };

  const bg = sticker ? "" : msg.fromMe ? "bg-[var(--bubble-out)]" : "bg-[var(--bubble-in)]";
  return (
    <div
      className={`flex ${msg.fromMe ? "justify-end" : "justify-start"} px-[4%] ${p.first ? "pt-2" : "pt-0.5"} ${
        (msg.reactions ?? []).some((r) => r.emoji) ? "pb-2" : ""
      }`}
      onContextMenu={openMenu}
      data-key={msg.key}
    >
      <div className="max-w-[min(65%,560px)] min-w-0">
        <div
          className={`group relative rounded-lg shadow-[0_1px_0.5px_rgba(11,20,26,0.13)] transition-shadow ${bg} ${
            sticker ? "" : mediaOnly ? "p-1" : "px-2 pb-1.5 pt-1"
          } ${p.highlighted ? "ring-2 ring-[var(--accent)]" : ""}`}
        >
          {!sticker && (
            <button
              type="button"
              aria-label="Opciones del mensaje"
              title="Opciones"
              onClick={(e) => {
                const r = e.currentTarget.getBoundingClientRect();
                p.onMenu(msg, { x: r.left, y: r.bottom });
              }}
              className="absolute right-1 top-1 z-10 hidden h-5 w-5 items-center justify-center rounded-full bg-[var(--panel)]/80 text-[var(--muted)] group-hover:flex"
            >
              <ChevronDown size={14} />
            </button>
          )}
          {p.showSender && (
            <div
              className="mb-0.5 truncate text-[12.5px] font-semibold"
              style={{ color: senderColor(msg.senderNativeId || msg.senderName) }}
              title={msg.senderName}
            >
              {msg.senderName || msg.senderNativeId}
            </div>
          )}
          {!deleted && msg.forwarded && (
            <div className="mb-0.5 flex items-center gap-1 text-[12px] italic text-[var(--muted)]">
              <Forward size={12} /> Reenviado
            </div>
          )}
          {!deleted && msg.fromStatusId && (
            <div className="mb-0.5 text-[12px] italic text-[var(--muted)]">Respuesta a un estado</div>
          )}
          {!deleted && <Quote msg={msg} onGo={() => p.onGoToQuoted(msg.quotedNativeId ?? "")} />}
          {deleted ? (
            <div className="flex items-center gap-1.5 text-[13.5px] italic text-[var(--muted)]">
              <Ban size={14} />
              {msg.fromMe ? "Eliminaste este mensaje" : "Se eliminó este mensaje"}
            </div>
          ) : (
            <Content p={p} />
          )}
          {sticker ? (
            <div className="mt-0.5 flex justify-end">
              <Footer msg={msg} overlay />
            </div>
          ) : (
            <div className="mt-0.5 flex justify-end">
              <Footer msg={msg} />
            </div>
          )}
        </div>
        {!deleted && <Reactions msg={msg} />}
      </div>
    </div>
  );
}

export const Bubble = React.memo(BubbleImpl);
export default Bubble;
