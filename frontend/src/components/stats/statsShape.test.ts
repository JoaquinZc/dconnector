import { describe, expect, it } from "vitest";
import {
  barShare,
  changeText,
  changeTone,
  formatBucket,
  formatFraction,
  formatSeconds,
  heatLevel,
  heatmapPeak,
  heatmapTotal,
  normalizeHeatmap,
  numOrNull,
  pctChange,
  pctChangeNullable,
  readConversationOverview,
  readGroupsOverview,
  toNum,
} from "./statsShape";

const summary = (p: Record<string, unknown> = {}) => ({
  sent: 10,
  received: 20,
  chats: 3,
  startedByMe: 2,
  startedByThem: 4,
  response: { mineMedianSec: 600, mineCount: 5, mineUnderHour: 0.8, theirsMedianSec: 1800, theirsCount: 4 },
  mix: { text: 25, image: 3, video: 1, voice: 1, document: 0, sticker: 0, other: 0 },
  ...p,
});

describe("valores por defecto", () => {
  it("`null`, `undefined` y un struct vacío no lanzan y dan ceros y listas vacías", () => {
    for (const raw of [null, undefined, {}, "x", 7, []]) {
      const v = readConversationOverview(raw);
      expect(v.current.sent).toBe(0);
      expect(v.current.response.mineMedianSec).toBeNull();
      expect(v.current.response.mineUnderHour).toBeNull();
      expect(v.perDay).toEqual([]);
      expect(v.topChats).toEqual([]);
      expect(v.frequent).toEqual([]);
      expect(v.heatmap).toHaveLength(7);
      expect(v.heatmap.every((r) => r.length === 24 && r.every((x) => x === 0))).toBe(true);
      expect(v.hasData).toBe(false);
      expect(v.period.granularity).toBe("day");
      expect(v.changes.sent).toBe(0); // 0 contra 0
    }
  });

  it("los grupos vacíos también", () => {
    const g = readGroupsOverview(undefined);
    expect(g).toMatchObject({ groupSent: 0, groupReceived: 0, groups: [], people: [], hasData: false });
  });

  it("toNum y numOrNull toleran basura", () => {
    expect(toNum("12")).toBe(12);
    expect(toNum("abc", 5)).toBe(5);
    expect(toNum(Number.NaN)).toBe(0);
    expect(numOrNull(null)).toBeNull();
    expect(numOrNull("")).toBeNull();
    expect(numOrNull("3")).toBe(3);
    expect(numOrNull({})).toBeNull();
  });
});

describe("forma real de C2", () => {
  const raw = {
    period: { fromMs: 1, toMs: 2, prevFromMs: 0, prevToMs: 1, days: 30, granularity: "week" },
    current: summary(),
    previous: summary({ sent: 5, received: 20, chats: 0 }),
    changes: { sent: 1, received: 0, chats: null, startedByMe: 0, startedByThem: 0, mineMedianSec: -0.25, theirsMedianSec: null },
    groupSent: 4,
    groupReceived: 6,
    perDay: [{ day: "2026-10-05", sent: 3, received: 4 }],
    heatmap: [[1, 2]],
    topChats: [{ chatKey: "wa|1", accountId: "wa", channel: "whatsapp", nativeId: "1", kind: "direct", title: "Ana", avatarUrl: "", total: 9, sent: 4, received: 5, activeDays: 3, lastAt: 99 }],
    frequent: [],
  };

  it("lee periodo, resúmenes, rankings y días", () => {
    const v = readConversationOverview(raw);
    expect(v.period.granularity).toBe("week");
    expect(v.period.days).toBe(30);
    expect(v.current.response.mineMedianSec).toBe(600);
    expect(v.current.response.mineUnderHour).toBe(0.8);
    expect(v.current.mix.text).toBe(25);
    expect(v.perDay).toEqual([{ day: "2026-10-05", sent: 3, received: 4 }]);
    expect(v.topChats[0]).toMatchObject({ chatKey: "wa|1", title: "Ana", total: 9, activeDays: 3 });
    expect(v.groupSent + v.groupReceived).toBe(10);
    expect(v.hasData).toBe(true);
  });

  it("la comparación con el periodo anterior usa `changes` del servicio, con `null` = sin base", () => {
    const v = readConversationOverview(raw);
    expect(v.changes.sent).toBe(1);
    expect(v.changes.chats).toBeNull(); // el servicio dijo null aunque aquí se calcularía otra cosa
    expect(v.changes.mineMedianSec).toBe(-0.25);
    expect(v.changes.theirsMedianSec).toBeNull();
  });

  it("si el servicio no trae `changes`, se calcula con los dos resúmenes", () => {
    const v = readConversationOverview({ ...raw, changes: undefined });
    expect(v.changes.sent).toBe(1); // 10 contra 5
    expect(v.changes.received).toBe(0);
    expect(v.changes.chats).toBeNull(); // antes 0, ahora 3
    expect(v.changes.mineMedianSec).toBe(0); // 600 contra 600
  });

  it("«respondes en menos de 1 h» siempre se calcula aquí", () => {
    const v = readConversationOverview({
      ...raw,
      previous: summary({ response: { mineMedianSec: 600, mineCount: 5, mineUnderHour: 0.4, theirsMedianSec: null, theirsCount: 0 } }),
    });
    expect(v.changes.mineUnderHour).toBeCloseTo(1, 5); // 0,4 → 0,8 = +100 %
  });

  it("una granularidad desconocida cae a «day»", () => {
    expect(readConversationOverview({ period: { granularity: "year" } }).period.granularity).toBe("day");
  });

  it("grupos: filas, personas y totales", () => {
    const g = readGroupsOverview({
      period: { days: 7, granularity: "day" },
      groupSent: 2,
      groupReceived: 8,
      groups: [{ chatKey: "wa|g1", accountId: "wa", channel: "whatsapp", nativeId: "g1", title: "Vecinos", avatarUrl: "", total: 10, mine: 2, authors: 4, activeDays: 3, lastAt: 5 }],
      people: [{ accountId: "wa", channel: "whatsapp", senderId: "p1", name: "Luis", avatarUrl: "", total: 6, groups: 2, lastAt: 7 }],
    });
    expect(g.groups[0]).toMatchObject({ title: "Vecinos", total: 10, mine: 2, authors: 4 });
    expect(g.people[0]).toMatchObject({ name: "Luis", total: 6, groups: 2 });
    expect(g.hasData).toBe(true);
  });
});

