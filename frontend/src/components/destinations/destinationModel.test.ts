import { describe, expect, it } from "vitest";
import type { Destination, DestinationState } from "@/api/types";
import {
  ALL_EVENT_NAMES,
  PROTOCOL_EVENTS,
  applyState,
  draftOfDest,
  emptyDestForm,
  formFromDraft,
  hasErrors,
  indexStates,
  pendingLabel,
  stateView,
  toDestination,
  toForm,
  validateDestForm,
  validateDestinationUrl,
} from "./destinationModel";

const ALL_CONTACTS = { mode: "all", includeListIds: [], excludeListIds: [] };

describe("validateDestinationUrl", () => {
  it("exige wss://", () => {
    expect(validateDestinationUrl("wss://bot.ejemplo.com/ws")).toBeNull();
    expect(validateDestinationUrl("WSS://bot.ejemplo.com")).toBeNull();
    expect(validateDestinationUrl("ws://bot.ejemplo.com")).toMatch(/wss/);
    expect(validateDestinationUrl("https://bot.ejemplo.com")).toMatch(/wss/);
    expect(validateDestinationUrl("http://localhost:8787")).toMatch(/wss/);
  });

  it("ws:// solo con localhost y 127.0.0.1", () => {
    expect(validateDestinationUrl("ws://localhost:8787")).toBeNull();
    expect(validateDestinationUrl("ws://127.0.0.1:8787/x")).toBeNull();
    expect(validateDestinationUrl("ws://[::1]:8787/x")).toBeNull();
    expect(validateDestinationUrl("ws://LOCALHOST")).toBeNull();
    expect(validateDestinationUrl("ws://192.168.1.10:8787")).toMatch(/localhost/);
    expect(validateDestinationUrl("ws://localhost.evil.com")).not.toBeNull();
    expect(validateDestinationUrl("ws://127.0.0.2")).not.toBeNull();
  });

  it("vacío o roto", () => {
    expect(validateDestinationUrl("")).toBe("Falta la dirección");
    expect(validateDestinationUrl("   ")).toBe("Falta la dirección");
    expect(validateDestinationUrl("no es una url")).toBe("La dirección no es válida");
    expect(validateDestinationUrl("wss://")).toBe("La dirección no es válida");
    expect(validateDestinationUrl("localhost:8787")).not.toBeNull();
  });

  it("ignora espacios alrededor", () => {
    expect(validateDestinationUrl("  wss://a.com  ")).toBeNull();
  });
});

describe("validateDestForm", () => {
  const ok = { ...emptyDestForm(), name: "Mi bot", url: "wss://bot.com" };

  it("un formulario correcto no tiene errores", () => {
    expect(hasErrors(validateDestForm(ok))).toBe(false);
  });

  it("pide nombre, dirección y al menos un evento", () => {
    const e = validateDestForm({ ...emptyDestForm(), events: [] });
    expect(e.name).toBeTruthy();
    expect(e.url).toBe("Falta la dirección");
    expect(e.events).toBe("Elige al menos un evento");
    expect(hasErrors(e)).toBe(true);
  });

  it("el nombre solo con espacios no vale", () => {
    expect(validateDestForm({ ...ok, name: "  " }).name).toBeTruthy();
  });
});

describe("eventos del protocolo", () => {
  it("son los 11 de 10 §4, en orden", () => {
    expect(ALL_EVENT_NAMES).toEqual([
      "account.state",
      "account.capabilities",
      "chat.updated",
      "message.received",
      "message.sent",
      "message.updated",
      "reaction",
      "receipt",
      "person.updated",
      "status.posted",
      "status.viewed",
    ]);
    expect(PROTOCOL_EVENTS.every((e) => e.label.length > 0)).toBe(true);
  });
});

