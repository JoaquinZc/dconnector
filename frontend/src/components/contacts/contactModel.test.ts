import { describe, expect, it } from "vitest";
import type { Contact, ContactList, Identity } from "@/api/types";
import {
  DEFAULT_CSV_WIZARD,
  EMPTY_TABLE,
  PAGE_SIZE,
  SOURCE_OPTIONS,
  addTags,
  appendPage,
  applyMerge,
  buildFilter,
  canSplit,
  channelsOf,
  chatKeyOf,
  cleanPairs,
  columnIndex,
  contactFromForm,
  contactTitle,
  countLabel,
  dropPairsWith,
  filterBySource,
  firstPage,
  formFromContact,
  hasActiveFilter,
  hasMore,
  importSummary,
  knownTags,
  listChanges,
  nextOffset,
  normalizeContact,
  parseTags,
  removeContact,
  repeatedColumns,
  replaceContact,
  sameForm,
  sourceOf,
  syncSummary,
  toCsvMapping,
  toggleId,
  validateCsvWizard,
  validateForm,
  validateListName,
} from "./contactModel";

const ident = (accountId: string, peer: string, channel = "whatsapp", p: Partial<Identity> = {}): Identity =>
  ({ accountId, channel, peerNativeId: peer, displayName: "", phone: "", ...p }) as unknown as Identity;

const contact = (id: string, p: Partial<Contact> = {}): Contact => ({
  id,
  name: `Contacto ${id}`,
  phone: "",
  notes: "",
  tags: [],
  identities: [],
  listIds: [],
  optedOut: false,
  source: "manual",
  createdAt: 1,
  updatedAt: 1,
  ...p,
});

describe("filtro y paginación", () => {
  it("buildFilter recorta la búsqueda y usa la página por defecto", () => {
    expect(buildFilter({ query: "  ana ", listId: "l1", tag: " vip " })).toEqual({
      query: "ana",
      listId: "l1",
      tag: "vip",
      offset: 0,
      limit: PAGE_SIZE,
    });
  });

  it("el desplazamiento nunca es negativo ni fraccionario", () => {
    expect(buildFilter({ query: "", listId: "", tag: "" }, -5).offset).toBe(0);
    expect(buildFilter({ query: "", listId: "", tag: "" }, 100.9).offset).toBe(100);
  });

  it("hasActiveFilter ignora espacios", () => {
    expect(hasActiveFilter({ query: "  ", listId: "", tag: "" })).toBe(false);
    expect(hasActiveFilter({ query: "", listId: "l1", tag: "" })).toBe(true);
    expect(hasActiveFilter({ query: "", listId: "", tag: "vip" })).toBe(true);
  });

  it("firstPage normaliza `null` a arreglos y el total nunca es menor que las filas", () => {
    const raw = { ...contact("a"), tags: null, identities: null, listIds: null } as unknown as Contact;
    const t = firstPage([raw], 0);
    expect(t.rows[0].tags).toEqual([]);
    expect(t.rows[0].identities).toEqual([]);
    expect(t.rows[0].listIds).toEqual([]);
    expect(t.total).toBe(1);
    expect(firstPage(null, 7)).toEqual({ rows: [], total: 7 });
  });

  it("appendPage añade sin repetir ids y calcula la página siguiente", () => {
    const p1 = firstPage([contact("a"), contact("b")], 5);
    expect(hasMore(p1)).toBe(true);
    expect(nextOffset(p1)).toBe(2);
    const p2 = appendPage(p1, [contact("b"), contact("c"), contact("d")], 5);
    expect(p2.rows.map((r) => r.id)).toEqual(["a", "b", "c", "d"]);
    expect(nextOffset(p2)).toBe(4);
    const p3 = appendPage(p2, [contact("e")], 5);
    expect(hasMore(p3)).toBe(false);
    expect(hasMore(EMPTY_TABLE)).toBe(false);
  });

  it("replaceContact cambia una fila y deja igual lo que no está cargado", () => {
    const t = firstPage([contact("a"), contact("b")], 2);
    const u = replaceContact(t, contact("b", { name: "Nuevo" }));
    expect(u.rows[1].name).toBe("Nuevo");
    expect(replaceContact(t, contact("zzz"))).toBe(t);
  });

  it("removeContact baja el total, también si la fila no estaba cargada", () => {
    const t = firstPage([contact("a"), contact("b")], 10);
    expect(removeContact(t, "a")).toEqual({ rows: [expect.objectContaining({ id: "b" })], total: 9 });
    expect(removeContact(t, "fuera").total).toBe(9);
    expect(removeContact(EMPTY_TABLE, "x").total).toBe(0);
  });

  it("unir: el absorbido desaparece y el que se queda se actualiza", () => {
    const t = firstPage([contact("a"), contact("b"), contact("c")], 3);
    const merged = contact("a", { name: "Ana unida", identities: [ident("w1", "1"), ident("t1", "2", "telegram")] });
    const u = applyMerge(t, merged, "b");
    expect(u.rows.map((r) => r.id)).toEqual(["a", "c"]);
    expect(u.rows[0].name).toBe("Ana unida");
    expect(u.total).toBe(2);
  });

  it("knownTags junta las etiquetas sin repetir (sin distinguir mayúsculas) y ordenadas", () => {
    const rows = [contact("a", { tags: ["vip", "Zona"] }), contact("b", { tags: ["VIP", "ana"] })];
    expect(knownTags(rows, ["nueva"])).toEqual(["ana", "nueva", "vip", "Zona"]);
  });
});