describe("pctChange y presentación", () => {
  it("pctChange: fracción, 0 contra 0 = 0, sin base = null", () => {
    expect(pctChange(15, 10)).toBeCloseTo(0.5);
    expect(pctChange(5, 10)).toBeCloseTo(-0.5);
    expect(pctChange(0, 0)).toBe(0);
    expect(pctChange(3, 0)).toBeNull();
    expect(pctChange(0, 4)).toBe(-1);
    expect(pctChange(Number.NaN, 1)).toBeNull();
  });

  it("pctChangeNullable: sin alguno de los dos, null", () => {
    expect(pctChangeNullable(null, 5)).toBeNull();
    expect(pctChangeNullable(5, null)).toBeNull();
    expect(pctChangeNullable(15, 10)).toBeCloseTo(0.5);
  });

  it("changeText: signo, redondeo y vacío sin base", () => {
    expect(changeText(0.254)).toBe("+25 %");
    expect(changeText(-0.1)).toBe("−10 %");
    expect(changeText(0)).toBe("0 %");
    expect(changeText(0.001)).toBe("0 %");
    expect(changeText(null)).toBe("");
    expect(changeText(undefined)).toBe("");
  });

  it("changeTone: subir es bueno salvo en los tiempos, donde bajar es bueno", () => {
    expect(changeTone(0.3)).toBe("good");
    expect(changeTone(-0.3)).toBe("bad");
    expect(changeTone(-0.3, true)).toBe("good");
    expect(changeTone(0.3, true)).toBe("bad");
    expect(changeTone(0)).toBe("flat");
    expect(changeTone(null)).toBe("flat");
  });

  it("formatSeconds, formatFraction y formatBucket", () => {
    expect(formatSeconds(null)).toBe("—");
    expect(formatSeconds(45)).toBe("45 s");
    expect(formatSeconds(720)).toBe("12 min");
    expect(formatSeconds(3 * 3600 + 20 * 60)).toBe("3 h 20 min");
    expect(formatSeconds(5 * 3600)).toBe("5 h");
    expect(formatSeconds(3 * 86400)).toBe("3 d");
    expect(formatFraction(0.456)).toBe("46 %");
    expect(formatFraction(null)).toBe("—");
    expect(formatBucket("2026-10-05")).toBe("5 oct");
    expect(formatBucket("2026-10")).toBe("oct 2026");
    expect(formatBucket("otra cosa")).toBe("otra cosa");
  });

  it("barShare", () => {
    expect(barShare(5, 10)).toBe(0.5);
    expect(barShare(0, 10)).toBe(0);
    expect(barShare(5, 0)).toBe(0);
    expect(barShare(20, 10)).toBe(1);
  });
});

describe("mapa de calor 7 × 24", () => {
  it("siempre 7 filas de 24 columnas, aunque llegue incompleto o con basura", () => {
    const g = normalizeHeatmap([[1, 2, "3", -4, null, Number.NaN], "x", null, [0, 0, 7]]);
    expect(g).toHaveLength(7);
    expect(g.every((r) => r.length === 24)).toBe(true);
    expect(g[0].slice(0, 6)).toEqual([1, 2, 3, 0, 0, 0]);
    expect(g[1].every((v) => v === 0)).toBe(true);
    expect(g[3][2]).toBe(7);
  });

  it("filas y columnas de más se ignoran", () => {
    const big = Array.from({ length: 9 }, () => Array.from({ length: 30 }, () => 1));
    const g = normalizeHeatmap(big);
    expect(g).toHaveLength(7);
    expect(g[0]).toHaveLength(24);
    expect(heatmapTotal(g)).toBe(7 * 24);
  });

  it("los días van de lunes (0) a domingo (6) y la hora de 0 a 23", () => {
    const raw = Array.from({ length: 7 }, () => Array.from({ length: 24 }, () => 0));
    raw[6][23] = 4; // domingo 23:00
    raw[0][0] = 4; // lunes 00:00
    const peak = heatmapPeak(normalizeHeatmap(raw));
    expect(peak).toEqual({ day: 0, hour: 0, value: 4 }); // empate: la primera
    raw[2][9] = 9;
    expect(heatmapPeak(normalizeHeatmap(raw))).toEqual({ day: 2, hour: 9, value: 9 });
  });

  it("sin mensajes no hay pico y la intensidad es 0", () => {
    expect(heatmapPeak(normalizeHeatmap(null))).toBeNull();
    expect(heatLevel(0, 10)).toBe(0);
    expect(heatLevel(5, 0)).toBe(0);
    expect(heatLevel(10, 10)).toBe(1);
    expect(heatLevel(1, 100)).toBeGreaterThan(0.12);
  });

  it("el mapa que llega en la respuesta se normaliza", () => {
    const v = readConversationOverview({ heatmap: [[0, 3]] });
    expect(v.heatmap[0][1]).toBe(3);
    expect(v.heatmap).toHaveLength(7);
  });
});
