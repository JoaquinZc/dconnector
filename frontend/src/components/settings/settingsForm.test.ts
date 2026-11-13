import { describe, expect, it } from "vitest";
import type { AppSettings } from "@/api/types";
import {
  AUTO_KINDS,
  DEFAULT_AUTO_DOWNLOAD,
  asTheme,
  formFromDraft,
  isDirty,
  normalizeKeywords,
  readAutoDownload,
  toAppSettings,
  toSettingsForm,
  validateSettings,
} from "./settingsForm";

const settings = (p: Partial<AppSettings> = {}): AppSettings => ({
  closeToTray: true,
  startWithWindows: false,
  startMinimized: false,
  notifications: true,
  notificationsSound: true,
  notificationsPreview: true,
  theme: "system",
  verboseLog: false,
  autoHistory: true,
  autoDownload: {
    image: { enabled: true, maxMB: 20 },
    audio: { enabled: true, maxMB: 20 },
    voice: { enabled: true, maxMB: 20 },
    video: { enabled: false, maxMB: 50 },
    document: { enabled: false, maxMB: 20 },
    sticker: { enabled: true, maxMB: 5 },
  },
  optOutKeywords: "baja,stop",
  optOutFooterText: "Responde BAJA",
  ...p,
});

describe("toSettingsForm", () => {
  it("copia los ajustes", () => {
    const f = toSettingsForm(settings({ theme: "dark", verboseLog: true, startWithWindows: true }));
    expect(f).toMatchObject({ theme: "dark", verboseLog: true, startWithWindows: true, closeToTray: true, optOutKeywords: "baja,stop" });
    expect(f.autoDownload.video).toEqual({ enabled: false, maxMB: 50 });
  });

  it("un tema desconocido es «system»", () => {
    expect(toSettingsForm(settings({ theme: "azul" })).theme).toBe("system");
    expect(asTheme(undefined)).toBe("system");
    expect(asTheme("light")).toBe("light");
  });

  it("sonido y vista previa de las notificaciones: lo guardado manda; sin dato quedan encendidos", () => {
    const f = toSettingsForm(settings({ notificationsSound: false, notificationsPreview: false }));
    expect(f.notificationsSound).toBe(false);
    expect(f.notificationsPreview).toBe(false);
    const { notificationsSound: _s, notificationsPreview: _p, ...old } = settings();
    const g = toSettingsForm(old as Partial<AppSettings>);
    expect(g.notificationsSound).toBe(true);
    expect(g.notificationsPreview).toBe(true);
    expect(toSettingsForm(null)).toMatchObject({ notificationsSound: true, notificationsPreview: true });
  });

  it("sonido y vista previa viajan siempre en AppSettings, también apagados", () => {
    const out = toAppSettings(toSettingsForm(settings({ notificationsSound: false, notificationsPreview: false })));
    expect(out.notificationsSound).toBe(false);
    expect(out.notificationsPreview).toBe(false);
    const on = toAppSettings(toSettingsForm(settings()));
    expect(on.notificationsSound).toBe(true);
    expect(on.notificationsPreview).toBe(true);
    expect(isDirty(toSettingsForm(settings()), toSettingsForm(settings({ notificationsSound: false })))).toBe(true);
    expect(isDirty(toSettingsForm(settings()), toSettingsForm(settings({ notificationsPreview: false })))).toBe(true);
  });

  it("autoHistory: lo guardado manda; sin dato queda encendido (como el backend)", () => {
    expect(toSettingsForm(settings({ autoHistory: false })).autoHistory).toBe(false);
    expect(toSettingsForm(settings({ autoHistory: true })).autoHistory).toBe(true);
    expect(toSettingsForm(null).autoHistory).toBe(true);
  });

  it("autoHistory viaja siempre en AppSettings, también apagado", () => {
    expect(toAppSettings(toSettingsForm(settings({ autoHistory: false }))).autoHistory).toBe(false);
    expect(toAppSettings(toSettingsForm(settings({ autoHistory: true }))).autoHistory).toBe(true);
    expect(isDirty(toSettingsForm(settings({ autoHistory: true })), toSettingsForm(settings({ autoHistory: false })))).toBe(true);
  });

  it("sin datos usa los valores por defecto", () => {
    const f = toSettingsForm(null);
    expect(f.closeToTray).toBe(true);
    expect(f.notifications).toBe(true);
    expect(f.autoDownload).toEqual(DEFAULT_AUTO_DOWNLOAD);
  });
});

