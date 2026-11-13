import { beforeEach, describe, expect, it } from "vitest";
import {
  channelsPresent,
  EMPTY_SELECTION,
  FILTER_KEY,
  isEmptySelection,
  loadSelection,
  pruneSelection,
  resolveAccountIds,
  saveSelection,
  selectionFromIds,
  toggleAccountChecked,
  toggleSelection,
  visibleAccountIds,
  type AccountLite,
} from "./accountSelection";

const accounts: AccountLite[] = [
  { id: "wa-1", label: "Ventas", color: "#25D366", channel: "whatsapp" },
  { id: "wa-2", label: "Personal", color: "#8B5CF6", channel: "whatsapp" },
  { id: "tg-1", label: "Soporte", color: "#229ED9", channel: "telegram" },
];

describe("selección de cuentas", () => {
  beforeEach(() => localStorage.clear());

  it("canales presentes, sin repetir", () => {
    expect(channelsPresent(accounts)).toEqual(["whatsapp", "telegram"]);
  });

  it("«Todas» limpia; los chips alternan", () => {
    let s = toggleSelection(EMPTY_SELECTION, { kind: "account", id: "wa-1" });
    s = toggleSelection(s, { kind: "channel", id: "telegram" });
    expect(s).toEqual({ channels: ["telegram"], accounts: ["wa-1"] });
    s = toggleSelection(s, { kind: "account", id: "wa-1" });
    expect(s.accounts).toEqual([]);
    expect(isEmptySelection(toggleSelection(s, { kind: "all" }))).toBe(true);
  });

  it("resolveAccountIds: unión de cuentas y canales, en orden; vacío = todas", () => {
    expect(resolveAccountIds(EMPTY_SELECTION, accounts)).toEqual([]);
    expect(resolveAccountIds({ channels: ["whatsapp"], accounts: [] }, accounts)).toEqual(["wa-1", "wa-2"]);
    expect(resolveAccountIds({ channels: ["telegram"], accounts: ["wa-2"] }, accounts)).toEqual(["wa-2", "tg-1"]);
  });

  it("pruneSelection quita lo que ya no existe y conserva la identidad si no cambia", () => {
    const ok = { channels: ["whatsapp"], accounts: ["wa-1"] };
    expect(pruneSelection(ok, accounts)).toBe(ok);
    expect(pruneSelection({ channels: ["messenger"], accounts: ["zz", "tg-1"] }, accounts)).toEqual({ channels: [], accounts: ["tg-1"] });
  });

  it("selectionFromIds: canales completos pasan a canal; todas o ninguna = sin filtro", () => {
    expect(selectionFromIds(["wa-1", "wa-2"], accounts)).toEqual({ channels: ["whatsapp"], accounts: [] });
    expect(selectionFromIds(["wa-1", "tg-1"], accounts)).toEqual({ channels: ["telegram"], accounts: ["wa-1"] });
    expect(selectionFromIds(["wa-1", "wa-2", "tg-1"], accounts)).toEqual(EMPTY_SELECTION);
    expect(selectionFromIds([], accounts)).toEqual(EMPTY_SELECTION);
  });

  it("toggleAccountChecked: desde «todas» desmarca una; recompone; no deja la lista vacía", () => {
    let s = toggleAccountChecked(EMPTY_SELECTION, accounts, "wa-2");
    expect(s).toEqual({ channels: ["telegram"], accounts: ["wa-1"] }); // Telegram queda completo
    s = toggleAccountChecked(s, accounts, "tg-1");
    expect(s).toEqual({ channels: [], accounts: ["wa-1"] });
    expect(toggleAccountChecked(s, accounts, "wa-1")).toBe(s); // la última se queda
    s = toggleAccountChecked(s, accounts, "wa-2");
    expect(s).toEqual({ channels: ["whatsapp"], accounts: [] });
    expect(isEmptySelection(toggleAccountChecked(s, accounts, "tg-1"))).toBe(true);
  });

  it("visibleAccountIds: sin filtro son todas", () => {
    expect(visibleAccountIds(EMPTY_SELECTION, accounts)).toEqual(["wa-1", "wa-2", "tg-1"]);
    expect(visibleAccountIds({ channels: ["telegram"], accounts: [] }, accounts)).toEqual(["tg-1"]);
  });

  it("se guarda y se recupera de localStorage; datos rotos se ignoran", () => {
    saveSelection({ channels: ["telegram"], accounts: ["wa-1"] });
    expect(loadSelection()).toEqual({ channels: ["telegram"], accounts: ["wa-1"] });
    localStorage.setItem(FILTER_KEY, "{no es json");
    expect(loadSelection()).toEqual(EMPTY_SELECTION);
  });
});
