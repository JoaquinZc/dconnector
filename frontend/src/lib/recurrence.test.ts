import { describe, expect, it } from "vitest";
import {
  PAST_GRACE_MS,
  buildRecurrence,
  defaultFirstMs,
  emptyRecurrenceForm,
  normalizeRecurrenceForm,
  parseDateInputEnd,
  parseLocalInput,
  quickShortcuts,
  recurrenceFormFromRule,
  repeatChipText,
  summarizeForm,
  summarizeRule,
  toDateInput,
  toLocalInput,
  validateRecurrenceForm,
  weekdaysLabel,
  whenError,
  type RecurrenceForm,
} from "./recurrence";

/** miércoles 11-nov-2026 15:30 hora local */
const NOW = new Date(2026, 10, 11, 15, 30, 0).getTime();
const MON_9 = new Date(2026, 10, 16, 9, 0).getTime(); // lunes

const form = (over: Partial<RecurrenceForm> = {}): RecurrenceForm => ({ ...emptyRecurrenceForm(), ...over });

describe("fecha local ↔ milisegundos", () => {
  it("datetime-local en los dos sentidos", () => {
    const ms = new Date(2026, 10, 12, 9, 5).getTime();
    expect(toLocalInput(ms)).toBe("2026-11-12T09:05");
    expect(parseLocalInput("2026-11-12T09:05")).toBe(ms);
    expect(parseLocalInput(toLocalInput(ms))).toBe(ms);
    expect(parseLocalInput("")).toBeNull();
    expect(parseLocalInput("no")).toBeNull();
  });

  it("el día final llega hasta el último instante de ese día", () => {
    const end = parseDateInputEnd("2026-12-31")!;
    expect(new Date(end).getHours()).toBe(23);
    expect(new Date(end).getMinutes()).toBe(59);
    expect(toDateInput(end)).toBe("2026-12-31");
    expect(parseDateInputEnd("31/12/2026")).toBeNull();
  });
});

describe("atajos y fecha por defecto", () => {
  it("por defecto, la próxima hora en punto (siempre posterior)", () => {
    expect(defaultFirstMs(NOW)).toBe(new Date(2026, 10, 11, 16, 0).getTime());
    const onTheHour = new Date(2026, 10, 11, 16, 0).getTime();
    expect(defaultFirstMs(onTheHour)).toBe(new Date(2026, 10, 11, 17, 0).getTime());
  });

  it("«En 1 hora», «Mañana 9:00» y «Lunes 9:00»", () => {
    const [h, t, m] = quickShortcuts(NOW);
    expect([h.label, t.label, m.label]).toEqual(["En 1 hora", "Mañana 9:00", "Lunes 9:00"]);
    expect(h.ms).toBe(new Date(2026, 10, 11, 16, 30).getTime());
    expect(t.ms).toBe(new Date(2026, 10, 12, 9, 0).getTime());
    expect(m.ms).toBe(MON_9);
  });

  it("«Lunes 9:00» en lunes es el lunes siguiente, y en domingo, mañana", () => {
    const monday = new Date(2026, 10, 16, 8, 0).getTime();
    expect(quickShortcuts(monday)[2].ms).toBe(new Date(2026, 10, 23, 9, 0).getTime());
    const sunday = new Date(2026, 10, 15, 20, 0).getTime();
    expect(quickShortcuts(sunday)[2].ms).toBe(MON_9);
  });

  it("el atajo «En 1 hora» redondea los segundos hacia arriba", () => {
    const now = new Date(2026, 10, 11, 15, 30, 20).getTime();
    expect(quickShortcuts(now)[0].ms).toBe(new Date(2026, 10, 11, 16, 31).getTime());
  });
});

describe("fecha válida (tolerancia de 1 h)", () => {
  it("sin fecha, pasada o dentro de la tolerancia", () => {
    expect(whenError(null, NOW)).toBe("Elige fecha y hora");
    expect(whenError(NOW - PAST_GRACE_MS - 1, NOW)).toBe("La fecha ya pasó");
    expect(whenError(NOW - PAST_GRACE_MS, NOW)).toBeNull();
    expect(whenError(NOW - 10 * 60_000, NOW)).toBeNull();
    expect(whenError(NOW + 1, NOW)).toBeNull();
  });
});

