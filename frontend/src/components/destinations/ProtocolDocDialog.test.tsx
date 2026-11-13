import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const hoisted = vi.hoisted(() => ({ ProtocolDoc: vi.fn() }));
vi.mock("@/api", () => ({ api: { destinations: { ProtocolDoc: hoisted.ProtocolDoc } } }));

import { useUI } from "@/store/ui";
import ProtocolDocDialog from "./ProtocolDocDialog";
import ProtocolLink from "./ProtocolLink";

const DOC = [
  "# Protocolo dConnector v1",
  "",
  "Este documento es para quien escribe un **servidor** que se conecta.",
  "",
  "## 1. Conexión",
  "",
  "- La dirección debe ser `wss://`.",
  "- Hay un ack por evento.",
  "",
  "| Cabecera | Valor |",
  "| --- | --- |",
  "| `Authorization` | `Bearer <token>` |",
  "",
  "```json",
  '{ "v": 1, "kind": "hello" }',
  "```",
  "",
  "## 2. Eventos",
  "",
  "```",
  "ack de prueba",
  "```",
  "",
].join("\n");

let writeText: ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.clearAllMocks();
  hoisted.ProtocolDoc.mockResolvedValue(DOC);
  writeText = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
  useUI.setState({ toasts: [], confirmReq: null });
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const openDialog = async () => {
  render(<ProtocolDocDialog open onClose={() => {}} />);
  await screen.findByRole("heading", { name: "Protocolo dConnector v1" });
};