describe("canales e identidades", () => {
  it("channelsOf: distintos y en el orden de la interfaz", () => {
    const c = contact("a", { identities: [ident("t", "1", "telegram"), ident("w", "2"), ident("w2", "3"), ident("i", "4", "instagram")] });
    expect(channelsOf(c)).toEqual(["whatsapp", "telegram", "instagram"]);
    expect(channelsOf(contact("b"))).toEqual([]);
  });

  it("la clave del chat es cuenta|id del par", () => {
    expect(chatKeyOf({ accountId: "wa-1" as never, peerNativeId: "593999@s.whatsapp.net" })).toBe("wa-1|593999@s.whatsapp.net");
  });

  it("solo se separa si queda otra identidad", () => {
    expect(canSplit(contact("a", { identities: [ident("w", "1")] }))).toBe(false);
    expect(canSplit(contact("a", { identities: [ident("w", "1"), ident("t", "2", "telegram")] }))).toBe(true);
  });

  it("contactTitle: nombre, si no teléfono, si no nombre de la identidad, si no «Sin nombre»", () => {
    expect(contactTitle({ name: " Ana ", phone: "+1", identities: [] })).toBe("Ana");
    expect(contactTitle({ name: "", phone: "+593999", identities: [] })).toBe("+593999");
    expect(contactTitle({ name: "", phone: "", identities: [ident("w", "1", "whatsapp", { displayName: "Pepe" })] })).toBe("Pepe");
    expect(contactTitle({ name: "", phone: "", identities: [] })).toBe("Sin nombre");
  });
});

describe("formulario", () => {
  it("parseTags/addTags separan por coma, punto y coma y salto, sin vacías ni repetidas", () => {
    expect(parseTags("uno, dos;tres\ncuatro,,  ")).toEqual(["uno", "dos", "tres", "cuatro"]);
    expect(addTags(["Vip"], "vip, nuevo, NUEVO")).toEqual(["Vip", "nuevo"]);
  });

  it("toggleId alterna", () => {
    expect(toggleId(["a"], "b")).toEqual(["a", "b"]);
    expect(toggleId(["a", "b"], "a")).toEqual(["b"]);
  });

  it("validateForm: hace falta nombre o teléfono, y el teléfono con 6 a 15 dígitos", () => {
    const base = formFromContact(null);
    expect(validateForm(base).name).toBeTruthy();
    expect(validateForm({ ...base, name: "Ana" })).toEqual({});
    expect(validateForm({ ...base, phone: "+593 99 123 4567" })).toEqual({});
    expect(validateForm({ ...base, name: "Ana", phone: "123" }).phone).toBeTruthy();
    expect(validateForm({ ...base, name: "Ana", phone: "1".repeat(16) }).phone).toBeTruthy();
  });

  it("sameForm ignora el orden y los espacios de los bordes", () => {
    const a = { ...formFromContact(null), name: "Ana", tags: ["x", "y"], listIds: ["l1", "l2"] };
    const b = { ...a, name: " Ana ", tags: ["y", "x"], listIds: ["l2", "l1"] };
    expect(sameForm(a, b)).toBe(true);
    expect(sameForm(a, { ...b, notes: "algo" })).toBe(false);
    expect(sameForm(a, { ...b, optedOut: true })).toBe(false);
  });

  it("contactFromForm conserva identidades y fechas del original y limpia el texto", () => {
    const base = contact("c1", { identities: [ident("w", "1")], createdAt: 5, updatedAt: 9 });
    const out = contactFromForm(base, { name: " Ana ", phone: " +593999 ", tags: ["a", "A", "b"], notes: " n ", optedOut: true, listIds: ["l1"] });
    expect(out).toMatchObject({ id: "c1", name: "Ana", phone: "+593999", notes: "n", tags: ["a", "b"], optedOut: true, listIds: ["l1"], createdAt: 5, updatedAt: 9 });
    expect(out.identities).toHaveLength(1);
    expect(contactFromForm(null, formFromContact(null)).id).toBe("");
  });

  it("listChanges: lo que hay que añadir y quitar", () => {
    expect(listChanges(["a", "b"], ["b", "c"])).toEqual([
      { listId: "c", add: true },
      { listId: "a", add: false },
    ]);
    expect(listChanges(["a"], ["a"])).toEqual([]);
  });

  it("validateListName: no vacío y sin repetir otra lista", () => {
    const lists = [{ id: "1", name: "Clientes", color: "#fff", memberCount: 0 }] as ContactList[];
    expect(validateListName("  ", lists)).toBeTruthy();
    expect(validateListName("clientes", lists)).toBeTruthy();
    expect(validateListName("clientes", lists, "1")).toBeNull();
    expect(validateListName("Proveedores", lists)).toBeNull();
  });
});