describe("readAutoDownload", () => {
  it("devuelve los 6 tipos aunque falten o vengan mal", () => {
    const a = readAutoDownload({ image: { enabled: false, maxMB: 3 }, audio: "x", video: { maxMB: "80" }, extra: { enabled: true } });
    expect(Object.keys(a)).toEqual(AUTO_KINDS.map((k) => k.id));
    expect(a.image).toEqual({ enabled: false, maxMB: 3 });
    expect(a.audio).toEqual(DEFAULT_AUTO_DOWNLOAD.audio);
    expect(a.video).toEqual({ enabled: false, maxMB: 80 }); // el interruptor toma el valor por defecto
    expect(a.voice).toEqual(DEFAULT_AUTO_DOWNLOAD.voice);
  });

  it("null o basura", () => {
    expect(readAutoDownload(null)).toEqual(DEFAULT_AUTO_DOWNLOAD);
    expect(readAutoDownload([1])).toEqual(DEFAULT_AUTO_DOWNLOAD);
  });

  it("no comparte objetos con los valores por defecto", () => {
    const a = readAutoDownload(null);
    a.image.maxMB = 999;
    expect(DEFAULT_AUTO_DOWNLOAD.image.maxMB).toBe(20);
  });
});

describe("toAppSettings", () => {
  it("ida y vuelta sin cambios", () => {
    const s = settings();
    expect(toAppSettings(toSettingsForm(s), s)).toEqual(s);
  });

  it("autoDownload sale como { tipo: { enabled, maxMB } } con números enteros", () => {
    const f = toSettingsForm(settings());
    f.autoDownload.video = { enabled: true, maxMB: 99.6 };
    const out = toAppSettings(f);
    expect(out.autoDownload?.video).toEqual({ enabled: true, maxMB: 100 });
    expect(Object.keys(out.autoDownload ?? {}).sort()).toEqual(["audio", "document", "image", "sticker", "video", "voice"]);
  });

  it("conserva tipos que la interfaz no conoce", () => {
    const s = settings({ autoDownload: { ...(settings().autoDownload as object), location: { enabled: true, maxMB: 1 } } });
    expect(toAppSettings(toSettingsForm(s), s).autoDownload?.location).toEqual({ enabled: true, maxMB: 1 });
  });

  it("normaliza palabras de baja y recorta el pie", () => {
    const f = { ...toSettingsForm(settings()), optOutKeywords: " Baja ,, stop\nSTOP; no más ", optOutFooterText: "  Responde BAJA  " };
    const out = toAppSettings(f);
    expect(out.optOutKeywords).toBe("Baja,stop,no más");
    expect(out.optOutFooterText).toBe("Responde BAJA");
  });

  it("el tema viaja tal cual", () => {
    expect(toAppSettings({ ...toSettingsForm(settings()), theme: "dark" }).theme).toBe("dark");
  });
});

describe("normalizeKeywords", () => {
  it("sin vacías ni repetidas (sin distinguir mayúsculas)", () => {
    expect(normalizeKeywords("")).toBe("");
    expect(normalizeKeywords(" , ,")).toBe("");
    expect(normalizeKeywords("a, A, b")).toBe("a,b");
  });
});

describe("isDirty", () => {
  it("espacios y mayúsculas repetidas no cuentan como cambio, un interruptor sí", () => {
    const a = toSettingsForm(settings());
    expect(isDirty(a, { ...a })).toBe(false);
    expect(isDirty(a, { ...a, optOutKeywords: "baja , stop " })).toBe(false);
    expect(isDirty(a, { ...a, optOutFooterText: " Responde BAJA " })).toBe(false);
    expect(isDirty(a, { ...a, notifications: false })).toBe(true);
    expect(isDirty(a, { ...a, theme: "dark" })).toBe(true);
    expect(isDirty(a, { ...a, autoDownload: { ...a.autoDownload, image: { enabled: true, maxMB: 21 } } })).toBe(true);
  });
});

describe("validateSettings", () => {
  it("el tamaño máximo va de 1 a 2048 MB", () => {
    const f = toSettingsForm(settings());
    expect(validateSettings(f)).toBeNull();
    expect(validateSettings({ ...f, autoDownload: { ...f.autoDownload, video: { enabled: true, maxMB: 0 } } })).toMatch(/Vídeos/);
    expect(validateSettings({ ...f, autoDownload: { ...f.autoDownload, image: { enabled: true, maxMB: NaN } } })).toMatch(/Fotos/);
    expect(validateSettings({ ...f, autoDownload: { ...f.autoDownload, sticker: { enabled: false, maxMB: 5000 } } })).toMatch(/Stickers/);
  });
});

describe("formFromDraft", () => {
  it("restaura un borrador del formulario", () => {
    const f = { ...toSettingsForm(settings()), theme: "dark" as const, notifications: false };
    const back = formFromDraft(JSON.parse(JSON.stringify(f)));
    expect(back).toEqual(f);
  });

  it("basura → null", () => {
    expect(formFromDraft(null)).toBeNull();
    expect(formFromDraft("x")).toBeNull();
  });
});
