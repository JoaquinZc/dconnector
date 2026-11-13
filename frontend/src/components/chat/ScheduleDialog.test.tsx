import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const hoisted = vi.hoisted(() => ({
  svc: {
    AccountsService: {},
    InboxService: {},
    MessagingService: {},
    StatusesService: {},
    ContactsService: {},
    CampaignsService: {},
    ScheduledService: {
      List: vi.fn(),
      ListForChat: vi.fn(),
      Save: vi.fn(),
      Update: vi.fn(),
      UpdateSeries: vi.fn(),
      Cancel: vi.fn(),
      CancelSeries: vi.fn(),
      Delete: vi.fn(),
      DeleteSeries: vi.fn(),
      SendNow: vi.fn(),
      PreviewRecurrence: vi.fn(),
    },
    StatsService: {},
    DestinationsService: {},
    SettingsService: {},
    FilesService: {},
  },
}));
const { svc } = hoisted;
const S = svc.ScheduledService;

vi.mock("@/api/transport", async () => (await import("@/test/svcTransport")).svcTransport(hoisted.svc));
vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(() => () => undefined) } }));

import type { Scheduled } from "@/api/types";
import { useUI } from "@/store/ui";
import { makeAttachment } from "./composerReducer";
import ScheduleDialog from "./ScheduleDialog";

const target = { accountId: "wa-1", chatNativeId: "c1", chatTitle: "Ana Pérez" };
const WHEN = "2099-01-05T09:00"; // lunes
const WHEN_MS = new Date(2099, 0, 5, 9, 0).getTime();
const TIMEOUT = { timeout: 3000 };

function setup(props: Partial<React.ComponentProps<typeof ScheduleDialog>> = {}) {
  const onClose = vi.fn();
  const onSaved = vi.fn();
  const onChanged = vi.fn();
  render(<ScheduleDialog target={target} initialText="Hola" onClose={onClose} onSaved={onSaved} onChanged={onChanged} {...props} />);
  return { onClose, onSaved, onChanged };
}

const when = () => screen.getByLabelText("Fecha y hora") as HTMLInputElement;
const setWhen = (v = WHEN) => fireEvent.change(when(), { target: { value: v } });
const choose = (label: string, value: string) => fireEvent.change(screen.getByLabelText(label), { target: { value } });
const save = () => fireEvent.click(screen.getByRole("button", { name: "Programar" }));

beforeEach(() => {
  vi.clearAllMocks();
  useUI.setState({ toasts: [] });
  S.Save.mockImplementation(async (s: Scheduled) => ({ ...s, id: "nuevo", seriesId: s.recurrence ? "serie" : "" }));
  S.Update.mockImplementation(async (s: Scheduled) => s);
  S.UpdateSeries.mockImplementation(async (_id: string, s: Scheduled) => s);
  S.PreviewRecurrence.mockImplementation(async (_r: unknown, first: number, n: number) => Array.from({ length: n }, (_, i) => first + i * 86_400_000));
});
afterEach(cleanup);

