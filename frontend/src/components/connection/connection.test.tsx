import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@wailsio/runtime", () => ({ Call: { ByName: vi.fn() }, Events: { On: vi.fn(() => () => undefined), Emit: vi.fn() } }));
vi.mock("@/store/app", () => ({ startApp: vi.fn(() => () => undefined) }));
vi.mock("@/components/layout/Shell", () => ({
  default: ({ children }: { children: React.ReactNode }) => <div data-testid="shell">{children}</div>,
}));
vi.mock("@/pages/registry", () => ({ PageView: () => <div>Página</div> }));
vi.mock("@/components/common/NotifyNotice", () => ({ default: () => null }));

import { ApiError } from "@/api/http";
import { useConnection, type ProbeResult } from "@/api/connection";
import { desktop } from "@/api/desktop";
import { startApp } from "@/store/app";
import { useUI } from "@/store/ui";
import App from "@/App";
import ConnectionSection from "@/components/settings/ConnectionSection";
import { ConnectionBanner, LoginScreen, OfflineScreen } from "./Screens";

const FP = Array.from({ length: 32 }, (_, i) => (i * 7 + 3).toString(16).padStart(2, "0").toUpperCase()).join(":");
const initial = useConnection.getState();
const boot = vi.fn(async () => undefined);
const retry = vi.fn(async () => undefined);
const login = vi.fn(async (_u: string, _p: string) => undefined);
const probe = vi.fn();
const saveRemote = vi.fn(async (..._a: unknown[]) => undefined);
const useLocal = vi.fn(async () => undefined);
const logout = vi.fn(async () => undefined);

function setConn(p: Partial<ReturnType<typeof useConnection.getState>>) {
  act(() => useConnection.setState(p));
}

beforeEach(() => {
  vi.clearAllMocks();
  useUI.setState({ toasts: [] });
  useConnection.setState({
    ...initial,
    mode: "local",
    baseUrl: "http://127.0.0.1:5000",
    user: "",
    link: "connecting",
    ready: false,
    needLogin: false,
    error: "",
    booting: false,
    warning: "",
    serverVersion: "",
    apiVersion: 0,
    tlsFingerprint: "",
    boot,
    retry,
    login,
    probe,
    saveRemote,
    useLocal,
    logout,
  });
});

afterEach(() => {
  cleanup();
  delete (window as unknown as { chrome?: unknown }).chrome;
});

describe("App: nunca queda en blanco", () => {
  it("al abrir arranca la conexión y muestra «Conectando»", () => {
    render(<App />);
    expect(boot).toHaveBeenCalledTimes(1);
    expect(screen.getByText("Conectando con el servidor…")).toBeInTheDocument();
    expect(startApp).not.toHaveBeenCalled();
  });

  it("servidor caído: pantalla «Sin conexión» con el motivo y Reintentar", () => {
    setConn({ link: "offline", error: "No se pudo conectar con el servidor." });
    render(<App />);
    expect(screen.getByRole("heading", { name: "Sin conexión" })).toBeInTheDocument();
    expect(screen.getByTestId("offline-error")).toHaveTextContent("No se pudo conectar con el servidor.");
    fireEvent.click(screen.getByRole("button", { name: "Reintentar" }));
    expect(retry).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId("shell")).not.toBeInTheDocument();
  });

  it("401: pantalla de inicio de sesión", () => {
    setConn({ needLogin: true, link: "connecting" });
    render(<App />);
    expect(screen.getByRole("form", { name: "Iniciar sesión" })).toBeInTheDocument();
  });

  it("lista: arma la interfaz y arranca los estados una vez", () => {
    setConn({ ready: true, link: "connected" });
    render(<App />);
    expect(screen.getByTestId("shell")).toBeInTheDocument();
    expect(screen.queryByTestId("conn-banner")).not.toBeInTheDocument();
    expect(startApp).toHaveBeenCalledTimes(1);
  });

  it("si el servidor cae con la interfaz abierta, no se desmonta: aparece la banda y se puede reintentar", () => {
    setConn({ ready: true, link: "connected" });
    render(<App />);
    setConn({ link: "reconnecting" });
    expect(screen.getByTestId("shell")).toBeInTheDocument();
    expect(screen.getByTestId("conn-banner")).toHaveTextContent("Reconectando con el servidor…");
    setConn({ link: "offline" });
    expect(screen.getByTestId("conn-banner")).toHaveTextContent("Sin conexión con el servidor.");
    fireEvent.click(screen.getByRole("button", { name: "Reintentar" }));
    expect(retry).toHaveBeenCalled();
    setConn({ link: "connected" });
    expect(screen.queryByTestId("conn-banner")).not.toBeInTheDocument();
    expect(startApp).toHaveBeenCalledTimes(1); // no reinicia los estados
  });

  it("una sesión caducada con la interfaz abierta pide la clave encima, sin perder lo que hay debajo", () => {
    setConn({ ready: true, link: "connected" });
    render(<App />);
    setConn({ needLogin: true });
    expect(screen.getByTestId("shell")).toBeInTheDocument();
    expect(screen.getByRole("form", { name: "Iniciar sesión" })).toBeInTheDocument();
  });
});

