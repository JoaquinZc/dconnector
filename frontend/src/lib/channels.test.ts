import { describe, expect, it } from "vitest";
import { ACCOUNT_PALETTE, channelInfo, colorForId, initialsOf, nextAccountColor } from "./channels";

describe("canales", () => {
  it("nombre, color y letra de cada canal", () => {
    expect(channelInfo("whatsapp")).toMatchObject({ name: "WhatsApp", color: "#25D366", letter: "W" });
    expect(channelInfo("telegram")).toMatchObject({ name: "Telegram", color: "#229ED9", letter: "T" });
    expect(channelInfo("messenger")).toMatchObject({ name: "Messenger", color: "#0084FF", letter: "M" });
    expect(channelInfo("instagram")).toMatchObject({ name: "Instagram", color: "#E1306C", letter: "I" });
  });
  it("un canal desconocido no rompe", () => {
    expect(channelInfo("otro").letter).toBe("?");
    expect(channelInfo(undefined).letter).toBe("?");
  });
});

describe("paleta de cuentas", () => {
  it("tiene 10 colores en el orden de 11 §5", () => {
    expect(ACCOUNT_PALETTE).toHaveLength(10);
    expect(ACCOUNT_PALETTE[0]).toBe("#25D366");
  });
  it("el siguiente color libre", () => {
    expect(nextAccountColor([])).toBe("#25D366");
    expect(nextAccountColor(["#25d366", "#229ED9"])).toBe("#8B5CF6");
    expect(nextAccountColor([...ACCOUNT_PALETTE])).toBe("#25D366");
  });
});

describe("colorForId", () => {
  it("es estable y sale de la lista", () => {
    expect(colorForId("593991234567@s.whatsapp.net")).toBe(colorForId("593991234567@s.whatsapp.net"));
    expect(colorForId("a")).toMatch(/^#[0-9a-f]{6}$/);
  });
  it("reparte ids distintos en colores distintos", () => {
    const set = new Set(Array.from({ length: 60 }, (_, i) => colorForId(`user:${i}`)));
    expect(set.size).toBeGreaterThan(5);
  });
});

describe("initialsOf", () => {
  it("dos iniciales como máximo", () => {
    expect(initialsOf("Ana López")).toBe("AL");
    expect(initialsOf("ana")).toBe("A");
    expect(initialsOf("Juan Carlos Pérez")).toBe("JC");
    expect(initialsOf("  ")).toBe("?");
    expect(initialsOf("@usuario")).toBe("U");
  });
});
