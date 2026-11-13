import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const svc = vi.hoisted(() => ({
  AccountsService: {},
  InboxService: {},
  MessagingService: {},
  StatusesService: {},
  ContactsService: {},
  CampaignsService: {},
  ScheduledService: {},
  StatsService: {},
  DestinationsService: {},
  SettingsService: {
    Get: vi.fn(),
    Save: vi.fn(),
    DataDir: vi.fn(),
    OpenDataDir: vi.fn(),
    OpenLogsDir: vi.fn(),
    Version: vi.fn(),
  },
  FilesService: {},
  NotificationsService: {
    Status: vi.fn(),
    RequestPermission: vi.fn(),
    SendTest: vi.fn(),
    OpenSystemSettings: vi.fn(),
    MarkAsked: vi.fn(),
    SetFocus: vi.fn(),
  },
}));

vi.mock("@/api/transport", async () => (await import("@/test/svcTransport")).svcTransport(svc));
vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(() => () => undefined) } }));

import type { AppSettings } from "@/api/types";
import { NOTIFY_REFRESH_MS, formatDecisionTime } from "@/components/settings/NotificationsSection";
import SettingsPage from "@/pages/SettingsPage";
import { useUI } from "@/store/ui";

const base = (): AppSettings => ({
  closeToTray: true,
  startWithWindows: false,
  startMinimized: false,
  notifications: true,
  notificationsSound: true,
  notificationsPreview: true,
  theme: "system",
  verboseLog: false,
  autoHistory: true,
  autoDownload: {
    image: { enabled: true, maxMB: 20 },
    audio: { enabled: true, maxMB: 20 },
    voice: { enabled: true, maxMB: 20 },
    video: { enabled: false, maxMB: 50 },
    document: { enabled: false, maxMB: 20 },
    sticker: { enabled: true, maxMB: 5 },
  },
  optOutKeywords: "baja,stop",
  optOutFooterText: "Responde BAJA",
});

const section = (name: string) => fireEvent.click(screen.getByRole("button", { name }));
const sw = (name: string) => screen.getByRole("switch", { name });
const loaded = () => screen.findByRole("region", { name: "General" });

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  svc.SettingsService.Get.mockResolvedValue(base());
  svc.SettingsService.Save.mockResolvedValue(undefined);
  svc.SettingsService.DataDir.mockResolvedValue("C:\\Users\\yo\\AppData\\dConnector");
  svc.SettingsService.Version.mockResolvedValue("0.1.0");
  svc.SettingsService.OpenDataDir.mockResolvedValue(undefined);
  svc.SettingsService.OpenLogsDir.mockResolvedValue(undefined);
  svc.NotificationsService.Status.mockResolvedValue({ state: "granted", reason: "Windows permite las notificaciones de dConnector.", listening: true, asked: true });
  svc.NotificationsService.OpenSystemSettings.mockResolvedValue(undefined);
  useUI.setState({ toasts: [], theme: "system" });
});
afterEach(cleanup);

