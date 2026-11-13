import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const svc = vi.hoisted(() => ({
  AccountsService: {},
  InboxService: {},
  MessagingService: {},
  StatusesService: { Post: vi.fn(), Delete: vi.fn(), Viewers: vi.fn(), Stats: vi.fn() },
  ContactsService: { Lists: vi.fn(), List: vi.fn() },
  CampaignsService: {},
  ScheduledService: { Save: vi.fn() },
  StatsService: {},
  DestinationsService: {},
  SettingsService: {},
  FilesService: { PickFiles: vi.fn() },
}));

vi.mock("@/api/transport", async () => (await import("@/test/svcTransport")).svcTransport(svc));
vi.mock("@wailsio/runtime", () => ({ Events: { On: vi.fn(() => () => undefined) } }));

import type { CapSet, Status } from "@/api/types";
import { useUI } from "@/store/ui";
import MyStatusDetail from "./MyStatusDetail";
import PostStatusModal from "./PostStatusModal";
import StatusStatsPanel from "./StatusStatsPanel";

const caps = (p: Partial<CapSet> = {}) => ({ postStatus: true, statusViewers: true, ...p }) as CapSet;

const mine = (p: Partial<Status> = {}): Status =>
  ({
    key: "wa-1|s9",
    accountId: "wa-1",
    nativeId: "s9",
    fromMe: true,
    postedAt: Date.now() - 60_000,
    expiresAt: Date.now() + 3_600_000,
    kind: "text",
    text: "Hola",
    bgColor: "#128C7E",
    font: 0,
    viewCount: 3,
    ...p,
  }) as unknown as Status;

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  svc.StatusesService.Post.mockResolvedValue({});
  svc.StatusesService.Delete.mockResolvedValue(undefined);
  svc.StatusesService.Viewers.mockResolvedValue([]);
  svc.ContactsService.Lists.mockResolvedValue([{ id: "l1", name: "Clientes", color: "#fff", memberCount: 2 }]);
  svc.ContactsService.List.mockResolvedValue({ contacts: [], total: 0 });
  svc.ScheduledService.Save.mockResolvedValue({});
  useUI.setState({ toasts: [] });
});
afterEach(cleanup);

