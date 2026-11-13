import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const svc = vi.hoisted(() => ({
  AccountsService: {
    List: vi.fn(),
    Create: vi.fn(),
    BeginPairing: vi.fn(),
    SubmitPairing: vi.fn(),
    Connect: vi.fn(),
    Disconnect: vi.fn(),
    Delete: vi.fn(),
    Logout: vi.fn(),
    Reorder: vi.fn(),
  },
  InboxService: {},
  MessagingService: {},
  StatusesService: {},
  ContactsService: {},
  CampaignsService: {},
  ScheduledService: {},
  StatsService: {},
  DestinationsService: {},
  SettingsService: { Get: vi.fn() },
  FilesService: {},
}));

vi.mock("@/api/transport", async () => (await import("@/test/svcTransport")).svcTransport(svc));
vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(() => () => undefined) } }));

import type { AccountStatus, AccountView, CapSet } from "@/api/types";
import { useAccounts } from "@/store/accounts";
import { useUI } from "@/store/ui";
import AccountsPage from "@/pages/AccountsPage";
import AddAccountWizard from "./AddAccountWizard";
import PairingFlow from "./PairingFlow";

const status = (id: string, state: string, extra: Record<string, unknown> = {}) =>
  ({ accountId: id, state, need: "", since: 1, ...extra }) as unknown as AccountStatus;

const view = (id: string, channel: string, label: string, state = "connected", extra: Record<string, unknown> = {}): AccountView =>
  ({
    account: { id, channel, label, color: "#25D366", enabled: true, selfName: "Mi nombre", selfHandle: "+593990000000", config: null },
    status: status(id, state, extra),
    capabilities: {} as CapSet,
    unread: 0,
  }) as unknown as AccountView;

function seed(views: AccountView[]) {
  const map: Record<string, AccountView> = {};
  views.forEach((v) => (map[v.account.id] = v));
  act(() => useAccounts.setState({ views: map, order: views.map((v) => v.account.id), loaded: true }));
}

const channelBtn = (n: string) => screen.getByRole("button", { name: new RegExp(`^${n}`) });

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  svc.AccountsService.List.mockResolvedValue([]);
  svc.AccountsService.BeginPairing.mockResolvedValue(undefined);
  svc.AccountsService.SubmitPairing.mockResolvedValue(undefined);
  useUI.setState({ toasts: [], confirmReq: null });
  seed([]);
});
afterEach(cleanup);

describe("PairingFlow · WhatsApp", () => {
  it("pide el QR y lo pinta cuando el adaptador lo entrega", async () => {
    seed([view("wa-1", "whatsapp", "Ventas", "pairing", { need: "scan_qr", qrCode: "2@abcdef" })]);
    render(<PairingFlow accountId="wa-1" channel="whatsapp" />);
    await waitFor(() => expect(svc.AccountsService.BeginPairing).toHaveBeenCalledWith("wa-1", { method: "qr" }));
    expect(screen.getByTestId("qr")).toBeTruthy();
    expect(screen.getByRole("tab", { name: "Código QR" })).toBeTruthy();
    expect(screen.getByRole("tab", { name: "Con número" })).toBeTruthy();
  });

  it("«Con número»: teléfono → código XXXX-XXXX en grande con 3 pasos", async () => {
    seed([view("wa-1", "whatsapp", "Ventas", "pairing", { need: "scan_qr", qrCode: "x" })]);
    render(<PairingFlow accountId="wa-1" channel="whatsapp" />);
    fireEvent.click(screen.getByRole("tab", { name: "Con número" }));
    fireEvent.change(screen.getByLabelText("Número de teléfono"), { target: { value: "+593 99 111 2222" } });
    fireEvent.click(screen.getByRole("button", { name: "Obtener código" }));
    await waitFor(() =>
      expect(svc.AccountsService.BeginPairing).toHaveBeenCalledWith("wa-1", { method: "phone", phone: "+593 99 111 2222" }),
    );
    // El adaptador responde con el código.
    act(() => useAccounts.setState((s) => ({
      views: { ...s.views, "wa-1": { ...s.views["wa-1"], status: status("wa-1", "pairing", { need: "enter_code", pairCode: "ABCD-EFGH" }) } },
    })));
    await waitFor(() => expect(screen.getByTestId("pair-code").textContent).toBe("ABCD-EFGH"));
    expect(screen.getAllByRole("listitem")).toHaveLength(3);
  });

  it("avisa una sola vez cuando queda conectada", async () => {
    seed([view("wa-1", "whatsapp", "Ventas", "pairing", { need: "scan_qr", qrCode: "x" })]);
    const onConnected = vi.fn();
    render(<PairingFlow accountId="wa-1" channel="whatsapp" onConnected={onConnected} />);
    act(() => useAccounts.setState((s) => ({ views: { ...s.views, "wa-1": { ...s.views["wa-1"], status: status("wa-1", "connected") } } })));
    await waitFor(() => expect(onConnected).toHaveBeenCalledTimes(1));
  });
});

