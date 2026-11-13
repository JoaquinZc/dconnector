import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import Bubble, { type BubbleProps } from "./Bubble";
import { nextVote, rateLabel } from "./MediaViews";
import { msg } from "./testkit";

function setup(over: Record<string, unknown> = {}, props: Partial<BubbleProps> = {}) {
  const m = msg(over);
  const handlers = {
    onMenu: vi.fn(),
    onOpenImage: vi.fn(),
    onGoToQuoted: vi.fn(),
    onSaveAs: vi.fn(),
    onVote: vi.fn(),
  };
  const utils = render(<Bubble msg={m} showSender={false} selfId="me@s.whatsapp.net" canVote {...handlers} {...props} />);
  return { m, ...handlers, ...utils };
}

describe("Bubble · pie y estados", () => {
  it.each([
    ["pending", "Pendiente"],
    ["sent", "Enviado"],
    ["delivered", "Entregado"],
    ["read", "Leído"],
    ["played", "Leído"],
    ["failed", "No se pudo enviar"],
  ])("mensaje propio %s → %s", (delivery, label) => {
    setup({ fromMe: true, delivery });
    expect(screen.getByLabelText(label)).toBeInTheDocument();
  });

  it("los mensajes ajenos no llevan ✓", () => {
    setup({ fromMe: false, delivery: "read" });
    expect(screen.queryByLabelText("Leído")).toBeNull();
  });

  it("leído se pinta azul, entregado gris", () => {
    const a = setup({ fromMe: true, delivery: "read" });
    expect(a.container.querySelector(".text-\\[\\#53bdeb\\]")).not.toBeNull();
    a.unmount();
    const b = setup({ fromMe: true, delivery: "delivered" });
    expect(b.container.querySelector(".text-\\[\\#53bdeb\\]")).toBeNull();
  });

  it("«editado» y la hora", () => {
    setup({ text: "x", editedAt: 5, timestamp: new Date(2026, 10, 11, 9, 7).getTime() });
    expect(screen.getByText("editado")).toBeInTheDocument();
    expect(screen.getByText("09:07")).toBeInTheDocument();
  });
});

