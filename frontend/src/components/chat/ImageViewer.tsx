// Visor de fotos a pantalla completa, con ← → entre las fotos del chat.
import { useEffect, useState } from "react";
import { mediaUrl } from "@/api/media";
import { ChevronLeft, ChevronRight, Download, X } from "lucide-react";
import type { Message } from "../../api/core/model/models.js";
import { formatClock } from "./chatFormat";
import { IconButton, Overlay } from "./parts";

export default function ImageViewer({
  images,
  startKey,
  onClose,
  onSaveAs,
}: {
  images: Message[];
  startKey: string;
  onClose: () => void;
  onSaveAs: (m: Message) => void;
}) {
  const [index, setIndex] = useState(Math.max(0, images.findIndex((m) => m.key === startKey)));
  const cur = images[index];

  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if (e.key === "ArrowLeft") setIndex((i) => Math.max(0, i - 1));
      if (e.key === "ArrowRight") setIndex((i) => Math.min(images.length - 1, i + 1));
    };
    window.addEventListener("keydown", key);
    return () => window.removeEventListener("keydown", key);
  }, [images.length]);

  if (!cur?.media) return null;
  return (
    <Overlay onClose={onClose} dark label="Visor de fotos">
      <div className="absolute inset-x-0 top-0 flex items-center gap-2 bg-gradient-to-b from-black/60 to-transparent px-4 py-3 text-white">
        <div className="min-w-0 flex-1">
          <div className="truncate text-[14px] font-medium">{cur.fromMe ? "Tú" : cur.senderName || "Foto"}</div>
          <div className="text-[12px] opacity-80">{formatClock(cur.timestamp)}</div>
        </div>
        <IconButton title="Guardar como" onClick={() => onSaveAs(cur)} className="!text-white hover:!bg-white/20">
          <Download size={20} />
        </IconButton>
        <IconButton title="Cerrar" onClick={onClose} className="!text-white hover:!bg-white/20">
          <X size={22} />
        </IconButton>
      </div>
      <img
        src={mediaUrl(cur.media.url)}
        alt={cur.media.fileName || "Foto"}
        className="max-h-[88vh] max-w-[92vw] object-contain"
        style={{ background: cur.media.thumb ? `center/cover url(${mediaUrl(cur.media.thumb)})` : undefined }}
      />
      {cur.text && (
        <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/60 to-transparent px-8 py-4 text-center text-[14px] text-white">
          {cur.text}
        </div>
      )}
      {index > 0 && (
        <button
          type="button"
          aria-label="Anterior"
          onClick={() => setIndex(index - 1)}
          className="absolute left-4 top-1/2 flex h-10 w-10 -translate-y-1/2 items-center justify-center rounded-full bg-white/15 text-white hover:bg-white/30"
        >
          <ChevronLeft size={24} />
        </button>
      )}
      {index < images.length - 1 && (
        <button
          type="button"
          aria-label="Siguiente"
          onClick={() => setIndex(index + 1)}
          className="absolute right-4 top-1/2 flex h-10 w-10 -translate-y-1/2 items-center justify-center rounded-full bg-white/15 text-white hover:bg-white/30"
        >
          <ChevronRight size={24} />
        </button>
      )}
    </Overlay>
  );
}
