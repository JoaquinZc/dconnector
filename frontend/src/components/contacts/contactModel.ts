// Lógica pura de Contactos: filtros, paginación, tabla en memoria, formulario, asistente de CSV y
// sugerencias de unión. Sin React ni enlaces, para probarla sin la ventana.
import type { Contact, ContactFilter, ContactList, CSVMapping, Identity } from "@/api/types";
import { CHANNEL_ORDER } from "@/lib/channels";
import { formatCount } from "@/lib/format";

/** Contactos por página (`ContactFilter.limit`). */
export const PAGE_SIZE = 100;

/** Color de una lista sin color propio. */
export const DEFAULT_LIST_COLOR = "#00a884";

// ---------------------------------------------------------------- filtros y paginación

export interface ContactQuery {
  query: string;
  listId: string;
  tag: string;
}

export const EMPTY_QUERY: ContactQuery = { query: "", listId: "", tag: "" };

/** `ContactFilter` de Go para una consulta y un desplazamiento. */
export function buildFilter(q: ContactQuery, offset = 0, limit = PAGE_SIZE): ContactFilter {
  return {
    query: q.query.trim(),
    listId: q.listId,
    tag: q.tag.trim(),
    offset: Math.max(0, Math.floor(offset)),
    limit,
  };
}

export function hasActiveFilter(q: ContactQuery): boolean {
  return q.query.trim() !== "" || q.listId !== "" || q.tag.trim() !== "";
}

/** El contacto con las listas siempre como arreglos (Go devuelve `null`). */
export function normalizeContact(c: Contact): Contact {
  return { ...c, tags: c.tags ?? [], identities: c.identities ?? [], listIds: c.listIds ?? [] };
}

export interface TableState {
  rows: Contact[];
  total: number;
}

export const EMPTY_TABLE: TableState = { rows: [], total: 0 };

/** Primera página: sustituye todo. */
export function firstPage(contacts: Contact[] | null | undefined, total: number): TableState {
  const rows = (contacts ?? []).map(normalizeContact);
  return { rows, total: Math.max(total, rows.length) };
}

/** Página siguiente: se añade sin repetir ids (una unión o un alta puede haber movido filas). */
export function appendPage(state: TableState, contacts: Contact[] | null | undefined, total: number): TableState {
  const seen = new Set(state.rows.map((r) => r.id));
  const fresh = (contacts ?? []).map(normalizeContact).filter((c) => !seen.has(c.id));
  const rows = [...state.rows, ...fresh];
  return { rows, total: Math.max(total, rows.length) };
}

/** ¿Faltan filas por pedir? */
export function hasMore(state: TableState): boolean {
  return state.rows.length < state.total;
}

/** Desplazamiento de la página siguiente. */
export function nextOffset(state: TableState): number {
  return state.rows.length;
}

/** Cambia un contacto ya cargado (tras guardarlo). Si no estaba, la tabla queda igual. */
export function replaceContact(state: TableState, c: Contact): TableState {
  const n = normalizeContact(c);
  if (!state.rows.some((r) => r.id === n.id)) return state;
  return { ...state, rows: state.rows.map((r) => (r.id === n.id ? n : r)) };
}

/** Quita un contacto (borrado o fusionado). */
export function removeContact(state: TableState, id: string): TableState {
  if (!state.rows.some((r) => r.id === id)) return { ...state, total: Math.max(0, state.total - 1) };
  return { rows: state.rows.filter((r) => r.id !== id), total: Math.max(0, state.total - 1) };
}

/** Resultado de unir: el que se queda se actualiza y el absorbido desaparece. */
export function applyMerge(state: TableState, merged: Contact, absorbedId: string): TableState {
  return replaceContact(removeContact(state, absorbedId), merged);
}

/** Etiquetas distintas de las filas cargadas (para el filtro), sin mayúsculas ni orden raro. */
export function knownTags(rows: Contact[], extra: string[] = []): string[] {
  const out = new Map<string, string>();
  for (const t of [...rows.flatMap((r) => r.tags ?? []), ...extra]) {
    const v = t.trim();
    if (v && !out.has(v.toLowerCase())) out.set(v.toLowerCase(), v);
  }
  return [...out.values()].sort((a, b) => a.localeCompare(b, "es"));
}

// ---------------------------------------------------------------- origen (manual / importado / automático)

export type ContactSource = "manual" | "import" | "auto";
/** Filtro «Origen» de la pantalla. `ContactFilter` de Go no lo admite: se aplica sobre lo ya cargado. */
export type SourceFilter = "all" | ContactSource;

export const SOURCE_OPTIONS: Array<{ id: SourceFilter; label: string }> = [
  { id: "all", label: "Todos" },
  { id: "auto", label: "Automáticos" },
  { id: "manual", label: "Manuales" },
  { id: "import", label: "Importados" },
];

export const SOURCE_LABEL: Record<ContactSource, string> = {
  manual: "Manual",
  import: "Importado",
  auto: "Automático",
};