describe("PairingFlow · Telegram", () => {
  it("teléfono → código → contraseña", async () => {
    seed([view("tg-1", "telegram", "TG", "pairing", { need: "phone" })]);
    render(<PairingFlow accountId="tg-1" channel="telegram" />);

    fireEvent.change(screen.getByLabelText("Número de teléfono"), { target: { value: "+593991112222" } });
    fireEvent.click(screen.getByRole("button", { name: "Enviar código" }));
    await waitFor(() =>
      expect(svc.AccountsService.BeginPairing).toHaveBeenCalledWith("tg-1", { method: "login", phone: "+593991112222" }),
    );

    act(() => useAccounts.setState((s) => ({ views: { ...s.views, "tg-1": { ...s.views["tg-1"], status: status("tg-1", "pairing", { need: "login_code" }) } } })));
    const code = await screen.findByLabelText("Código recibido");
    fireEvent.change(code, { target: { value: "12345" } });
    fireEvent.click(screen.getByRole("button", { name: "Continuar" }));
    await waitFor(() => expect(svc.AccountsService.SubmitPairing).toHaveBeenCalledWith("tg-1", { code: "12345" }));

    act(() => useAccounts.setState((s) => ({ views: { ...s.views, "tg-1": { ...s.views["tg-1"], status: status("tg-1", "pairing", { need: "password" }) } } })));
    const pass = await screen.findByLabelText("Contraseña");
    fireEvent.change(pass, { target: { value: "secreto" } });
    fireEvent.click(screen.getByRole("button", { name: "Continuar" }));
    await waitFor(() => expect(svc.AccountsService.SubmitPairing).toHaveBeenCalledWith("tg-1", { password: "secreto" }));
  });

  it("muestra el detalle del adaptador (código incorrecto)", () => {
    seed([view("tg-1", "telegram", "TG", "pairing", { need: "login_code", detail: "Código incorrecto" })]);
    render(<PairingFlow accountId="tg-1" channel="telegram" />);
    expect(screen.getByRole("alert").textContent).toBe("Código incorrecto");
  });
});

describe("PairingFlow · Meta", () => {
  it("cookies: caja grande, ayuda plegable y envío", async () => {
    seed([view("fb-1", "instagram", "IG", "pairing", { need: "cookies" })]);
    render(<PairingFlow accountId="fb-1" channel="instagram" />);
    expect(screen.getByRole("tab", { name: "Usuario y contraseña" })).toBeTruthy(); // también en Instagram
    fireEvent.click(screen.getByRole("tab", { name: "Pegar cookies" }));
    const box = screen.getByPlaceholderText("Pega aquí las cookies");
    expect(screen.getByText("Cómo copiar las cookies")).toBeTruthy();
    fireEvent.change(box, { target: { value: "sessionid=1; ds_user_id=2" } });
    fireEvent.click(screen.getByRole("button", { name: "Vincular" }));
    await waitFor(() => expect(svc.AccountsService.SubmitPairing).toHaveBeenCalledWith("fb-1", { cookies: "sessionid=1; ds_user_id=2" }));
  });

  it("Messenger: «Usuario y contraseña» y, si no se admite, vuelve a cookies", async () => {
    seed([view("fb-2", "messenger", "MS", "pairing", { need: "cookies" })]);
    svc.AccountsService.BeginPairing.mockRejectedValue(new Error("no admitido por este canal"));
    render(<PairingFlow accountId="fb-2" channel="messenger" />);
    fireEvent.click(screen.getByRole("tab", { name: "Usuario y contraseña" }));
    fireEvent.change(screen.getByLabelText("Usuario"), { target: { value: "yo" } });
    fireEvent.change(screen.getByLabelText("Contraseña"), { target: { value: "pw" } });
    fireEvent.click(screen.getByRole("button", { name: "Iniciar sesión" }));
    await waitFor(() => expect(screen.getByPlaceholderText("Pega aquí las cookies")).toBeTruthy());
    expect(screen.queryByRole("tab", { name: "Usuario y contraseña" })).toBeNull();
    expect(svc.AccountsService.SubmitPairing).not.toHaveBeenCalled();
  });
});

