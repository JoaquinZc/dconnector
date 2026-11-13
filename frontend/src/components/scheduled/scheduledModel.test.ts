import { describe, expect, it } from "vitest";
import type { CapSet, Scheduled } from "@/api/types";
import {
  buildScheduled,
  contentSummary,
  countByFilter,
  destinationLabel,
  emptyScheduledForm,
  filterScheduled,
  formFromScheduled,
  kindForFile,
  kindLabel,
  outgoingFromForm,
  parseLocalInput,
  rowActions,
  scheduledState,
  sortScheduled,
  toLocalInput,
  validateScheduledForm,
  type ScheduledForm,
} from "./scheduledModel";

const NOW = new Date(2026, 10, 11, 10, 0, 0).getTime();

const sch = (id: string, state: string, sendAt: number, over: Partial<Record<keyof Scheduled, unknown>> = {}): Scheduled =>
  ({
    id,
    accountId: "wa-1",
    kind: "message",
    chatNativeId: "5939@s.whatsapp.net",
    chatTitle: "Ana",
    message: { kind: "text", text: "Hola" },
    sendAt,
    state,
    sentAt: 0,
    error: "",
    createdAt: 1,
    ...over,
  }) as unknown as Scheduled;

const CAPS = {
  sendText: true,
  sendImage: true,
  sendVideo: false,
  sendAudio: true,
  sendDocument: true,
  maxTextLen: 20,
  maxFileSize: 1000,
  maxImageSize: 500,
  maxVideoSize: 0,
} as unknown as CapSet;

function form(over: Partial<ScheduledForm> = {}): ScheduledForm {
  return {
    ...emptyScheduledForm(NOW),
    accountId: "wa-1",
    chatNativeId: "c1",
    chatTitle: "Ana",
    text: "Hola",
    when: "2026-11-12T09:00",
    ...over,
  };
}

describe("fecha y hora local ↔ milisegundos", () => {
  it("convierte en los dos sentidos", () => {
    const ms = new Date(2026, 10, 12, 9, 5).getTime();
    expect(toLocalInput(ms)).toBe("2026-11-12T09:05");
    expect(parseLocalInput("2026-11-12T09:05")).toBe(ms);
    expect(parseLocalInput(toLocalInput(ms))).toBe(ms);
  });

  it("rellena con ceros y no depende de segundos", () => {
    expect(toLocalInput(new Date(2026, 0, 3, 4, 7, 59).getTime())).toBe("2026-01-03T04:07");
  });

  it("rechaza lo que no es una fecha", () => {
    expect(parseLocalInput("")).toBeNull();
    expect(parseLocalInput("mañana")).toBeNull();
  });
});

describe("filtros por estado", () => {
  const list = [sch("a", "pending", 3), sch("b", "sent", 2), sch("c", "failed", 1), sch("d", "canceled", 4), sch("e", "pending", 5)];

  it("filtra y cuenta", () => {
    expect(filterScheduled(list, "all")).toHaveLength(5);
    expect(filterScheduled(list, "pending").map((s) => s.id)).toEqual(["a", "e"]);
    expect(filterScheduled(list, "sent").map((s) => s.id)).toEqual(["b"]);
    expect(filterScheduled(list, "failed").map((s) => s.id)).toEqual(["c"]);
    expect(filterScheduled(list, "canceled").map((s) => s.id)).toEqual(["d"]);
    expect(countByFilter(list)).toEqual({ all: 5, pending: 2, sent: 1, failed: 1, canceled: 1 });
  });

  it("pendientes primero y la más próxima arriba; el resto, lo más reciente", () => {
    expect(sortScheduled(list).map((s) => s.id)).toEqual(["a", "e", "d", "b", "c"]);
  });

  it("texto y tono del estado", () => {
    expect(scheduledState("pending")).toEqual({ label: "Pendiente", tone: "info" });
    expect(scheduledState("sent").tone).toBe("ok");
    expect(scheduledState("failed").tone).toBe("danger");
    expect(scheduledState("canceled").label).toBe("Cancelado");
  });
});

describe("acciones por fila", () => {
  it("editar solo si está pendiente o fallido y es un mensaje", () => {
    expect(rowActions({ state: "pending", kind: "message" }).edit).toBe(true);
    expect(rowActions({ state: "failed", kind: "message" }).edit).toBe(true);
    expect(rowActions({ state: "sent", kind: "message" }).edit).toBe(false);
    expect(rowActions({ state: "canceled", kind: "message" }).edit).toBe(false);
    expect(rowActions({ state: "pending", kind: "status" }).edit).toBe(false);
  });

  it("enviar ahora y cancelar", () => {
    expect(rowActions({ state: "pending", kind: "message" })).toMatchObject({ sendNow: true, cancel: true });
    expect(rowActions({ state: "failed", kind: "message" })).toMatchObject({ sendNow: true, cancel: false });
    expect(rowActions({ state: "sent", kind: "message" })).toMatchObject({ sendNow: false, cancel: false });
  });

  it("los de estado se pueden cancelar y borrar, no editar", () => {
    const a = rowActions({ state: "pending", kind: "status" });
    expect(a).toMatchObject({ edit: false, cancel: true, remove: true, sendNow: true });
  });

  it("borrar siempre", () => {
    for (const s of ["pending", "sent", "failed", "canceled"]) expect(rowActions({ state: s, kind: "message" }).remove).toBe(true);
  });
});