/** Origen de un contacto; vacío (o desconocido) cuenta como manual, como en el backend. */
export function sourceOf(c: Pick<Contact, "source">): ContactSource {
  const v = String(c.source ?? "");
  return v === "auto" || v === "import" ? v : "manual";
}

export function filterBySource(rows: Contact[], f: SourceFilter): Contact[] {
  return f === "all" ? rows : rows.filter((c) => sourceOf(c) === f);
}

/** «1 contacto», «275 contactos». */
export function countLabel(n: number): string {
  const v = Math.max(0, Math.round(n));
  return v === 1 ? "1 contacto" : `${formatCount(v)} contactos`;
}

/** Aviso tras «Actualizar desde las cuentas». */
export function syncSummary(created: number): string {
  if (created <= 0) return "Los contactos ya están al día";
  return created === 1 ? "Se agregó 1 contacto nuevo" : `Se agregaron ${formatCount(created)} contactos nuevos`;
}

// ---------------------------------------------------------------- canales e identidades

/** Canales distintos de las identidades del contacto, en el orden de la interfaz. */
export function channelsOf(c: Pick<Contact, "identities">): string[] {
  const found = new Set((c.identities ?? []).map((i) => String(i.channel)));
  const ordered: string[] = CHANNEL_ORDER.filter((ch) => found.has(ch));
  for (const ch of found) if (!ordered.includes(ch)) ordered.push(ch);
  return ordered;
}

/** Clave del chat directo de una identidad: `cuenta|chatNativeId` (en chats directos es el id del par). */
export function chatKeyOf(i: Pick<Identity, "accountId" | "peerNativeId">): string {
  return `${i.accountId}|${i.peerNativeId}`;
}

/** Solo se puede separar una identidad si queda al menos otra. */
export function canSplit(c: Pick<Contact, "identities">): boolean {
  return (c.identities ?? []).length > 1;
}

/** Texto principal de una fila: nombre, si no teléfono, si no «Sin nombre». */
export function contactTitle(c: Pick<Contact, "name" | "phone" | "identities">): string {
  const name = c.name?.trim();
  if (name) return name;
  if (c.phone?.trim()) return c.phone.trim();
  const n = (c.identities ?? []).map((i) => i.displayName?.trim()).find(Boolean);
  return n || "Sin nombre";
}

// ---------------------------------------------------------------- formulario

export interface ContactForm {
  name: string;
  phone: string;
  tags: string[];
  notes: string;
  optedOut: boolean;
  listIds: string[];
}

export const EMPTY_FORM: ContactForm = { name: "", phone: "", tags: [], notes: "", optedOut: false, listIds: [] };

export function formFromContact(c: Contact | null | undefined): ContactForm {
  if (!c) return { ...EMPTY_FORM, tags: [], listIds: [] };
  const n = normalizeContact(c);
  return {
    name: n.name ?? "",
    phone: n.phone ?? "",
    tags: [...(n.tags ?? [])],
    notes: n.notes ?? "",
    optedOut: !!n.optedOut,
    listIds: [...(n.listIds ?? [])],
  };
}

/** «uno, dos;tres» → ["uno","dos","tres"], sin vacíos ni repetidas (sin distinguir mayúsculas). */
export function parseTags(text: string): string[] {
  return addTags([], text);
}

/** Añade las etiquetas que haya en `text` (separadas por coma, punto y coma o salto de línea). */
export function addTags(current: string[], text: string): string[] {
  const out = [...current];
  const seen = new Set(out.map((t) => t.toLowerCase()));
  for (const raw of text.split(/[,;\n]/)) {
    const t = raw.trim();
    if (t && !seen.has(t.toLowerCase())) {
      seen.add(t.toLowerCase());
      out.push(t);
    }
  }
  return out;
}

export function removeTag(current: string[], tag: string): string[] {
  return current.filter((t) => t !== tag);
}

/** Alterna un id en una lista de ids (listas del contacto). */
export function toggleId(ids: string[], id: string): string[] {
  return ids.includes(id) ? ids.filter((x) => x !== id) : [...ids, id];
}

/** Quita lo que no son dígitos ni el `+` inicial (solo para validar; el servicio normaliza). */
export function phoneDigits(phone: string): string {
  return phone.replace(/\D/g, "");
}

export interface FormErrors {
  name?: string;
  phone?: string;
}

/** Sin nombre ni teléfono no hay contacto; un teléfono, si se escribe, debe tener entre 6 y 15 dígitos. */
export function validateForm(f: ContactForm): FormErrors {
  const errors: FormErrors = {};
  const digits = phoneDigits(f.phone);
  if (!f.name.trim() && !f.phone.trim()) errors.name = "Escribe un nombre o un teléfono";
  if (f.phone.trim() && (digits.length < 6 || digits.length > 15)) errors.phone = "Teléfono no válido";
  return errors;
}

export function sameForm(a: ContactForm, b: ContactForm): boolean {
  const key = (f: ContactForm) =>
    JSON.stringify([f.name.trim(), f.phone.trim(), f.notes.trim(), f.optedOut, [...f.tags].sort(), [...f.listIds].sort()]);
  return key(a) === key(b);
}