describe("conversión formulario ↔ destino", () => {
  const dest = (p: Partial<Destination> = {}): Destination => ({
    id: "d1",
    name: "Bot",
    url: "wss://bot.com",
    token: "tok",
    enabled: true,
    allowCommands: true,
    filter: { accountIds: ["wa-1"], channels: ["whatsapp"] as never, events: ["message.received"], includeFromMe: true, includeGroups: false, contactScope: ALL_CONTACTS },
    createdAt: 99,
    ...p,
  });

  it("toForm copia el destino", () => {
    const f = toForm(dest());
    expect(f).toMatchObject({
      name: "Bot",
      token: "tok",
      allowCommands: true,
      accountIds: ["wa-1"],
      channels: ["whatsapp"],
      events: ["message.received"],
      includeFromMe: true,
      includeGroups: false,
    });
  });

  it("sin eventos guardados = todos marcados; sin filtro tampoco falla", () => {
    expect(toForm(dest({ filter: { accountIds: null, channels: null, events: null, includeFromMe: false, includeGroups: false, contactScope: ALL_CONTACTS } })).events).toEqual(
      ALL_EVENT_NAMES,
    );
    expect(toForm({ ...dest(), filter: undefined as never }).accountIds).toEqual([]);
  });

  it("ignora eventos que el protocolo no conoce", () => {
    const f = toForm(dest({ filter: { accountIds: [], channels: [], events: ["message.received", "otro.raro"], includeFromMe: false, includeGroups: false, contactScope: ALL_CONTACTS } }));
    expect(f.events).toEqual(["message.received"]);
  });

  it("toDestination recorta espacios y conserva id y fecha del existente", () => {
    const f = { ...emptyDestForm(), name: "  Bot  ", url: " wss://bot.com ", token: " t " };
    const d = toDestination(f, { id: "d1", createdAt: 99 });
    expect(d).toMatchObject({ id: "d1", name: "Bot", url: "wss://bot.com", token: "t", createdAt: 99 });
    expect(toDestination(f).id).toBe("");
  });

  it("todos los eventos marcados se guardan como «todos» (vacío)", () => {
    expect(toDestination(emptyDestForm()).filter.events).toEqual([]);
    const some = toDestination({ ...emptyDestForm(), events: ["receipt", "message.sent"] });
    expect(some.filter.events).toEqual(["message.sent", "receipt"]); // en el orden del protocolo
  });

  it("ida y vuelta conserva los valores", () => {
    const f = toForm(dest());
    const back = toForm(toDestination(f, dest()));
    expect(back).toEqual(f);
  });

  it("el token vacío al crear se manda vacío (el servicio lo genera)", () => {
    expect(toDestination({ ...emptyDestForm(), name: "a", url: "wss://a.com" }).token).toBe("");
  });
});

describe("borrador", () => {
  it("no guarda el token", () => {
    const d = draftOfDest({ ...emptyDestForm(), token: "secreto", name: "x" });
    expect("token" in d).toBe(false);
    expect(JSON.stringify(d)).not.toContain("secreto");
  });

  it("restaura con validación", () => {
    const f = formFromDraft({ name: "x", url: "wss://a", events: ["receipt", "falso"], channels: ["telegram", 3], allowCommands: true } as never);
    expect(f.name).toBe("x");
    expect(f.events).toEqual(["receipt"]);
    expect(f.channels).toEqual(["telegram"]);
    expect(f.allowCommands).toBe(true);
    expect(f.token).toBe("");
    expect(formFromDraft(null)).toEqual(emptyDestForm());
  });
});

describe("estado en vivo", () => {
  const st = (p: Partial<DestinationState>): DestinationState =>
    ({ destinationId: "d1", state: "online", lastError: "", connectedAt: 1, pending: 0, ...p }) as DestinationState;

  it("textos y tonos", () => {
    expect(stateView(true, st({ state: "online" }))).toMatchObject({ tone: "ok", label: "En línea" });
    expect(stateView(true, st({ state: "connecting" }))).toMatchObject({ tone: "warn", label: "Conectando…" });
    expect(stateView(true, st({ state: "error", lastError: "Token rechazado" }))).toMatchObject({
      tone: "danger",
      label: "Error",
      error: "Token rechazado",
    });
    expect(stateView(true, st({ state: "off" }))).toMatchObject({ tone: "muted", label: "Sin conexión" });
    expect(stateView(true, undefined)).toMatchObject({ tone: "muted", label: "Sin conexión" });
  });

  it("un destino desactivado se ve desactivado, diga lo que diga el estado", () => {
    expect(stateView(false, st({ state: "online" }))).toMatchObject({ tone: "muted", label: "Desactivado", error: "" });
  });

  it("pendientes", () => {
    expect(stateView(true, st({ pending: 4 })).pending).toBe(4);
    expect(pendingLabel(1)).toBe("1 pendiente");
    expect(pendingLabel(12)).toBe("12 pendientes");
  });

  it("indexa y aplica cambios sin mutar", () => {
    const m = indexStates([st({ destinationId: "a" }), st({ destinationId: "b", state: "error" })]);
    expect(Object.keys(m)).toEqual(["a", "b"]);
    const next = applyState(m, st({ destinationId: "a", state: "connecting" }));
    expect(next.a.state).toBe("connecting");
    expect(m.a.state).toBe("online");
    expect(next.b).toBe(m.b);
  });
});