describe("diálogo «Programar»", () => {
  it("trae el texto del compositor y, por defecto, la próxima hora en punto", () => {
    setup();
    expect(screen.getByRole("heading", { name: "Programar mensaje" })).toBeInTheDocument();
    expect(screen.getByLabelText("Mensaje")).toHaveValue("Hola");
    expect(screen.getByText("Ana Pérez")).toBeInTheDocument();
    const ms = new Date(when().value).getTime();
    expect(ms).toBeGreaterThan(Date.now());
    expect(ms - Date.now()).toBeLessThanOrEqual(3_600_000);
    expect(when().value.endsWith(":00")).toBe(true);
    expect(screen.getByLabelText("Repetir")).toHaveValue("none");
  });

  it("programa un mensaje suelto con Save y se cierra", async () => {
    const { onSaved, onClose, onChanged } = setup();
    setWhen();
    save();
    await waitFor(() => expect(S.Save).toHaveBeenCalledTimes(1));
    expect(S.Save.mock.calls[0][0]).toMatchObject({
      id: "",
      accountId: "wa-1",
      kind: "message",
      chatNativeId: "c1",
      chatTitle: "Ana Pérez",
      message: { kind: "text", text: "Hola" },
      sendAt: WHEN_MS,
      state: "pending",
      recurrence: null,
    });
    await waitFor(() => expect(onSaved).toHaveBeenCalledTimes(1));
    expect(onSaved.mock.calls[0][0]).toHaveLength(1);
    expect(onChanged).toHaveBeenCalled();
    expect(onClose).toHaveBeenCalled();
    expect(useUI.getState().toasts.some((t) => t.message === "Mensaje programado")).toBe(true);
  });

  it("sin texto en el compositor se puede escribir ahí mismo", async () => {
    setup({ initialText: "" });
    setWhen();
    save();
    expect(screen.getByRole("alert")).toHaveTextContent("Escribe un mensaje o adjunta un archivo");
    fireEvent.change(screen.getByLabelText("Mensaje"), { target: { value: "Recordatorio" } });
    save();
    await waitFor(() => expect(S.Save).toHaveBeenCalledTimes(1));
    expect(S.Save.mock.calls[0][0].message.text).toBe("Recordatorio");
  });

  it("los atajos fijan la fecha", () => {
    setup();
    fireEvent.click(screen.getByRole("button", { name: "Mañana 9:00" }));
    const t = new Date(when().value);
    expect([t.getHours(), t.getMinutes()]).toEqual([9, 0]);
    const tomorrow = new Date();
    tomorrow.setDate(tomorrow.getDate() + 1);
    expect(t.getDate()).toBe(tomorrow.getDate());
    fireEvent.click(screen.getByRole("button", { name: "Lunes 9:00" }));
    expect(new Date(when().value).getDay()).toBe(1);
    fireEvent.click(screen.getByRole("button", { name: "En 1 hora" }));
    expect(new Date(when().value).getTime() - Date.now()).toBeGreaterThan(3_400_000);
  });

  it("una fecha pasada no se guarda; la tolerancia de 1 h sí", async () => {
    setup();
    setWhen("2020-01-01T08:00");
    save();
    expect(screen.getByRole("alert")).toHaveTextContent("La fecha ya pasó");
    expect(S.Save).not.toHaveBeenCalled();
    const recent = new Date(Date.now() - 20 * 60_000);
    const pad = (n: number) => String(n).padStart(2, "0");
    setWhen(`${recent.getFullYear()}-${pad(recent.getMonth() + 1)}-${pad(recent.getDate())}T${pad(recent.getHours())}:${pad(recent.getMinutes())}`);
    save();
    await waitFor(() => expect(S.Save).toHaveBeenCalledTimes(1));
  });

  it("Cancelar cierra sin guardar", () => {
    const { onClose } = setup();
    fireEvent.click(screen.getByRole("button", { name: "Cancelar" }));
    expect(onClose).toHaveBeenCalled();
    expect(S.Save).not.toHaveBeenCalled();
  });
});