describe("SettingsPage", () => {
  it("carga los ajustes y no muestra la barra de guardar hasta que hay cambios", async () => {
    render(<SettingsPage />);
    await loaded();
    expect(sw("Cerrar a la bandeja")).toHaveAttribute("aria-checked", "true");
    expect(sw("Iniciar con Windows")).toHaveAttribute("aria-checked", "false");
    expect(screen.queryByTestId("save-bar")).toBeNull();

    fireEvent.click(sw("Iniciar con Windows"));
    expect(screen.getByTestId("save-bar")).toHaveTextContent("Cambios sin guardar");
  });

  it("Guardar manda el AppSettings completo y aplica el tema", async () => {
    render(<SettingsPage />);
    await loaded();
    fireEvent.click(screen.getByRole("radio", { name: "Oscuro" }));
    section("Notificaciones");
    fireEvent.click(sw("Notificaciones de escritorio"));
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));

    await waitFor(() => expect(svc.SettingsService.Save).toHaveBeenCalledTimes(1));
    expect(svc.SettingsService.Save.mock.calls[0][0]).toEqual({ ...base(), notifications: false, theme: "dark" });
    expect(useUI.getState().theme).toBe("dark");
    await waitFor(() => expect(screen.queryByTestId("save-bar")).toBeNull());
    expect(useUI.getState().toasts.some((t) => t.message === "Ajustes guardados")).toBe(true);
  });

  it("«Guardar el historial automáticamente» sigue al ajuste y siempre se envía en Save", async () => {
    const LABEL = "Guardar el historial automáticamente (despacio, en segundo plano)";
    svc.SettingsService.Get.mockResolvedValue({ ...base(), autoHistory: false });
    render(<SettingsPage />);
    await loaded();
    expect(sw(LABEL)).toHaveAttribute("aria-checked", "false");
    fireEvent.click(sw(LABEL));
    expect(sw(LABEL)).toHaveAttribute("aria-checked", "true");
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(svc.SettingsService.Save).toHaveBeenCalledTimes(1));
    expect(svc.SettingsService.Save.mock.calls[0][0].autoHistory).toBe(true);
  });

  it("un ajuste sin autoHistory (versión vieja) se muestra encendido y se envía como booleano", async () => {
    const { autoHistory: _omit, ...old } = base();
    svc.SettingsService.Get.mockResolvedValue(old);
    render(<SettingsPage />);
    await loaded();
    section("Notificaciones");
    fireEvent.click(sw("Notificaciones de escritorio"));
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(svc.SettingsService.Save).toHaveBeenCalledTimes(1));
    expect(typeof svc.SettingsService.Save.mock.calls[0][0].autoHistory).toBe("boolean");
  });

  it("Descartar vuelve a lo guardado", async () => {
    render(<SettingsPage />);
    await loaded();
    fireEvent.click(sw("Cerrar a la bandeja"));
    expect(sw("Cerrar a la bandeja")).toHaveAttribute("aria-checked", "false");
    fireEvent.click(screen.getByRole("button", { name: "Descartar" }));
    expect(sw("Cerrar a la bandeja")).toHaveAttribute("aria-checked", "true");
    expect(svc.SettingsService.Save).not.toHaveBeenCalled();
    expect(useUI.getState().theme).toBe("system");
  });

  it("descargas automáticas: un interruptor y un tamaño por tipo", async () => {
    render(<SettingsPage />);
    await loaded();
    section("Descargas automáticas");
    expect(screen.getByRole("region", { name: "Descargas automáticas" })).toBeInTheDocument();
    for (const t of ["Fotos", "Audios", "Notas de voz", "Vídeos", "Documentos", "Stickers"]) {
      expect(screen.getByLabelText(`Tamaño máximo de ${t} en MB`)).toBeInTheDocument();
    }
    // el tamaño de lo desactivado no se edita
    expect(screen.getByLabelText("Tamaño máximo de Vídeos en MB")).toBeDisabled();
    fireEvent.click(sw("Descargar vídeos"));
    const mb = screen.getByLabelText("Tamaño máximo de Vídeos en MB");
    expect(mb).not.toBeDisabled();
    fireEvent.change(mb, { target: { value: "120" } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));

    await waitFor(() => expect(svc.SettingsService.Save).toHaveBeenCalled());
    expect(svc.SettingsService.Save.mock.calls[0][0].autoDownload.video).toEqual({ enabled: true, maxMB: 120 });
  });

  it("un tamaño inválido bloquea Guardar y lo dice", async () => {
    render(<SettingsPage />);
    await loaded();
    section("Descargas automáticas");
    fireEvent.change(screen.getByLabelText("Tamaño máximo de Fotos en MB"), { target: { value: "0" } });
    expect(screen.getByTestId("save-bar")).toHaveTextContent(/Fotos/);
    expect(screen.getByRole("button", { name: "Guardar" })).toBeDisabled();
    expect(svc.SettingsService.Save).not.toHaveBeenCalled();
  });

  it("campañas: palabras de baja y texto del pie", async () => {
    render(<SettingsPage />);
    await loaded();
    section("Campañas");
    expect(screen.getByLabelText("Palabras de baja")).toHaveValue("baja,stop");
    fireEvent.change(screen.getByLabelText("Palabras de baja"), { target: { value: "baja, stop, ya no" } });
    fireEvent.change(screen.getByLabelText("Texto del pie"), { target: { value: "Escribe BAJA" } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(svc.SettingsService.Save).toHaveBeenCalled());
    expect(svc.SettingsService.Save.mock.calls[0][0]).toMatchObject({ optOutKeywords: "baja,stop,ya no", optOutFooterText: "Escribe BAJA" });
  });

  it("datos: carpeta, abrir carpeta, abrir registros y registro detallado", async () => {
    render(<SettingsPage />);
    await loaded();
    section("Datos");
    await waitFor(() => expect(screen.getByTestId("data-dir")).toHaveTextContent("C:\\Users\\yo\\AppData\\dConnector"));
    expect(screen.getByTestId("data-dir")).toHaveAttribute("title", "C:\\Users\\yo\\AppData\\dConnector");
    fireEvent.click(screen.getByRole("button", { name: "Abrir carpeta" }));
    fireEvent.click(screen.getByRole("button", { name: /Abrir registros/ }));
    await waitFor(() => expect(svc.SettingsService.OpenDataDir).toHaveBeenCalledTimes(1));
    expect(svc.SettingsService.OpenLogsDir).toHaveBeenCalledTimes(1);

    fireEvent.click(sw("Registro detallado"));
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(svc.SettingsService.Save).toHaveBeenCalled());
    expect(svc.SettingsService.Save.mock.calls[0][0].verboseLog).toBe(true);
  });

  it("acerca de: versión, protocolo v1 y licencias", async () => {
    render(<SettingsPage />);
    await loaded();
    section("Acerca de");
    await waitFor(() => expect(screen.getByTestId("app-version")).toHaveTextContent("0.1.0"));
    expect(screen.getByText("v1")).toBeInTheDocument();
    const list = within(screen.getByRole("list"));
    for (const [n, l] of [
      ["whatsmeow", "MPL-2.0"],
      ["gotd", "MIT"],
      ["messagix", "AGPL-3.0"],
      ["Wails", "MIT"],
      ["modernc sqlite", "BSD-3-Clause"],
    ]) {
      expect(list.getByText(n)).toBeInTheDocument();
      expect(list.getAllByText(l).length).toBeGreaterThan(0);
    }
  });

  it("los cambios sin guardar quedan como borrador y se restauran", async () => {
    const first = render(<SettingsPage />);
    await loaded();
    fireEvent.click(sw("Iniciar minimizado"));
    expect(JSON.parse(localStorage.getItem("draft:settings:main")!).startMinimized).toBe(true);
    first.unmount();

    render(<SettingsPage />);
    await loaded();
    expect(sw("Iniciar minimizado")).toHaveAttribute("aria-checked", "true");
    expect(screen.getByTestId("save-bar")).toBeInTheDocument();
    // guardar borra el borrador
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(svc.SettingsService.Save).toHaveBeenCalled());
    await waitFor(() => expect(localStorage.getItem("draft:settings:main")).toBeNull());
  });

  it("un borrador incompleto o raro no rompe la pantalla", async () => {
    localStorage.setItem("draft:settings:main", JSON.stringify({ closeToTray: true }));
    render(<SettingsPage />);
    await loaded();
    expect(sw("Iniciar con Windows")).toHaveAttribute("aria-checked", "false");
  });

  it("si Guardar falla no se pierden los cambios", async () => {
    svc.SettingsService.Save.mockRejectedValue(new Error("disco lleno"));
    render(<SettingsPage />);
    await loaded();
    section("Notificaciones");
    fireEvent.click(sw("Notificaciones de escritorio"));
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(useUI.getState().toasts.some((t) => t.message === "disco lleno")).toBe(true));
    expect(screen.getByTestId("save-bar")).toBeInTheDocument();
    expect(sw("Notificaciones de escritorio")).toHaveAttribute("aria-checked", "false");
    expect(useUI.getState().theme).toBe("system");
  });
});