describe("OfflineScreen", () => {
  it("«Usar este equipo» solo en el escritorio con servidor remoto", () => {
    setConn({ mode: "remote", baseUrl: "https://srv:8443", link: "offline", error: "x" });
    const { unmount } = render(<OfflineScreen />);
    expect(screen.queryByRole("button", { name: "Usar este equipo" })).not.toBeInTheDocument();
    unmount();
    (window as unknown as { chrome: unknown }).chrome = { webview: { postMessage: () => undefined } };
    render(<OfflineScreen />);
    fireEvent.click(screen.getByRole("button", { name: "Usar este equipo" }));
    expect(useLocal).toHaveBeenCalled();
  });

  it("los botones pueden bajar de línea (caben en 360 px) y la dirección se recorta", () => {
    setConn({ mode: "remote", baseUrl: "https://un-servidor-con-un-nombre-muy-largo.ejemplo.com:8443", link: "offline" });
    render(<OfflineScreen />);
    expect(screen.getByRole("button", { name: "Reintentar" }).parentElement).toHaveClass("flex-wrap");
    expect(screen.getByTitle("https://un-servidor-con-un-nombre-muy-largo.ejemplo.com:8443")).toHaveClass("truncate");
  });
});

describe("LoginScreen", () => {
  it("«Entrar» sigue desactivado hasta tener usuario y clave; envía y limpia la clave", async () => {
    render(<LoginScreen />);
    const enter = screen.getByRole("button", { name: "Entrar" });
    expect(enter).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Usuario"), { target: { value: "ana" } });
    fireEvent.change(screen.getByLabelText("Clave"), { target: { value: "una-clave-larga" } });
    expect(enter).toBeEnabled();
    fireEvent.click(enter);
    await waitFor(() => expect(login).toHaveBeenCalledWith("ana", "una-clave-larga"));
    await waitFor(() => expect(screen.getByLabelText("Clave")).toHaveValue(""));
  });

  it("muestra el error del servidor tal cual", async () => {
    login.mockRejectedValueOnce(new ApiError("unauthorized", "Usuario o clave incorrectos.", 401));
    render(<LoginScreen />);
    fireEvent.change(screen.getByLabelText("Usuario"), { target: { value: "ana" } });
    fireEvent.change(screen.getByLabelText("Clave"), { target: { value: "mala" } });
    fireEvent.click(screen.getByRole("button", { name: "Entrar" }));
    expect(await screen.findByTestId("login-error")).toHaveTextContent("Usuario o clave incorrectos.");
  });

  it("precarga el usuario guardado", () => {
    setConn({ user: "ana" });
    render(<LoginScreen />);
    expect(screen.getByLabelText("Usuario")).toHaveValue("ana");
  });
});

describe("ConnectionBanner", () => {
  it("no se ve con el enlace bueno ni mientras se conecta por primera vez", () => {
    setConn({ link: "connected" });
    const { rerender } = render(<ConnectionBanner />);
    expect(screen.queryByTestId("conn-banner")).not.toBeInTheDocument();
    setConn({ link: "connecting" });
    rerender(<ConnectionBanner />);
    expect(screen.queryByTestId("conn-banner")).not.toBeInTheDocument();
  });
});

