import { describe, expect, it } from "vitest";
import type { Destination } from "@/api/types";
import {
  contactScopeSummary,
  draftOfDest,
  emptyDestForm,
  formFromDraft,
  hasErrors,
  indexListNames,
  joinNames,
  toDestination,
  toForm,
  validateContactScope,
  validateDestForm,
} from "./destinationModel";

const dest = (scope: unknown): Destination =>
  ({
    id: "d1",
    name: "Bot",
    url: "wss://bot.com",
    token: "tok",
    enabled: true,
    allowCommands: false,
    filter: { accountIds: [], channels: [], events: [], includeFromMe: false, includeGroups: false, contactScope: scope },
    createdAt: 1,
  }) as Destination;

describe("alcance de contactos: conversión", () => {
  it("un formulario nuevo recibe de todos los contactos", () => {
    expect(emptyDestForm()).toMatchObject({ contactMode: "all", includeListIds: [], excludeListIds: [] });
  });

  it("un destino antiguo (sin el campo) se lee como «todos»", () => {
    const old = { ...dest(undefined), filter: { accountIds: [], channels: [], events: [], includeFromMe: false, includeGroups: false } } as never;
    expect(toForm(old)).toMatchObject({ contactMode: "all", includeListIds: [], excludeListIds: [] });
    expect(toForm(dest(null))).toMatchObject({ contactMode: "all" });
    expect(toForm(dest({ mode: "", includeListIds: null, excludeListIds: null }))).toMatchObject({
      contactMode: "all",
      includeListIds: [],
      excludeListIds: [],
    });
  });

  it("toForm y toDestination conservan las listas", () => {
    const d = dest({ mode: "lists", includeListIds: ["vip", "fam"], excludeListIds: ["ami"] });
    const f = toForm(d);
    expect(f).toMatchObject({ contactMode: "lists", includeListIds: ["vip", "fam"], excludeListIds: ["ami"] });
    expect(toDestination(f, d).filter.contactScope).toEqual({ mode: "lists", includeListIds: ["vip", "fam"], excludeListIds: ["ami"] });
    expect(toForm(toDestination(f, d))).toEqual(f);
  });

  it("con «todos» las listas incluidas no se guardan, las excluidas sí", () => {
    const f = { ...emptyDestForm(), contactMode: "all" as const, includeListIds: ["vip"], excludeListIds: ["fam"] };
    expect(toDestination(f).filter.contactScope).toEqual({ mode: "all", includeListIds: [], excludeListIds: ["fam"] });
  });

  it("el borrador conserva el alcance y descarta lo raro", () => {
    const d = draftOfDest({ ...emptyDestForm(), contactMode: "lists", includeListIds: ["a"], excludeListIds: ["b"] });
    expect(formFromDraft(d)).toMatchObject({ contactMode: "lists", includeListIds: ["a"], excludeListIds: ["b"] });
    expect(formFromDraft({ contactMode: "raro", includeListIds: [1, "x"], excludeListIds: "no" } as never)).toMatchObject({
      contactMode: "all",
      includeListIds: ["x"],
      excludeListIds: [],
    });
    // Un borrador anterior a este campo vuelve a «todos».
    expect(formFromDraft({ name: "x" } as never)).toMatchObject({ contactMode: "all", includeListIds: [], excludeListIds: [] });
  });
});

describe("alcance de contactos: validación", () => {
  it("«solo listas» pide al menos una y no admite una lista en los dos lados", () => {
    const f = { contactMode: "lists" as const, includeListIds: [] as string[], excludeListIds: [] as string[] };
    expect(validateContactScope(f)).toMatch(/al menos una lista/);
    expect(validateContactScope({ ...f, includeListIds: ["a"] })).toBeNull();
    expect(validateContactScope({ ...f, includeListIds: ["a", "b"], excludeListIds: ["b"] })).toMatch(/a la vez/);
    // Con «todos» las incluidas no cuentan, aunque choquen con las excluidas.
    expect(validateContactScope({ contactMode: "all", includeListIds: ["a"], excludeListIds: ["a"] })).toBeNull();
    expect(validateContactScope({ contactMode: "all", includeListIds: [], excludeListIds: [] })).toBeNull();
    const many = Array.from({ length: 101 }, (_, i) => "l" + i);
    expect(validateContactScope({ contactMode: "all", includeListIds: [], excludeListIds: many })).toMatch(/100/);
  });

  it("validateDestForm lleva el error a «contacts» y bloquea el guardado", () => {
    const base = { ...emptyDestForm(), name: "Bot", url: "wss://bot.com" };
    expect(hasErrors(validateDestForm(base))).toBe(false);
    const e = validateDestForm({ ...base, contactMode: "lists" });
    expect(e.contacts).toBeTruthy();
    expect(hasErrors(e)).toBe(true);
  });
});

describe("alcance de contactos: frase", () => {
  const names = indexListNames([
    { id: "fam", name: "Familia", color: "#f00", memberCount: 2 },
    { id: "ami", name: "Amigos", color: "#0f0", memberCount: 3 },
    { id: "vip", name: "VIP", color: "#00f", memberCount: 1 },
  ]);
  const scope = (contactMode: "all" | "lists", includeListIds: string[], excludeListIds: string[]) => ({ contactMode, includeListIds, excludeListIds });

  it("une nombres: A, A y B, A, B y C", () => {
    expect(joinNames([])).toBe("");
    expect(joinNames(["A"])).toBe("A");
    expect(joinNames(["A", "B"])).toBe("A y B");
    expect(joinNames(["A", "B", "C"])).toBe("A, B y C");
  });

  it("todos los contactos", () => {
    expect(contactScopeSummary(scope("all", [], []), names)).toBe("Recibe: todos los contactos");
    expect(contactScopeSummary(scope("all", [], []), null)).toBe("Recibe: todos los contactos");
  });

  it("todos menos listas", () => {
    expect(contactScopeSummary(scope("all", [], ["fam", "ami"]), names)).toBe("Recibe: todos los contactos, excepto las listas Familia y Amigos");
    expect(contactScopeSummary(scope("all", [], ["fam"]), names)).toBe("Recibe: todos los contactos, excepto la lista Familia");
  });

  it("solo ciertas listas, con y sin exclusión", () => {
    expect(contactScopeSummary(scope("lists", ["vip"], []), names)).toBe("Recibe: solo los contactos de la lista VIP");
    expect(contactScopeSummary(scope("lists", ["vip", "ami"], ["fam"]), names)).toBe(
      "Recibe: solo los contactos de las listas VIP y Amigos, excepto la lista Familia",
    );
  });

  it("ignora las listas que ya no existen", () => {
    expect(contactScopeSummary(scope("all", [], ["borrada"]), names)).toBe("Recibe: todos los contactos");
    expect(contactScopeSummary(scope("lists", ["vip", "borrada"], ["borrada"]), names)).toBe("Recibe: solo los contactos de la lista VIP");
    expect(contactScopeSummary(scope("lists", ["borrada"], []), names)).toMatch(/No recibirá nada/);
    expect(contactScopeSummary(scope("lists", [], []), names)).toMatch(/No recibirá nada/);
  });

  it("sin los nombres cargados, una frase genérica", () => {
    expect(contactScopeSummary(scope("lists", ["vip"], []), null)).toMatch(/listas de contactos/);
    expect(contactScopeSummary(scope("all", [], ["fam"]), null)).toMatch(/listas de contactos/);
  });
});
