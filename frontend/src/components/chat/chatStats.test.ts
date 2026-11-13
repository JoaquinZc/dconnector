import { describe, expect, it } from "vitest";
import { extractChatStats } from "./chatStats";

describe("extractChatStats", () => {
  it("lee el resumen y normaliza segundos a milisegundos", () => {
    const s = extractChatStats({ summary: { sent: 4, received: 6 }, first: 1_700_000_000, last: 1_700_000_000_000, activeDays: 3 });
    expect(s).toEqual({ sent: 4, received: 6, total: 10, first: 1_700_000_000_000, last: 1_700_000_000_000, activeDays: 3 });
  });
  it("devuelve null con datos vacíos o desconocidos (el tipo de Go aún está vacío)", () => {
    expect(extractChatStats({})).toBeNull();
    expect(extractChatStats(null)).toBeNull();
    expect(extractChatStats("x")).toBeNull();
  });
});