describe("asistente de CSV → CSVMapping", () => {
  it("columnIndex: número de la interfaz (1…) a índice base 0, 0 o inválido = -1", () => {
    expect(columnIndex(1)).toBe(0);
    expect(columnIndex(5)).toBe(4);
    expect(columnIndex(0)).toBe(-1);
    expect(columnIndex(-3)).toBe(-1);
    expect(columnIndex(1.5)).toBe(-1);
    expect(columnIndex(Number.NaN)).toBe(-1);
  });

  it("el valor por defecto es nombre en la columna 1, teléfono en la 2, con encabezado", () => {
    expect(toCsvMapping(DEFAULT_CSV_WIZARD)).toEqual({ Name: 0, Phone: 1, Tags: -1, Notes: -1, ListID: "", HasHeader: true });
  });

  it("convierte todas las columnas, la lista y la casilla de encabezado", () => {
    expect(toCsvMapping({ name: 3, phone: 0, tags: 4, notes: 2, listId: "l9", hasHeader: false })).toEqual({
      Name: 2,
      Phone: -1,
      Tags: 3,
      Notes: 1,
      ListID: "l9",
      HasHeader: false,
    });
  });

  it("validateCsvWizard: archivo y al menos nombre o teléfono", () => {
    expect(validateCsvWizard(DEFAULT_CSV_WIZARD, false)).toBeTruthy();
    expect(validateCsvWizard({ ...DEFAULT_CSV_WIZARD, name: 0, phone: 0 }, true)).toBeTruthy();
    expect(validateCsvWizard({ ...DEFAULT_CSV_WIZARD, name: 0, phone: 3 }, true)).toBeNull();
    expect(validateCsvWizard(DEFAULT_CSV_WIZARD, true)).toBeNull();
  });

  it("repeatedColumns avisa de una columna usada dos veces (los 0 no cuentan)", () => {
    expect(repeatedColumns({ ...DEFAULT_CSV_WIZARD, name: 1, phone: 1 })).toBe(true);
    expect(repeatedColumns({ ...DEFAULT_CSV_WIZARD, tags: 0, notes: 0 })).toBe(false);
  });

  it("importSummary con singular y plural", () => {
    expect(importSummary({ Created: 1, Updated: 2, Skipped: 0 })).toBe("1 creado · 2 actualizados · 0 omitidos");
  });
});

describe("sugerencias de unión", () => {
  it("cleanPairs descarta pares incompletos, iguales y repetidos (también al revés)", () => {
    expect(cleanPairs([["a", "b"], ["b", "a"], ["a", "a"], ["c"], null, ["", "d"], ["c", "d", "e"], undefined])).toEqual([
      ["a", "b"],
      ["c", "d"],
    ]);
  });

  it("dropPairsWith quita los pares que tocan al absorbido", () => {
    expect(dropPairsWith([["a", "b"], ["c", "d"], ["b", "e"]], "b")).toEqual([["c", "d"]]);
  });

  it("normalizeContact no pisa lo que ya hay", () => {
    const c = contact("a", { tags: ["x"] });
    expect(normalizeContact(c).tags).toEqual(["x"]);
  });
});

describe("origen del contacto", () => {
  const rows = [
    contact("a", { source: "auto" }),
    contact("b", { source: "manual" }),
    contact("c", { source: "import" }),
    contact("d", { source: "" }),
    contact("e", { source: "raro" }),
  ];

  it("sourceOf: vacío o desconocido cuenta como manual", () => {
    expect(rows.map(sourceOf)).toEqual(["auto", "manual", "import", "manual", "manual"]);
  });

  it("filterBySource filtra en el cliente y «todos» devuelve la misma lista", () => {
    expect(filterBySource(rows, "all")).toBe(rows);
    expect(filterBySource(rows, "auto").map((c) => c.id)).toEqual(["a"]);
    expect(filterBySource(rows, "manual").map((c) => c.id)).toEqual(["b", "d", "e"]);
    expect(filterBySource(rows, "import").map((c) => c.id)).toEqual(["c"]);
  });

  it("las opciones del selector", () => {
    expect(SOURCE_OPTIONS.map((o) => o.label)).toEqual(["Todos", "Automáticos", "Manuales", "Importados"]);
  });

  it("countLabel: singular, plural y miles", () => {
    expect(countLabel(0)).toBe("0 contactos");
    expect(countLabel(1)).toBe("1 contacto");
    expect(countLabel(275)).toBe("275 contactos");
    expect(countLabel(12345)).toBe("12.345 contactos");
  });

  it("syncSummary", () => {
    expect(syncSummary(0)).toBe("Los contactos ya están al día");
    expect(syncSummary(1)).toBe("Se agregó 1 contacto nuevo");
    expect(syncSummary(5)).toBe("Se agregaron 5 contactos nuevos");
  });

  it("al guardar se devuelve el origen tal cual llegó (lo decide el almacén)", () => {
    const base = contact("a", { source: "auto" });
    expect(contactFromForm(base, formFromContact(base)).source).toBe("auto");
    expect(contactFromForm(null, formFromContact(null)).source).toBe("");
  });
});
