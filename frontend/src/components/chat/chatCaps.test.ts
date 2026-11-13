import { describe, expect, it } from "vitest";
import {
  attachOptions,
  canDeleteMessageForAll,
  canEditMessage,
  canSendAnything,
  hasAnyOption,
  messageMenuOptions,
  withinWindow,
} from "./chatCaps";
import { FULL_CAPS, msg, NOW } from "./testkit";

describe("ventanas de tiempo", () => {
  it("0 o ausente significa sin límite", () => {
    expect(withinWindow(0, 0, NOW)).toBe(true);
    expect(withinWindow(0, undefined, NOW)).toBe(true);
  });
  it("respeta el límite", () => {
    expect(withinWindow(NOW - 10_000, 60_000, NOW)).toBe(true);
    expect(withinWindow(NOW - 61_000, 60_000, NOW)).toBe(false);
  });
});

describe("messageMenuOptions", () => {
  it("mensaje propio reciente: todo según las capacidades", () => {
    const m = msg({ fromMe: true, timestamp: NOW - 60_000 });
    const o = messageMenuOptions(m, FULL_CAPS, NOW);
    expect(o).toEqual({
      reply: true, react: true, forward: true, copy: true,
      edit: true, deleteForMe: true, deleteForAll: true, info: true,
    });
  });

  it("editar caduca con la ventana, borrar para todos dura más", () => {
    const m = msg({ fromMe: true, timestamp: NOW - 20 * 60_000 });
    const o = messageMenuOptions(m, FULL_CAPS, NOW);
    expect(o.edit).toBe(false);
    expect(o.deleteForAll).toBe(true);
    const old = msg({ fromMe: true, timestamp: NOW - 3 * 24 * 3_600_000 });
    expect(messageMenuOptions(old, FULL_CAPS, NOW).deleteForAll).toBe(false);
  });

  it("un mensaje ajeno no se edita ni se borra para todos", () => {
    const m = msg({ fromMe: false });
    const o = messageMenuOptions(m, FULL_CAPS, NOW);
    expect(o.edit).toBe(false);
    expect(o.deleteForAll).toBe(false);
    expect(o.deleteForMe).toBe(true);
  });

  it("sin capacidades solo queda copiar, info y eliminar para mí", () => {
    const m = msg({ fromMe: true, timestamp: NOW });
    const o = messageMenuOptions(m, {}, NOW);
    expect(o).toMatchObject({ reply: false, react: false, forward: false, edit: false, deleteForAll: false });
    expect(o.copy && o.info && o.deleteForMe).toBe(true);
    expect(messageMenuOptions(m, null, NOW).reply).toBe(false);
  });

  it("solo los textos se editan", () => {
    const m = msg({ fromMe: true, timestamp: NOW, kind: "image" });
    expect(canEditMessage(m, FULL_CAPS, NOW)).toBe(false);
    expect(canDeleteMessageForAll(m, FULL_CAPS, NOW)).toBe(true);
  });

  it("pendiente o fallido: sin acciones remotas", () => {
    for (const delivery of ["pending", "failed"]) {
      const m = msg({ fromMe: true, timestamp: NOW, delivery });
      const o = messageMenuOptions(m, FULL_CAPS, NOW);
      expect(o).toMatchObject({ reply: false, react: false, forward: false, edit: false, deleteForAll: false });
    }
  });

  it("borrado: solo eliminar para mí y nada de copiar", () => {
    const m = msg({ fromMe: true, timestamp: NOW, deletedAt: NOW });
    const o = messageMenuOptions(m, FULL_CAPS, NOW);
    expect(o.copy).toBe(false);
    expect(o.reply || o.react || o.forward || o.edit || o.deleteForAll).toBe(false);
    expect(o.deleteForMe).toBe(true);
  });

  it("sistema y no compatible: casi nada; las encuestas no se reenvían; sin texto no se copia", () => {
    expect(hasAnyOption(messageMenuOptions(msg({ kind: "system" }), FULL_CAPS, NOW))).toBe(false);
    expect(messageMenuOptions(msg({ kind: "unsupported" }), FULL_CAPS, NOW).reply).toBe(false);
    expect(messageMenuOptions(msg({ kind: "poll" }), FULL_CAPS, NOW).forward).toBe(false);
    expect(messageMenuOptions(msg({ kind: "image", text: "" }), FULL_CAPS, NOW).copy).toBe(false);
  });
});

describe("adjuntos y envío", () => {
  it("attachOptions sale de las capacidades", () => {
    expect(attachOptions({ sendImage: true, sendPoll: true })).toEqual({
      media: true, document: false, sticker: false, location: false, contact: false, poll: true,
    });
    expect(attachOptions(null).media).toBe(false);
  });
  it("canSendAnything", () => {
    expect(canSendAnything(null)).toBe(true);
    expect(canSendAnything({})).toBe(false);
    expect(canSendAnything({ sendVoice: true })).toBe(true);
  });
});
