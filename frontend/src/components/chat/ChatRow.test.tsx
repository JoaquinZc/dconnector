import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import ChatRow from "./ChatRow";
import { chat, NOW } from "./testkit";
import { muteUntil } from "./chatFormat";

const acc = { label: "Ventas de la tienda grande", color: "#25D366" };

describe("ChatRow", () => {
  it("título, hora de hoy y vista previa", () => {
    render(<ChatRow chat={chat()} account={acc} now={NOW} onClick={() => undefined} />);
    expect(screen.getByText("Ana Pérez")).toBeInTheDocument();
    expect(screen.getByText("14:05")).toBeInTheDocument();
    expect(screen.getByText("Hola, ¿cómo estás?")).toBeInTheDocument();
  });

  it("la cuenta es texto pequeño con punto de color (sin píldora), recortado por CSS a 76 px", () => {
    render(<ChatRow chat={chat()} account={acc} now={NOW} onClick={() => undefined} />);
    const el = screen.getByTestId("row-account");
    expect(el).toHaveAttribute("title", acc.label);
    expect(el.textContent).toBe(acc.label); // el texto va completo: lo recorta `truncate`
    expect(el.className).toContain("max-w-[76px]");
    expect(el.className).not.toContain("rounded-full");
    expect(el.querySelector("i")).toHaveStyle({ background: acc.color });
    expect(el.querySelector("span")?.className).toContain("truncate");
  });

  it("sin cuenta conocida no hay texto de cuenta", () => {
    render(<ChatRow chat={chat()} now={NOW} onClick={() => undefined} />);
    expect(screen.queryByTestId("row-account")).toBeNull();
  });

  it("insignia de canal con icono (sin letra) y borde del color de fondo de la fila", () => {
    const { rerender } = render(<ChatRow chat={chat()} now={NOW} onClick={() => undefined} />);
    const badge = screen.getByTestId("row-channel");
    expect(badge).toHaveAttribute("title", "WhatsApp");
    expect(badge.querySelector("svg")).not.toBeNull();
    expect(badge.textContent).toBe("");
    expect(badge.className).toContain("border-[var(--rowbg)]");
    rerender(<ChatRow chat={chat({ channel: "instagram" })} now={NOW} onClick={() => undefined} />);
    expect(screen.getByTestId("row-channel")).toHaveAttribute("title", "Instagram");
  });

  it("seleccionada: fondo --sel y barra de acento; sin seleccionar no hay barra", () => {
    const { rerender } = render(<ChatRow chat={chat()} now={NOW} onClick={() => undefined} />);
    expect(screen.queryByTestId("row-selected-bar")).toBeNull();
    expect(screen.getByTestId("chat-row").className).toContain("[--rowbg:var(--panel)]");
    rerender(<ChatRow chat={chat()} selected now={NOW} onClick={() => undefined} />);
    expect(screen.getByTestId("row-selected-bar")).toBeInTheDocument();
    expect(screen.getByTestId("chat-row").className).toContain("[--rowbg:var(--sel)]");
  });

  it("mensaje mío: prefijo «Tú: » y ✓✓ azul si fue leído", () => {
    const c = chat({ lastFromMe: true, lastPreview: "Listo, gracias" });
    const { container } = render(<ChatRow chat={c} now={NOW} lastDelivery="read" onClick={() => undefined} />);
    expect(screen.getByText("Tú: Listo, gracias")).toBeInTheDocument();
    expect(screen.getByTestId("row-tick")).toBeInTheDocument();
    expect(screen.getByTestId("row-tick")).toHaveAttribute("data-tick", "read");
    expect(container.querySelector(".text-\\[\\#1d9bd1\\]")).not.toBeNull();
  });

  it("mensaje mío sin dato de entrega: un solo ✓ gris", () => {
    const c = chat({ lastFromMe: true });
    const { container } = render(<ChatRow chat={c} now={NOW} onClick={() => undefined} />);
    expect(container.querySelector(".text-\\[\\#1d9bd1\\]")).toBeNull();
    expect(screen.getByTestId("row-tick")).toHaveAttribute("data-tick", "sent");
  });

  it("mensaje ajeno: sin ✓", () => {
    render(<ChatRow chat={chat()} now={NOW} onClick={() => undefined} />);
    expect(screen.queryByTestId("row-tick")).toBeNull();
  });

  it("contador de no leídos (99+), fijado y silenciado", () => {
    const c = chat({ unreadCount: 120, pinned: true, mutedUntil: muteUntil("always", NOW) });
    render(<ChatRow chat={c} now={NOW} onClick={() => undefined} />);
    expect(screen.getByTestId("row-unread").textContent).toBe("99+");
    expect(screen.getByTitle("Fijado")).toBeInTheDocument();
    expect(screen.getByTitle("Silenciado")).toBeInTheDocument();
  });

  it("fijado y silenciado son iconos (no emojis); el contador silenciado es gris", () => {
    const c = chat({ unreadCount: 3, pinned: true, mutedUntil: muteUntil("always", NOW) });
    render(<ChatRow chat={c} now={NOW} onClick={() => undefined} />);
    const pin = screen.getByTitle("Fijado");
    const bell = screen.getByTitle("Silenciado");
    expect(pin.querySelector("svg")).not.toBeNull();
    expect(bell.querySelector("svg")).not.toBeNull();
    expect(pin.textContent).toBe("");
    expect(bell.textContent).toBe("");
    const badge = screen.getByTestId("row-unread");
    expect(badge.className).toContain("bg-[var(--mute-bg)]");
    expect(badge.className).not.toContain("--badge-bg");
  });

  it("sin silenciar: el contador usa la píldora de insignia y la hora resalta", () => {
    render(<ChatRow chat={chat({ unreadCount: 2 })} now={NOW} onClick={() => undefined} />);
    expect(screen.getByTestId("row-unread").className).toContain("bg-[var(--badge-bg)]");
    expect(screen.getByTestId("row-time").className).toContain("text-[var(--accent-text)]");
    expect(screen.queryByTitle("Fijado")).toBeNull();
    expect(screen.queryByTitle("Silenciado")).toBeNull();
  });

  it("silenciado con no leídos: la hora no resalta", () => {
    render(<ChatRow chat={chat({ unreadCount: 2, mutedUntil: muteUntil("always", NOW) })} now={NOW} onClick={() => undefined} />);
    expect(screen.getByTestId("row-time").className).not.toContain("--accent-text");
  });

  it("sin no leídos no hay contador", () => {
    render(<ChatRow chat={chat({ unreadCount: 0 })} now={NOW} onClick={() => undefined} />);
    expect(screen.queryByTestId("row-unread")).toBeNull();
  });

  it("clic y Enter abren el chat", () => {
    const onClick = vi.fn();
    render(<ChatRow chat={chat()} now={NOW} onClick={onClick} />);
    fireEvent.click(screen.getByTestId("chat-row"));
    fireEvent.keyDown(screen.getByTestId("chat-row"), { key: "Enter" });
    expect(onClick).toHaveBeenCalledTimes(2);
  });

  it("hora de ayer", () => {
    const c = chat({ lastMessageAt: new Date(2026, 10, 10, 22, 0).getTime() });
    render(<ChatRow chat={c} now={NOW} onClick={() => undefined} />);
    expect(screen.getByText("Ayer")).toBeInTheDocument();
  });

  describe("sin vista previa", () => {
    it("directo: muestra el teléfono en gris, sin cursiva", () => {
      const c = chat({ lastPreview: "", title: "Ana Pérez", peerNativeId: "593999111222@s.whatsapp.net" });
      render(<ChatRow chat={c} now={NOW} onClick={() => undefined} />);
      const el = screen.getByTestId("row-preview-empty");
      expect(el).toHaveTextContent("+593999111222");
      expect(el.className).not.toContain("italic");
    });

    it("sin teléfono que mostrar: «Sin mensajes cargados» en cursiva", () => {
      const c = chat({ lastPreview: "   ", kind: "group", title: "Equipo", peerNativeId: "", nativeId: "1203@g.us" });
      render(<ChatRow chat={c} now={NOW} onClick={() => undefined} />);
      const el = screen.getByTestId("row-preview-empty");
      expect(el).toHaveTextContent("Sin mensajes cargados");
      expect(el.className).toContain("italic");
    });

    it("la fila conserva su altura de 72 px y no pone «Tú:» ni ✓ a un texto de relleno", () => {
      const c = chat({ lastPreview: "", lastFromMe: true });
      render(<ChatRow chat={c} now={NOW} onClick={() => undefined} />);
      expect(screen.getByTestId("chat-row")).toHaveStyle({ height: "72px" });
      expect(screen.getByTestId("row-preview-empty").textContent).not.toContain("Tú:");
      expect(screen.queryByTestId("row-tick")).toBeNull(); // sin mensaje no hay ✓
    });

    it("con vista previa no aparece el relleno", () => {
      render(<ChatRow chat={chat()} now={NOW} onClick={() => undefined} />);
      expect(screen.queryByTestId("row-preview-empty")).toBeNull();
    });
  });
});
