import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import AccountPill from "./AccountPill";
import ConfirmDialog from "./ConfirmDialog";
import Modal, { closeTopModal } from "./Modal";
import WaText from "./WaText";

describe("ConfirmDialog", () => {
  it("con requireText solo confirma al escribir la etiqueta exacta", () => {
    const onConfirm = vi.fn();
    render(<ConfirmDialog open title="Eliminar" danger requireText="Ventas" onConfirm={onConfirm} onCancel={() => undefined} />);
    const ok = screen.getByTestId("confirm-ok") as HTMLButtonElement;
    expect(ok.disabled).toBe(true);
    const input = screen.getByRole("textbox");
    fireEvent.change(input, { target: { value: "ventas" } });
    expect(ok.disabled).toBe(true);
    fireEvent.change(input, { target: { value: "Ventas" } });
    expect(ok.disabled).toBe(false);
    fireEvent.click(ok);
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it("sin requireText confirma directo", () => {
    const onConfirm = vi.fn();
    render(<ConfirmDialog open title="¿Seguro?" onConfirm={onConfirm} onCancel={() => undefined} />);
    fireEvent.click(screen.getByTestId("confirm-ok"));
    expect(onConfirm).toHaveBeenCalled();
  });
});

describe("Modal", () => {
  it("closeTopModal cierra el de arriba y avisa si no hay", () => {
    const close1 = vi.fn();
    const close2 = vi.fn();
    const { unmount } = render(
      <>
        <Modal open onClose={close1} title="Uno">
          a
        </Modal>
        <Modal open onClose={close2} title="Dos">
          b
        </Modal>
      </>,
    );
    expect(closeTopModal()).toBe(true);
    expect(close2).toHaveBeenCalledTimes(1);
    expect(close1).not.toHaveBeenCalled();
    unmount();
    expect(closeTopModal()).toBe(false);
  });

  it("no pinta nada cerrado y la X cierra", () => {
    const onClose = vi.fn();
    const { rerender } = render(
      <Modal open={false} onClose={onClose} title="T" size="lg">
        contenido
      </Modal>,
    );
    expect(screen.queryByText("contenido")).toBeNull();
    rerender(
      <Modal open onClose={onClose} title="T" size="full">
        contenido
      </Modal>,
    );
    expect(screen.getByText("contenido")).toBeTruthy();
    fireEvent.click(screen.getByLabelText("Cerrar"));
    expect(onClose).toHaveBeenCalled();
  });
});

describe("AccountPill", () => {
  it("corta a 12 caracteres y deja el texto completo en title", () => {
    render(<AccountPill label="Distribuidora Norte" color="#8B5CF6" />);
    const pill = screen.getByTitle("Distribuidora Norte");
    expect(pill.textContent).toBe("Distribuido…");
  });
});

describe("WaText", () => {
  it("pinta formato y enlaces sin HTML crudo", () => {
    const { container } = render(<WaText text={"*hola* <b>x</b> https://a.com"} />);
    expect(container.querySelector("strong")?.textContent).toBe("hola");
    expect(container.querySelector("b")).toBeNull();
    const a = container.querySelector("a")!;
    expect(a.getAttribute("href")).toBe("https://a.com");
    expect(a.getAttribute("target")).toBe("_blank");
  });
});