describe("ConnectionSection (Ajustes → Conexión)", () => {
  const PROBE: ProbeResult = {
    base: "https://srv:8443",
    version: "0.3.0",
    apiVersion: 1,
    tls: true,
    user: "ana",
    token: "TK",
    tlsFingerprint: "AB:CD:EF",
    warning: "",
  };

  function fill(url = "srv:8443", user = "ana", pass = "clave-larga-12") {
    fireEvent.change(screen.getByLabelText("Dirección del servidor"), { target: { value: url } });
    fireEvent.change(screen.getByLabelText("Usuario"), { target: { value: user } });
    fireEvent.change(screen.getByLabelText("Clave"), { target: { value: pass } });
  }

  it("muestra el estado del enlace y la versión del servidor", () => {
    setConn({ ready: true, link: "connected", serverVersion: "0.3.0", apiVersion: 1 });
    render(<ConnectionSection />);
    expect(screen.getByTestId("conn-state")).toHaveTextContent("Conectado");
    expect(screen.getByTestId("conn-version")).toHaveTextContent("0.3.0 · API 1");
    expect(screen.getByRole("radio", { name: "Local" })).toHaveAttribute("aria-checked", "true");
    expect(screen.queryByTestId("conn-remote")).not.toBeInTheDocument();
    setConn({ link: "reconnecting" });
    expect(screen.getByTestId("conn-state")).toHaveTextContent("Reconectando");
    setConn({ link: "offline" });
    expect(screen.getByTestId("conn-state")).toHaveTextContent("Sin conexión");
  });

  it("avisa si el servidor usa otra versión de la API", () => {
    setConn({ warning: "Este servidor usa otra versión de la API." });
    render(<ConnectionSection />);
    expect(screen.getByTestId("conn-warning")).toHaveTextContent("Este servidor usa otra versión de la API.");
  });

  it("Remoto: pide dirección, usuario y clave; Probar muestra versión, API y huella", async () => {
    probe.mockResolvedValue(PROBE);
    render(<ConnectionSection />);
    fireEvent.click(screen.getByRole("radio", { name: "Remoto" }));
    const test = screen.getByRole("button", { name: "Probar" });
    expect(test).toBeDisabled();
    fill();
    expect(test).toBeEnabled();
    fireEvent.click(test);
    expect(await screen.findByTestId("conn-probe-ok")).toHaveTextContent("Versión 0.3.0 · API 1 · TLS");
    expect(screen.getByTestId("conn-fp")).toHaveTextContent("AB:CD:EF");
    expect(probe).toHaveBeenCalledWith("srv:8443", "ana", "clave-larga-12");
  });

  it("Probar con fallo muestra el error en español y no guarda nada", async () => {
    probe.mockRejectedValue(new ApiError("unauthorized", "Usuario o clave incorrectos.", 401));
    render(<ConnectionSection />);
    fireEvent.click(screen.getByRole("radio", { name: "Remoto" }));
    fill();
    fireEvent.click(screen.getByRole("button", { name: "Probar" }));
    expect(await screen.findByTestId("conn-error")).toHaveTextContent("Usuario o clave incorrectos.");
    expect(screen.queryByTestId("conn-probe-ok")).not.toBeInTheDocument();
    expect(saveRemote).not.toHaveBeenCalled();
  });

  it("Guardar y conectar reutiliza el token de la prueba", async () => {
    probe.mockResolvedValue(PROBE);
    render(<ConnectionSection />);
    fireEvent.click(screen.getByRole("radio", { name: "Remoto" }));
    fill();
    fireEvent.click(screen.getByRole("button", { name: "Probar" }));
    await screen.findByTestId("conn-probe-ok");
    fireEvent.click(screen.getByRole("button", { name: "Guardar y conectar" }));
    await waitFor(() => expect(saveRemote).toHaveBeenCalledWith("https://srv:8443", "ana", "clave-larga-12", "TK"));
    expect(probe).toHaveBeenCalledTimes(1);
  });

  it("en el escritorio guarda y conecta por el programa (sin probar desde la ventana) y, con certificado sin confirmar, ofrece confiar y reintenta", async () => {
    (window as unknown as { chrome: unknown }).chrome = { webview: { postMessage: () => undefined } };
    const trust = vi.spyOn(desktop, "tlsTrust").mockResolvedValue();
    vi.spyOn(desktop, "keepRunningInBackground").mockResolvedValue(false);
    saveRemote
      .mockRejectedValueOnce(new Error(`El servidor usa un certificado que no es de confianza todavía. Huella SHA-256: ${FP.toLowerCase()}`))
      .mockResolvedValueOnce(undefined);
    render(<ConnectionSection />);
    fireEvent.click(screen.getByRole("radio", { name: "Remoto" }));
    fill();
    expect(screen.queryByRole("button", { name: "Probar" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Guardar y conectar" }));
    expect(await screen.findByTestId("tls-trust")).toHaveAttribute("data-changed", "0");
    expect(screen.getByTestId("tls-fingerprint")).toHaveTextContent(FP);
    expect(screen.queryByTestId("conn-error")).not.toBeInTheDocument();
    expect(probe).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Confiar en este certificado" }));
    await waitFor(() => expect(trust).toHaveBeenCalledWith(FP));
    await waitFor(() => expect(saveRemote).toHaveBeenCalledTimes(2));
    expect(saveRemote).toHaveBeenLastCalledWith("srv:8443", "ana", "clave-larga-12");
  });

  it("si la huella cambió avisa con claridad y muestra la huella nueva", async () => {
    (window as unknown as { chrome: unknown }).chrome = { webview: { postMessage: () => undefined } };
    vi.spyOn(desktop, "keepRunningInBackground").mockResolvedValue(false);
    saveRemote.mockRejectedValueOnce(
      new Error(`La huella del certificado de srv:8443 cambió: puede que el servidor renovara su certificado. Huella SHA-256: ${FP}`),
    );
    render(<ConnectionSection />);
    fireEvent.click(screen.getByRole("radio", { name: "Remoto" }));
    fill();
    fireEvent.click(screen.getByRole("button", { name: "Guardar y conectar" }));
    const box = await screen.findByTestId("tls-trust");
    expect(box).toHaveAttribute("data-changed", "1");
    expect(box).toHaveTextContent("El certificado del servidor cambió");
    expect(screen.getByTestId("tls-fingerprint")).toHaveTextContent(FP);
    expect(screen.getByTestId("tls-fingerprint")).toHaveClass("break-all");
  });

  it("un error sin huella sigue siendo un error normal", async () => {
    (window as unknown as { chrome: unknown }).chrome = { webview: { postMessage: () => undefined } };
    vi.spyOn(desktop, "keepRunningInBackground").mockResolvedValue(false);
    saveRemote.mockRejectedValueOnce(new Error("Usuario o clave incorrectos."));
    render(<ConnectionSection />);
    fireEvent.click(screen.getByRole("radio", { name: "Remoto" }));
    fill();
    fireEvent.click(screen.getByRole("button", { name: "Guardar y conectar" }));
    expect(await screen.findByTestId("conn-error")).toHaveTextContent("Usuario o clave incorrectos.");
    expect(screen.queryByTestId("tls-trust")).not.toBeInTheDocument();
  });

  it("con servidor remoto activo: muestra la huella guardada, «Usar este equipo» y «Cerrar sesión»", async () => {
    setConn({ mode: "remote", baseUrl: "https://srv:8443", user: "ana", tlsFingerprint: "11:22", ready: true, link: "connected" });
    const { unmount } = render(<ConnectionSection />);
    expect(screen.getByTestId("conn-saved-fp")).toHaveTextContent("11:22");
    fireEvent.click(screen.getByRole("radio", { name: "Remoto" }));
    expect(screen.getByLabelText("Usuario")).toHaveValue("ana");
    fireEvent.click(screen.getByRole("button", { name: "Cerrar sesión" }));
    expect(logout).toHaveBeenCalled();
    unmount();
    // en modo Local elegido con un remoto activo
    setConn({ mode: "remote" });
    render(<ConnectionSection />);
    fireEvent.click(screen.getByRole("radio", { name: "Local" }));
    fireEvent.click(screen.getByRole("button", { name: "Usar este equipo" }));
    await waitFor(() => expect(useLocal).toHaveBeenCalled());
  });

  it("«Seguir en segundo plano» solo existe en el escritorio y guarda el cambio", async () => {
    const { unmount } = render(<ConnectionSection />);
    expect(screen.queryByRole("switch", { name: "Seguir en segundo plano" })).not.toBeInTheDocument();
    unmount();
    (window as unknown as { chrome: unknown }).chrome = { webview: { postMessage: () => undefined } };
    vi.spyOn(desktop, "keepRunningInBackground").mockResolvedValue(false);
    const set = vi.spyOn(desktop, "setKeepRunningInBackground").mockResolvedValue();
    render(<ConnectionSection />);
    const sw = await screen.findByRole("switch", { name: "Seguir en segundo plano" });
    fireEvent.click(sw);
    await waitFor(() => expect(set).toHaveBeenCalledWith(true));
    expect(sw).toHaveAttribute("aria-checked", "true");
  });

  it("los campos y botones se adaptan a 360 px (columnas que apilan, botones que bajan de línea)", () => {
    render(<ConnectionSection />);
    fireEvent.click(screen.getByRole("radio", { name: "Remoto" }));
    expect(screen.getByLabelText("Usuario").closest(".grid")).toHaveClass("grid-cols-1");
    expect(screen.getByRole("button", { name: "Probar" }).parentElement).toHaveClass("flex-wrap");
  });
});

describe("estado del escritorio en la banda y en «Sin conexión»", () => {
  const desk = (state: string, extra: object = {}) => ({ mode: "local" as const, state: state as never, error: "", retryInMs: 0, at: Date.now(), ...extra });

  it("la banda muestra «Reiniciando…» y la cuenta atrás, aunque el enlace aún figure conectado", () => {
    vi.useFakeTimers();
    try {
      setConn({ ready: true, link: "connected", desktop: desk("restarting", { retryInMs: 5000 }) });
      render(<ConnectionBanner />);
      expect(screen.getByTestId("conn-banner")).toHaveTextContent("Reiniciando el servidor local… Reintentando en 5 s.");
      act(() => {
        vi.advanceTimersByTime(2000);
      });
      expect(screen.getByTestId("conn-banner")).toHaveTextContent("Reintentando en 3 s.");
      expect(screen.getByTestId("conn-banner")).toHaveAttribute("data-link", "connected");
      setConn({ desktop: desk("ready") });
      expect(screen.queryByTestId("conn-banner")).not.toBeInTheDocument();
    } finally {
      vi.useRealTimers();
    }
  });

  it("la banda puede bajar de línea (360 px)", () => {
    setConn({ ready: true, link: "offline", desktop: desk("unavailable", { error: "no arrancó" }) });
    render(<ConnectionBanner />);
    expect(screen.getByTestId("conn-banner")).toHaveTextContent("El servidor local no está disponible.");
    expect(screen.getByTestId("desktop-status").parentElement).toHaveClass("break-words");
  });

  it("«Sin conexión» muestra el estado del escritorio", () => {
    setConn({ link: "offline", error: "No arrancó.", desktop: desk("stopped") });
    render(<OfflineScreen />);
    expect(screen.getByTestId("desktop-status")).toHaveTextContent("El servidor local está detenido.");
    expect(screen.getByTestId("offline-error")).toHaveTextContent("No arrancó.");
  });

  it("«Sin conexión» por un certificado sin confirmar: huella, «Confiar en este certificado» y reintento", async () => {
    (window as unknown as { chrome: unknown }).chrome = { webview: { postMessage: () => undefined } };
    const trust = vi.spyOn(desktop, "tlsTrust").mockResolvedValue();
    setConn({ mode: "remote", link: "offline", error: `La huella del certificado de srv:8443 cambió. Huella SHA-256: ${FP}` });
    render(<OfflineScreen />);
    expect(screen.getByTestId("offline-error")).toHaveTextContent("No se pudo conectar de forma segura.");
    expect(screen.getByTestId("tls-trust")).toHaveAttribute("data-changed", "1");
    fireEvent.click(screen.getByRole("button", { name: "Confiar en este certificado" }));
    await waitFor(() => expect(trust).toHaveBeenCalledWith(FP));
    await waitFor(() => expect(retry).toHaveBeenCalledTimes(1));
  });

  it("si confiar falla muestra el motivo y no reintenta", async () => {
    (window as unknown as { chrome: unknown }).chrome = { webview: { postMessage: () => undefined } };
    vi.spyOn(desktop, "tlsTrust").mockRejectedValue(new Error("La huella no coincide con la del último intento."));
    setConn({ mode: "remote", link: "offline", error: `Huella SHA-256: ${FP}` });
    render(<OfflineScreen />);
    fireEvent.click(screen.getByRole("button", { name: "Confiar en este certificado" }));
    expect(await screen.findByTestId("tls-error")).toHaveTextContent("La huella no coincide");
    expect(retry).not.toHaveBeenCalled();
  });
});
