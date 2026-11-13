// «Ritmo de la cuenta»: grupos de campos de PacingSettings, valores seguros, validación y conversión
// entre el formulario (texto) y los números. Lógica pura.
import type { PacingSettings } from "@/api/types";

/** Valores por defecto de `model.DefaultPacing` (03-DOMINIO.md). */
export const DEFAULT_PACING: PacingSettings = {
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
};

export type PacingKey = keyof PacingSettings;

export interface PacingField {
  key: PacingKey;
  label: string;
  /** Unidad que se pinta junto al campo. */
  unit?: string;
  /** Texto de ayuda corto (va en `title`). */
  hint?: string;
  type: "int" | "percent" | "bool";
}

export interface PacingGroup {
  id: string;
  title: string;
  fields: PacingField[];
}

export const PACING_GROUPS: PacingGroup[] = [
  {
    id: "waits",
    title: "Esperas",
    fields: [
      { key: "minDelaySec", label: "Mínima", unit: "s", hint: "Espera entre envíos a conocidos", type: "int" },
      { key: "maxDelaySec", label: "Máxima", unit: "s", hint: "Espera entre envíos a conocidos", type: "int" },
      { key: "coldMinDelaySec", label: "Mínima (fríos)", unit: "s", hint: "Espera con contactos sin conversación", type: "int" },
      { key: "coldMaxDelaySec", label: "Máxima (fríos)", unit: "s", hint: "Espera con contactos sin conversación", type: "int" },
      { key: "typingMsPerChar", label: "Escribiendo…", unit: "ms por letra", hint: "Tiempo de «escribiendo…» antes de enviar", type: "int" },
    ],
  },
  {
    id: "limits",
    title: "Límites",
    fields: [
      { key: "hourlyLimit", label: "Por hora", hint: "0 = sin límite", type: "int" },
      { key: "dailyLimit", label: "Por día", hint: "0 = sin límite", type: "int" },
      { key: "coldDailyLimit", label: "Fríos por día", hint: "0 = sin límite propio", type: "int" },
    ],
  },
  {
    id: "batches",
    title: "Tandas",
    fields: [
      { key: "batchSize", label: "Tamaño", unit: "envíos", hint: "0 = sin tandas", type: "int" },
      { key: "batchRestMinutes", label: "Descanso", unit: "min", hint: "Entre tandas (±30 %)", type: "int" },
    ],
  },
  {
    id: "warmup",
    title: "Calentamiento",
    fields: [
      { key: "warmupEnabled", label: "Activado", type: "bool" },
      { key: "warmupStart", label: "Primer día", unit: "envíos", hint: "Envíos permitidos el primer día", type: "int" },
      { key: "warmupDays", label: "Duración", unit: "días", hint: "Días hasta llegar al límite diario", type: "int" },
    ],
  },
  {
    id: "brake",
    title: "Freno",
    fields: [
      { key: "brakeEnabled", label: "Activado", type: "bool" },
      { key: "brakeFailRate", label: "Fallos", unit: "%", hint: "Frena si fallan más de este porcentaje", type: "percent" },
      { key: "brakeMinSent", label: "Mínimo de envíos", unit: "envíos", hint: "Antes de medir los fallos", type: "int" },
      { key: "brakeUnreadRate", label: "Sin leer", unit: "%", hint: "Frena si quedan sin leer más de este porcentaje", type: "percent" },
      { key: "brakeOptOuts", label: "Bajas", unit: "bajas", hint: "Frena al llegar a este número de bajas", type: "int" },
    ],
  },
  {
    id: "frequency",
    title: "Frecuencia",
    fields: [
      { key: "freqMax", label: "Campañas por contacto", hint: "0 = sin límite", type: "int" },
      { key: "freqDays", label: "En", unit: "días", type: "int" },
    ],
  },
];

export const ALL_PACING_FIELDS: PacingField[] = PACING_GROUPS.flatMap((g) => g.fields);

/** Texto de cada campo (los booleanos como «true»/«false»). */
export type PacingText = Record<PacingKey, string>;
export type PacingErrors = Partial<Record<PacingKey, string>>;

export function pacingToText(p: PacingSettings): PacingText {
  const out = {} as PacingText;
  for (const f of ALL_PACING_FIELDS) out[f.key] = String(p[f.key]);
  return out;
}

/** Los valores seguros (`DefaultPacing`) como texto. */
export function safePacingText(): PacingText {
  return pacingToText(DEFAULT_PACING);
}

/** Texto → números, con los errores por campo. Si hay errores, `pacing` es `null`. */
export function parsePacing(text: PacingText): { pacing: PacingSettings | null; errors: PacingErrors } {
  const errors: PacingErrors = {};
  const values: Record<string, number | boolean> = {};
  for (const f of ALL_PACING_FIELDS) {
    const raw = text[f.key];
    if (f.type === "bool") {
      values[f.key] = raw === "true";
      continue;
    }
    const s = String(raw ?? "").trim().replace(",", ".");
    const n = s === "" ? NaN : Number(s);
    if (!Number.isFinite(n) || n < 0) {
      errors[f.key] = "Número no válido";
      continue;
    }
    if (f.type === "int" && !Number.isInteger(n)) {
      errors[f.key] = "Debe ser entero";
      continue;
    }
    if (f.type === "percent" && n > 100) {
      errors[f.key] = "Entre 0 y 100";
      continue;
    }
    values[f.key] = n;
  }

  const num = (k: PacingKey) => (typeof values[k] === "number" ? (values[k] as number) : null);
  const minMax = (lo: PacingKey, hi: PacingKey) => {
    const a = num(lo);
    const b = num(hi);
    if (a !== null && b !== null && b < a && !errors[hi]) errors[hi] = "No puede ser menor que la mínima";
  };
  minMax("minDelaySec", "maxDelaySec");
  minMax("coldMinDelaySec", "coldMaxDelaySec");
  if (values.warmupEnabled && num("warmupDays") === 0 && !errors.warmupDays) errors.warmupDays = "Al menos 1 día";
  if (num("freqMax") && num("freqDays") === 0 && !errors.freqDays) errors.freqDays = "Al menos 1 día";
  if (num("batchSize") && num("batchRestMinutes") === 0 && !errors.batchRestMinutes) errors.batchRestMinutes = "Al menos 1 minuto";

  if (Object.keys(errors).length > 0) return { pacing: null, errors };
  return { pacing: values as unknown as PacingSettings, errors };
}

/** ¿Son iguales los dos ritmos? */
export function samePacing(a: PacingSettings, b: PacingSettings): boolean {
  return ALL_PACING_FIELDS.every((f) => a[f.key] === b[f.key]);
}

/** Resumen de una línea: «20–45 s entre envíos · 60 por hora · 200 al día». */
export function pacingSummary(p: PacingSettings): string {
  const parts = [`${p.minDelaySec}–${p.maxDelaySec} s entre envíos`];
  parts.push(p.hourlyLimit > 0 ? `${p.hourlyLimit} por hora` : "sin límite por hora");
  parts.push(p.dailyLimit > 0 ? `${p.dailyLimit} al día` : "sin límite al día");
  return parts.join(" · ");
}
