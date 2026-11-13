import { describe, expect, it } from "vitest";
import type { Scheduled } from "@/api/types";
import { emptyRecurrenceForm } from "@/lib/recurrence";
import { makeAttachment } from "./composerReducer";
import { buildChatScheduled, outgoingsFor, savedToast, scheduleCounts, validateScheduleDraft } from "./scheduleModel";

const NOW = new Date(2026, 10, 11, 15, 30, 0).getTime();
const target = { accountId: "wa-1", chatNativeId: "c1", chatTitle: "Ana" };
const img = makeAttachment({ path: "C:\\tmp\\a.jpg", name: "a.jpg", mime: "image/jpeg", size: 10 });
const doc = makeAttachment({ path: "C:\\tmp\\b.pdf", name: "b.pdf", mime: "application/pdf", size: 10 });

describe("qué mensajes se programan", () => {
  it("solo texto", () => {
    const o = outgoingsFor("  Hola  ", []);
    expect(o).toHaveLength(1);
    expect(o[0]).toMatchObject({ kind: "text", text: "Hola" });
  });

  it("el texto y luego cada adjunto, por separado", () => {
    const o = outgoingsFor("Mira", [img, doc]);
    expect(o.map((m) => m.kind)).toEqual(["text", "image", "document"]);
    expect(o[1]).toMatchObject({ filePath: "C:\\tmp\\a.jpg", fileName: "a.jpg", mime: "image/jpeg" });
  });

  it("sin texto, solo los adjuntos; sin nada, nada", () => {
    expect(outgoingsFor("   ", [img]).map((m) => m.kind)).toEqual(["image"]);
    expect(outgoingsFor("", [])).toEqual([]);
  });
});

describe("validación del diálogo", () => {
  const ok = { text: "Hola", fileCount: 0, when: "2026-11-12T09:00", recurrence: emptyRecurrenceForm() };

  it("vale", () => {
    expect(validateScheduleDraft(ok, NOW)).toBeNull();
  });

  it("pide mensaje o adjunto (al editar, el archivo que ya tiene cuenta)", () => {
    expect(validateScheduleDraft({ ...ok, text: "  " }, NOW)).toBe("Escribe un mensaje o adjunta un archivo");
    expect(validateScheduleDraft({ ...ok, text: "", fileCount: 1 }, NOW)).toBeNull();
    expect(validateScheduleDraft({ ...ok, text: "", keepsFile: true }, NOW)).toBeNull();
  });

  it("texto demasiado largo según el canal", () => {
    expect(validateScheduleDraft({ ...ok, text: "x".repeat(21), maxTextLen: 20 }, NOW)).toBe("El texto es demasiado largo");
    expect(validateScheduleDraft({ ...ok, text: "x".repeat(21), maxTextLen: 0 }, NOW)).toBeNull();
  });

  it("fecha vacía, pasada (más de 1 h) o dentro de la tolerancia", () => {
    expect(validateScheduleDraft({ ...ok, when: "" }, NOW)).toBe("Elige fecha y hora");
    expect(validateScheduleDraft({ ...ok, when: "2026-11-11T10:00" }, NOW)).toBe("La fecha ya pasó");
    expect(validateScheduleDraft({ ...ok, when: "2026-11-11T14:45" }, NOW)).toBeNull();
  });

  it("incluye la validación de la repetición", () => {
    const rec = { ...emptyRecurrenceForm(), repeat: "custom" as const, customEvery: 3, customUnit: "minutes" as const };
    expect(validateScheduleDraft({ ...ok, recurrence: rec }, NOW)).toBe("La repetición mínima es cada 5 minutos");
  });
});

describe("el Scheduled que se guarda", () => {
  it("nuevo: sin id, pendiente, con la fecha en ms y la repetición", () => {
    const rec = { ...emptyRecurrenceForm(), repeat: "daily" as const };
    const s = buildChatScheduled(target, outgoingsFor("Hola", [])[0], "2026-11-12T09:00", rec);
    expect(s).toMatchObject({
      id: "",
      accountId: "wa-1",
      kind: "message",
      chatNativeId: "c1",
      chatTitle: "Ana",
      state: "pending",
      seriesId: "",
      sendAt: new Date(2026, 10, 12, 9, 0).getTime(),
      recurrence: { frequency: "daily", every: 1 },
    });
  });

  it("sin repetición manda null", () => {
    expect(buildChatScheduled(target, outgoingsFor("Hola", [])[0], "2026-11-12T09:00", emptyRecurrenceForm()).recurrence).toBeNull();
  });

  it("editar conserva id, serie, creación y la regla guardada (count/firstAt)", () => {
    const base = {
      id: "p1",
      seriesId: "s1",
      createdAt: 42,
      recurrence: { frequency: "daily", every: 1, endAt: 0, maxCount: 0, count: 2, firstAt: 99 },
    } as unknown as Scheduled;
    const rec = { ...emptyRecurrenceForm(), repeat: "daily" as const };
    const s = buildChatScheduled(target, outgoingsFor("Nuevo", [])[0], "2026-11-12T09:00", rec, base);
    expect(s).toMatchObject({ id: "p1", seriesId: "s1", createdAt: 42, recurrence: { count: 2, firstAt: 99 } });
  });
});

describe("avisos y recuentos", () => {
  it("texto del aviso", () => {
    expect(savedToast(1, false)).toBe("Mensaje programado");
    expect(savedToast(3, false)).toBe("3 mensajes programados");
    expect(savedToast(1, true)).toBe("Cambios guardados");
  });

  it("pendientes y fallidos", () => {
    expect(scheduleCounts([{ state: "pending" }, { state: "pending" }, { state: "failed" }, { state: "sent" }, { state: "canceled" }])).toEqual({
      pending: 2,
      failed: 1,
    });
    expect(scheduleCounts([])).toEqual({ pending: 0, failed: 0 });
  });
});