describe("formulario → regla", () => {
  it("sin repetición no hay regla", () => {
    expect(buildRecurrence(form())).toBeNull();
  });

  it("diaria, mensual y anual", () => {
    expect(buildRecurrence(form({ repeat: "daily" }))).toEqual({ frequency: "daily", every: 1, endAt: 0, maxCount: 0, count: 0, firstAt: 0 });
    expect(buildRecurrence(form({ repeat: "monthly" }))).toMatchObject({ frequency: "monthly", every: 1 });
    expect(buildRecurrence(form({ repeat: "yearly" }))).toMatchObject({ frequency: "yearly", every: 1 });
  });

  it("semanal con días ordenados de lunes a domingo y cada N semanas", () => {
    const r = buildRecurrence(form({ repeat: "weekly", weekdays: [0, 4, 1], weeklyEvery: 2 }))!;
    expect(r).toMatchObject({ frequency: "weekly", every: 2, weekdays: [1, 4, 0] });
    expect(r.unit).toBeUndefined();
  });

  it("personalizada con unidad", () => {
    const r = buildRecurrence(form({ repeat: "custom", customEvery: 30, customUnit: "minutes" }))!;
    expect(r).toMatchObject({ frequency: "custom", every: 30, unit: "minutes" });
    expect(r.weekdays).toBeUndefined();
  });

  it("el final: nunca, un día o un número de veces", () => {
    expect(buildRecurrence(form({ repeat: "daily", endMode: "never", endDate: "2026-12-31", maxCount: 5 }))).toMatchObject({ endAt: 0, maxCount: 0 });
    const byDate = buildRecurrence(form({ repeat: "daily", endMode: "date", endDate: "2026-12-31", maxCount: 5 }))!;
    expect(byDate.endAt).toBe(parseDateInputEnd("2026-12-31"));
    expect(byDate.maxCount).toBe(0);
    expect(buildRecurrence(form({ repeat: "daily", endMode: "count", maxCount: 5 }))).toMatchObject({ endAt: 0, maxCount: 5 });
  });

  it("conserva count y firstAt de la serie que se edita", () => {
    const base = { frequency: "daily", every: 1, endAt: 0, maxCount: 0, count: 3, firstAt: 123 };
    expect(buildRecurrence(form({ repeat: "daily" }), base)).toMatchObject({ count: 3, firstAt: 123 });
  });
});

describe("regla → formulario", () => {
  it("sin regla, no se repite", () => {
    expect(recurrenceFormFromRule(null).repeat).toBe("none");
    expect(recurrenceFormFromRule(undefined).repeat).toBe("none");
  });

  it("ida y vuelta de cada frecuencia", () => {
    for (const f of [
      form({ repeat: "daily" }),
      form({ repeat: "monthly" }),
      form({ repeat: "yearly" }),
      form({ repeat: "weekly", weekdays: [1, 4], weeklyEvery: 2 }),
      form({ repeat: "custom", customEvery: 3, customUnit: "hours" }),
      form({ repeat: "daily", endMode: "count", maxCount: 7 }),
      form({ repeat: "daily", endMode: "date", endDate: "2026-12-31" }),
    ]) {
      const back = recurrenceFormFromRule(buildRecurrence(f), MON_9);
      expect(buildRecurrence(back)).toEqual(buildRecurrence(f));
    }
  });

  it("«cada 2 días» y «cada 2 meses» de otro cliente se muestran como personalizado", () => {
    const base = { endAt: 0, maxCount: 0, count: 0, firstAt: 0 };
    expect(recurrenceFormFromRule({ ...base, frequency: "daily", every: 2 })).toMatchObject({ repeat: "custom", customEvery: 2, customUnit: "days" });
    expect(recurrenceFormFromRule({ ...base, frequency: "monthly", every: 2 })).toMatchObject({ repeat: "custom", customEvery: 2, customUnit: "months" });
    expect(recurrenceFormFromRule({ ...base, frequency: "yearly", every: 2 })).toMatchObject({ repeat: "custom", customEvery: 24, customUnit: "months" });
  });

  it("semanal sin días usa el de la fecha del primer envío", () => {
    const r = { frequency: "weekly", every: 1, weekdays: null, endAt: 0, maxCount: 0, count: 0, firstAt: 0 };
    expect(recurrenceFormFromRule(r, MON_9).weekdays).toEqual([1]);
  });

  it("un borrador viejo o incompleto se completa", () => {
    expect(normalizeRecurrenceForm(undefined)).toEqual(emptyRecurrenceForm());
    expect(normalizeRecurrenceForm({ repeat: "weekly", weekdays: [1, 9, 2.5] as number[] }).weekdays).toEqual([1]);
    expect(normalizeRecurrenceForm({ repeat: "nope" as never }).repeat).toBe("none");
  });
});