describe("repetición", () => {
  it("semanal: marca el día de la fecha, resume la regla y muestra las próximas 5 fechas", async () => {
    setup();
    setWhen();
    choose("Repetir", "weekly");
    expect(screen.getByRole("button", { name: "lunes", pressed: true })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "jueves" }));
    expect(screen.getByTestId("recurrence-summary")).toHaveTextContent("Cada semana, lunes y jueves, a las 09:00");
    const list = await screen.findByTestId("recurrence-preview", {}, TIMEOUT);
    expect(within(list).getAllByRole("listitem")).toHaveLength(5);
    expect(S.PreviewRecurrence).toHaveBeenCalled();
    const [rule, first, n] = S.PreviewRecurrence.mock.calls.at(-1)!;
    expect(rule).toMatchObject({ frequency: "weekly", every: 1, weekdays: [1, 4], endAt: 0, maxCount: 0 });
    expect(first).toBe(WHEN_MS);
    expect(n).toBe(5);
  });

  it("la vista previa espera a que se deje de teclear (debounce)", async () => {
    setup();
    setWhen();
    choose("Repetir", "custom");
    const every = screen.getByLabelText("Cada");
    fireEvent.change(every, { target: { value: "6" } });
    fireEvent.change(every, { target: { value: "7" } });
    fireEvent.change(every, { target: { value: "8" } });
    await screen.findByTestId("recurrence-preview", {}, TIMEOUT);
    expect(S.PreviewRecurrence).toHaveBeenCalledTimes(1);
    expect(S.PreviewRecurrence.mock.calls[0][0]).toMatchObject({ frequency: "custom", every: 8, unit: "days" });
  });

  it("muestra el error del backend en español sin cerrar el diálogo", async () => {
    S.PreviewRecurrence.mockRejectedValue(new Error("el máximo de repeticiones es 1000"));
    const { onClose } = setup();
    setWhen();
    choose("Repetir", "daily");
    expect(await screen.findByTestId("recurrence-error", {}, TIMEOUT)).toHaveTextContent("el máximo de repeticiones es 1000");
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });

  it("personalizado: mínimo 5 minutos, sin llamar al backend", () => {
    setup();
    setWhen();
    choose("Repetir", "custom");
    choose("Unidad", "minutes");
    fireEvent.change(screen.getByLabelText("Cada"), { target: { value: "3" } });
    expect(screen.getByTestId("recurrence-error")).toHaveTextContent("La repetición mínima es cada 5 minutos");
    save();
    expect(screen.getByRole("alert")).toHaveTextContent("La repetición mínima es cada 5 minutos");
    expect(S.Save).not.toHaveBeenCalled();
    expect(S.PreviewRecurrence).not.toHaveBeenCalled();
  });

  it("semanal sin ningún día no se guarda", () => {
    setup();
    setWhen();
    choose("Repetir", "weekly");
    fireEvent.click(screen.getByRole("button", { name: "lunes" }));
    save();
    expect(screen.getByRole("alert")).toHaveTextContent("Elige al menos un día de la semana");
    expect(S.Save).not.toHaveBeenCalled();
  });

  it("termina: el día… y después de N veces", async () => {
    setup();
    setWhen();
    choose("Repetir", "daily");
    choose("Termina", "date");
    fireEvent.change(screen.getByLabelText("Día en que termina"), { target: { value: "2099-02-01" } });
    save();
    await waitFor(() => expect(S.Save).toHaveBeenCalledTimes(1));
    expect(S.Save.mock.calls[0][0].recurrence).toMatchObject({ frequency: "daily", endAt: new Date(2099, 1, 1, 23, 59, 59, 999).getTime(), maxCount: 0 });
  });

  it("guarda una serie por conteo", async () => {
    setup();
    setWhen();
    choose("Repetir", "monthly");
    choose("Termina", "count");
    fireEvent.change(screen.getByLabelText("Número de veces"), { target: { value: "6" } });
    expect(screen.getByTestId("recurrence-summary")).toHaveTextContent("Cada mes, el día 5, a las 09:00, 6 veces en total");
    save();
    await waitFor(() => expect(S.Save).toHaveBeenCalledTimes(1));
    expect(S.Save.mock.calls[0][0].recurrence).toMatchObject({ frequency: "monthly", every: 1, maxCount: 6, endAt: 0 });
  });

  it("un error de validación del backend al guardar se ve en el diálogo y no se cierra", async () => {
    S.Save.mockRejectedValue(new Error("la repetición mínima es cada 5 minutos"));
    const { onClose, onSaved } = setup();
    setWhen();
    choose("Repetir", "daily");
    save();
    expect(await screen.findByRole("alert")).toHaveTextContent("la repetición mínima es cada 5 minutos");
    expect(onClose).not.toHaveBeenCalled();
    expect(onSaved).not.toHaveBeenCalled();
    // y sin un segundo aviso emergente: el error ya está junto a los campos
    expect(useUI.getState().toasts.filter((t) => t.kind === "error")).toHaveLength(0);
  });
});

describe("adjuntos del compositor", () => {
  const a = makeAttachment({ path: "C:\\t\\a.jpg", name: "a.jpg", mime: "image/jpeg", size: 2048 });
  const b = makeAttachment({ path: "C:\\t\\b.pdf", name: "b.pdf", mime: "application/pdf", size: 4096 });

  it("el texto y cada adjunto se programan por separado, en orden", async () => {
    const { onSaved } = setup({ files: [a, b] });
    setWhen();
    expect(screen.getByTestId("schedule-count")).toHaveTextContent("3 mensajes");
    save();
    await waitFor(() => expect(S.Save).toHaveBeenCalledTimes(3));
    expect(S.Save.mock.calls.map((c) => c[0].message.kind)).toEqual(["text", "image", "document"]);
    expect(S.Save.mock.calls[1][0].message).toMatchObject({ filePath: "C:\\t\\a.jpg", fileName: "a.jpg", mime: "image/jpeg" });
    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    expect(onSaved.mock.calls[0][0]).toHaveLength(3);
    expect(useUI.getState().toasts.some((t) => t.message === "3 mensajes programados")).toBe(true);
  });

  it("quitar un adjunto lo deja fuera", async () => {
    setup({ files: [a, b], initialText: "" });
    setWhen();
    fireEvent.click(screen.getByRole("button", { name: "Quitar a.jpg" }));
    save();
    await waitFor(() => expect(S.Save).toHaveBeenCalledTimes(1));
    expect(S.Save.mock.calls[0][0].message.kind).toBe("document");
  });

  it("si uno falla, lo ya programado sale del diálogo y reintentar no lo duplica", async () => {
    S.Save.mockImplementationOnce(async (s: Scheduled) => ({ ...s, id: "1" })).mockRejectedValueOnce(new Error("sin disco"));
    const { onSaved, onChanged } = setup({ files: [a, b] });
    setWhen();
    save();
    expect(await screen.findByRole("alert")).toHaveTextContent("sin disco");
    expect(onSaved).not.toHaveBeenCalled();
    expect(onChanged).toHaveBeenCalled();
    expect(screen.getByLabelText("Mensaje")).toHaveValue("");
    expect(screen.getByRole("button", { name: "Quitar a.jpg" })).toBeInTheDocument();
    save();
    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    // 1 texto + 1 fallo + 2 adjuntos al reintentar
    expect(S.Save).toHaveBeenCalledTimes(4);
    expect(S.Save.mock.calls.map((c) => c[0].message.kind)).toEqual(["text", "image", "image", "document"]);
  });
});

