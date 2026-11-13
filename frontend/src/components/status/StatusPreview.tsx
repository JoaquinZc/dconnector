import { Film } from "lucide-react";
import { mediaUrl } from "@/api/media";
import type { Status } from "@/api/types";
import { bgOf, fontCss } from "./statusModel";

interface Props {
  status: Pick<Status, "kind" | "text" | "bgColor" | "font" | "media">;
  className?: string;
}

/** Miniatura de un estado: color y texto, o la foto/el fotograma. */
export default function StatusPreview({ status, className }: Props) {
  const kind = String(status.kind);
  if (kind === "text") {
    return (
      <div
        className={`flex items-center justify-center overflow-hidden p-2 text-center text-[11px] font-semibold leading-tight text-white ${className ?? ""}`}
        style={{ background: bgOf(status.bgColor), fontFamily: fontCss(status.font) }}
      >
        <span className="line-clamp-5 break-words">{status.text}</span>
      </div>
    );
  }
  const src = status.media?.thumb || (kind === "image" ? status.media?.url : "");
  return (
    <div className={`relative overflow-hidden bg-neutral-800 ${className ?? ""}`}>
      {src ? <img src={mediaUrl(src)} alt="" draggable={false} className="h-full w-full object-cover" /> : null}
      {kind === "video" && (
        <span className="absolute bottom-1 right-1 rounded bg-black/60 p-0.5 text-white">
          <Film size={12} />
        </span>
      )}
    </div>
  );
}
