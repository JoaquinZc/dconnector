import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ComponentProps } from "react";

const hoisted = vi.hoisted(() => ({
  handlers: {} as Record<string, (e: { data: unknown }) => void>,
  svc: {
    AccountsService: { List: vi.fn() },
    InboxService: {},
    MessagingService: {},
    StatusesService: {},
    ContactsService: { Lists: vi.fn(), List: vi.fn(), Get: vi.fn() },
    CampaignsService: {
      List: vi.fn(),
      Save: vi.fn(),
      Review: vi.fn(),
      Launch: vi.fn(),
      Pause: vi.fn(),
      Resume: vi.fn(),
      Cancel: vi.fn(),
      Delete: vi.fn(),
      Queue: vi.fn(),
      GetPacing: vi.fn(),
      SetPacing: vi.fn(),
    },
    ScheduledService: {},
    StatsService: {},
    DestinationsService: {},
    SettingsService: { Get: vi.fn() },
    FilesService: { PickFiles: vi.fn() },
  },
}));
const { handlers, svc } = hoisted;

vi.mock("@/api/transport", async () => (await import("@/test/svcTransport")).svcTransport(hoisted.svc));
vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(() => () => undefined) } }));

import { emitEvent, hasEventListeners } from "@/api/events";
import type { AccountView, Campaign, CampaignReview, CapSet } from "@/api/types";
import CampaignsPage from "@/pages/CampaignsPage";
import { useAccounts } from "@/store/accounts";
import { useUI } from "@/store/ui";
import CampaignWizard from "./CampaignWizard";
import PacingEditor from "./PacingEditor";
import { DEFAULT_PACING } from "./pacingModel";

const emit = (name: string, data: unknown) => act(() => emitEvent(name, data));

const view = (id: string, channel: string, label: string): AccountView =>
  ({
    account: { id, channel, label, color: "#25D366", enabled: true, selfName: "", selfHandle: "+593990000000", config: null },
    status: { accountId: id, state: "connected", need: "", since: 1 },
    capabilities: {} as CapSet,
    unread: 0,
  }) as unknown as AccountView;

const camp = (id: string, over: Partial<Record<keyof Campaign, unknown>> = {}): Campaign =>
  ({
    id,
    name: `Campaña ${id}`,
    accountId: "wa-1",
    state: "running",
    text: "Hola {nombre}",
    variants: null,
    filePath: "",
    listIds: ["l1"],
    contactIds: null,
    startAt: 0,
    window: { fromMinute: 540, toMinute: 1080, weekdays: [1, 2, 3, 4, 5] },
    pacing: DEFAULT_PACING,
    optOutFooter: true,
    abEnabled: false,
    brakeReason: "",
    createdAt: 1,
    updatedAt: 2,
    totals: { queued: 6, sent: 3, failed: 1, skipped: 0, replied: 2, optOut: 1 },
    ...over,
  }) as unknown as Campaign;

const green: CampaignReview = {
  recipients: 12,
  risks: { warm: 7, lukewarm: 3, cold: 2 },
  requiredVariants: 1,
  items: [{ level: "green", code: "ok", message: "Mensaje listo" }],
  level: "green",
  estimate: { seconds: 2 * 3600 + 300, days: 1 },
  warnings: null,
  skipped: { optedOut: 1, noIdentity: 2, frequency: 0 },
};

