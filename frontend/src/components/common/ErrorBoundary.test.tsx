import { render, screen, fireEvent } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import ErrorBoundary from "./ErrorBoundary";

let boom = true;
function Bomb() {
  if (boom) throw new Error("secreto-token-123");
  return <p>todo bien</p>;
}

describe("ErrorBoundary", () => {
  it("muestra un aviso en español, no deja la pantalla en blanco y se recupera al reintentar", () => {
    const err = vi.spyOn(console, "error").mockImplementation(() => undefined);
    boom = true;
    render(
      <ErrorBoundary level="root">
        <Bomb />
      </ErrorBoundary>,
    );
    expect(screen.getByRole("alert")).toBeTruthy();
    expect(screen.getByText("Algo salió mal en esta pantalla")).toBeTruthy();
    expect(screen.getByText("Recargar")).toBeTruthy();
    boom = false;
    fireEvent.click(screen.getByText("Reintentar"));
    expect(screen.getByText("todo bien")).toBeTruthy();
    err.mockRestore();
  });

  it("se vuelve a armar al cambiar resetKey (otra página)", () => {
    const err = vi.spyOn(console, "error").mockImplementation(() => undefined);
    boom = true;
    const { rerender } = render(
      <ErrorBoundary level="page" resetKey="a">
        <Bomb />
      </ErrorBoundary>,
    );
    expect(screen.getByRole("alert")).toBeTruthy();
    boom = false;
    rerender(
      <ErrorBoundary level="page" resetKey="b">
        <Bomb />
      </ErrorBoundary>,
    );
    expect(screen.getByText("todo bien")).toBeTruthy();
    err.mockRestore();
  });
});