/** El contacto que se envía a `Save`: lo del formulario sobre el original (o uno nuevo). */
export function contactFromForm(base: Contact | null | undefined, f: ContactForm): Contact {
  const b = base ? normalizeContact(base) : null;
  return {
    id: b?.id ?? "",
    name: f.name.trim(),
    phone: f.phone.trim(),
    notes: f.notes.trim(),
    tags: addTags([], f.tags.join(",")),
    identities: b?.identities ?? [],
    listIds: [...f.listIds],
    optedOut: f.optedOut,
    // El origen lo decide el almacén; se devuelve tal cual llegó.
    source: b?.source ?? "",
    createdAt: b?.createdAt ?? 0,
    updatedAt: b?.updatedAt ?? 0,
  };
}

export interface ListChange {
  listId: string;
  add: boolean;
}

/** Listas a las que hay que añadir o quitar al contacto para pasar de `before` a `after`. */
export function listChanges(before: string[], after: string[]): ListChange[] {
  const b = new Set(before);
  const a = new Set(after);
  return [
    ...after.filter((id) => !b.has(id)).map((listId) => ({ listId, add: true })),
    ...before.filter((id) => !a.has(id)).map((listId) => ({ listId, add: false })),
  ];
}

// ---------------------------------------------------------------- listas

export function sortLists(lists: ContactList[]): ContactList[] {
  return [...lists].sort((a, b) => a.name.localeCompare(b.name, "es"));
}

export function listName(lists: ContactList[], id: string): string {
  return lists.find((l) => l.id === id)?.name ?? "";
}

/** Nombre de lista válido: no vacío y distinto de otro existente (sin distinguir mayúsculas). */
export function validateListName(name: string, lists: ContactList[], selfId = ""): string | null {
  const n = name.trim();
  if (!n) return "Escribe un nombre";
  if (lists.some((l) => l.id !== selfId && l.name.trim().toLowerCase() === n.toLowerCase())) return "Ya existe una lista con ese nombre";
  return null;
}

// ---------------------------------------------------------------- asistente de CSV

/** Lo que elige la persona: columnas por número (1, 2, 3…; 0 = no importar ese dato). */
export interface CsvWizard {
  name: number;
  phone: number;
  tags: number;
  notes: number;
  listId: string;
  hasHeader: boolean;
}

export const DEFAULT_CSV_WIZARD: CsvWizard = { name: 1, phone: 2, tags: 0, notes: 0, listId: "", hasHeader: true };

/** Número de columna de la interfaz (1…) → índice de Go (base 0; -1 = no). */
export function columnIndex(n: number): number {
  return Number.isInteger(n) && n >= 1 ? n - 1 : -1;
}

/** Asistente → `CSVMapping` (índices base 0, -1 = no). */
export function toCsvMapping(w: CsvWizard): CSVMapping {
  return {
    Name: columnIndex(w.name),
    Phone: columnIndex(w.phone),
    Tags: columnIndex(w.tags),
    Notes: columnIndex(w.notes),
    ListID: w.listId,
    HasHeader: w.hasHeader,
  };
}

/** Error del asistente o `null`. Hace falta al menos nombre o teléfono. */
export function validateCsvWizard(w: CsvWizard, hasFile: boolean): string | null {
  if (!hasFile) return "Elige un archivo";
  if (w.name < 1 && w.phone < 1) return "Indica la columna del nombre o del teléfono";
  return null;
}

/** Columnas repetidas entre datos distintos (no es un error, pero se avisa). */
export function repeatedColumns(w: CsvWizard): boolean {
  const used = [w.name, w.phone, w.tags, w.notes].filter((n) => n >= 1);
  return new Set(used).size !== used.length;
}

/** «Se crearon 3 · se actualizaron 2 · se omitieron 1». */
export function importSummary(r: { Created?: number; Updated?: number; Skipped?: number }): string {
  const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;
  return [
    plural(r.Created ?? 0, "creado", "creados"),
    plural(r.Updated ?? 0, "actualizado", "actualizados"),
    plural(r.Skipped ?? 0, "omitido", "omitidos"),
  ].join(" · ");
}

// ---------------------------------------------------------------- sugerencias de unión

/** Pares válidos (dos ids distintos), sin repetir el mismo par ni en otro orden. */
export function cleanPairs(raw: Array<string[] | null | undefined>): Array<[string, string]> {
  const seen = new Set<string>();
  const out: Array<[string, string]> = [];
  for (const p of raw) {
    if (!p || p.length < 2 || !p[0] || !p[1] || p[0] === p[1]) continue;
    const key = [p[0], p[1]].sort().join("|");
    if (seen.has(key)) continue;
    seen.add(key);
    out.push([p[0], p[1]]);
  }
  return out;
}

/** Quita de la lista de pares los que ya no existen tras unir (los que tocan al contacto absorbido). */
export function dropPairsWith(pairs: Array<[string, string]>, id: string): Array<[string, string]> {
  return pairs.filter(([a, b]) => a !== id && b !== id);
}