describe("columnas", () => {
  it("tipo, destino y contenido", () => {
    expect(kindLabel({ kind: "message" })).toBe("Mensaje");
    expect(kindLabel({ kind: "status" })).toBe("Estado");
    expect(destinationLabel(sch("a", "pending", 1))).toBe("Ana");
    expect(destinationLabel(sch("a", "pending", 1, { chatTitle: "", chatNativeId: "c9" }))).toBe("c9");
    expect(destinationLabel(sch("a", "pending", 1, { kind: "status", chatTitle: "" }))).toBe("Estados");
    expect(contentSummary(sch("a", "pending", 1))).toBe("Hola");
    expect(contentSummary(sch("a", "pending", 1, { message: { kind: "image", text: "Oferta" } }))).toBe("Foto · Oferta");
    expect(contentSummary(sch("a", "pending", 1, { message: { kind: "document", text: "" } }))).toBe("Documento");
    expect(contentSummary(sch("a", "pending", 1, { kind: "status", message: null, status: { kind: "text", text: "Hoy abrimos" } }))).toBe("Hoy abrimos");
    expect(contentSummary(sch("a", "pending", 1, { kind: "status", message: null, status: { kind: "video", text: "" } }))).toBe("Vídeo");
  });
});

describe("tipo de archivo", () => {
  it("por mime y, si falta, por extensión", () => {
    expect(kindForFile({ name: "a.jpg", mime: "image/jpeg" })).toBe("image");
    expect(kindForFile({ name: "a.mp4", mime: "video/mp4" })).toBe("video");
    expect(kindForFile({ name: "a.mp3", mime: "audio/mpeg" })).toBe("audio");
    expect(kindForFile({ name: "a.pdf", mime: "application/pdf" })).toBe("document");
    expect(kindForFile({ name: "foto.PNG", mime: "" })).toBe("image");
    expect(kindForFile({ name: "clip.mov", mime: "" })).toBe("video");
    expect(kindForFile({ name: "nota.ogg", mime: "" })).toBe("audio");
    expect(kindForFile({ name: "datos.xlsx", mime: "" })).toBe("document");
    expect(kindForFile(null)).toBe("text");
  });

  it("el mensaje: texto solo, o archivo con pie", () => {
    expect(outgoingFromForm(form())).toEqual({ kind: "text", text: "Hola" });
    const o = outgoingFromForm(form({ filePath: "C:\\a.jpg", fileName: "a.jpg", fileMime: "image/jpeg", text: "Pie" }));
    expect(o).toEqual({ kind: "image", text: "Pie", filePath: "C:\\a.jpg", fileName: "a.jpg", mime: "image/jpeg" });
  });
});

describe("validación del formulario", () => {
  it("en orden: cuenta, chat, contenido, fecha", () => {
    expect(validateScheduledForm(form({ accountId: "" }), CAPS, NOW)).toBe("Elige una cuenta");
    expect(validateScheduledForm(form({ chatNativeId: "" }), CAPS, NOW)).toBe("Elige un chat");
    expect(validateScheduledForm(form({ text: " " }), CAPS, NOW)).toBe("Escribe un mensaje o adjunta un archivo");
    expect(validateScheduledForm(form({ when: "2026-11-11T09:00" }), CAPS, NOW)).toBe("La fecha ya pasó");
    expect(validateScheduledForm(form({ when: "" }), CAPS, NOW)).toBe("Elige fecha y hora");
    expect(validateScheduledForm(form(), CAPS, NOW)).toBeNull();
  });

  it("un archivo sin texto vale", () => {
    expect(
      validateScheduledForm(form({ text: "", filePath: "C:\\a.pdf", fileName: "a.pdf", fileMime: "application/pdf" }), CAPS, NOW),
    ).toBeNull();
  });

  it("respeta las capacidades de la cuenta", () => {
    const video = form({ filePath: "C:\\a.mp4", fileName: "a.mp4", fileMime: "video/mp4" });
    expect(validateScheduledForm(video, CAPS, NOW)).toBe("Esta cuenta no envía vídeos");
    expect(validateScheduledForm(form({ text: "x".repeat(21) }), CAPS, NOW)).toBe("El texto es demasiado largo");
    const big = form({ filePath: "C:\\a.jpg", fileName: "a.jpg", fileMime: "image/jpeg", fileSize: 900 });
    expect(validateScheduledForm(big, CAPS, NOW)).toMatch(/^El archivo pesa más de/);
    expect(validateScheduledForm({ ...big, fileSize: 100 }, CAPS, NOW)).toBeNull();
  });

  it("sin capacidades conocidas no se comprueban", () => {
    expect(validateScheduledForm(form({ text: "x".repeat(5000) }), null, NOW)).toBeNull();
  });
});

describe("formulario ↔ Scheduled", () => {
  it("guarda un mensaje pendiente con la fecha en milisegundos", () => {
    const s = buildScheduled(form({ text: "Hola" }));
    expect(s).toMatchObject({
      id: "",
      accountId: "wa-1",
      kind: "message",
      chatNativeId: "c1",
      chatTitle: "Ana",
      state: "pending",
      sentAt: 0,
      error: "",
      sendAt: new Date(2026, 10, 12, 9, 0).getTime(),
    });
    expect(s.message).toEqual({ kind: "text", text: "Hola" });
  });

  it("editar uno fallido lo deja pendiente y conserva createdAt", () => {
    const failed = sch("x", "failed", NOW, { error: "La cuenta no estuvo conectada", createdAt: 42 });
    const f = formFromScheduled(failed);
    expect(f.id).toBe("x");
    expect(f.when).toBe(toLocalInput(NOW));
    const s = buildScheduled({ ...f, text: "Corregido" }, failed);
    expect(s).toMatchObject({ id: "x", state: "pending", error: "", createdAt: 42 });
    expect(s.message?.text).toBe("Corregido");
  });
});