function seedAccounts() {
  useAccounts.setState({
    views: {
      "wa-1": view("wa-1", "whatsapp", "Ventas"),
      "tg-1": view("tg-1", "telegram", "Soporte"),
      "fb-1": view("fb-1", "messenger", "Página"),
    },
    order: ["wa-1", "tg-1", "fb-1"],
    loaded: true,
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  for (const k of Object.keys(handlers)) delete handlers[k];
  seedAccounts();
  useUI.setState({ toasts: [], confirmReq: null });
  svc.AccountsService.List.mockResolvedValue([]);
  svc.CampaignsService.List.mockResolvedValue([]);
  svc.CampaignsService.Review.mockResolvedValue(green);
  svc.CampaignsService.GetPacing.mockResolvedValue({ ...DEFAULT_PACING });
  svc.CampaignsService.SetPacing.mockResolvedValue(undefined);
  svc.CampaignsService.Queue.mockResolvedValue([]);
  svc.CampaignsService.Save.mockImplementation(async (c: Campaign) => ({ ...c, id: c.id || "nueva" }));
  svc.CampaignsService.Launch.mockResolvedValue(undefined);
  for (const k of ["Pause", "Resume", "Cancel", "Delete"] as const) svc.CampaignsService[k].mockResolvedValue(undefined);
  svc.ContactsService.Lists.mockResolvedValue([{ id: "l1", name: "Clientes", color: "#25D366", memberCount: 12 }]);
  svc.ContactsService.List.mockResolvedValue({ contacts: [], total: 0 });
  svc.ContactsService.Get.mockResolvedValue({ id: "k1", name: "Luis", phone: "", identities: [] });
  svc.SettingsService.Get.mockResolvedValue({ optOutFooterText: "Responde BAJA para no recibir más" });
  svc.FilesService.PickFiles.mockResolvedValue([]);
});
afterEach(cleanup);

describe("Ritmo de la cuenta", () => {
  it("pinta los seis grupos con los valores de GetPacing", async () => {
    svc.CampaignsService.GetPacing.mockResolvedValue({ ...DEFAULT_PACING, minDelaySec: 33 });
    render(<PacingEditor accountId="wa-1" />);
    for (const id of ["waits", "limits", "batches", "warmup", "brake", "frequency"]) {
      expect(screen.getByTestId(`pacing-group-${id}`)).toBeInTheDocument();
    }
    for (const t of ["Esperas", "Límites", "Tandas", "Calentamiento", "Freno", "Frecuencia"]) {
      expect(screen.getByRole("heading", { name: t })).toBeInTheDocument();
    }
    await waitFor(() => expect(screen.getByLabelText("Mínima")).toHaveValue("33"));
    expect(svc.CampaignsService.GetPacing).toHaveBeenCalledWith("wa-1");
  });

  it("«Restablecer valores seguros» devuelve los de fábrica y se guardan tal cual", async () => {
    svc.CampaignsService.GetPacing.mockResolvedValue({ ...DEFAULT_PACING, minDelaySec: 1, maxDelaySec: 2, dailyLimit: 999, brakeEnabled: false });
    const onSaved = vi.fn();
    render(<PacingEditor accountId="wa-1" onSaved={onSaved} />);
    await waitFor(() => expect(screen.getByLabelText("Mínima")).toHaveValue("1"));
    const reset = screen.getByRole("button", { name: /Restablecer valores seguros/ });
    expect(reset).toBeEnabled();
    fireEvent.click(reset);
    expect(screen.getByLabelText("Mínima")).toHaveValue("20");
    expect(screen.getByLabelText("Por día")).toHaveValue("200");
    expect(screen.getByRole("switch", { name: "Freno: Activado" })).toHaveAttribute("aria-checked", "true");
    expect(reset).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(svc.CampaignsService.SetPacing).toHaveBeenCalledTimes(1));
    expect(svc.CampaignsService.SetPacing).toHaveBeenCalledWith("wa-1", DEFAULT_PACING);
    expect(onSaved).toHaveBeenCalledWith(DEFAULT_PACING);
  });

  it("un valor que no vale no se guarda y se señala", async () => {
    render(<PacingEditor accountId="wa-1" />);
    await waitFor(() => expect(screen.getByLabelText("Mínima")).toHaveValue("20"));
    fireEvent.change(screen.getByLabelText("Por hora"), { target: { value: "abc" } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    expect(await screen.findByText("Número no válido")).toBeInTheDocument();
    expect(svc.CampaignsService.SetPacing).not.toHaveBeenCalled();
  });

  it("Guardar solo se habilita si algo cambió", async () => {
    render(<PacingEditor accountId="wa-1" />);
    await waitFor(() => expect(screen.getByLabelText("Mínima")).toHaveValue("20"));
    const save = screen.getByRole("button", { name: "Guardar" });
    expect(save).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Por día"), { target: { value: "150" } });
    expect(save).toBeEnabled();
  });
});

describe("lista de campañas", () => {
  it("nombre, cuenta, estado, progreso y espera; el evento campaign:progress actualiza en vivo", async () => {
    svc.CampaignsService.List.mockResolvedValue([
      camp("a", { name: "Promo" }),
      camp("b", { name: "Frenada", state: "braked", brakeReason: "Muchos fallos" }),
      camp("c", { name: "Borrador", state: "draft", totals: { queued: 0, sent: 0, failed: 0, skipped: 0, replied: 0, optOut: 0 } }),
    ]);
    render(<CampaignsPage />);
    const rows = await screen.findAllByTestId("campaign-row");
    expect(rows).toHaveLength(3);
    // En curso primero.
    expect(within(rows[0]).getByText("Promo")).toBeInTheDocument();
    expect(within(rows[0]).getByText("Ventas")).toBeInTheDocument();
    expect(within(rows[0]).getByText("En curso")).toBeInTheDocument();
    expect(within(rows[0]).getByTestId("campaign-progress")).toHaveTextContent("3/10");
    expect(within(rows[0]).getByTestId("campaign-line")).toHaveTextContent("Enviando…");
    expect(within(rows[1]).getByTestId("campaign-line")).toHaveTextContent("Frenada: Muchos fallos");
    expect(within(rows[2]).getAllByText("Borrador").length).toBeGreaterThan(0);

    await waitFor(() => expect(hasEventListeners("campaign:progress")).toBe(true));
    emit("campaign:progress", {
      campaignId: "a",
      state: "running",
      totals: { queued: 4, sent: 5, failed: 1, skipped: 0, replied: 2, optOut: 1 },
      nextAt: 0,
      waiting: "window",
      brakeReason: "",
    });
    const first = screen.getAllByTestId("campaign-row")[0];
    expect(within(first).getByTestId("campaign-progress")).toHaveTextContent("5/10");
    expect(within(first).getByTestId("campaign-line")).toHaveTextContent("Fuera de horario");

    for (const [waiting, text] of [
      ["hourly", "Límite por hora"],
      ["daily", "Límite del día"],
      ["batch_rest", "Descanso"],
      ["disconnected", "Cuenta desconectada"],
    ]) {
      emit("campaign:progress", { campaignId: "a", state: "running", totals: { queued: 4, sent: 5, failed: 1, skipped: 0, replied: 0, optOut: 0 }, nextAt: 0, waiting });
      expect(within(screen.getAllByTestId("campaign-row")[0]).getByTestId("campaign-line")).toHaveTextContent(text);
    }

    // Freno desde el motor.
    emit("campaign:progress", { campaignId: "a", state: "braked", totals: { queued: 4, sent: 5, failed: 1, skipped: 0, replied: 0, optOut: 0 }, nextAt: 0, waiting: "", brakeReason: "Bajas" });
    const braked = screen.getAllByTestId("campaign-row").find((r) => within(r).queryByText("Promo"))!;
    expect(within(braked).getByText("Frenada")).toBeInTheDocument();
    expect(within(braked).getByTestId("campaign-line")).toHaveTextContent("Frenada: Bajas");
  });

  it("una campaña que no estaba en la lista recarga", async () => {
    render(<CampaignsPage />);
    await screen.findByText("Sin campañas");
    svc.CampaignsService.List.mockResolvedValue([camp("z", { name: "Nueva" })]);
    await waitFor(() => expect(hasEventListeners("campaign:progress")).toBe(true));
    emit("campaign:progress", { campaignId: "z", state: "running", totals: {}, nextAt: 0, waiting: "" });
    expect(await screen.findByText("Nueva")).toBeInTheDocument();
  });

  it("vacío corto con botón de nueva campaña", async () => {
    render(<CampaignsPage />);
    expect(await screen.findByText("Sin campañas")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: /Nueva campaña/ }).length).toBeGreaterThan(0);
  });
});

