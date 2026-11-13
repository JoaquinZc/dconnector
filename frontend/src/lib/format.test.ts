import { describe, expect, it } from "vitest";
import {
  dayKey,
  formatClock,
  formatDateTime,
  formatDayLabel,
  formatDuration,
  formatFileSize,
  formatListTime,
  truncate,
} from "./format";

// Miércoles 11-nov-2026, 15:00 (hora local: las funciones formatean en hora local).
const NOW = new Date(2026, 10, 11, 15, 0, 0).getTime();
const at = (y: number, m: number, d: number, h = 0, min = 0) => new Date(y, m - 1, d, h, min).getTime();

describe("formatListTime", () => {
  it("hoy: HH:mm", () => {
    expect(formatListTime(at(2026, 11, 11, 9, 5), NOW)).toBe("09:05");
    expect(formatListTime(at(2026, 11, 11, 0, 0), NOW)).toBe("00:00");
  });
  it("ayer: «Ayer» (también si fue pasada la medianoche)", () => {
    expect(formatListTime(at(2026, 11, 10, 23, 59), NOW)).toBe("Ayer");
    expect(formatListTime(at(2026, 11, 10, 0, 1), NOW)).toBe("Ayer");
  });
  it("esta semana: el día", () => {
    expect(formatListTime(at(2026, 11, 9, 12), NOW)).toBe("lunes");
    expect(formatListTime(at(2026, 11, 5, 12), NOW)).toBe("jueves");
  });
  it("más antiguo: dd/MM/yy", () => {
    expect(formatListTime(at(2026, 11, 4, 12), NOW)).toBe("04/11/26");
    expect(formatListTime(at(2025, 1, 2, 12), NOW)).toBe("02/01/25");
  });
  it("sin fecha: vacío", () => {
    expect(formatListTime(0, NOW)).toBe("");
  });
});

describe("otras fechas", () => {
  it("formatClock y formatDateTime", () => {
    expect(formatClock(at(2026, 3, 4, 7, 8))).toBe("07:08");
    expect(formatDateTime(at(2026, 3, 4, 7, 8))).toBe("04/03/2026 07:08");
  });
  it("separador de día", () => {
    expect(formatDayLabel(at(2026, 11, 11, 1), NOW)).toBe("Hoy");
    expect(formatDayLabel(at(2026, 11, 10, 1), NOW)).toBe("Ayer");
    expect(formatDayLabel(at(2026, 11, 8, 1), NOW)).toBe("domingo");
    expect(formatDayLabel(at(2026, 5, 3, 1), NOW)).toBe("3 de mayo de 2026");
  });
  it("dayKey agrupa por día local", () => {
    expect(dayKey(at(2026, 11, 11, 0, 1))).toBe(dayKey(at(2026, 11, 11, 23, 59)));
    expect(dayKey(at(2026, 11, 11, 23, 59))).not.toBe(dayKey(at(2026, 11, 12, 0, 0)));
  });
});

describe("formatFileSize", () => {
  it("unidades y coma decimal", () => {
    expect(formatFileSize(0)).toBe("0 B");
    expect(formatFileSize(512)).toBe("512 B");
    expect(formatFileSize(1024)).toBe("1 KB");
    expect(formatFileSize(1536)).toBe("1,5 KB");
    expect(formatFileSize(5 * 1024 * 1024)).toBe("5 MB");
    expect(formatFileSize(1.5 * 1024 * 1024 * 1024)).toBe("1,5 GB");
  });
  it("valores no válidos", () => {
    expect(formatFileSize(-1)).toBe("");
    expect(formatFileSize(NaN)).toBe("");
  });
});

describe("formatDuration", () => {
  it("mm:ss", () => {
    expect(formatDuration(0)).toBe("00:00");
    expect(formatDuration(5)).toBe("00:05");
    expect(formatDuration(65.9)).toBe("01:05");
    expect(formatDuration(600)).toBe("10:00");
  });
  it("h:mm:ss desde una hora", () => {
    expect(formatDuration(3725)).toBe("1:02:05");
  });
  it("negativos y NaN", () => {
    expect(formatDuration(-4)).toBe("00:00");
    expect(formatDuration(NaN)).toBe("00:00");
  });
});

describe("truncate", () => {
  it("recorta con «…»", () => {
    expect(truncate("abcdef", 6)).toBe("abcdef");
    expect(truncate("abcdefg", 6)).toBe("abcde…");
    expect(truncate("Distribuidora Norte", 12)).toBe("Distribuido…");
  });
});