describe("Bubble · tipos", () => {
  it("texto con formato y enlace", () => {
    const { container } = setup({ text: "*hola* https://ejemplo.com" });
    expect(container.querySelector("strong")?.textContent).toBe("hola");
    expect(container.querySelector("a")?.getAttribute("target")).toBe("_blank");
  });

  it("nombre del emisor en grupos", () => {
    setup({ senderName: "Luis" }, { showSender: true });
    expect(screen.getByText("Luis")).toBeInTheDocument();
  });

  it("foto: clic abre el visor y el pie se muestra", () => {
    const media = { id: "m1", mime: "image/jpeg", fileName: "a.jpg", size: 10, width: 400, height: 300, duration: 0, thumb: "data:image/jpeg;base64,xx", url: "/media/wa-1/m1" };
    const { onOpenImage, m } = setup({ kind: "image", text: "Mira esto", media });
    expect(screen.getByText("Mira esto")).toBeInTheDocument();
    fireEvent.click(screen.getByTitle("Ver foto"));
    expect(onOpenImage).toHaveBeenCalledWith(m);
  });

  it("documento: nombre, tamaño y «Guardar como»", () => {
    const media = { id: "d1", mime: "application/pdf", fileName: "factura.pdf", size: 2048, width: 0, height: 0, duration: 0, thumb: "", url: "/media/wa-1/d1" };
    const { onSaveAs, m } = setup({ kind: "document", text: "", media });
    expect(screen.getByText("factura.pdf")).toBeInTheDocument();
    expect(screen.getByText("2,0 KB")).toBeInTheDocument();
    fireEvent.click(screen.getByTitle("Guardar como"));
    expect(onSaveAs).toHaveBeenCalledWith(m);
  });

  it("voz: reproductor con velocidades 1× → 1,5× → 2× → 1×", () => {
    const media = { id: "v1", mime: "audio/ogg", fileName: "", size: 10, width: 0, height: 0, duration: 12, thumb: "", url: "/media/wa-1/v1" };
    setup({ kind: "voice", text: "", media });
    const btn = screen.getByTitle("Velocidad de reproducción");
    expect(btn.textContent).toBe("1×");
    fireEvent.click(btn);
    expect(btn.textContent).toBe("1,5×");
    fireEvent.click(btn);
    expect(btn.textContent).toBe("2×");
    fireEvent.click(btn);
    expect(btn.textContent).toBe("1×");
    expect(screen.getByText("0:12")).toBeInTheDocument();
    expect(rateLabel(1.5)).toBe("1,5×");
  });

  it("ubicación: enlace a OpenStreetMap y sin mapa embebido", () => {
    const { container } = setup({ kind: "location", text: "", location: { lat: -0.18, lng: -78.47, name: "Oficina", address: "", live: false } });
    const a = screen.getByTitle("Abrir en OpenStreetMap") as HTMLAnchorElement;
    expect(a.href).toContain("openstreetmap.org");
    expect(a.href).toContain("mlat=-0.18");
    expect(container.querySelector("iframe")).toBeNull();
    expect(screen.getByText("Oficina")).toBeInTheDocument();
  });

  it("contacto: «Abrir chat» solo si existe el chat", () => {
    const card = { name: "Marta", phone: "+593999000111", vcard: "" };
    const onOpenChat = vi.fn();
    const a = setup({ kind: "contact", text: "", contacts: [card] }, { findContactChat: () => "wa-1|593999000111@s.whatsapp.net", onOpenChat });
    fireEvent.click(screen.getByText("Abrir chat"));
    expect(onOpenChat).toHaveBeenCalledWith("wa-1|593999000111@s.whatsapp.net");
    a.unmount();
    setup({ kind: "contact", text: "", contacts: [card] });
    expect(screen.queryByText("Abrir chat")).toBeNull();
  });

  it("encuesta: barras, votos y votar", () => {
    const poll = { question: "¿Cuándo?", multi: false, options: [{ name: "Hoy", voters: ["a", "b"] }, { name: "Mañana", voters: ["me@s.whatsapp.net"] }] };
    const { onVote, m } = setup({ kind: "poll", text: "", poll });
    expect(screen.getByText("¿Cuándo?")).toBeInTheDocument();
    expect(screen.getByText("3 votos")).toBeInTheDocument();
    fireEvent.click(screen.getByText("Hoy"));
    expect(onVote).toHaveBeenCalledWith(m, ["Hoy"]);
  });

  it("encuesta: sin permiso de votar los botones no responden", () => {
    const poll = { question: "¿Cuándo?", multi: false, options: [{ name: "Hoy", voters: [] }] };
    const { onVote } = setup({ kind: "poll", text: "", poll }, { canVote: false });
    fireEvent.click(screen.getByText("Hoy"));
    expect(onVote).not.toHaveBeenCalled();
  });

  it("sistema: centrado y sin burbuja", () => {
    const { container } = setup({ kind: "system", text: "Ana añadió a Luis" });
    expect(screen.getByText("Ana añadió a Luis")).toBeInTheDocument();
    expect(container.querySelector("[data-key]")).toBeNull();
  });

  it("no compatible sin texto: aviso genérico", () => {
    setup({ kind: "unsupported", text: "" });
    expect(screen.getByText("Mensaje no compatible")).toBeInTheDocument();
    setup({ kind: "unsupported", text: "   " });
    expect(screen.getAllByText("Mensaje no compatible")).toHaveLength(2);
  });

  it("no compatible con texto: se muestra ese texto, discreto y en cursiva, y no el genérico", () => {
    setup({ kind: "unsupported", text: "Mensaje de una sola vez: ábrelo en tu teléfono" });
    const el = screen.getByTestId("unsupported");
    expect(el).toHaveTextContent("Mensaje de una sola vez: ábrelo en tu teléfono");
    expect(el.className).toContain("italic");
    expect(screen.queryByText("Mensaje no compatible")).toBeNull();
  });

  it("«Esperando este mensaje…» también se ve", () => {
    setup({ kind: "unsupported", text: "Esperando este mensaje…" });
    expect(screen.getByText("Esperando este mensaje…")).toBeInTheDocument();
  });

  it("eliminado", () => {
    setup({ text: "secreto", deletedAt: 5, fromMe: true });
    expect(screen.queryByText("secreto")).toBeNull();
    expect(screen.getByText("Eliminaste este mensaje")).toBeInTheDocument();
  });

  it("cita: clic va al mensaje citado", () => {
    const { onGoToQuoted } = setup({ quotedNativeId: "q9", quotedPreview: "Original", quotedSender: "Luis" });
    fireEvent.click(screen.getByText("Original"));
    expect(onGoToQuoted).toHaveBeenCalledWith("q9");
  });

  it("reacciones en píldora y menú con clic derecho", () => {
    const { container, onMenu, m } = setup({ reactions: [{ senderNativeId: "a", senderName: "A", emoji: "👍", at: 1 }, { senderNativeId: "b", senderName: "B", emoji: "👍", at: 2 }] });
    expect(screen.getByText("👍")).toBeInTheDocument();
    expect(screen.getByText("2")).toBeInTheDocument();
    fireEvent.contextMenu(container.querySelector("[data-key]")!, { clientX: 10, clientY: 20 });
    expect(onMenu).toHaveBeenCalledWith(m, { x: 10, y: 20 });
  });
});

describe("nextVote", () => {
  const single = { question: "?", multi: false, options: [{ name: "A", voters: ["me"] }, { name: "B", voters: [] }] };
  const multi = { question: "?", multi: true, options: [{ name: "A", voters: ["me"] }, { name: "B", voters: [] }] };
  it("selección única: cambia o quita", () => {
    expect(nextVote(single, "me", "B")).toEqual(["B"]);
    expect(nextVote(single, "me", "A")).toEqual([]);
  });
  it("selección múltiple: añade o quita", () => {
    expect(nextVote(multi, "me", "B")).toEqual(["A", "B"]);
    expect(nextVote(multi, "me", "A")).toEqual([]);
  });
});
