import { describe, expect, it } from "vitest";
import {
  NO_PREVIEW_LABEL,
  UNSUPPORTED_LABEL,
  dayLabel,
  emptyPreview,
  deliveryTick,
  formatBytes,
  formatDuration,
  formatListTime,
  initials,
  isMuted,
  messagePreview,
  normId,
  phoneFromNative,
  previewParts,
  sameId,
  senderColor,
  truncate,
  unsupportedLabel,
} from "./chatFormat";
import { at, chat, msg, NOW } from "./testkit";

describe("formatListTime", () => {
  it("hoy: HH:mm", () => expect(formatListTime(at(11, 9, 5), NOW)).toBe("09:05"));
  it("ayer", () => expect(formatListTime(at(10, 23, 59), NOW)).toBe("Ayer"));
  it("esta semana: el día", () => {
    // 8 de noviembre de 2026 es domingo
    expect(formatListTime(at(8, 12), NOW)).toBe("domingo");
  });
  it("más antiguo: dd/MM/yy", () => expect(formatListTime(at(1, 12), NOW)).toBe("01/11/26"));
  it("sin fecha: vacío", () => expect(formatListTime(0, NOW)).toBe(""));
});

describe("dayLabel", () => {
  it("hoy, ayer, día de la semana y fecha larga", () => {
    expect(dayLabel(at(11, 1), NOW)).toBe("Hoy");
    expect(dayLabel(at(10, 1), NOW)).toBe("Ayer");
    expect(dayLabel(at(8, 1), NOW)).toBe("Domingo");
    expect(dayLabel(at(1, 1), NOW)).toBe("1 de noviembre de 2026");
  });
});

describe("previewParts", () => {
  it("«Tú: » solo si el último es mío", () => {
    expect(previewParts(chat({ lastFromMe: true, lastPreview: "Listo" }))).toEqual({ prefix: "Tú: ", text: "Listo" });
    expect(previewParts(chat({ lastFromMe: false, lastPreview: "Hola" }))).toEqual({ prefix: "", text: "Hola" });
  });
  it("sin texto no hay prefijo y los saltos de línea se aplanan", () => {
    expect(previewParts(chat({ lastFromMe: true, lastPreview: "" })).prefix).toBe("");
    expect(previewParts(chat({ lastPreview: "a\n\nb" })).text).toBe("a b");
  });
});

describe("deliveryTick", () => {
  const t = (delivery: string, fromMe = true) => deliveryTick({ fromMe, delivery, kind: "text" } as never);
  it("estados de un mensaje propio", () => {
    expect(t("pending")).toBe("pending");
    expect(t("sent")).toBe("sent");
    expect(t("delivered")).toBe("delivered");
    expect(t("read")).toBe("read");
    expect(t("played")).toBe("read");
    expect(t("failed")).toBe("failed");
  });
  it("no aplica a mensajes ajenos ni de sistema", () => {
    expect(t("read", false)).toBeNull();
    expect(deliveryTick({ fromMe: true, delivery: "sent", kind: "system" } as never)).toBeNull();
  });
});

