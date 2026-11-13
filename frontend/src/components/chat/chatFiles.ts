// Archivos del chat a partir de los mensajes cargados (panel de información).
import type { Message } from "../../api/core/model/models.js";

export interface ChatFiles {
  visual: Message[];
  documents: Message[];
}

/** Fotos/vídeos y documentos, de más reciente a más antiguo; sin mensajes eliminados. */
export function chatFiles(messages: Message[]): ChatFiles {
  const visual: Message[] = [];
  const documents: Message[] = [];
  for (const m of messages) {
    if (!m.media || m.deletedAt > 0) continue;
    const k = m.kind as string;
    if (k === "image" || k === "video") visual.push(m);
    else if (k === "document") documents.push(m);
  }
  return { visual: visual.reverse(), documents: documents.reverse() };
}