describe("validación", () => {
  it("sin repetición siempre vale", () => {
    expect(validateRecurrenceForm(form(), null)).toBeNull();
  });

  it("semanal pide al menos un día y cada 1 a 365", () => {
    expect(validateRecurrenceForm(form({ repeat: "weekly", weekdays: [] }), MON_9)).toBe("Elige al menos un día de la semana");
    expect(validateRecurrenceForm(form({ repeat: "weekly", weekdays: [1], weeklyEvery: 0 }), MON_9)).toMatch(/Cada cuántas semanas/);
    expect(validateRecurrenceForm(form({ repeat: "weekly", weekdays: [1] }), MON_9)).toBeNull();
  });

  it("personalizada: mínimo 5 minutos y topes de 1 a 365", () => {
    expect(validateRecurrenceForm(form({ repeat: "custom", customEvery: 4, customUnit: "minutes" }), MON_9)).toBe("La repetición mínima es cada 5 minutos");
    expect(validateRecurrenceForm(form({ repeat: "custom", customEvery: 5, customUnit: "minutes" }), MON_9)).toBeNull();
    expect(validateRecurrenceForm(form({ repeat: "custom", customEvery: 4, customUnit: "hours" }), MON_9)).toBeNull();
    expect(validateRecurrenceForm(form({ repeat: "custom", customEvery: 0 }), MON_9)).toMatch(/1 a 365/);
    expect(validateRecurrenceForm(form({ repeat: "custom", customEvery: 366 }), MON_9)).toMatch(/1 a 365/);
    expect(validateRecurrenceForm(form({ repeat: "custom", customEvery: 1.5 }), MON_9)).toMatch(/1 a 365/);
  });

  it("el final: día obligatorio y posterior al primer envío; veces de 1 a 1000", () => {
    expect(validateRecurrenceForm(form({ repeat: "daily", endMode: "date", endDate: "" }), MON_9)).toBe("Elige el día en que termina");
    expect(validateRecurrenceForm(form({ repeat: "daily", endMode: "date", endDate: "2026-11-15" }), MON_9)).toBe(
      "El día final debe ser posterior al primer envío",
    );
    // El mismo día del primer envío cuenta hasta las 23:59, así que vale.
    expect(validateRecurrenceForm(form({ repeat: "daily", endMode: "date", endDate: "2026-11-16" }), MON_9)).toBeNull();
    expect(validateRecurrenceForm(form({ repeat: "daily", endMode: "count", maxCount: 0 }), MON_9)).toMatch(/1 a 1000/);
    expect(validateRecurrenceForm(form({ repeat: "daily", endMode: "count", maxCount: 1001 }), MON_9)).toMatch(/1 a 1000/);
    expect(validateRecurrenceForm(form({ repeat: "daily", endMode: "count", maxCount: 1000 }), MON_9)).toBeNull();
  });
});

