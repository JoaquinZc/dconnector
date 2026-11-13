import { describe, expect, it } from "vitest";
import type { PickedFile } from "@/api/types";
import {
  DEFAULT_MAX_TEXT,
  buildOutgoing,
  defaultScheduleMs,
  draftOf,
  emptyForm,
  formFromDraft,
  isFormBlank,
  mediaKindOf,
  parseLocalInput,
  scheduleError,
  toLocalInput,
  validateForm,
} from "./statusForm";

const file = (p: Partial<PickedFile> = {}): PickedFile => ({ path: "C:\\x\\a.jpg", name: "a.jpg", mime: "image/jpeg", size: 1000, ...p });
const opts = { audienceChoice: true, caps: { maxImageSize: 5000, maxVideoSize: 20000, maxTextLen: 0 } };

describe("validateForm", () => {
  it("texto vacío no se publica", () => {
    expect(validateForm(emptyForm(), opts)).toBe("Escribe el texto del estado");
    expect(validateForm({ ...emptyForm(), text: "   " }, opts)).toBe("Escribe el texto del estado");
    expect(validateForm({ ...emptyForm(), text: "Hola" }, opts)).toBeNull();
  });

  it("foto o vídeo exige archivo y respeta el tamaño máximo", () => {
    const f = { ...emptyForm(), mode: "media" as const };
    expect(validateForm(f, opts)).toBe("Elige una foto o un vídeo");
    expect(validateForm({ ...f, file: file() }, opts)).toBeNull();
    expect(validateForm({ ...f, file: file({ size: 6000 }) }, opts)).toMatch(/máximo/);
    // el límite de vídeo es otro
    expect(validateForm({ ...f, file: file({ name: "v.mp4", mime: "video/mp4", size: 6000 }) }, opts)).toBeNull();
  });

  it("limita el texto (700 por defecto o el de la cuenta)", () => {
    const long = "x".repeat(DEFAULT_MAX_TEXT + 1);
    expect(validateForm({ ...emptyForm(), text: long }, opts)).toMatch(/700/);
    expect(validateForm({ ...emptyForm(), text: "x".repeat(20) }, { ...opts, caps: { ...opts.caps, maxTextLen: 10 } })).toMatch(/10/);
  });

  it("«solo con…» y «excepto…» piden a alguien; «mis contactos» no", () => {
    const f = { ...emptyForm(), text: "Hola" };
    expect(validateForm({ ...f, audienceMode: "allow" }, opts)).toBe("Elige con quién compartirlo");
    expect(validateForm({ ...f, audienceMode: "deny" }, opts)).toBe("Elige a quién excluir");
    expect(validateForm({ ...f, audienceMode: "allow", listIds: ["l1"] }, opts)).toBeNull();
    expect(validateForm({ ...f, audienceMode: "deny", peerIds: ["p"] }, opts)).toBeNull();
    expect(validateForm({ ...f, audienceMode: "contacts" }, opts)).toBeNull();
  });

  it("sin elección de audiencia no se exige nada", () => {
    const f = { ...emptyForm(), text: "Hola", audienceMode: "allow" as const };
    expect(validateForm(f, { audienceChoice: false })).toBeNull();
  });
});