describe("utilidades", () => {
  it("formatBytes y formatDuration", () => {
    expect(formatBytes(0)).toBe("");
    expect(formatBytes(900)).toBe("900 B");
    expect(formatBytes(2048)).toBe("2,0 KB");
    expect(formatBytes(5 * 1024 * 1024)).toBe("5,0 MB");
    expect(formatDuration(65)).toBe("1:05");
    expect(formatDuration(3725)).toBe("1:02:05");
    expect(formatDuration(0)).toBe("0:00");
  });
  it("truncate, initials y senderColor estable", () => {
    expect(truncate("Distribuidora Andina", 12)).toBe("Distribuido…");
    expect(truncate("Corto", 12)).toBe("Corto");
    expect(initials("ana pérez")).toBe("AP");
    expect(initials("")).toBe("?");
    expect(senderColor("abc")).toBe(senderColor("abc"));
    expect(senderColor("abc")).toMatch(/^#[0-9a-f]{6}$/);
  });
  it("silenciado: 0 no, -1 siempre, futuro sí", () => {
    expect(isMuted(0, NOW)).toBe(false);
    expect(isMuted(-1, NOW)).toBe(true);
    expect(isMuted(NOW + 1000, NOW)).toBe(true);
    expect(isMuted(NOW - 1000, NOW)).toBe(false);
  });
  it("ids nativos con sufijo de dispositivo", () => {
    expect(normId("593999:12@s.whatsapp.net")).toBe("593999@s.whatsapp.net");
    expect(sameId("593999:12@s.whatsapp.net", "593999@s.whatsapp.net")).toBe(true);
    expect(sameId("", "")).toBe(false);
  });
  it("teléfono desde el id nativo", () => {
    expect(phoneFromNative("593999111222@s.whatsapp.net")).toBe("+593999111222");
    expect(phoneFromNative("user:123")).toBe("");
  });
});

describe("messagePreview", () => {
  it("etiqueta por tipo", () => {
    expect(messagePreview(msg({ kind: "text", text: " hola \n mundo " }))).toBe("hola mundo");
    expect(messagePreview(msg({ kind: "image", text: "" }))).toBe("📷 Foto");
    expect(messagePreview(msg({ kind: "voice" }))).toBe("🎤 Nota de voz");
    expect(messagePreview(msg({ kind: "document", media: { fileName: "a.pdf" } }))).toBe("📄 a.pdf");
    expect(messagePreview(msg({ kind: "text", deletedAt: 5 }))).toBe("Mensaje eliminado");
  });
});

describe("mensajes no compatibles", () => {
  it("con texto se muestra ese texto; vacío o solo espacios, el aviso genérico", () => {
    expect(unsupportedLabel({ text: "Mensaje de una sola vez: ábrelo en tu teléfono" })).toBe(
      "Mensaje de una sola vez: ábrelo en tu teléfono",
    );
    expect(unsupportedLabel({ text: "  Esperando \n este mensaje…  " })).toBe("Esperando este mensaje…");
    expect(unsupportedLabel({ text: "" })).toBe(UNSUPPORTED_LABEL);
    expect(unsupportedLabel({ text: "   " })).toBe("Mensaje no compatible");
    expect(unsupportedLabel({})).toBe("Mensaje no compatible");
    expect(unsupportedLabel({ text: null })).toBe("Mensaje no compatible");
  });

  it("la vista previa de una cita o respuesta usa el mismo criterio", () => {
    expect(messagePreview(msg({ kind: "unsupported", text: "Esperando este mensaje…" }))).toBe("Esperando este mensaje…");
    expect(messagePreview(msg({ kind: "unsupported", text: "" }))).toBe("Mensaje no compatible");
  });
});

describe("emptyPreview (chat sin vista previa)", () => {
  it("directo: el teléfono, si el título no lo repite", () => {
    expect(emptyPreview(chat({ title: "Ana Pérez", peerNativeId: "593999111222@s.whatsapp.net" }))).toEqual({
      text: "+593999111222",
      placeholder: false,
    });
  });

  it("directo cuyo título ya es el teléfono: «Sin mensajes cargados»", () => {
    expect(emptyPreview(chat({ title: "+593 999 111 222", peerNativeId: "593999111222@s.whatsapp.net" }))).toEqual({
      text: NO_PREVIEW_LABEL,
      placeholder: true,
    });
  });

  it("grupos y ids que no son teléfono: «Sin mensajes cargados»", () => {
    expect(emptyPreview(chat({ kind: "group", title: "Equipo", peerNativeId: "" , nativeId: "1203@g.us" })).text).toBe("Sin mensajes cargados");
    expect(emptyPreview(chat({ title: "ana_tg", peerNativeId: "user:77", nativeId: "user:77" })).placeholder).toBe(true);
  });
});
