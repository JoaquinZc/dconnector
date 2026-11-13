// Archivos del lado del cliente cuando no hay diálogo de escritorio (modo remoto o navegador): se elige con
// `<input type=file>`, se sube a `POST /files` y el resto de la interfaz usa `upload://<id>` donde iría una ruta.
import { ApiError, request } from "./http";
import type { PickedFile } from "./types";

export const UPLOAD_SCHEME = "upload://";

interface UploadedFile {
  id: string;
  name: string;
  mime: string;
  size: number;
}

/** Tope del servidor por archivo (100 MiB): se avisa antes de gastar red. */
export const MAX_UPLOAD_BYTES = 100 * 1024 * 1024;

/** Quita separadores de ruta y caracteres de control (y los de inversión de texto) de un nombre de archivo. */
export function safeFileName(name: string, fallback = "archivo"): string {
  // eslint-disable-next-line no-control-regex
  const clean = name.replace(/[\u0000-\u001f\u007f\u202a-\u202e]/g, "").split(/[\\/]/).pop()?.trim() ?? "";
  return clean && clean !== "." && clean !== ".." ? clean.slice(0, 200) : fallback;
}

/** Sube un archivo y devuelve la ficha con `path = upload://<id>`. */
export async function uploadFile(file: Blob, name: string, signal?: AbortSignal): Promise<PickedFile> {
  if (file.size > MAX_UPLOAD_BYTES) {
    throw new ApiError("too_large", "El archivo pesa más de 100 MB. Elige uno más pequeño.", 413);
  }
  name = safeFileName(name);
  const form = new FormData();
  form.append("file", file, name);
  const res = await request("/files", { method: "POST", body: form, signal });
  const f = (await res.json()) as UploadedFile;
  return { path: `${UPLOAD_SCHEME}${f.id}`, name: f.name || name, mime: f.mime || file.type || "", size: f.size ?? file.size };
}

const ACCEPT: Record<string, string> = {
  media: "image/*,video/*",
  sticker: "image/webp,image/png,image/*",
  csv: ".csv,text/csv,text/plain",
  document: "",
  any: "",
};

/** Abre el selector del navegador; `[]` si se cancela. */
export function chooseFiles(kind: string, multiple = true): Promise<File[]> {
  return new Promise((resolve) => {
    const input = document.createElement("input");
    input.type = "file";
    input.multiple = multiple;
    const accept = ACCEPT[kind] ?? "";
    if (accept) input.accept = accept;
    input.style.display = "none";
    const done = (files: File[]) => {
      input.remove();
      resolve(files);
    };
    input.addEventListener("change", () => done(Array.from(input.files ?? [])));
    input.addEventListener("cancel", () => done([]));
    document.body.appendChild(input);
    input.click();
  });
}

/** `Files.PickFiles` sin escritorio: elegir y subir. */
export async function webPickFiles(kind: string): Promise<PickedFile[]> {
  const files = await chooseFiles(kind, kind !== "csv");
  const out: PickedFile[] = [];
  for (const f of files) out.push(await uploadFile(f, f.name));
  return out;
}

export function base64ToBlob(b64: string, mime = "application/octet-stream"): Blob {
  const bin = atob(b64);
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  return new Blob([bytes], { type: mime });
}

const MIME_BY_EXT: Record<string, string> = {
  ogg: "audio/ogg",
  oga: "audio/ogg",
  opus: "audio/ogg",
  mp3: "audio/mpeg",
  png: "image/png",
  jpg: "image/jpeg",
  jpeg: "image/jpeg",
  webp: "image/webp",
  gif: "image/gif",
  pdf: "application/pdf",
};

/** `Files.SaveTemp` sin escritorio: sube los bytes recibidos en base64. */
export function webSaveTemp(name: string, b64: string): Promise<PickedFile> {
  const ext = name.includes(".") ? name.split(".").pop()!.toLowerCase() : "";
  return uploadFile(base64ToBlob(b64, MIME_BY_EXT[ext]), name);
}

/** `Files.Info` sin escritorio: la subida ya devolvió la ficha; aquí solo hay lo que se sabe del id. */
export async function webFileInfo(path: string): Promise<PickedFile> {
  if (!path.startsWith(UPLOAD_SCHEME)) throw new ApiError("unsupported", "Solo se pueden consultar archivos subidos.");
  return { path, name: "", mime: "", size: 0 };
}

const EXT_BY_MIME: Record<string, string> = {
  "image/jpeg": ".jpg",
  "image/png": ".png",
  "image/webp": ".webp",
  "image/gif": ".gif",
  "video/mp4": ".mp4",
  "audio/ogg": ".ogg",
  "audio/mpeg": ".mp3",
  "audio/mp4": ".m4a",
  "application/pdf": ".pdf",
};

function filenameFrom(res: Response, fallback: string): string {
  const cd = res.headers.get("Content-Disposition") ?? "";
  const star = /filename\*=UTF-8''([^;]+)/i.exec(cd);
  if (star) {
    try {
      return safeFileName(decodeURIComponent(star[1]), fallback);
    } catch {
      /* sigue */
    }
  }
  const plain = /filename="?([^";]+)"?/i.exec(cd);
  if (plain) return safeFileName(plain[1], fallback);
  const ext = EXT_BY_MIME[(res.headers.get("Content-Type") ?? "").split(";")[0].trim()] ?? "";
  return `${fallback}${ext}`;
}

/** `Messaging.SaveMediaAs` sin escritorio: descarga `/media/…` y guarda con el navegador. Devuelve el nombre. */
export async function webSaveMediaAs(accountId: string, mediaId: string): Promise<string> {
  const res = await request(`/media/${encodeURIComponent(accountId)}/${encodeURIComponent(mediaId)}`);
  const blob = await res.blob();
  const name = filenameFrom(res, mediaId);
  const url = URL.createObjectURL(blob);
  try {
    const a = document.createElement("a");
    a.href = url;
    a.download = name;
    a.style.display = "none";
    document.body.appendChild(a);
    a.click();
    a.remove();
  } finally {
    setTimeout(() => URL.revokeObjectURL(url), 10_000);
  }
  return name;
}