describe("PostStatusModal", () => {
  it("sin elección de audiencia muestra el texto fijo y publica un estado de texto", async () => {
    const onDone = vi.fn();
    render(<PostStatusModal accountId="wa-1" caps={caps({ statusAudience: false })} onClose={() => undefined} onDone={onDone} />);
    expect(screen.getByText("Se usa la privacidad de estados del teléfono")).toBeInTheDocument();
    expect(screen.queryByRole("radiogroup", { name: "Audiencia" })).toBeNull();

    fireEvent.change(screen.getByLabelText("Texto"), { target: { value: "Hola a todos" } });
    fireEvent.click(screen.getByRole("radio", { name: "Color #EF4444" }));
    fireEvent.click(screen.getByRole("radio", { name: "Mono" }));
    expect(screen.getByTestId("status-preview")).toHaveTextContent("Hola a todos");
    fireEvent.click(screen.getByRole("button", { name: "Publicar" }));

    await waitFor(() => expect(svc.StatusesService.Post).toHaveBeenCalledTimes(1));
    const [acc, out] = svc.StatusesService.Post.mock.calls[0];
    expect(acc).toBe("wa-1");
    expect(out).toMatchObject({
      kind: "text",
      text: "Hola a todos",
      bgColor: "#EF4444",
      font: 2,
      audience: { mode: "contacts", peerIds: [], listIds: [] },
    });
    await waitFor(() => expect(onDone).toHaveBeenCalledWith("posted"));
  });

  it("ofrece 12 colores y 4 letras", () => {
    render(<PostStatusModal accountId="wa-1" caps={caps()} onClose={() => undefined} />);
    expect(screen.getAllByRole("radio", { name: /^Color #/ })).toHaveLength(12);
    expect(within(screen.getByRole("radiogroup", { name: "Letra" })).getAllByRole("radio")).toHaveLength(4);
  });

  it("no publica un texto vacío y lo dice", () => {
    render(<PostStatusModal accountId="wa-1" caps={caps()} onClose={() => undefined} />);
    fireEvent.click(screen.getByRole("button", { name: "Publicar" }));
    expect(screen.getByRole("alert")).toHaveTextContent("Escribe el texto del estado");
    expect(svc.StatusesService.Post).not.toHaveBeenCalled();
  });

  it("con elección de audiencia, «solo con…» sin nadie no se publica", async () => {
    render(<PostStatusModal accountId="wa-1" caps={caps({ statusAudience: true })} onClose={() => undefined} />);
    fireEvent.change(screen.getByLabelText("Texto"), { target: { value: "Hola" } });
    fireEvent.click(screen.getByRole("radio", { name: "Solo compartir con…" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "Clientes" })).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: "Publicar" }));
    expect(screen.getByRole("alert")).toHaveTextContent("Elige con quién compartirlo");
    expect(svc.StatusesService.Post).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "Clientes" }));
    fireEvent.click(screen.getByRole("button", { name: "Publicar" }));
    await waitFor(() => expect(svc.StatusesService.Post).toHaveBeenCalledTimes(1));
    expect(svc.StatusesService.Post.mock.calls[0][1].audience).toEqual({ mode: "allow", peerIds: [], listIds: ["l1"] });
  });

  it("Programar guarda un programado de tipo estado", async () => {
    const onDone = vi.fn();
    render(<PostStatusModal accountId="wa-1" caps={caps()} onClose={() => undefined} onDone={onDone} />);
    fireEvent.change(screen.getByLabelText("Texto"), { target: { value: "Mañana abrimos" } });
    fireEvent.click(screen.getByRole("button", { name: "Programar" }));
    expect(screen.getByLabelText("Fecha y hora")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Confirmar" }));

    await waitFor(() => expect(svc.ScheduledService.Save).toHaveBeenCalledTimes(1));
    const saved = svc.ScheduledService.Save.mock.calls[0][0];
    expect(saved.kind).toBe("status");
    expect(saved.accountId).toBe("wa-1");
    expect(saved.state).toBe("pending");
    expect(saved.sendAt).toBeGreaterThan(Date.now());
    expect(saved.status).toMatchObject({ kind: "text", text: "Mañana abrimos" });
    expect(svc.StatusesService.Post).not.toHaveBeenCalled();
    await waitFor(() => expect(onDone).toHaveBeenCalledWith("scheduled"));
  });

  it("rechaza programar en el pasado", () => {
    render(<PostStatusModal accountId="wa-1" caps={caps()} onClose={() => undefined} />);
    fireEvent.change(screen.getByLabelText("Texto"), { target: { value: "Hola" } });
    fireEvent.click(screen.getByRole("button", { name: "Programar" }));
    fireEvent.change(screen.getByLabelText("Fecha y hora"), { target: { value: "2020-01-01T10:00" } });
    fireEvent.click(screen.getByRole("button", { name: "Confirmar" }));
    expect(screen.getByRole("alert")).toHaveTextContent("La fecha ya pasó");
    expect(svc.ScheduledService.Save).not.toHaveBeenCalled();
  });

  it("guarda borrador y lo restaura (sin archivo)", () => {
    const { unmount } = render(<PostStatusModal accountId="wa-1" caps={caps()} onClose={() => undefined} />);
    fireEvent.change(screen.getByLabelText("Texto"), { target: { value: "A medias" } });
    expect(JSON.parse(localStorage.getItem("draft:statuses:wa-1")!).text).toBe("A medias");
    unmount();
    render(<PostStatusModal accountId="wa-1" caps={caps()} onClose={() => undefined} />);
    expect(screen.getByLabelText("Texto")).toHaveValue("A medias");
  });

  it("elegir un vídeo muestra su nombre y tamaño y pide el pie", async () => {
    svc.FilesService.PickFiles.mockResolvedValue([{ path: "C:\\v.mp4", name: "v.mp4", mime: "video/mp4", size: 2048 }]);
    render(<PostStatusModal accountId="wa-1" caps={caps()} onClose={() => undefined} />);
    fireEvent.click(screen.getByRole("tab", { name: "Foto o vídeo" }));
    fireEvent.click(screen.getByRole("button", { name: /Elegir foto o vídeo/ }));
    await waitFor(() => expect(screen.getByText("v.mp4")).toBeInTheDocument());
    fireEvent.change(screen.getByLabelText("Pie"), { target: { value: "Mira" } });
    fireEvent.click(screen.getByRole("button", { name: "Publicar" }));
    await waitFor(() => expect(svc.StatusesService.Post).toHaveBeenCalled());
    expect(svc.StatusesService.Post.mock.calls[0][1]).toMatchObject({ kind: "video", text: "Mira", filePath: "C:\\v.mp4" });
  });
});

