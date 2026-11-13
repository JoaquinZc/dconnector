import { describe, expect, it } from "vitest";
import { buildTimeline, indexOfNative, searchIndexes, upsertMessage } from "./timeline";
import { at, msg, NOW } from "./testkit";

describe("buildTimeline", () => {
  it("separa por día y rotula Hoy / Ayer", () => {
    const items = buildTimeline(
      [
        msg({ key: "a", timestamp: at(9, 10) }),
        msg({ key: "b", timestamp: at(10, 9) }),
        msg({ key: "c", timestamp: at(10, 20) }),
        msg({ key: "d", timestamp: at(11, 8) }),
      ],
      false,
      NOW,
    );
    expect(items.map((i) => i.type)).toEqual(["day", "msg", "day", "msg", "msg", "day", "msg"]);
    const labels = items.filter((i) => i.type === "day").map((i) => (i as { label: string }).label);
    expect(labels).toEqual(["Lunes", "Ayer", "Hoy"]);
  });

  it("ordena y quita duplicados por clave", () => {
    const items = buildTimeline(
      [msg({ key: "b", timestamp: at(11, 11) }), msg({ key: "a", timestamp: at(11, 10) }), msg({ key: "a", timestamp: at(11, 10) })],
      false,
      NOW,
    );
    const keys = items.filter((i) => i.type === "msg").map((i) => i.key);
    expect(keys).toEqual(["a", "b"]);
  });

  it("en grupos muestra el emisor al empezar cada racha, no en los mensajes propios", () => {
    const items = buildTimeline(
      [
        msg({ key: "1", senderNativeId: "x", timestamp: at(11, 10, 0) }),
        msg({ key: "2", senderNativeId: "x", timestamp: at(11, 10, 1) }),
        msg({ key: "3", senderNativeId: "y", timestamp: at(11, 10, 2) }),
        msg({ key: "4", senderNativeId: "me", fromMe: true, timestamp: at(11, 10, 3) }),
        msg({ key: "5", senderNativeId: "x", timestamp: at(11, 10, 4) }),
      ],
      true,
      NOW,
    );
    const rows = items.filter((i) => i.type === "msg") as Extract<(typeof items)[number], { type: "msg" }>[];
    expect(rows.map((r) => r.showSender)).toEqual([true, false, true, false, true]);
    expect(rows.map((r) => r.first)).toEqual([true, false, true, true, true]);
  });

  it("en chats directos no muestra emisor", () => {
    const items = buildTimeline([msg({ key: "1" })], false, NOW);
    expect(items.some((i) => i.type === "msg" && i.showSender)).toBe(false);
  });

  it("un cambio de día reinicia la racha", () => {
    const items = buildTimeline(
      [msg({ key: "1", senderNativeId: "x", timestamp: at(10, 23, 59) }), msg({ key: "2", senderNativeId: "x", timestamp: at(11, 0, 1) })],
      true,
      NOW,
    );
    const rows = items.filter((i) => i.type === "msg") as Extract<(typeof items)[number], { type: "msg" }>[];
    expect(rows.map((r) => r.showSender)).toEqual([true, true]);
  });

  it("los mensajes de sistema no llevan emisor y cortan la racha", () => {
    const items = buildTimeline(
      [
        msg({ key: "1", senderNativeId: "x", timestamp: at(11, 10, 0) }),
        msg({ key: "2", kind: "system", timestamp: at(11, 10, 1) }),
        msg({ key: "3", senderNativeId: "x", timestamp: at(11, 10, 2) }),
      ],
      true,
      NOW,
    );
    const rows = items.filter((i) => i.type === "msg") as Extract<(typeof items)[number], { type: "msg" }>[];
    expect(rows.map((r) => r.showSender)).toEqual([true, false, true]);
  });
});

describe("utilidades de lista", () => {
  it("upsertMessage reemplaza o inserta ordenado", () => {
    const a = msg({ key: "a", timestamp: 1 });
    const b = msg({ key: "b", timestamp: 3 });
    const c = msg({ key: "c", timestamp: 2 });
    expect(upsertMessage([a, b], c).map((m) => m.key)).toEqual(["a", "c", "b"]);
    const a2 = msg({ key: "a", timestamp: 1, text: "nuevo" });
    expect(upsertMessage([a, b], a2)[0].text).toBe("nuevo");
  });

  it("indexOfNative y searchIndexes (sin tildes ni mayúsculas)", () => {
    const items = buildTimeline(
      [msg({ key: "a", nativeId: "n1", text: "Dirección de entrega", timestamp: at(11, 9) }), msg({ key: "b", nativeId: "n2", text: "gracias", timestamp: at(11, 10) })],
      false,
      NOW,
    );
    const i = indexOfNative(items, "n2");
    expect(items[i].key).toBe("b");
    expect(indexOfNative(items, "zz")).toBe(-1);
    const hits = searchIndexes(items, "DIRECCION");
    expect(hits.map((h) => items[h].key)).toEqual(["a"]);
    expect(searchIndexes(items, "")).toEqual([]);
  });
});
