import { describe, expect, it } from "vitest";
import {
  ALL_PACING_FIELDS,
  DEFAULT_PACING,
  PACING_GROUPS,
  pacingSummary,
  pacingToText,
  parsePacing,
  safePacingText,
  samePacing,
} from "./pacingModel";

describe("agrupación de campos", () => {
  it("seis grupos en el orden pedido", () => {
    expect(PACING_GROUPS.map((g) => g.title)).toEqual(["Esperas", "Límites", "Tandas", "Calentamiento", "Freno", "Frecuencia"]);
  });

  it("cubre todos los campos de PacingSettings, cada uno una vez", () => {
    const keys = ALL_PACING_FIELDS.map((f) => f.key).sort();
    expect(keys).toEqual(Object.keys(DEFAULT_PACING).sort());
    expect(new Set(keys).size).toBe(keys.length);
  });

  it("qué campo va en qué grupo", () => {
    const of = (id: string) => PACING_GROUPS.find((g) => g.id === id)!.fields.map((f) => f.key);
    expect(of("waits")).toEqual(["minDelaySec", "maxDelaySec", "coldMinDelaySec", "coldMaxDelaySec", "typingMsPerChar"]);
    expect(of("limits")).toEqual(["hourlyLimit", "dailyLimit", "coldDailyLimit"]);
    expect(of("batches")).toEqual(["batchSize", "batchRestMinutes"]);
    expect(of("warmup")).toEqual(["warmupEnabled", "warmupStart", "warmupDays"]);
    expect(of("brake")).toEqual(["brakeEnabled", "brakeFailRate", "brakeMinSent", "brakeUnreadRate", "brakeOptOuts"]);
    expect(of("frequency")).toEqual(["freqMax", "freqDays"]);
  });
});

describe("valores seguros", () => {
  it("son los de DefaultPacing (03-DOMINIO)", () => {
    expect(DEFAULT_PACING).toMatchObject({
      minDelaySec: 20,
      maxDelaySec: 45,
      coldMinDelaySec: 60,
      coldMaxDelaySec: 120,
      dailyLimit: 200,
      coldDailyLimit: 20,
      typingMsPerChar: 300,
      batchSize: 25,
      batchRestMinutes: 10,
      hourlyLimit: 60,
      warmupEnabled: true,
      warmupStart: 20,
      warmupDays: 14,
      brakeEnabled: true,
      brakeFailRate: 30,
      brakeMinSent: 30,
      brakeUnreadRate: 95,
      brakeOptOuts: 3,
      freqMax: 3,
      freqDays: 7,
    });
  });

  it("texto → números devuelve exactamente los valores seguros", () => {
    const r = parsePacing(safePacingText());
    expect(r.errors).toEqual({});
    expect(r.pacing).toEqual(DEFAULT_PACING);
    expect(samePacing(r.pacing!, DEFAULT_PACING)).toBe(true);
  });

  it("ida y vuelta de un ritmo cualquiera", () => {
    const p = { ...DEFAULT_PACING, minDelaySec: 5, maxDelaySec: 9, warmupEnabled: false, brakeFailRate: 12.5 };
    expect(parsePacing(pacingToText(p)).pacing).toEqual(p);
    expect(samePacing(p, DEFAULT_PACING)).toBe(false);
  });
});

describe("validación del formulario", () => {
  const bad = (patch: Record<string, string>) => parsePacing({ ...safePacingText(), ...patch } as never);

  it("vacío, texto o negativo: «Número no válido»", () => {
    expect(bad({ dailyLimit: "" }).errors.dailyLimit).toBe("Número no válido");
    expect(bad({ dailyLimit: "abc" }).errors.dailyLimit).toBe("Número no válido");
    expect(bad({ dailyLimit: "-1" }).errors.dailyLimit).toBe("Número no válido");
    expect(bad({ dailyLimit: "" }).pacing).toBeNull();
  });

  it("los enteros no admiten decimales; los porcentajes sí, hasta 100", () => {
    expect(bad({ batchSize: "2.5" }).errors.batchSize).toBe("Debe ser entero");
    expect(bad({ brakeFailRate: "12,5" }).errors).toEqual({});
    expect(bad({ brakeFailRate: "101" }).errors.brakeFailRate).toBe("Entre 0 y 100");
    expect(bad({ brakeUnreadRate: "100" }).errors).toEqual({});
  });

  it("la máxima no puede ser menor que la mínima", () => {
    expect(bad({ minDelaySec: "50", maxDelaySec: "10" }).errors.maxDelaySec).toBe("No puede ser menor que la mínima");
    expect(bad({ coldMinDelaySec: "100", coldMaxDelaySec: "60" }).errors.coldMaxDelaySec).toBeDefined();
    expect(bad({ minDelaySec: "10", maxDelaySec: "10" }).errors).toEqual({});
  });

  it("0 en límites y tandas es «sin límite»", () => {
    const r = bad({ dailyLimit: "0", hourlyLimit: "0", coldDailyLimit: "0", batchSize: "0", freqMax: "0" });
    expect(r.errors).toEqual({});
    expect(r.pacing).toMatchObject({ dailyLimit: 0, batchSize: 0, freqMax: 0 });
  });

  it("calentamiento y frecuencia piden al menos un día cuando están activos", () => {
    expect(bad({ warmupDays: "0" }).errors.warmupDays).toBe("Al menos 1 día");
    expect(bad({ warmupEnabled: "false", warmupDays: "0" }).errors).toEqual({});
    expect(bad({ freqDays: "0" }).errors.freqDays).toBe("Al menos 1 día");
    expect(bad({ freqMax: "0", freqDays: "0" }).errors).toEqual({});
    expect(bad({ batchRestMinutes: "0" }).errors.batchRestMinutes).toBe("Al menos 1 minuto");
  });
});

describe("resumen", () => {
  it("una línea con el rango de espera y los límites", () => {
    expect(pacingSummary(DEFAULT_PACING)).toBe("20–45 s entre envíos · 60 por hora · 200 al día");
    expect(pacingSummary({ ...DEFAULT_PACING, hourlyLimit: 0, dailyLimit: 0 })).toBe(
      "20–45 s entre envíos · sin límite por hora · sin límite al día",
    );
  });
});
