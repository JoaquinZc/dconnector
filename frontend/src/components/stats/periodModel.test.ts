import { describe, expect, it } from "vitest";
import {
  addDays,
  defaultPeriod,
  isValidPeriod,
  localDay,
  parseDay,
  presetLabel,
  rangeKey,
  spanDays,
  toRange,
  validateCustom,
  type PeriodState,
} from "./periodModel";

const today = new Date(2026, 10, 11, 15, 30); // 11-nov-2026, hora local

describe("fechas locales", () => {
  it("localDay usa la fecha local (no UTC) con ceros a la izquierda", () => {
    expect(localDay(new Date(2026, 0, 5, 23, 59))).toBe("2026-01-05");
    expect(localDay(new Date(2026, 11, 31, 0, 0))).toBe("2026-12-31");
  });

  it("parseDay solo acepta fechas reales", () => {
    expect(parseDay("2026-11-11")).not.toBeNull();
    expect(parseDay("2026-02-30")).toBeNull();
    expect(parseDay("2026-13-01")).toBeNull();
    expect(parseDay("11/11/2026")).toBeNull();
    expect(parseDay("")).toBeNull();
  });

  it("addDays cruza meses, años y bisiestos", () => {
    expect(addDays("2026-11-30", 1)).toBe("2026-12-01");
    expect(addDays("2026-12-31", 1)).toBe("2027-01-01");
    expect(addDays("2028-02-28", 1)).toBe("2028-02-29");
    expect(addDays("2026-03-01", -1)).toBe("2026-02-28");
    expect(addDays("mala", 3)).toBe("mala");
  });

  it("spanDays cuenta ambos extremos", () => {
    expect(spanDays("2026-11-01", "2026-11-07")).toBe(7);
    expect(spanDays("2026-11-01", "2026-11-01")).toBe(1);
    expect(spanDays("2026-10-30", "2026-11-02")).toBe(4);
    expect(spanDays("x", "2026-11-02")).toBe(0);
  });
});

describe("selector de periodo → stats.Range", () => {
  it("7, 30 y 90 días: solo `days`", () => {
    for (const d of [7, 30, 90] as const) {
      expect(toRange({ preset: d, from: "2026-01-01", to: "2026-01-02" })).toEqual({ days: d, from: "", to: "" });
    }
  });

  it("personalizado: fechas AAAA-MM-DD y `to` = último día + 1 (el rango de Go es [from, to))", () => {
    expect(toRange({ preset: "custom", from: "2026-11-01", to: "2026-11-07" })).toEqual({ days: 0, from: "2026-11-01", to: "2026-11-08" });
    expect(toRange({ preset: "custom", from: "2026-12-25", to: "2026-12-31" })).toEqual({ days: 0, from: "2026-12-25", to: "2027-01-01" });
  });

  it("un solo día es válido y da un rango de un día", () => {
    const p: PeriodState = { preset: "custom", from: "2026-11-05", to: "2026-11-05" };
    expect(isValidPeriod(p, today)).toBe(true);
    expect(toRange(p)).toEqual({ days: 0, from: "2026-11-05", to: "2026-11-06" });
  });

  it("validateCustom: fechas, orden y futuro", () => {
    expect(validateCustom("", "2026-11-01", today)).toBeTruthy();
    expect(validateCustom("2026-11-05", "2026-11-01", today)).toBeTruthy();
    expect(validateCustom("2026-11-01", "2026-11-12", today)).toBeTruthy(); // mañana
    expect(validateCustom("2026-11-01", "2026-11-11", today)).toBeNull(); // hoy
  });

  it("los preajustes siempre son válidos; el personalizado depende de las fechas", () => {
    expect(isValidPeriod({ preset: 7, from: "", to: "" }, today)).toBe(true);
    expect(isValidPeriod({ preset: "custom", from: "", to: "" }, today)).toBe(false);
  });

  it("el periodo por defecto son 30 días y el personalizado arranca con dos semanas hasta hoy", () => {
    const d = defaultPeriod(today);
    expect(d.preset).toBe(30);
    expect(d.to).toBe("2026-11-11");
    expect(d.from).toBe("2026-10-29");
    expect(spanDays(d.from, d.to)).toBe(14);
  });

  it("rangeKey distingue días de fechas y presetLabel está en español", () => {
    expect(rangeKey({ days: 7, from: "", to: "" })).toBe("d7");
    expect(rangeKey({ days: 0, from: "2026-11-01", to: "2026-11-08" })).toBe("2026-11-01..2026-11-08");
    expect(presetLabel(90)).toBe("90 días");
    expect(presetLabel("custom")).toBe("Personalizado");
  });
});
