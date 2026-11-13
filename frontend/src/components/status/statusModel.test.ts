import { describe, expect, it } from "vitest";
import type { Status, StatusAuthor } from "@/api/types";
import {
  STATUS_COLORS,
  STATUS_FONTS,
  STORY_IMAGE_MS,
  STORY_VIDEO_FALLBACK_MS,
  advanceProgress,
  authorKey,
  buildSections,
  countText,
  flattenSections,
  newText,
  stableOrder,
  type AccountMeta,
  barFill,
  bgOf,
  fontCss,
  formatAgo,
  formatExpiry,
  isLive,
  liveSorted,
  nextCursor,
  prevCursor,
  ringSegments,
  splitAuthors,
  startItem,
  storyDurationMs,
} from "./statusModel";

const st = (p: Partial<Status>): Status =>
  ({ key: "a|1", kind: "text", media: undefined, viewed: false, fromMe: false, deletedAt: 0, expiresAt: 0, postedAt: 1, ...p }) as unknown as Status;

describe("constantes", () => {
  it("12 colores de fondo válidos y 4 letras", () => {
    expect(STATUS_COLORS).toHaveLength(12);
    expect(new Set(STATUS_COLORS).size).toBe(12);
    for (const c of STATUS_COLORS) expect(c).toMatch(/^#[0-9A-F]{6}$/i);
    expect(STATUS_FONTS).toHaveLength(4);
  });

  it("bgOf y fontCss caen en un valor por defecto", () => {
    expect(bgOf("#ff0000")).toBe("#ff0000");
    expect(bgOf("rojo")).toBe(STATUS_COLORS[0]);
    expect(bgOf("")).toBe(STATUS_COLORS[0]);
    expect(fontCss(2)).toContain("Consolas");
    expect(fontCss(99)).toBe(STATUS_FONTS[0].css);
  });
});

describe("duración en el visor", () => {
  it("5 s para foto y texto", () => {
    expect(storyDurationMs(st({ kind: "text" as never }))).toBe(STORY_IMAGE_MS);
    expect(storyDurationMs(st({ kind: "image" as never }))).toBe(5000);
  });

  it("el vídeo dura lo que dura", () => {
    const v = st({ kind: "video" as never, media: { duration: 12.5 } as never });
    expect(storyDurationMs(v)).toBe(12500);
    expect(storyDurationMs(v, 8)).toBe(8000); // la del elemento <video> manda
  });

  it("vídeo sin duración conocida usa un valor por defecto", () => {
    expect(storyDurationMs(st({ kind: "video" as never }))).toBe(STORY_VIDEO_FALLBACK_MS);
    expect(storyDurationMs(st({ kind: "video" as never, media: { duration: 0 } as never }))).toBe(STORY_VIDEO_FALLBACK_MS);
  });
});

describe("avance y pausa", () => {
  it("avanza con el tiempo y termina al llegar a 1", () => {
    expect(advanceProgress(0, 1000, 5000, false)).toEqual({ value: 0.2, done: false });
    const r = advanceProgress(0.9, 600, 5000, false);
    expect(r.value).toBe(1);
    expect(r.done).toBe(true);
  });

  it("en pausa no avanza", () => {
    expect(advanceProgress(0.4, 3000, 5000, true)).toEqual({ value: 0.4, done: false });
  });

  it("ignora tiempos o duraciones inválidos", () => {
    expect(advanceProgress(0.3, -5, 5000, false).value).toBe(0.3);
    expect(advanceProgress(0.3, 100, 0, false).value).toBe(0.3);
    expect(advanceProgress(-1, 0, 5000, false).value).toBe(0);
  });

  it("barFill: llenas las anteriores, vacías las siguientes", () => {
    expect(barFill(0, 2, 0.5)).toBe(1);
    expect(barFill(2, 2, 0.5)).toBe(0.5);
    expect(barFill(3, 2, 0.5)).toBe(0);
    expect(barFill(2, 2, 7)).toBe(1);
  });
});

describe("posición del visor", () => {
  const sizes = [2, 3, undefined];

  it("avanza dentro del autor, luego al siguiente, y termina", () => {
    expect(nextCursor({ group: 0, item: 0 }, sizes)).toEqual({ group: 0, item: 1 });
    expect(nextCursor({ group: 0, item: 1 }, sizes)).toEqual({ group: 1, item: 0 });
    expect(nextCursor({ group: 1, item: 2 }, sizes)).toEqual({ group: 2, item: 0 });
    expect(nextCursor({ group: 2, item: 4 }, sizes)).toBeNull();
  });

  it("un autor sin estados se salta", () => {
    expect(nextCursor({ group: 0, item: 0 }, [0, 2])).toEqual({ group: 1, item: 0 });
  });

  it("retrocede y en el primero se queda", () => {
    expect(prevCursor({ group: 1, item: 2 })).toEqual({ group: 1, item: 1 });
    expect(prevCursor({ group: 1, item: 0 })).toEqual({ group: 0, item: 0 });
    expect(prevCursor({ group: 0, item: 0 })).toEqual({ group: 0, item: 0 });
  });

  it("empieza por el primer estado no visto", () => {
    expect(startItem([st({ viewed: true }), st({ viewed: true }), st({ viewed: false })])).toBe(2);
    expect(startItem([st({ viewed: true }), st({ viewed: true })])).toBe(0);
    expect(startItem([])).toBe(0);
  });
});

describe("anillos segmentados", () => {
  it("sin estados no hay anillo", () => {
    expect(ringSegments(0, 0)).toEqual([]);
  });

  it("un estado es la vuelta entera", () => {
    expect(ringSegments(1, 1)).toEqual([{ start: 0, end: 1, viewed: false }]);
    expect(ringSegments(1, 0)[0].viewed).toBe(true);
  });

  it("los vistos van primero y los no vistos después", () => {
    const s = ringSegments(4, 3);
    expect(s.map((x) => x.viewed)).toEqual([true, false, false, false]);
  });

  it("los segmentos no se solapan y quedan dentro de la vuelta", () => {
    const s = ringSegments(5, 2);
    expect(s).toHaveLength(5);
    s.forEach((x, i) => {
      expect(x.end).toBeGreaterThan(x.start);
      expect(x.start).toBeGreaterThanOrEqual(0);
      expect(x.end).toBeLessThanOrEqual(1);
      if (i > 0) expect(x.start).toBeGreaterThan(s[i - 1].end);
    });
  });

  it("limita a 30 y acota los no vistos", () => {
    expect(ringSegments(80, 0)).toHaveLength(30);
    expect(ringSegments(3, 10).every((x) => !x.viewed)).toBe(true);
  });
});

describe("tiempos y vigencia", () => {
  const now = 1_000_000_000_000;
  it("formatAgo", () => {
    expect(formatAgo(now - 20_000, now)).toBe("ahora");
    expect(formatAgo(now - 5 * 60_000, now)).toBe("hace 5 min");
    expect(formatAgo(now - 3 * 3_600_000, now)).toBe("hace 3 h");
    expect(formatAgo(now - 49 * 3_600_000, now)).toBe("hace 2 d");
  });

  it("formatExpiry", () => {
    expect(formatExpiry(now + 20 * 60_000, now)).toBe("Caduca en 20 min");
    expect(formatExpiry(now + 5 * 3_600_000, now)).toBe("Caduca en 5 h");
    expect(formatExpiry(now - 1, now)).toBe("Caducado");
    expect(formatExpiry(0, now)).toBe("");
  });

  it("isLive y liveSorted descartan borrados y caducados y ordenan por fecha", () => {
    const list = [
      st({ key: "c", postedAt: 30, expiresAt: now + 1 }),
      st({ key: "a", postedAt: 10, expiresAt: now + 1 }),
      st({ key: "x", postedAt: 20, expiresAt: now - 1 }),
      st({ key: "d", postedAt: 40, deletedAt: 5, expiresAt: now + 1 }),
    ];
    expect(isLive(list[2], now)).toBe(false);
    expect(liveSorted(list, now).map((s) => s.key)).toEqual(["a", "c"]);
  });
});

describe("splitAuthors", () => {
  const a = (id: string, unviewed: number, lastAt: number, count = 2): StatusAuthor =>
    ({ accountId: "wa-1", authorId: id, authorName: id, avatarUrl: "", count, unviewed, lastAt }) as StatusAuthor;

  it("separa recientes y vistos, sin mí, del más nuevo al más antiguo", () => {
    const r = splitAuthors([a("v1", 0, 5), a("n1", 1, 1), a("me", 1, 99), a("n2", 2, 7), a("v2", 0, 9), a("vacío", 0, 1, 0)], "me");
    expect(r.recent.map((x) => x.authorId)).toEqual(["n2", "n1"]);
    expect(r.seen.map((x) => x.authorId)).toEqual(["v2", "v1"]);
    expect(r.ordered.map((x) => x.authorId)).toEqual(["n2", "n1", "v2", "v1"]);
  });
});

describe("secciones por canal y cuenta", () => {
  const au = (accountId: string, id: string, unviewed: number, lastAt: number, count = 2): StatusAuthor =>
    ({ accountId, authorId: id, authorName: id, avatarUrl: "", count, unviewed, lastAt }) as StatusAuthor;
  const accs: AccountMeta[] = [
    { id: "tg-1", channel: "telegram", label: "Canal", color: "#229ED9", selfId: "yo-t" },
    { id: "wa-1", channel: "whatsapp", label: "Ventas", color: "#25D366", selfId: "yo-w" },
    { id: "wa-2", channel: "whatsapp", label: "Personal", color: "#8B5CF6" },
  ];
  const all = [
    au("wa-1", "a", 2, 5),
    au("wa-1", "b", 0, 9),
    au("wa-1", "yo-w", 1, 99),
    au("wa-2", "c", 1, 3),
    au("tg-1", "d", 0, 4),
    au("tg-1", "e", 3, 8),
  ];

  it("ordena canales (WhatsApp antes que Telegram), cuentas y, dentro, recientes y vistos", () => {
    const s = buildSections(all, accs);
    expect(s.map((c) => c.channel)).toEqual(["whatsapp", "telegram"]);
    expect(s[0].accounts.map((b) => b.account.id)).toEqual(["wa-1", "wa-2"]);
    expect(s[0].accounts[0].recent.map((x) => x.authorId)).toEqual(["a"]);
    expect(s[0].accounts[0].seen.map((x) => x.authorId)).toEqual(["b"]); // sin «yo-w»
    expect(s[0].newCount).toBe(3); // 2 de «a» y 1 de «c»
    expect(s[1].newCount).toBe(3);
    expect(flattenSections(s).map(authorKey)).toEqual(["wa-1|a", "wa-1|b", "wa-2|c", "tg-1|e", "tg-1|d"]);
  });

  it("filtra por canal y oculta vistos salvo el elegido", () => {
    expect(buildSections(all, accs, { channel: "telegram" }).map((c) => c.channel)).toEqual(["telegram"]);
    const hidden = buildSections(all, accs, { hideSeen: true });
    expect(flattenSections(hidden).map((x) => x.authorId)).toEqual(["a", "c", "e"]);
    const kept = buildSections(all, accs, { hideSeen: true, keepKey: "tg-1|d" });
    expect(flattenSections(kept).map((x) => x.authorId)).toEqual(["a", "c", "e", "d"]);
    expect(buildSections(all, accs, { hideSeen: true }).find((c) => c.channel === "telegram")?.newCount).toBe(3);
  });

  it("un canal sin nadie que mostrar no sale", () => {
    expect(buildSections([au("wa-1", "b", 0, 9)], accs, { hideSeen: true })).toEqual([]);
  });

  it("stableOrder conserva el orden anterior, añade lo nuevo y mantiene el elegido", () => {
    expect(stableOrder(["a", "b", "c"], ["c", "x", "a"], null)).toEqual(["a", "c", "x"]);
    expect(stableOrder(["a", "b", "c"], ["c", "a"], "b")).toEqual(["a", "b", "c"]);
    expect(stableOrder([], ["z", "y"])).toEqual(["z", "y"]);
  });

  it("textos de cantidad", () => {
    expect(countText(1)).toBe("1 estado");
    expect(countText(3)).toBe("3 estados");
    expect(newText(1)).toBe("1 nuevo");
    expect(newText(2)).toBe("2 nuevos");
  });
});