describe("resumen legible", () => {
  const base = { endAt: 0, maxCount: 0 };

  it("semanal con días", () => {
    expect(summarizeRule({ ...base, frequency: "weekly", every: 1, weekdays: [4, 1] }, MON_9)).toBe("Cada semana, lunes y jueves, a las 09:00");
    expect(summarizeRule({ ...base, frequency: "weekly", every: 2, weekdays: [1, 3, 5] }, MON_9)).toBe(
      "Cada 2 semanas, lunes, miércoles y viernes, a las 09:00",
    );
    expect(summarizeRule({ ...base, frequency: "weekly", every: 1, weekdays: [] }, MON_9)).toBe("Cada semana, lunes, a las 09:00");
  });

  it("nombres de conjuntos de días", () => {
    expect(weekdaysLabel([1, 2, 3, 4, 5])).toBe("de lunes a viernes");
    expect(weekdaysLabel([0, 1, 2, 3, 4, 5, 6])).toBe("todos los días");
    expect(weekdaysLabel([0, 6])).toBe("sábado y domingo");
  });

  it("diaria, mensual (día 31) y anual", () => {
    expect(summarizeRule({ ...base, frequency: "daily", every: 1 }, MON_9)).toBe("Cada día, a las 09:00");
    const jan31 = new Date(2026, 0, 31, 8, 0).getTime();
    expect(summarizeRule({ ...base, frequency: "monthly", every: 1 }, jan31)).toBe("Cada mes, el día 31 o el último del mes, a las 08:00");
    expect(summarizeRule({ ...base, frequency: "monthly", every: 1 }, MON_9)).toBe("Cada mes, el día 16, a las 09:00");
    const mar15 = new Date(2026, 2, 15, 18, 30).getTime();
    expect(summarizeRule({ ...base, frequency: "yearly", every: 1 }, mar15)).toBe("Cada año, el 15 de marzo, a las 18:30");
  });

  it("la hora «1» va en singular", () => {
    const one = new Date(2026, 10, 16, 1, 0).getTime();
    expect(summarizeRule({ ...base, frequency: "daily", every: 1 }, one)).toBe("Cada día, a la 01:00");
  });

  it("personalizada", () => {
    expect(summarizeRule({ ...base, frequency: "custom", every: 30, unit: "minutes" }, MON_9)).toBe("Cada 30 minutos");
    expect(summarizeRule({ ...base, frequency: "custom", every: 1, unit: "hours" }, MON_9)).toBe("Cada hora");
    expect(summarizeRule({ ...base, frequency: "custom", every: 3, unit: "days" }, MON_9)).toBe("Cada 3 días, a las 09:00");
    expect(summarizeRule({ ...base, frequency: "custom", every: 2, unit: "weeks" }, MON_9)).toBe("Cada 2 semanas, el lunes, a las 09:00");
    expect(summarizeRule({ ...base, frequency: "custom", every: 2, unit: "months" }, MON_9)).toBe("Cada 2 meses, el día 16, a las 09:00");
  });

  it("el final", () => {
    const until = parseDateInputEnd("2026-12-31")!;
    expect(summarizeRule({ frequency: "daily", every: 1, endAt: until, maxCount: 0 }, MON_9)).toBe("Cada día, a las 09:00, hasta el 31/12/2026");
    expect(summarizeRule({ frequency: "daily", every: 1, endAt: 0, maxCount: 10 }, MON_9)).toBe("Cada día, a las 09:00, 10 veces en total");
    expect(summarizeRule({ frequency: "daily", every: 1, endAt: 0, maxCount: 1 }, MON_9)).toMatch(/1 vez en total$/);
  });

  it("del formulario: «No se repite» o vacío si no vale", () => {
    expect(summarizeForm(form(), MON_9)).toBe("No se repite");
    expect(summarizeForm(form({ repeat: "weekly", weekdays: [1, 4] }), MON_9)).toBe("Cada semana, lunes y jueves, a las 09:00");
    expect(summarizeForm(form({ repeat: "weekly", weekdays: [] }), MON_9)).toBe("");
    expect(summarizeForm(form({ repeat: "daily" }), null)).toBe("");
  });

  it("el distintivo de la serie", () => {
    const r = { frequency: "weekly", every: 1, weekdays: [1, 4], endAt: 0, maxCount: 0, count: 0, firstAt: MON_9 };
    expect(repeatChipText(r, MON_9 + 7 * 86_400_000)).toBe("Se repite: cada semana, lunes y jueves, a las 09:00");
    expect(repeatChipText(null, MON_9)).toBe("");
  });
});