describe("diálogo del protocolo", () => {
  it("el enlace abre «Protocolo v1 — documento completo»", async () => {
    render(<ProtocolLink />);
    expect(screen.queryByRole("dialog")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /Ver el protocolo/ }));
    expect(await screen.findByRole("dialog")).toHaveTextContent("Protocolo v1 — documento completo");
    await screen.findByRole("heading", { name: "Protocolo dConnector v1" });
    expect(hoisted.ProtocolDoc).toHaveBeenCalledTimes(1);
  });

  it("pinta títulos, listas, tabla y código", async () => {
    await openDialog();
    expect(screen.getByRole("heading", { name: "1. Conexión", level: 2 })).toBeInTheDocument();
    expect(screen.getByText("servidor").tagName).toBe("STRONG");
    expect(screen.getByText("wss://").tagName).toBe("CODE");
    expect(screen.getAllByRole("listitem")).toHaveLength(2);
    const table = screen.getByRole("table");
    expect(within(table).getByRole("columnheader", { name: "Cabecera" })).toBeInTheDocument();
    expect(within(table).getByText("Authorization")).toBeInTheDocument();
    expect(screen.getAllByTestId("md-code")).toHaveLength(2);
    expect(screen.getByText(/"kind": "hello"/)).toBeInTheDocument();
  });

  it("«Copiar todo» copia el Markdown completo y avisa «Copiado»", async () => {
    await openDialog();
    fireEvent.click(screen.getByRole("button", { name: "Copiar todo" }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(DOC));
    await waitFor(() => expect(useUI.getState().toasts.some((t) => t.kind === "success" && t.message === "Copiado")).toBe(true));
    expect(await screen.findByRole("button", { name: "Copiado" })).toBeInTheDocument();
  });

  it("cada bloque de código tiene su «Copiar» y copia solo ese bloque", async () => {
    await openDialog();
    const blocks = screen.getAllByTestId("md-code");
    fireEvent.click(within(blocks[0]).getByRole("button", { name: "Copiar" }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('{ "v": 1, "kind": "hello" }'));
    expect(await within(blocks[0]).findByRole("button", { name: "Copiado" })).toBeInTheDocument();
    // El otro bloque sigue diciendo «Copiar».
    expect(within(blocks[1]).getByRole("button", { name: "Copiar" })).toBeInTheDocument();
    fireEvent.click(within(blocks[1]).getByRole("button", { name: "Copiar" }));
    await waitFor(() => expect(writeText).toHaveBeenLastCalledWith("ack de prueba"));
  });

  it("si el portapapeles falla, avisa y no dice «Copiado»", async () => {
    writeText.mockRejectedValue(new Error("denegado"));
    // Sin execCommand tampoco hay plan B.
    (document as unknown as { execCommand?: unknown }).execCommand = undefined;
    await openDialog();
    fireEvent.click(screen.getByRole("button", { name: "Copiar todo" }));
    await waitFor(() => expect(useUI.getState().toasts.some((t) => t.kind === "error" && t.message === "No se pudo copiar")).toBe(true));
    expect(screen.queryByRole("button", { name: "Copiado" })).toBeNull();
  });

  it("«Copiado» vuelve a «Copiar todo» pasado un momento", async () => {
    await openDialog();
    vi.useFakeTimers();
    fireEvent.click(screen.getByRole("button", { name: "Copiar todo" }));
    await act(async () => {
      await Promise.resolve();
    });
    expect(screen.getByRole("button", { name: "Copiado" })).toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(2000);
    });
    expect(screen.getByRole("button", { name: "Copiar todo" })).toBeInTheDocument();
  });

  it("«Ver texto sin formato» muestra el Markdown crudo, seleccionable, en un <pre>", async () => {
    await openDialog();
    expect(screen.queryByTestId("md-raw")).toBeNull();
    fireEvent.click(screen.getByRole("switch", { name: "Ver texto sin formato" }));
    const raw = screen.getByTestId("md-raw");
    expect(raw.tagName).toBe("PRE");
    expect(raw.textContent).toBe(DOC);
    expect(raw.className).toContain("select-text");
    expect(screen.queryByRole("table")).toBeNull();
    fireEvent.click(screen.getByRole("switch", { name: "Ver texto sin formato" }));
    expect(screen.queryByTestId("md-raw")).toBeNull();
    expect(screen.getByRole("table")).toBeInTheDocument();
  });

  it("buscar resalta, cuenta y salta entre coincidencias", async () => {
    const scroll = vi.fn();
    Element.prototype.scrollIntoView = scroll;
    await openDialog();
    const box = screen.getByRole("searchbox", { name: "Buscar en el documento" });
    fireEvent.change(box, { target: { value: "ACK" } });
    // «ack» aparece en: el elemento de lista y el bloque de código (sin tildes ni mayúsculas).
    await waitFor(() => expect(screen.getByTestId("md-count")).toHaveTextContent("1 de 2"));
    const marks = () => Array.from(document.querySelectorAll("mark[data-hit]"));
    expect(marks()).toHaveLength(2);
    expect(marks()[0]).toHaveAttribute("data-current", "true");
    expect(marks()[1]).toHaveAttribute("data-current", "false");
    expect(scroll).toHaveBeenCalled();

    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => expect(screen.getByTestId("md-count")).toHaveTextContent("2 de 2"));
    expect(marks()[1]).toHaveAttribute("data-current", "true");
    // Da la vuelta.
    fireEvent.click(screen.getByRole("button", { name: "Resultado siguiente" }));
    await waitFor(() => expect(screen.getByTestId("md-count")).toHaveTextContent("1 de 2"));
    fireEvent.click(screen.getByRole("button", { name: "Resultado anterior" }));
    await waitFor(() => expect(screen.getByTestId("md-count")).toHaveTextContent("2 de 2"));
    fireEvent.keyDown(box, { key: "Enter", shiftKey: true });
    await waitFor(() => expect(screen.getByTestId("md-count")).toHaveTextContent("1 de 2"));
  });

  it("encuentra sin distinguir tildes y avisa si no hay resultados", async () => {
    await openDialog();
    const box = screen.getByRole("searchbox", { name: "Buscar en el documento" });
    fireEvent.change(box, { target: { value: "conexion" } });
    await waitFor(() => expect(screen.getByTestId("md-count")).toHaveTextContent("1 de 1"));
    fireEvent.change(box, { target: { value: "nada de nada" } });
    await waitFor(() => expect(screen.getByTestId("md-count")).toHaveTextContent("Sin resultados"));
    expect(screen.getByRole("button", { name: "Resultado siguiente" })).toBeDisabled();
    fireEvent.change(box, { target: { value: "" } });
    expect(screen.queryByTestId("md-count")).toBeNull();
    expect(document.querySelectorAll("mark")).toHaveLength(0);
  });

  it("la búsqueda también resalta en el texto sin formato", async () => {
    await openDialog();
    fireEvent.click(screen.getByRole("switch", { name: "Ver texto sin formato" }));
    fireEvent.change(screen.getByRole("searchbox", { name: "Buscar en el documento" }), { target: { value: "hello" } });
    await waitFor(() => expect(screen.getByTestId("md-count")).toHaveTextContent("1 de 1"));
    expect(within(screen.getByTestId("md-raw")).getByText("hello").tagName).toBe("MARK");
  });

  it("si el documento no carga, lo dice", async () => {
    hoisted.ProtocolDoc.mockRejectedValue(new Error("falló"));
    render(<ProtocolDocDialog open onClose={() => {}} />);
    expect(await screen.findByRole("alert")).toHaveTextContent("No se pudo cargar el documento");
    expect(screen.getByRole("button", { name: "Copiar todo" })).toBeDisabled();
  });
});
