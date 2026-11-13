import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { PAGE_ORDER, applyTheme, useUI } from "./ui";

beforeEach(() => {
  vi.useFakeTimers();
  useUI.setState({ toasts: [], page: "chats", openChatKey: null, confirmReq: null });
});
afterEach(() => {
  vi.useRealTimers();
});

describe("páginas", () => {
  it("las 9 páginas en el orden de Ctrl+1…9", () => {
    expect(PAGE_ORDER).toEqual([
      "chats",
      "statuses",
      "campaigns",
      "scheduled",
      "contacts",
      "stats",
      "accounts",
      "destinations",
      "settings",
    ]);
  });
  it("openChat cambia a chats y fija la clave", () => {
    useUI.getState().setPage("accounts");
    useUI.getState().openChat("wa-1|abc");
    expect(useUI.getState().page).toBe("chats");
    expect(useUI.getState().openChatKey).toBe("wa-1|abc");
    useUI.getState().clearOpenChat();
    expect(useUI.getState().openChatKey).toBeNull();
  });
});

describe("toasts", () => {
  it("se añaden, se cierran y caducan solos", () => {
    useUI.getState().toast("info", "Hola");
    expect(useUI.getState().toasts).toHaveLength(1);
    vi.advanceTimersByTime(5100);
    expect(useUI.getState().toasts).toHaveLength(0);
  });
  it("los errores duran más", () => {
    useUI.getState().toast("error", "Falló");
    vi.advanceTimersByTime(5100);
    expect(useUI.getState().toasts).toHaveLength(1);
    vi.advanceTimersByTime(3000);
    expect(useUI.getState().toasts).toHaveLength(0);
  });
  it("un aviso repetido no se apila", () => {
    useUI.getState().toast("error", "Igual");
    useUI.getState().toast("error", "Igual");
    expect(useUI.getState().toasts).toHaveLength(1);
  });
  it("máximo 4 a la vez y los vacíos se ignoran", () => {
    for (let i = 0; i < 6; i++) useUI.getState().toast("info", `m${i}`);
    useUI.getState().toast("info", "   ");
    const t = useUI.getState().toasts;
    expect(t).toHaveLength(4);
    expect(t[0].message).toBe("m2");
  });
  it("dismissToast", () => {
    useUI.getState().toast("success", "Listo");
    useUI.getState().dismissToast(useUI.getState().toasts[0].id);
    expect(useUI.getState().toasts).toHaveLength(0);
  });
});

describe("tema", () => {
  it("setTheme pone y quita la clase dark de <html>", () => {
    useUI.getState().setTheme("dark");
    expect(document.documentElement.classList.contains("dark")).toBe(true);
    expect(useUI.getState().theme).toBe("dark");
    useUI.getState().setTheme("light");
    expect(document.documentElement.classList.contains("dark")).toBe(false);
  });
  it("«system» sigue al sistema", () => {
    const listeners: Array<() => void> = [];
    const mq = {
      matches: true,
      addEventListener: (_: string, l: () => void) => listeners.push(l),
      removeEventListener: () => undefined,
    };
    vi.stubGlobal("matchMedia", () => mq);
    applyTheme("system");
    expect(document.documentElement.classList.contains("dark")).toBe(true);
    mq.matches = false;
    listeners.forEach((l) => l());
    expect(document.documentElement.classList.contains("dark")).toBe(false);
    vi.unstubAllGlobals();
  });
});

describe("confirmar", () => {
  it("resuelve con la decisión", async () => {
    const p = useUI.getState().confirm({ title: "¿Seguro?" });
    expect(useUI.getState().confirmReq?.options.title).toBe("¿Seguro?");
    useUI.getState().resolveConfirm(true);
    await expect(p).resolves.toBe(true);
    expect(useUI.getState().confirmReq).toBeNull();
  });
  it("una nueva petición cancela la anterior", async () => {
    const first = useUI.getState().confirm({ title: "A" });
    const second = useUI.getState().confirm({ title: "B" });
    await expect(first).resolves.toBe(false);
    useUI.getState().resolveConfirm(false);
    await expect(second).resolves.toBe(false);
  });
});
