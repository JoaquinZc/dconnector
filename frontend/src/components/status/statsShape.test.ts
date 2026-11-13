import { describe, expect, it } from "vitest";
import { EMPTY_STATS, curveLabel, readStatusStats, toNum } from "./statsShape";

/** Lo que devuelve hoy `stats.StatusStats` de B3 (internal/core/stats/status.go). */
const real = {
  period: { fromMs: 1, toMs: 2, prevFromMs: 0, prevToMs: 1, days: 7, granularity: "day" },
  current: { posts: 6, views: 74, avgViews: 12.333, reach: 31, reactions: 5, replies: 2, engagement: 0.09, frequency: 2.4, loyalty: 0.4, engagedPeople: 4 },
  previous: { posts: 4, views: 40, avgViews: 10, reach: 20, reactions: 1, replies: 0 },
  changes: { posts: 0.5, views: 0.85, avgViews: 0.2333, reach: null, reactions: 4, replies: null },
  perDay: [
    { day: "2026-11-01", posts: 2, views: 20, reactions: 1, replies: 0 },
    { day: "2026-11-02", posts: 4, views: 54, reactions: 4, replies: 2 },
  ],
  perStatus: [],
  curve: [
    { hour: 0, views: 0 },
    { hour: 1, views: 30 },
    { hour: 24, views: 74 },
  ],
  loyalty: [],
};

describe("readStatusStats", () => {
  it("el struct vacío de hoy da valores por defecto, sin datos", () => {
    expect(readStatusStats({})).toEqual(EMPTY_STATS);
    expect(readStatusStats(null)).toEqual(EMPTY_STATS);
    expect(readStatusStats(undefined)).toEqual(EMPTY_STATS);
    expect(readStatusStats("raro")).toEqual(EMPTY_STATS);
    expect(readStatusStats([1, 2])).toEqual(EMPTY_STATS);
  });

  it("no devuelve nunca el mismo objeto de referencia", () => {
    expect(readStatusStats({})).not.toBe(EMPTY_STATS);
    expect(readStatusStats(null).changes).not.toBe(EMPTY_STATS.changes);
  });

  it("lee la forma real de B3", () => {
    const s = readStatusStats(real);
    expect(s.published).toBe(6);
    expect(s.totalViews).toBe(74);
    expect(s.avgViews).toBe(12.3);
    expect(s.reach).toBe(31);
    expect(s.reactions).toBe(5);
    expect(s.replies).toBe(2);
    expect(s.curve).toEqual([
      { label: "01/11", views: 20 },
      { label: "02/11", views: 54 },
    ]);
    expect(s.hourly).toEqual([
      { label: "0 h", views: 0 },
      { label: "1 h", views: 30 },
      { label: "24 h", views: 74 },
    ]);
    expect(s.hasData).toBe(true);
  });

  it("los cambios llegan como fracción y se pasan a %, con null donde no hay base", () => {
    const c = readStatusStats(real).changes;
    expect(c.published).toBe(50);
    expect(c.views).toBe(85);
    expect(c.avgViews).toBe(23.3);
    expect(c.reach).toBeNull();
    expect(c.reactions).toBe(400);
  });

  it("también entiende nombres alternativos y sub-objetos", () => {
    const s = readStatusStats({ summary: { published: 4, totalViews: 40, reach: 8 } });
    expect(s.published).toBe(4);
    expect(s.totalViews).toBe(40);
    expect(s.avgViews).toBe(10); // total / publicados
    expect(s.reach).toBe(8);
    expect(readStatusStats({ published: 3, avgViews: 2 }).published).toBe(3);
  });

  it("acepta series de números y de pares", () => {
    expect(readStatusStats({ perDay: [3, 5, 0] }).curve).toEqual([
      { label: "1", views: 3 },
      { label: "2", views: 5 },
      { label: "3", views: 0 },
    ]);
    expect(readStatusStats({ series: [["lun", 2]] }).curve).toEqual([{ label: "lun", views: 2 }]);
  });

  it("valores raros se convierten en 0 sin lanzar", () => {
    const s = readStatusStats({
      current: { posts: "7", avgViews: "x", reactions: NaN },
      perDay: [{ views: "abc" }, { views: null }, null],
      changes: { posts: "no", views: Infinity },
    });
    expect(s.published).toBe(7);
    expect(s.avgViews).toBe(0);
    expect(s.reactions).toBe(0);
    expect(s.curve.map((p) => p.views)).toEqual([0, 0, 0]);
    expect(s.changes.published).toBeNull();
    expect(s.changes.views).toBeNull();
  });

  it("una serie con vistas cuenta como datos aunque no haya cifras", () => {
    expect(readStatusStats({ perDay: [{ day: "2026-11-01", views: 2 }] }).hasData).toBe(true);
    expect(readStatusStats({ perDay: [{ day: "2026-11-01", views: 0 }] }).hasData).toBe(false);
  });
});

describe("ayudas", () => {
  it("toNum", () => {
    expect(toNum("3.5")).toBe(3.5);
    expect(toNum("")).toBe(0);
    expect(toNum(Infinity, 9)).toBe(9);
    expect(toNum({}, 2)).toBe(2);
  });

  it("curveLabel", () => {
    expect(curveLabel("2026-03-09", 0)).toBe("09/03");
    expect(curveLabel("2026-03", 0)).toBe("03/2026");
    expect(curveLabel("lun", 0)).toBe("lun");
    expect(curveLabel(undefined, 4)).toBe("5");
    expect(curveLabel(12, 0)).toBe("12");
  });
});