describe("AddAccountWizard", () => {
  it("muestra los 4 canales", () => {
    render(<AddAccountWizard open onClose={() => undefined} />);
    for (const n of ["WhatsApp", "Telegram", "Messenger", "Instagram"]) expect(channelBtn(n)).toBeTruthy();
  });

  it("Meta exige la casilla «Entiendo» antes de continuar", async () => {
    render(<AddAccountWizard open onClose={() => undefined} />);
    fireEvent.click(channelBtn("Messenger"));
    fireEvent.change(screen.getByLabelText("Etiqueta"), { target: { value: "Soporte" } });
    expect(screen.getByText("Vía no oficial: Meta puede bloquear la cuenta")).toBeTruthy();
    const next = screen.getByRole("button", { name: "Continuar" }) as HTMLButtonElement;
    expect(next.disabled).toBe(true);
    fireEvent.click(screen.getByLabelText("Entiendo"));
    expect(next.disabled).toBe(false);
  });

  it("Telegram pide api_id y api_hash y los manda en la configuración", async () => {
    svc.AccountsService.Create.mockResolvedValue({ id: "tg-9" });
    svc.AccountsService.List.mockResolvedValue([view("tg-9", "telegram", "Mi TG", "pairing", { need: "phone" })]);
    render(<AddAccountWizard open onClose={() => undefined} />);
    fireEvent.click(channelBtn("Telegram"));
    fireEvent.change(screen.getByLabelText("Etiqueta"), { target: { value: "Mi TG" } });
    const next = screen.getByRole("button", { name: "Continuar" }) as HTMLButtonElement;
    expect(next.disabled).toBe(true);
    expect(screen.getByText("my.telegram.org").closest("a")?.getAttribute("href")).toBe("https://my.telegram.org");
    fireEvent.change(screen.getByLabelText("api_id"), { target: { value: "12345" } });
    fireEvent.change(screen.getByLabelText("api_hash"), { target: { value: "abcdef" } });
    expect(next.disabled).toBe(false);
    fireEvent.click(next);
    await waitFor(() =>
      expect(svc.AccountsService.Create).toHaveBeenCalledWith("telegram", "Mi TG", expect.stringMatching(/^#/), {
        api_id: "12345",
        api_hash: "abcdef",
      }),
    );
    // Pasa a la vinculación: pide el teléfono.
    expect(await screen.findByLabelText("Número de teléfono")).toBeTruthy();
  });

  it("guarda borrador y lo restaura al volver a abrir", () => {
    const { unmount } = render(<AddAccountWizard open onClose={() => undefined} />);
    fireEvent.click(channelBtn("WhatsApp"));
    fireEvent.change(screen.getByLabelText("Etiqueta"), { target: { value: "Borrador" } });
    expect(JSON.parse(localStorage.getItem("draft:accounts:new")!).label).toBe("Borrador");
    unmount();
    render(<AddAccountWizard open onClose={() => undefined} />);
    expect((screen.getByLabelText("Etiqueta") as HTMLInputElement).value).toBe("Borrador");
  });

  it("al conectarse muestra «Conectada ✓» y «Ir a chats»", async () => {
    svc.AccountsService.Create.mockResolvedValue({ id: "wa-7" });
    const connected = view("wa-7", "whatsapp", "Ventas", "connected");
    svc.AccountsService.List.mockResolvedValue([connected]);
    render(<AddAccountWizard open onClose={() => undefined} />);
    fireEvent.click(channelBtn("WhatsApp"));
    fireEvent.change(screen.getByLabelText("Etiqueta"), { target: { value: "Ventas" } });
    fireEvent.click(screen.getByRole("button", { name: "Continuar" }));
    expect(await screen.findByText("Conectada ✓")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Ir a chats" })).toBeTruthy();
  });
});

describe("AccountsPage", () => {
  it("sin cuentas: vacío corto con botón", () => {
    render(<AccountsPage />);
    expect(screen.getByText("Sin cuentas")).toBeTruthy();
  });

  it("tarjetas con estado en vivo, en el orden del store", () => {
    seed([
      view("wa-1", "whatsapp", "Ventas", "connected"),
      view("tg-1", "telegram", "Soporte", "error", { detail: "Sin red" }),
    ]);
    render(<AccountsPage />);
    const cards = screen.getAllByTestId("account-card");
    expect(cards.map((c) => c.getAttribute("data-account-id"))).toEqual(["wa-1", "tg-1"]);
    expect(cards[0].textContent).toContain("Conectada");
    expect(cards[1].textContent).toContain("Error");
    expect(cards[1].textContent).toContain("Sin red");
    // Un evento cambia el estado en vivo.
    act(() => useAccounts.setState((s) => ({ views: { ...s.views, "tg-1": { ...s.views["tg-1"], status: status("tg-1", "connected") } } })));
    expect(screen.getAllByTestId("account-state")[1].textContent).toContain("Conectada");
  });

  it("Conectar / Desconectar según el estado", async () => {
    seed([view("wa-1", "whatsapp", "Ventas", "connected"), view("wa-2", "whatsapp", "Otra", "disconnected")]);
    svc.AccountsService.Disconnect.mockResolvedValue(undefined);
    svc.AccountsService.Connect.mockResolvedValue(undefined);
    render(<AccountsPage />);
    fireEvent.click(screen.getByRole("button", { name: /Desconectar/ }));
    await waitFor(() => expect(svc.AccountsService.Disconnect).toHaveBeenCalledWith("wa-1"));
    fireEvent.click(screen.getByRole("button", { name: /^Conectar/ }));
    await waitFor(() => expect(svc.AccountsService.Connect).toHaveBeenCalledWith("wa-2"));
  });

  it("Eliminar exige escribir la etiqueta", async () => {
    seed([view("wa-1", "whatsapp", "Ventas", "connected")]);
    svc.AccountsService.Delete.mockResolvedValue(undefined);
    const { container } = render(<AccountsPage />);
    fireEvent.click(screen.getByLabelText("Más acciones"));
    fireEvent.click(screen.getByRole("menuitem", { name: /Eliminar/ }));
    // El diálogo global lo pinta el Shell: aquí se resuelve la petición escribiendo la etiqueta.
    await waitFor(() => expect(useUI.getState().confirmReq).not.toBeNull());
    expect(useUI.getState().confirmReq?.options.requireText).toBe("Ventas");
    useUI.getState().resolveConfirm(true);
    await waitFor(() => expect(svc.AccountsService.Delete).toHaveBeenCalledWith("wa-1"));
    expect(container).toBeTruthy();
  });

  it("Cerrar sesión pide confirmación y, si se cancela, no hace nada", async () => {
    seed([view("wa-1", "whatsapp", "Ventas", "connected")]);
    render(<AccountsPage />);
    fireEvent.click(screen.getByLabelText("Más acciones"));
    fireEvent.click(screen.getByRole("menuitem", { name: /Cerrar sesión/ }));
    await waitFor(() => expect(useUI.getState().confirmReq).not.toBeNull());
    useUI.getState().resolveConfirm(false);
    await new Promise((r) => setTimeout(r, 20));
    expect(svc.AccountsService.Logout).not.toHaveBeenCalled();
  });

  it("reordenar con el teclado llama a Reorder", async () => {
    seed([view("a", "whatsapp", "A"), view("b", "telegram", "B")]);
    svc.AccountsService.Reorder.mockResolvedValue(undefined);
    render(<AccountsPage />);
    const handles = screen.getAllByLabelText("Arrastrar para ordenar");
    fireEvent.keyDown(handles[1], { key: "ArrowUp" });
    await waitFor(() => expect(svc.AccountsService.Reorder).toHaveBeenCalledWith(["b", "a"]));
  });
});
