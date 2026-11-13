import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import MessageMenu, { QUICK_EMOJIS } from "./MessageMenu";
import { FULL_CAPS, msg, NOW } from "./testkit";

function setup(over: Record<string, unknown>, caps: Parameters<typeof MessageMenu>[0]["caps"], extra: { myReaction?: string } = {}) {
  const h = {
    onClose: vi.fn(),
    onReply: vi.fn(),
    onReact: vi.fn(),
    onForward: vi.fn(),
    onCopy: vi.fn(),
    onEdit: vi.fn(),
    onDelete: vi.fn(),
    onInfo: vi.fn(),
  };
  render(<MessageMenu msg={msg(over)} at={{ x: 10, y: 10 }} caps={caps} now={NOW} {...h} {...extra} />);
  return h;
}

describe("MessageMenu", () => {
  it("mensaje propio reciente con todas las capacidades: todas las entradas y 6 emojis", () => {
    setup({ fromMe: true, timestamp: NOW - 60_000 }, FULL_CAPS);
    for (const label of ["Responder", "Copiar", "Reenviar", "Editar", "Info.", "Eliminar"]) {
      expect(screen.getByText(label)).toBeInTheDocument();
    }
    for (const e of QUICK_EMOJIS) expect(screen.getByText(e)).toBeInTheDocument();
    expect(QUICK_EMOJIS).toHaveLength(6);
    expect(screen.getByTitle("Más emojis")).toBeInTheDocument();
  });

  it("sin permiso de editar, responder, reenviar ni reaccionar solo quedan las de siempre", () => {
    setup({ fromMe: false }, { reply: false, forward: false, react: false, edit: false });
    expect(screen.queryByText("Responder")).toBeNull();
    expect(screen.queryByText("Reenviar")).toBeNull();
    expect(screen.queryByText("Editar")).toBeNull();
    expect(screen.queryByTitle("Más emojis")).toBeNull();
    expect(screen.getByText("Copiar")).toBeInTheDocument();
    expect(screen.getByText("Eliminar")).toBeInTheDocument();
  });

  it("pasada la ventana de edición, «Editar» desaparece", () => {
    setup({ fromMe: true, timestamp: NOW - 30 * 60_000 }, FULL_CAPS);
    expect(screen.queryByText("Editar")).toBeNull();
    expect(screen.getByText("Responder")).toBeInTheDocument();
  });

  it("las acciones se ejecutan y el menú se cierra", () => {
    const h = setup({ fromMe: true, timestamp: NOW }, FULL_CAPS);
    fireEvent.click(screen.getByText("Responder"));
    expect(h.onReply).toHaveBeenCalled();
    expect(h.onClose).toHaveBeenCalled();
    fireEvent.click(screen.getByText("👍"));
    expect(h.onReact).toHaveBeenCalledWith("👍");
    fireEvent.click(screen.getByText("Eliminar"));
    expect(h.onDelete).toHaveBeenCalled();
  });

  it("la reacción propia se marca para poder quitarla", () => {
    setup({ fromMe: false }, FULL_CAPS, { myReaction: "❤️" });
    expect(screen.getByTitle("Quitar reacción").textContent).toBe("❤️");
  });
});
