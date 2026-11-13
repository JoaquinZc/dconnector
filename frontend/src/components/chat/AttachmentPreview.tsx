// Vista previa de los archivos a enviar, con pie (texto) por archivo.
import { FileText, Plus, Send, X } from "lucide-react";
import { mediaUrl } from "@/api/media";
import type { Attachment } from "./composerReducer";
import { formatBytes } from "./chatFormat";
import { IconButton, Overlay } from "./parts";

/** Vista previa de un adjunto: archivo subido (`upload://`, modo remoto) o ruta local (`/local?path=`, escritorio). */
export const localUrl = (path: string) =>
  path.startsWith("upload://") ? mediaUrl(path) : `/local?path=${encodeURIComponent(path)}`;

function Big({ a }: { a: Attachment }) {
  if (a.kind === "image" || a.kind === "sticker") {
    return <img src={localUrl(a.path)} alt={a.name} className="max-h-[52vh] max-w-full rounded-lg object-contain" />;
  }
  if (a.kind === "video") {
    return (
      <video src={localUrl(a.path)} controls className="max-h-[52vh] max-w-full rounded-lg bg-black">
        <track kind="captions" />
      </video>
    );
  }
  return (
    <div className="flex h-48 w-72 flex-col items-center justify-center gap-2 rounded-lg bg-[var(--panel-2)]">
      <FileText size={56} className="text-[var(--muted)]" />
      <div className="max-w-[90%] truncate text-[14px]" title={a.name}>{a.name}</div>
      <div className="text-[12px] text-[var(--muted)]">{formatBytes(a.size)}</div>
    </div>
  );
}

function Thumb({ a, active, onClick, onRemove }: { a: Attachment; active: boolean; onClick: () => void; onRemove: () => void }) {
  return (
    <div className={`group relative h-14 w-14 shrink-0 cursor-pointer overflow-hidden rounded-lg border-2 ${active ? "border-[var(--accent)]" : "border-transparent"}`} onClick={onClick}>
      {a.kind === "image" || a.kind === "sticker" ? (
        <img src={localUrl(a.path)} alt="" className="h-full w-full object-cover" />
      ) : (
        <div className="flex h-full w-full items-center justify-center bg-[var(--panel-2)] text-[var(--muted)]">
          <FileText size={22} />
        </div>
      )}
      <button
        type="button"
        title="Quitar"
        onClick={(e) => {
          e.stopPropagation();
          onRemove();
        }}
        className="absolute right-0 top-0 hidden h-5 w-5 items-center justify-center rounded-bl-lg bg-black/60 text-white group-hover:flex"
      >
        <X size={12} />
      </button>
    </div>
  );
}

export default function AttachmentPreview({
  attachments,
  active,
  sending,
  onSelect,
  onRemove,
  onCaption,
  onAddMore,
  onCancel,
  onSend,
}: {
  attachments: Attachment[];
  active: number;
  sending: boolean;
  onSelect: (i: number) => void;
  onRemove: (id: string) => void;
  onCaption: (id: string, caption: string) => void;
  onAddMore?: () => void;
  onCancel: () => void;
  onSend: () => void;
}) {
  const cur = attachments[active];
  if (!cur) return null;
  const captionable = cur.kind === "image" || cur.kind === "video" || cur.kind === "document";
  return (
    <Overlay onClose={sending ? () => undefined : onCancel} width={560} label="Vista previa">
      <div className="flex items-center gap-2 border-b border-[var(--border)] px-3 py-2">
        <IconButton title="Cancelar" onClick={onCancel} disabled={sending}>
          <X size={20} />
        </IconButton>
        <span className="min-w-0 flex-1 truncate text-[14px] font-medium" title={cur.name}>{cur.name}</span>
      </div>
      <div className="flex min-h-[220px] flex-1 items-center justify-center overflow-auto bg-[var(--panel-2)] p-4">
        <Big a={cur} />
      </div>
      <div className="flex items-center gap-2 border-t border-[var(--border)] p-3">
        <input
          key={cur.id}
          value={captionable ? cur.caption : ""}
          disabled={!captionable || sending}
          onChange={(e) => onCaption(cur.id, e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !sending) onSend();
          }}
          placeholder={captionable ? "Añade un pie" : "Sin pie para este tipo"}
          className="min-w-0 flex-1 rounded-lg border border-[var(--border)] bg-[var(--panel-2)] px-3 py-2 text-[14px] outline-none focus:border-[var(--accent)] disabled:opacity-60"
          autoFocus
        />
      </div>
      <div className="flex items-center gap-2 px-3 pb-3">
        <div className="flex min-w-0 flex-1 items-center gap-2 overflow-x-auto">
          {attachments.map((a, i) => (
            <Thumb key={a.id} a={a} active={i === active} onClick={() => onSelect(i)} onRemove={() => onRemove(a.id)} />
          ))}
          {onAddMore && (
            <button type="button" title="Añadir más" onClick={onAddMore} className="flex h-14 w-14 shrink-0 items-center justify-center rounded-lg border border-dashed border-[var(--border)] text-[var(--muted)] hover:bg-black/5 dark:hover:bg-white/10">
              <Plus size={20} />
            </button>
          )}
        </div>
        <button
          type="button"
          onClick={onSend}
          disabled={sending}
          title="Enviar"
          className="flex h-12 w-12 shrink-0 items-center justify-center rounded-full bg-[var(--accent)] text-white disabled:opacity-60"
        >
          {sending ? <span className="h-5 w-5 animate-spin rounded-full border-2 border-white/40 border-t-white" /> : <Send size={20} />}
        </button>
      </div>
    </Overlay>
  );
}