describe("editar", () => {
  const item = (over: Partial<Record<keyof Scheduled, unknown>> = {}): Scheduled =>
    ({
      id: "p1",
      accountId: "wa-1",
      kind: "message",
      chatNativeId: "c1",
      chatTitle: "Ana Pérez",
      message: { kind: "text", text: "Texto viejo" },
      sendAt: WHEN_MS,
      state: "pending",
      sentAt: 0,
      error: "",
      createdAt: 7,
      recurrence: null,
      seriesId: "",
      ...over,
    }) as unknown as Scheduled;

  it("un suelto usa Update con su id", async () => {
    const { onSaved } = setup({ item: item() });
    expect(screen.getByRole("heading", { name: "Editar programado" })).toBeInTheDocument();
    expect(screen.getByLabelText("Mensaje")).toHaveValue("Texto viejo");
    expect(when().value).toBe(WHEN);
    fireEvent.change(screen.getByLabelText("Mensaje"), { target: { value: "Texto nuevo" } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(S.Update).toHaveBeenCalledTimes(1));
    expect(S.Update.mock.calls[0][0]).toMatchObject({ id: "p1", createdAt: 7, message: { kind: "text", text: "Texto nuevo" }, recurrence: null });
    expect(S.UpdateSeries).not.toHaveBeenCalled();
    expect(S.Save).not.toHaveBeenCalled();
    await waitFor(() => expect(onSaved).toHaveBeenCalled());
    expect(useUI.getState().toasts.some((t) => t.message === "Cambios guardados")).toBe(true);
  });

  it("una serie usa UpdateSeries con su seriesId y trae la regla", async () => {
    const rule = { frequency: "weekly", every: 1, weekdays: [1, 4], endAt: 0, maxCount: 0, count: 2, firstAt: WHEN_MS };
    setup({ item: item({ seriesId: "s1", recurrence: rule }) });
    expect(screen.getByLabelText("Repetir")).toHaveValue("weekly");
    expect(screen.getByRole("button", { name: "jueves", pressed: true })).toBeInTheDocument();
    expect(screen.getByText(/valen para esta repetición y las siguientes/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(S.UpdateSeries).toHaveBeenCalledTimes(1));
    const [seriesId, built] = S.UpdateSeries.mock.calls[0];
    expect(seriesId).toBe("s1");
    expect(built).toMatchObject({ id: "p1", seriesId: "s1", recurrence: { frequency: "weekly", weekdays: [1, 4], count: 2, firstAt: WHEN_MS } });
    expect(S.Update).not.toHaveBeenCalled();
  });

  it("conserva el archivo del programado (solo se cambia el texto)", async () => {
    const message = { kind: "image", text: "Pie", filePath: "C:\\x\\f.jpg", fileName: "f.jpg", mime: "image/jpeg" };
    setup({ item: item({ message }) });
    expect(screen.getByText("f.jpg")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Mensaje"), { target: { value: "" } });
    fireEvent.click(screen.getByRole("button", { name: "Guardar" }));
    await waitFor(() => expect(S.Update).toHaveBeenCalledTimes(1));
    expect(S.Update.mock.calls[0][0].message).toMatchObject({ kind: "image", text: "", filePath: "C:\\x\\f.jpg" });
  });

  it("un fallido se corrige suelto: sin editor de repetición", () => {
    setup({ item: item({ state: "failed", error: "sin conexión" }) });
    expect(screen.queryByLabelText("Repetir")).toBeNull();
  });
});
