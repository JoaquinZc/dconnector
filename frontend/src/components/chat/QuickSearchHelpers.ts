// Ayudas puras de la búsqueda rápida: recorte alrededor de la coincidencia y resaltado.
import { fold } from "./timeline";

/** Recorta el texto alrededor de la primera coincidencia. */
export function snippetAround(text: string, query: string, radius = 70): string {
  const t = (text ?? "").replace(/\s+/g, " ").trim();
  const i = fold(t).indexOf(fold(query));
  if (i < 0 || t.length <= radius * 2) return t.length > radius * 2 ? t.slice(0, radius * 2) + "…" : t;
  const from = Math.max(0, i - radius);
  const to = Math.min(t.length, i + query.length + radius);
  return (from > 0 ? "…" : "") + t.slice(from, to) + (to < t.length ? "…" : "");
}

/** Parte el texto en trozos «coincide / no coincide» sin distinguir mayúsculas ni tildes. */
export function highlightParts(text: string, query: string): { text: string; hit: boolean }[] {
  const q = fold(query);
  if (!q) return [{ text, hit: false }];
  // se pliega carácter a carácter para poder volver a la posición original
  const chars = Array.from(text);
  const folded = chars.map((c) => fold(c));
  const flat = folded.join("");
  const map: number[] = [];
  folded.forEach((f, idx) => {
    for (let k = 0; k < f.length; k++) map.push(idx);
  });
  const out: { text: string; hit: boolean }[] = [];
  let pos = 0;
  let from = 0;
  while (from <= flat.length) {
    const hit = flat.indexOf(q, from);
    if (hit < 0) break;
    const startChar = map[hit];
    const endChar = map[hit + q.length - 1] + 1;
    if (startChar > pos) out.push({ text: chars.slice(pos, startChar).join(""), hit: false });
    out.push({ text: chars.slice(startChar, endChar).join(""), hit: true });
    pos = endChar;
    from = hit + q.length;
  }
  if (pos < chars.length) out.push({ text: chars.slice(pos).join(""), hit: false });
  return out.length ? out : [{ text, hit: false }];
}