describe("detalle de campaña", () => {
  const open = async (c: Campaign) => {
    svc.CampaignsService.List.mockResolvedValue([c]);
    render(<CampaignsPage />);
    fireEvent.click(await screen.findByTestId("campaign-row"));
    await screen.findByTestId("detail-progress");
  };

  it("totales, respuestas y bajas", async () => {
    await open(camp("a", { name: "Promo" }));
    expect(screen.getByTestId("detail-progress")).toHaveTextContent("3/10");
    expect(screen.getByTestId("stat-queued")).toHaveTextContent("6");
    expect(screen.getByTestId("stat-sent")).toHaveTextContent("3");
    expect(screen.getByTestId("stat-failed")).toHaveTextContent("1");
    expect(screen.getByTestId("stat-replied")).toHaveTextContent("2");
    expect(screen.getByTestId("stat-optout")).toHaveTextContent("1");
  });

  it("las pestañas piden la cola por estado", async () => {
    await open(camp("a"));
    await waitFor(() => expect(svc.CampaignsService.Queue).toHaveBeenCalledWith("a", ["pending", "held"], 200));
    svc.CampaignsService.Queue.mockResolvedValue([
      { id: "q1", campaignId: "a", contactId: "k1", chatNativeId: "593999111222@s.whatsapp.net", risk: "warm", variant: 1, abSample: false, text: "Hola Ana", state: "sent", sendAfter: 0, sentAt: 5, messageKey: "", error: "" },
      { id: "q2", campaignId: "a", contactId: "k2", chatNativeId: "593999000111@s.whatsapp.net", risk: "cold", variant: 0, abSample: false, text: "", state: "failed", sendAfter: 0, sentAt: 0, messageKey: "", error: "Sin conexión" },
    ]);
    fireEvent.click(screen.getByRole("tab", { name: /Enviados/ }));
    await waitFor(() => expect(svc.CampaignsService.Queue).toHaveBeenCalledWith("a", ["sent"], 200));
    expect(await screen.findAllByTestId("queue-item")).toHaveLength(2);
    expect(screen.getByText("593999111222")).toBeInTheDocument();
    expect(screen.getByText("Sin conexión")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab", { name: /Fallidos/ }));
    await waitFor(() => expect(svc.CampaignsService.Queue).toHaveBeenCalledWith("a", ["failed"], 200));
    fireEvent.click(screen.getByRole("tab", { name: /Omitidos/ }));
    await waitFor(() => expect(svc.CampaignsService.Queue).toHaveBeenCalledWith("a", ["skipped"], 200));
  });

  it("pausar una campaña en curso", async () => {
    await open(camp("a"));
    fireEvent.click(screen.getByRole("button", { name: /Pausar/ }));
    await waitFor(() => expect(svc.CampaignsService.Pause).toHaveBeenCalledWith("a"));
    expect(screen.queryByRole("button", { name: /Reanudar/ })).toBeNull();
  });

  it("reanudar una frenada", async () => {
    await open(camp("a", { state: "braked", brakeReason: "Bajas" }));
    fireEvent.click(screen.getByRole("button", { name: /Reanudar/ }));
    await waitFor(() => expect(svc.CampaignsService.Resume).toHaveBeenCalledWith("a"));
  });

  it("cancelar y eliminar piden confirmación", async () => {
    const confirm = vi.fn().mockResolvedValue(true);
    useUI.setState({ confirm });
    await open(camp("a", { state: "paused" }));
    fireEvent.click(screen.getByRole("button", { name: /^Cancelar/ }));
    await waitFor(() => expect(svc.CampaignsService.Cancel).toHaveBeenCalledWith("a"));
    expect(confirm).toHaveBeenCalledWith(expect.objectContaining({ danger: true }));
    fireEvent.click(screen.getByRole("button", { name: /Eliminar/ }));
    await waitFor(() => expect(svc.CampaignsService.Delete).toHaveBeenCalledWith("a"));
    // Tras borrar vuelve a la lista.
    expect(await screen.findByRole("heading", { name: "Campañas" })).toBeInTheDocument();
  });

  it("sin confirmar no se borra", async () => {
    useUI.setState({ confirm: vi.fn().mockResolvedValue(false) });
    await open(camp("a", { state: "done" }));
    fireEvent.click(screen.getByRole("button", { name: /Eliminar/ }));
    await Promise.resolve();
    expect(svc.CampaignsService.Delete).not.toHaveBeenCalled();
  });

  it("en curso no se elimina", async () => {
    await open(camp("a", { state: "running" }));
    expect(screen.getByRole("button", { name: /Eliminar/ })).toBeDisabled();
  });
});

describe("asistente", () => {
  const WAIT = { timeout: 4000 };

  async function toStep(n: number) {
    const next = () => fireEvent.click(screen.getByRole("button", { name: "Siguiente" }));
    fireEvent.change(screen.getByLabelText("Nombre"), { target: { value: "Promo noviembre" } });
    fireEvent.click(screen.getByRole("radio", { name: /Ventas/ }));
    if (n >= 2) {
      next();
      fireEvent.click(await screen.findByRole("button", { name: /Clientes/ }));
    }
    if (n >= 3) {
      next();
      fireEvent.change(await screen.findByLabelText("Mensaje A"), { target: { value: "Hola {nombre}, tenemos novedades" } });
    }
    if (n >= 4) {
      next();
      await screen.findByText("Ritmo de la cuenta");
    }
    if (n >= 5) {
      next();
      await screen.findByTestId("launch-button");
    }
  }

  const renderWizard = (props: Partial<ComponentProps<typeof CampaignWizard>> = {}) => {
    const onClose = vi.fn();
    const onDone = vi.fn();
    render(<CampaignWizard onClose={onClose} onDone={onDone} {...props} />);
    return { onClose, onDone };
  };

  it("paso 1: solo cuentas de WhatsApp o Telegram", () => {
    renderWizard();
    expect(screen.getByRole("radio", { name: /Ventas/ })).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: /Soporte/ })).toBeInTheDocument();
    expect(screen.queryByRole("radio", { name: /Página/ })).toBeNull();
  });

  it("no avanza sin nombre ni cuenta y dice qué falta", () => {
    renderWizard();
    fireEvent.click(screen.getByRole("button", { name: "Siguiente" }));
    expect(screen.getByRole("alert")).toHaveTextContent("Ponle un nombre");
    fireEvent.change(screen.getByLabelText("Nombre"), { target: { value: "X" } });
    fireEvent.click(screen.getByRole("button", { name: "Siguiente" }));
    expect(screen.getByRole("alert")).toHaveTextContent("Elige una cuenta");
  });

  it("paso 2: recuento por riesgo y omitidos de Review", async () => {
    renderWizard();
    await toStep(2);
    await waitFor(() => expect(screen.getByTestId("recipients-count")).toHaveTextContent("12"), WAIT);
    expect(screen.getByTestId("risk-warm")).toHaveTextContent("7");
    expect(screen.getByTestId("risk-lukewarm")).toHaveTextContent("3");
    expect(screen.getByTestId("risk-cold")).toHaveTextContent("2");
    expect(screen.getByTestId("skipped-optedOut")).toHaveTextContent("1");
    expect(screen.getByTestId("skipped-noIdentity")).toHaveTextContent("2");
    expect(screen.getByTestId("skipped-frequency")).toHaveTextContent("0");
    // Review recibe la campaña del formulario.
    const sent = svc.CampaignsService.Review.mock.calls.at(-1)![0] as Campaign;
    expect(sent.accountId).toBe("wa-1");
    expect(sent.listIds).toEqual(["l1"]);
  });

  it("paso 2 sin lista ni contacto no avanza", async () => {
    renderWizard();
    fireEvent.change(screen.getByLabelText("Nombre"), { target: { value: "X" } });
    fireEvent.click(screen.getByRole("radio", { name: /Ventas/ }));
    fireEvent.click(screen.getByRole("button", { name: "Siguiente" }));
    await screen.findByRole("button", { name: /Clientes/ });
    fireEvent.click(screen.getByRole("button", { name: "Siguiente" }));
    expect(screen.getByRole("alert")).toHaveTextContent("Elige una lista o un contacto");
  });

  it("paso 3: revisa en vivo con retraso (una sola llamada al teclear de corrido) y pinta los niveles", async () => {
    svc.CampaignsService.Review.mockResolvedValue({
      ...green,
      level: "yellow",
      items: [
        { level: "yellow", code: "a", message: "Las variantes se parecen" },
        { level: "green", code: "b", message: "Hay personalización" },
      ],
    });
    renderWizard();
    await toStep(3);
    await screen.findByText("Revisa los avisos", undefined, WAIT);
    expect(screen.getByText("Las variantes se parecen")).toBeInTheDocument();
    expect(screen.getByText("Hay personalización")).toBeInTheDocument();
    const callsBefore = svc.CampaignsService.Review.mock.calls.length;
    const box = screen.getByLabelText("Mensaje A");
    fireEvent.change(box, { target: { value: "H" } });
    fireEvent.change(box, { target: { value: "Ho" } });
    fireEvent.change(box, { target: { value: "Hol" } });
    await waitFor(() => expect(svc.CampaignsService.Review.mock.calls.length).toBe(callsBefore + 1), WAIT);
    await new Promise((r) => setTimeout(r, 800));
    expect(svc.CampaignsService.Review.mock.calls.length).toBe(callsBefore + 1);
    expect((svc.CampaignsService.Review.mock.calls.at(-1)![0] as Campaign).text).toBe("Hol");
  });

  it("paso 3: variantes B…E, vista previa y pie de baja", async () => {
    renderWizard();
    await toStep(3);
    fireEvent.click(screen.getByRole("button", { name: "Añadir variante" }));
    fireEvent.change(screen.getByLabelText("Variante B"), { target: { value: "Buenas {nombre}" } });
    expect(screen.getAllByRole("tab")).toHaveLength(2);
    fireEvent.click(screen.getByRole("button", { name: "Añadir variante" }));
    fireEvent.click(screen.getByRole("button", { name: "Añadir variante" }));
    fireEvent.click(screen.getByRole("button", { name: "Añadir variante" }));
    expect(screen.queryByRole("button", { name: "Añadir variante" })).toBeNull(); // A + B…E
    expect(screen.getAllByRole("tab")).toHaveLength(5);
    expect(await screen.findByText("Responde BAJA para no recibir más")).toBeInTheDocument();
    expect(screen.getByTestId("message-preview")).toHaveTextContent("Hola Ana, tenemos novedades");
  });

  it("paso 3: adjunto con PickFiles", async () => {
    svc.FilesService.PickFiles.mockResolvedValue([{ path: "C:\\x\\oferta.jpg", name: "oferta.jpg", mime: "image/jpeg", size: 10 }]);
    renderWizard();
    await toStep(3);
    fireEvent.click(screen.getByRole("button", { name: /Adjuntar/ }));
    expect(await screen.findByText("oferta.jpg")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Quitar adjunto" }));
    expect(screen.queryByText("oferta.jpg")).toBeNull();
  });

  it("paso 4: ventana, días y enlace al ritmo de la cuenta", async () => {
    renderWizard();
    await toStep(4);
    expect(screen.getByLabelText("Desde")).toHaveValue("09:00");
    expect(screen.getByLabelText("Hasta")).toHaveValue("18:00");
    expect(screen.getByRole("button", { name: "Lun" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "Sáb" })).toHaveAttribute("aria-pressed", "false");
    fireEvent.click(screen.getByRole("button", { name: "Sáb" }));
    expect(screen.getByRole("button", { name: "Sáb" })).toHaveAttribute("aria-pressed", "true");
    await waitFor(() => expect(screen.getByText(/20–45 s entre envíos/)).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: "Ajustar" }));
    expect(await screen.findByRole("heading", { name: "Ritmo de la cuenta" })).toBeInTheDocument();
    expect(await screen.findByLabelText("Por día")).toBeInTheDocument();
  });

  it("paso 4: ventana al revés o sin días no avanza", async () => {
    renderWizard();
    await toStep(4);
    fireEvent.change(screen.getByLabelText("Desde"), { target: { value: "19:00" } });
    fireEvent.click(screen.getByRole("button", { name: "Siguiente" }));
    expect(screen.getByRole("alert")).toHaveTextContent("La hora de inicio debe ser menor que la de fin");
    fireEvent.change(screen.getByLabelText("Desde"), { target: { value: "08:00" } });
    for (const d of ["Lun", "Mar", "Mié", "Jue", "Vie"]) fireEvent.click(screen.getByRole("button", { name: d }));
    fireEvent.click(screen.getByRole("button", { name: "Siguiente" }));
    expect(screen.getByRole("alert")).toHaveTextContent("Elige al menos un día");
  });

  it("paso 5: estimación y «Lanzar» lanza (guarda y lanza)", async () => {
    const { onDone, onClose } = renderWizard();
    await toStep(5);
    await waitFor(() => expect(screen.getByTestId("summary-estimate")).toHaveTextContent("≈ 2 h 5 min"), WAIT);
    const launch = screen.getByTestId("launch-button");
    await waitFor(() => expect(launch).toBeEnabled(), WAIT);
    expect(launch).toHaveClass("btn-primary");
    fireEvent.click(launch);
    await waitFor(() => expect(svc.CampaignsService.Launch).toHaveBeenCalledWith("nueva"));
    const saved = svc.CampaignsService.Save.mock.calls[0][0] as Campaign;
    expect(saved).toMatchObject({
      name: "Promo noviembre",
      accountId: "wa-1",
      listIds: ["l1"],
      text: "Hola {nombre}, tenemos novedades",
      startAt: 0,
      optOutFooter: true,
      window: { fromMinute: 540, toMinute: 1080, weekdays: [1, 2, 3, 4, 5] },
    });
    expect(saved.pacing).toEqual(DEFAULT_PACING);
    expect(onDone).toHaveBeenCalledWith("launched", expect.objectContaining({ id: "nueva" }));
    expect(onClose).toHaveBeenCalled();
    expect(localStorage.getItem("draft:campaigns:new")).toBeNull();
  });

  it("paso 5: con nivel rojo «Lanzar» queda deshabilitado, en rojo, y dice por qué", async () => {
    svc.CampaignsService.Review.mockResolvedValue({
      ...green,
      level: "red",
      items: [{ level: "red", code: "similar", message: "Las variantes son casi iguales" }],
    });
    renderWizard();
    await toStep(5);
    await waitFor(() => expect(screen.getByTestId("launch-blockers")).toHaveTextContent("Las variantes son casi iguales"), WAIT);
    const launch = screen.getByTestId("launch-button");
    expect(launch).toBeDisabled();
    expect(launch).toHaveClass("btn-danger-solid");
    fireEvent.click(launch);
    expect(svc.CampaignsService.Save).not.toHaveBeenCalled();
    expect(svc.CampaignsService.Launch).not.toHaveBeenCalled();
  });

  it("si Launch falla, un segundo intento reutiliza la campaña guardada", async () => {
    svc.CampaignsService.Launch.mockRejectedValueOnce(new Error("sin cola"));
    const { onClose } = renderWizard();
    await toStep(5);
    const launch = screen.getByTestId("launch-button");
    await waitFor(() => expect(launch).toBeEnabled(), WAIT);
    fireEvent.click(launch);
    await waitFor(() => expect(svc.CampaignsService.Launch).toHaveBeenCalledTimes(1));
    expect(onClose).not.toHaveBeenCalled();
    await waitFor(() => expect(launch).toBeEnabled());
    fireEvent.click(launch);
    await waitFor(() => expect(svc.CampaignsService.Launch).toHaveBeenCalledTimes(2));
    expect((svc.CampaignsService.Save.mock.calls[1][0] as Campaign).id).toBe("nueva");
  });

  it("«Guardar borrador» guarda sin lanzar", async () => {
    const { onDone } = renderWizard();
    fireEvent.change(screen.getByLabelText("Nombre"), { target: { value: "Solo borrador" } });
    fireEvent.click(screen.getByRole("radio", { name: /Ventas/ }));
    fireEvent.click(screen.getByRole("button", { name: "Guardar borrador" }));
    await waitFor(() => expect(svc.CampaignsService.Save).toHaveBeenCalledTimes(1));
    expect(svc.CampaignsService.Launch).not.toHaveBeenCalled();
    expect((svc.CampaignsService.Save.mock.calls[0][0] as Campaign).state).toBe("draft");
    expect(onDone).toHaveBeenCalledWith("saved", expect.anything());
  });

  it("guarda borrador en draft:campaigns:new y lo restaura", async () => {
    const first = renderWizard();
    fireEvent.change(screen.getByLabelText("Nombre"), { target: { value: "A medias" } });
    expect(JSON.parse(localStorage.getItem("draft:campaigns:new")!)).toMatchObject({ name: "A medias" });
    first.onClose();
    cleanup();
    renderWizard();
    expect(screen.getByLabelText("Nombre")).toHaveValue("A medias");
    fireEvent.click(screen.getByRole("button", { name: "Descartar borrador" }));
    expect(screen.getByLabelText("Nombre")).toHaveValue("");
    expect(localStorage.getItem("draft:campaigns:new")).toBeNull();
  });

  it("abrir y cerrar sin tocar nada no deja borrador", () => {
    renderWizard();
    expect(localStorage.getItem("draft:campaigns:new")).toBeNull();
  });

  it("editar un borrador existente usa su id en la clave del borrador", () => {
    renderWizard({ campaign: camp("d1", { state: "draft", name: "Existente" }) });
    expect(screen.getByLabelText("Nombre")).toHaveValue("Existente");
    fireEvent.change(screen.getByLabelText("Nombre"), { target: { value: "Existente 2" } });
    expect(localStorage.getItem("draft:campaigns:d1")).not.toBeNull();
    expect(localStorage.getItem("draft:campaigns:new")).toBeNull();
  });
});