describe("buildOutgoing", () => {
  it("estado de texto con color y letra", () => {
    const o = buildOutgoing({ ...emptyForm(), text: " Hola ", color: "#EF4444", font: 3 }, opts);
    expect(o.kind).toBe("text");
    expect(o.text).toBe("Hola");
    expect(o.bgColor).toBe("#EF4444");
    expect(o.font).toBe(3);
    expect(o.audience).toEqual({ mode: "contacts", peerIds: [], listIds: [] });
  });

  it("foto y vídeo llevan ruta, tipo y pie", () => {
    const base = { ...emptyForm(), mode: "media" as const, text: "Pie" };
    const img = buildOutgoing({ ...base, file: file() }, opts);
    expect(img).toMatchObject({ kind: "image", text: "Pie", filePath: "C:\\x\\a.jpg", mime: "image/jpeg", bgColor: "" });
    const vid = buildOutgoing({ ...base, file: file({ name: "v.mp4", mime: "video/mp4" }) }, opts);
    expect(vid.kind).toBe("video");
  });

  it("la audiencia viaja con listas y personas; sin elección siempre «contacts»", () => {
    const f = { ...emptyForm(), text: "Hola", audienceMode: "allow" as const, listIds: ["l1"], peerIds: ["p1"] };
    expect(buildOutgoing(f, opts).audience).toEqual({ mode: "allow", peerIds: ["p1"], listIds: ["l1"] });
    expect(buildOutgoing(f, { audienceChoice: false }).audience).toEqual({ mode: "contacts", peerIds: [], listIds: [] });
    // «mis contactos» descarta lo que quedara elegido antes
    expect(buildOutgoing({ ...f, audienceMode: "contacts" }, opts).audience).toEqual({ mode: "contacts", peerIds: [], listIds: [] });
  });
});

describe("mediaKindOf", () => {
  it("por MIME y, si falta, por extensión", () => {
    expect(mediaKindOf("video/mp4")).toBe("video");
    expect(mediaKindOf("image/png")).toBe("image");
    expect(mediaKindOf("", "clip.MOV")).toBe("video");
    expect(mediaKindOf(undefined, "foto.heic")).toBe("image");
  });
});

describe("borrador", () => {
  it("no guarda el archivo y se restaura con validación", () => {
    const f = { ...emptyForm(), text: "Hola", file: file(), peerIds: ["p"] };
    const d = draftOf(f);
    expect("file" in d).toBe(false);
    const back = formFromDraft(JSON.parse(JSON.stringify(d)));
    expect(back.text).toBe("Hola");
    expect(back.peerIds).toEqual(["p"]);
    expect(back.file).toBeNull();
  });

  it("ignora valores inválidos", () => {
    const back = formFromDraft({ color: "#123456", font: 77, audienceMode: "xx", listIds: [1, "a"] } as never);
    expect(back.color).toBe(emptyForm().color);
    expect(back.font).toBe(0);
    expect(back.audienceMode).toBe("contacts");
    expect(back.listIds).toEqual(["a"]);
    expect(formFromDraft(null)).toEqual(emptyForm());
  });

  it("isFormBlank", () => {
    expect(isFormBlank(emptyForm())).toBe(true);
    expect(isFormBlank({ ...emptyForm(), text: "a" })).toBe(false);
    expect(isFormBlank({ ...emptyForm(), listIds: ["l"] })).toBe(false);
  });
});

describe("programar", () => {
  it("convierte hora local ↔ campo datetime-local", () => {
    const ms = new Date(2026, 10, 12, 9, 30).getTime();
    expect(toLocalInput(ms)).toBe("2026-11-12T09:30");
    expect(parseLocalInput("2026-11-12T09:30")).toBe(ms);
    expect(parseLocalInput("")).toBeNull();
    expect(parseLocalInput("mañana")).toBeNull();
  });

  it("no deja programar en el pasado ni sin fecha", () => {
    const now = new Date(2026, 10, 12, 9, 0).getTime();
    expect(scheduleError("", now)).toBe("Elige fecha y hora");
    expect(scheduleError("2026-11-12T08:59", now)).toBe("La fecha ya pasó");
    expect(scheduleError("2026-11-12T09:00", now)).toBe("La fecha ya pasó");
    expect(scheduleError("2026-11-12T09:01", now)).toBeNull();
  });

  it("la fecha por defecto es dentro de una hora, a los 5 minutos", () => {
    const now = new Date(2026, 10, 12, 9, 2).getTime();
    const d = defaultScheduleMs(now);
    expect(d - now).toBeGreaterThanOrEqual(60 * 60_000);
    expect(d - now).toBeLessThan(66 * 60_000);
    expect(new Date(d).getMinutes() % 5).toBe(0);
  });
});