describe("SettingsPage · Notificaciones", () => {
  const open = async () => {
    render(<SettingsPage />);
    await loaded();
    section("Notificaciones");
    return screen.findByRole("region", { name: "Notificaciones" });
  };

  it("muestra los tres interruptores con lo guardado y la frase explicativa", async () => {
    svc.SettingsService.Get.mockResolvedValue({ ...base(), notificationsSound: false });
    await open();
    expect(sw("Notificaciones de escritorio")).toHaveAttribute("aria-checked", "true");
    expect(sw("Sonido")).toHaveAttribute("aria-checked", "false");
    expect(sw("Mostrar el texto del mensaje")).toHaveAttribute("aria-checked", "true");
    expect(
      screen.getByText("Si las notificaciones están apagadas o bloqueadas, solo sonará un aviso. Los chats silenciados no avisan."),
    ).toBeInTheDocument();
  });

  it("Guardar envía sonido y vista previa, también apagados", async () => {
    await open();
    fireEvent.click(sw("Sonido"));
    fireEvent.click(sw("Mostrar el texto del mensaje"));
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(svc.SettingsService.Save).toHaveBeenCalledTimes(1));
    expect(svc.SettingsService.Save.mock.calls[0][0]).toMatchObject({
      notifications: true,
      notificationsSound: false,
      notificationsPreview: false,
    });
  });

  it("concedido: insignia verde con el motivo y sin botón de abrir Windows", async () => {
    await open();
    await waitFor(() => expect(screen.getByTestId("notify-badge")).toHaveTextContent("Concedido"));
    expect(screen.getByTestId("notify-reason")).toHaveTextContent("Windows permite las notificaciones de dConnector.");
    expect(screen.queryByRole("button", { name: "Abrir ajustes de Windows" })).toBeNull();
    expect(screen.getByRole("button", { name: "Probar notificación" })).toBeInTheDocument();
  });

  it("bloqueado: insignia, motivo y botón «Abrir ajustes de Windows»", async () => {
    svc.NotificationsService.Status.mockResolvedValue({
      state: "denied",
      reason: "Las notificaciones de dConnector están bloqueadas en Windows.",
      listening: true,
      asked: true,
    });
    await open();
    await waitFor(() => expect(screen.getByTestId("notify-badge")).toHaveTextContent("Bloqueado en Windows"));
    expect(screen.getByTestId("notify-reason")).toHaveTextContent("bloqueadas en Windows");
    fireEvent.click(screen.getByRole("button", { name: "Abrir ajustes de Windows" }));
    await waitFor(() => expect(svc.NotificationsService.OpenSystemSettings).toHaveBeenCalledTimes(1));
  });

  it("estado desconocido o no admitido", async () => {
    svc.NotificationsService.Status.mockResolvedValue({ state: "unknown", reason: "No se pudo leer.", listening: true, asked: true });
    await open();
    await waitFor(() => expect(screen.getByTestId("notify-badge")).toHaveTextContent("Desconocido"));
    cleanup();
    svc.NotificationsService.Status.mockResolvedValue({ state: "unsupported", reason: "Solo en Windows.", listening: false, asked: true });
    await open();
    await waitFor(() => expect(screen.getByTestId("notify-badge")).toHaveTextContent("No disponible"));
  });

  it("si el estado no se puede leer lo dice y la pantalla sigue funcionando", async () => {
    svc.NotificationsService.Status.mockRejectedValue(new Error("sin servicio"));
    await open();
    await waitFor(() => expect(screen.getByText("No se pudo leer")).toBeInTheDocument());
    expect(sw("Sonido")).toBeInTheDocument();
  });

  const status = (over: Record<string, unknown> = {}) => ({
    state: "granted",
    reason: "Windows permite las notificaciones de dConnector.",
    listening: true,
    asked: true,
    quiet: "inactive",
    quietCause: "",
    quietReason: "",
    suppressed: false,
    lastDecision: "",
    lastDecisionAt: 0,
    testMessage: "",
    testShown: false,
    testVisible: false,
    testSound: false,
    ...over,
  });

  const DND =
    "Windows tiene activado «No molestar»: los avisos no aparecen en pantalla (se guardan en el centro de notificaciones). Los sonidos de dConnector siguen sonando.";

  it("«Probar notificación» sin supresión dice «Aviso enviado»", async () => {
    svc.NotificationsService.SendTest.mockResolvedValue(status({ testMessage: "Aviso enviado", testShown: true, testVisible: true, testSound: true }));
    await open();
    fireEvent.click(screen.getByRole("button", { name: "Probar notificación" }));
    await waitFor(() => expect(svc.NotificationsService.SendTest).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(useUI.getState().toasts.some((t) => t.kind === "success" && t.message === "Aviso enviado")).toBe(true));
    expect(await screen.findByTestId("notify-test-result")).toHaveTextContent("Aviso enviado");
  });

  it("«Probar notificación» con No molestar no dice «enviado» a ciegas: explica que no se ve pero suena", async () => {
    const msg = "Windows está en «No molestar»: no verás el aviso en pantalla, pero sonará el aviso. Está en el centro de notificaciones.";
    svc.NotificationsService.SendTest.mockResolvedValue(
      status({ quiet: "active", quietCause: "dnd", quietReason: DND, suppressed: true, testMessage: msg, testShown: true, testVisible: false, testSound: true }),
    );
    await open();
    fireEvent.click(screen.getByRole("button", { name: "Probar notificación" }));
    await waitFor(() => expect(useUI.getState().toasts.some((t) => t.kind === "info" && t.message === msg)).toBe(true));
    expect(useUI.getState().toasts.some((t) => t.message === "Aviso enviado")).toBe(false);
    expect(await screen.findByTestId("notify-test-result")).toHaveTextContent("no verás el aviso en pantalla, pero sonará el aviso");
    await waitFor(() => expect(screen.getByTestId("notify-quiet-badge")).toHaveTextContent("Activado"));
  });

  it("«Probar notificación» bloqueada dice el motivo y actualiza la insignia", async () => {
    const msg = "Las notificaciones están desactivadas en Windows. No verás el aviso en pantalla, pero sonará el aviso.";
    svc.NotificationsService.SendTest.mockResolvedValue(
      status({ state: "denied", reason: "Las notificaciones están desactivadas en Windows.", suppressed: true, testMessage: msg, testSound: true }),
    );
    await open();
    fireEvent.click(screen.getByRole("button", { name: "Probar notificación" }));
    await waitFor(() => expect(useUI.getState().toasts.some((t) => t.message === msg)).toBe(true));
    await waitFor(() => expect(screen.getByTestId("notify-badge")).toHaveTextContent("Bloqueado en Windows"));
  });

  it("dos insignias: el permiso y el modo No molestar, y «Concedido» con supresión va en tono de advertencia", async () => {
    svc.NotificationsService.Status.mockResolvedValue(
      status({ quiet: "active", quietCause: "dnd", quietReason: DND, suppressed: true }),
    );
    await open();
    await waitFor(() => expect(screen.getByTestId("notify-quiet-badge")).toHaveTextContent("Activado"));
    expect(screen.getByTestId("notify-badge")).toHaveTextContent("Concedido");
    expect(screen.getByTestId("notify-badge")).toHaveAttribute("data-tone", "warn");
    expect(screen.getByTestId("notify-quiet-badge")).toHaveAttribute("data-tone", "warn");
    expect(screen.getByTestId("notify-quiet-reason")).toHaveTextContent(DND);
    expect(screen.getByText("Modo No molestar de Windows")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Abrir ajustes de Windows" }));
    await waitFor(() => expect(svc.NotificationsService.OpenSystemSettings).toHaveBeenCalledTimes(1));
  });

  it("sin supresión: «Concedido» verde, «Desactivado» y sin botón", async () => {
    svc.NotificationsService.Status.mockResolvedValue(status());
    await open();
    await waitFor(() => expect(screen.getByTestId("notify-quiet-badge")).toHaveTextContent("Desactivado"));
    expect(screen.getByTestId("notify-quiet-badge")).toHaveAttribute("data-tone", "ok");
    expect(screen.getByTestId("notify-badge")).toHaveAttribute("data-tone", "ok");
    expect(screen.queryByRole("button", { name: "Abrir ajustes de Windows" })).toBeNull();
  });

  it("No molestar desconocido no afirma nada (también si el backend no informa el campo)", async () => {
    svc.NotificationsService.Status.mockResolvedValue(status({ quiet: "unknown" }));
    await open();
    await waitFor(() => expect(screen.getByTestId("notify-quiet-badge")).toHaveTextContent("Desconocido"));
    expect(screen.getByTestId("notify-quiet-reason")).toHaveTextContent("No se pudo comprobar");
    cleanup();
    svc.NotificationsService.Status.mockResolvedValue({ state: "granted", reason: "ok", listening: true, asked: true });
    await open();
    await waitFor(() => expect(screen.getByTestId("notify-quiet-badge")).toHaveTextContent("Desconocido"));
    expect(screen.getByTestId("notify-badge")).toHaveAttribute("data-tone", "ok");
  });

  it("muestra la última decisión con su hora, o que aún no llegó ningún mensaje", async () => {
    svc.NotificationsService.Status.mockResolvedValue(status());
    await open();
    await waitFor(() => expect(screen.getByTestId("notify-last-decision-text")).toHaveTextContent("Todavía no ha llegado ningún mensaje"));
    cleanup();
    const at = new Date(2026, 10, 12, 10, 5, 7).getTime();
    svc.NotificationsService.Status.mockResolvedValue(status({ lastDecision: "Último mensaje: no avisó: el chat está silenciado", lastDecisionAt: at }));
    await open();
    await waitFor(() => expect(screen.getByTestId("notify-last-decision-text")).toHaveTextContent("Último mensaje: no avisó: el chat está silenciado"));
    expect(screen.getByTestId("notify-last-decision")).toHaveTextContent(formatDecisionTime(at));
    expect(screen.getByText("Última decisión")).toBeInTheDocument();
  });

  it("relee el estado cada 15 s mientras la sección está abierta", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      await open();
      await waitFor(() => expect(svc.NotificationsService.Status).toHaveBeenCalledTimes(1));
      svc.NotificationsService.Status.mockResolvedValue(status({ quiet: "active", quietCause: "dnd", quietReason: DND, suppressed: true }));
      await vi.advanceTimersByTimeAsync(NOTIFY_REFRESH_MS + 100);
      await waitFor(() => expect(screen.getByTestId("notify-quiet-badge")).toHaveTextContent("Activado"));
      expect(svc.NotificationsService.Status.mock.calls.length).toBeGreaterThanOrEqual(2);
      // al salir de la sección ya no se lee
      cleanup();
      const calls = svc.NotificationsService.Status.mock.calls.length;
      await vi.advanceTimersByTimeAsync(NOTIFY_REFRESH_MS * 3);
      expect(svc.NotificationsService.Status.mock.calls.length).toBe(calls);
    } finally {
      vi.useRealTimers();
    }
  });

  it("al volver a la ventana se vuelve a leer el estado", async () => {
    await open();
    await waitFor(() => expect(svc.NotificationsService.Status).toHaveBeenCalledTimes(1));
    svc.NotificationsService.Status.mockResolvedValue({ state: "denied", reason: "Bloqueadas.", listening: true, asked: true });
    fireEvent.focus(window);
    await waitFor(() => expect(screen.getByTestId("notify-badge")).toHaveTextContent("Bloqueado en Windows"));
  });
});