describe("MyStatusDetail", () => {
  it("muestra el contador y las vistas con su reacción, de la más nueva a la más antigua", async () => {
    svc.StatusesService.Viewers.mockResolvedValue([
      { statusKey: "wa-1|s9", viewerId: "a", viewerName: "Ana", at: 1000, reaction: "" },
      { statusKey: "wa-1|s9", viewerId: "b", viewerName: "Beto", at: 9000, reaction: "🔥" },
    ]);
    render(<MyStatusDetail status={mine({ viewCount: 1 })} accountId="wa-1" caps={caps()} onClose={() => undefined} />);
    await waitFor(() => expect(screen.getByText("Beto")).toBeInTheDocument());
    expect(svc.StatusesService.Viewers).toHaveBeenCalledWith("wa-1", "s9");
    expect(screen.getByTestId("view-count")).toHaveTextContent("2"); // el mayor entre contador y lista
    const names = screen.getAllByRole("listitem").map((li) => li.textContent ?? "");
    expect(names[0]).toContain("Beto");
    expect(names[0]).toContain("🔥");
    expect(names[1]).toContain("Ana");
  });

  it("sin capacidad de ver quién lo vio solo hay contador", () => {
    render(<MyStatusDetail status={mine()} accountId="wa-1" caps={caps({ statusViewers: false })} onClose={() => undefined} />);
    expect(screen.getByTestId("view-count")).toHaveTextContent("3");
    expect(screen.queryByText("Vistas")).toBeNull();
    expect(svc.StatusesService.Viewers).not.toHaveBeenCalled();
  });

  it("borrar pide confirmación y borra por el id nativo", async () => {
    const confirm = vi.fn().mockResolvedValue(true);
    useUI.setState({ confirm });
    const onDeleted = vi.fn();
    const onClose = vi.fn();
    render(<MyStatusDetail status={mine()} accountId="wa-1" caps={caps()} onClose={onClose} onDeleted={onDeleted} />);
    fireEvent.click(screen.getByRole("button", { name: "Borrar" }));
    await waitFor(() => expect(svc.StatusesService.Delete).toHaveBeenCalledWith("wa-1", "s9"));
    expect(confirm).toHaveBeenCalledWith(expect.objectContaining({ danger: true }));
    await waitFor(() => expect(onDeleted).toHaveBeenCalled());
    expect(onClose).toHaveBeenCalled();
  });

  it("si no se confirma no se borra", async () => {
    useUI.setState({ confirm: vi.fn().mockResolvedValue(false) });
    render(<MyStatusDetail status={mine()} accountId="wa-1" caps={caps()} onClose={() => undefined} />);
    fireEvent.click(screen.getByRole("button", { name: "Borrar" }));
    await act(async () => {
      await Promise.resolve();
    });
    expect(svc.StatusesService.Delete).not.toHaveBeenCalled();
  });
});

describe("StatusStatsPanel", () => {
  it("pide 7 días, pinta las cifras y cambia de periodo", async () => {
    svc.StatusesService.Stats.mockResolvedValue({
      current: { posts: 5, views: 62, avgViews: 12.5, reach: 31 },
      changes: { posts: 0.5, views: null, avgViews: -0.1, reach: null },
      perDay: [],
    });
    render(<StatusStatsPanel accountId="wa-1" />);
    await waitFor(() => expect(screen.getByTestId("kpi-Publicados")).toHaveTextContent("5"));
    expect(svc.StatusesService.Stats).toHaveBeenLastCalledWith("wa-1", { days: 7, from: "", to: "" });
    expect(screen.getByTestId("kpi-Vistas medias")).toHaveTextContent("12,5");
    expect(screen.getByTestId("kpi-Alcance")).toHaveTextContent("31");
    expect(screen.getByTestId("change-Publicados")).toHaveTextContent("+50 %");
    expect(screen.getByTestId("change-Vistas medias")).toHaveTextContent("−10 %");
    expect(screen.queryByTestId("change-Alcance")).toBeNull(); // sin base para comparar

    fireEvent.click(screen.getByRole("radio", { name: "30 días" }));
    await waitFor(() => expect(svc.StatusesService.Stats).toHaveBeenLastCalledWith("wa-1", { days: 30, from: "", to: "" }));
    fireEvent.click(screen.getByRole("radio", { name: "90 días" }));
    await waitFor(() => expect(svc.StatusesService.Stats).toHaveBeenLastCalledWith("wa-1", { days: 90, from: "", to: "" }));
  });

  it("con el struct vacío de hoy no falla: ceros y «Sin datos»", async () => {
    svc.StatusesService.Stats.mockResolvedValue({});
    render(<StatusStatsPanel accountId="wa-1" />);
    await waitFor(() => expect(screen.getByTestId("kpi-Publicados")).toHaveTextContent("0"));
    expect(screen.getByText("Sin datos")).toBeInTheDocument();
  });
});
