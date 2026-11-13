// Esqueletos a la medida: cubren todas las páginas, son accesibles y copian la maquetación de la página real.
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { cleanup, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { TABS } from "@/components/chat/ChatSidebar";
import { FILTERS } from "@/components/scheduled/scheduledModel";
import { SCHEDULED_COLS } from "@/components/scheduled/ScheduledTable";
import { CONTACT_COLUMNS, ROW_HEIGHT as CONTACT_ROW } from "@/components/contacts/ContactsTable";
import { ROW_HEIGHT as CHAT_ROW } from "@/components/chat/ChatRow";
import { AUTO_HISTORY_LABEL } from "@/components/settings/GeneralSection";
import { PRESET_DAYS, presetLabel } from "@/components/stats/periodModel";
import { useAccounts } from "@/store/accounts";
import { PAGE_ORDER, type Page } from "@/store/ui";
import { PAGE_SKELETONS } from ".";
import AccountsSkeleton from "./AccountsSkeleton";
import CampaignsSkeleton from "./CampaignsSkeleton";
import ChatsSkeleton, { CHAT_TABS } from "./ChatsSkeleton";
import ContactsSkeleton from "./ContactsSkeleton";
import DestinationsSkeleton from "./DestinationsSkeleton";
import ScheduledSkeleton, { SCHEDULED_FILTER_LABELS } from "./ScheduledSkeleton";
import SettingsSkeleton from "./SettingsSkeleton";
import StatsSkeleton, { STATS_PERIODS, STATS_TABS } from "./StatsSkeleton";
import StatusesSkeleton, { STATUS_TABS } from "./StatusesSkeleton";
import {
  CAMPAIGN_ROW_COLUMNS,
  CHAT_ROW_HEIGHT,
  CONTACT_COLUMNS as ZONE_CONTACT_COLUMNS,
  CONTACT_ROW_HEIGHT,
  CONVERSATION_KPIS,
  GROUP_KPIS,
  GENERAL_ROWS,
  SCHEDULED_GRID,
  SETTINGS_SECTIONS,
  AccountsGridSkeleton,
  ChatRowsSkeleton,
  ContactRowsSkeleton,
  ConversationsStatsSkeleton,
  GroupsStatsSkeleton,
  ListRowsSkeleton,
  MyStatusesGridSkeleton,
  SettingsSectionSkeleton,
  StatusListSkeleton,
  StatusStatsSkeleton,
} from "./zones";

/** Texto de un archivo de la página real (las clases de maquetación deben coincidir con las del esqueleto). */
const source = (file: string) => readFileSync(resolve(__dirname, "../..", file), "utf-8");

beforeEach(() => {
  useAccounts.setState({ views: {}, order: [], loaded: false });
});
afterEach(cleanup);

describe("PAGE_SKELETONS", () => {
  it("cubre todas las páginas", () => {
    expect(Object.keys(PAGE_SKELETONS).sort()).toEqual([...PAGE_ORDER].sort());
    for (const p of PAGE_ORDER) expect(typeof PAGE_SKELETONS[p]).toBe("function");
  });

  it.each(PAGE_ORDER)("%s: una sola región «Cargando» (sin avisos anidados) y bloques ocultos a los lectores", (page: Page) => {
    const Sk = PAGE_SKELETONS[page];
    const { container } = render(<Sk />);
    const status = screen.getAllByRole("status", { name: "Cargando" });
    expect(status).toHaveLength(1);
    expect(status[0]).toHaveAttribute("data-testid", `${page}-skeleton`);
    const blocks = container.querySelectorAll(".animate-pulse");
    expect(blocks.length).toBeGreaterThan(3);
    for (const b of Array.from(blocks)) {
      expect(b.hasAttribute("aria-hidden") || b.closest("[aria-hidden='true']")).toBeTruthy();
      expect(b.className).toContain("motion-reduce:animate-none");
    }
  });

  it("los bloques usan el gris del tema (--muted), sin colores fijos", () => {
    const { container } = render(<ChatsSkeleton />);
    const blocks = Array.from(container.querySelectorAll(".animate-pulse"));
    expect(blocks.every((b) => b.className.includes("var(--muted)"))).toBe(true);
  });
});

describe("Chats", () => {
  it("copia el marco de la lista (360 px, título, buscador, pestañas reales) y el panel vacío", () => {
    render(<ChatsSkeleton />);
    expect(screen.getByRole("heading", { name: "Chats" })).toBeInTheDocument();
    expect(screen.getByText("Buscar chat")).toBeInTheDocument();
    expect(screen.getByText("dConnector")).toBeInTheDocument();
    for (const t of TABS) expect(screen.getByText(t.label)).toBeInTheDocument();
    expect(CHAT_TABS).toEqual(TABS.map((t) => t.label));
    expect(screen.getByTestId("chat-sidebar-skeleton").className).toContain("w-[360px]");
    const src = source("components/chat/ChatSidebar.tsx");
    expect(src).toContain("flex h-full w-[360px] shrink-0 flex-col border-r");
    expect(src).toContain("flex h-[68px] shrink-0 items-center");
    expect(src).toContain("flex h-[42px] shrink-0 gap-5 border-b");
  });

  it("las filas miden lo que la fila real (72 px, avatar de 48)", () => {
    expect(CHAT_ROW_HEIGHT).toBe(CHAT_ROW);
    render(<ChatRowsSkeleton />);
    const rows = screen.getAllByTestId("chat-row-skeleton");
    expect(rows.length).toBeGreaterThanOrEqual(9);
    expect(rows[0]).toHaveStyle({ height: "72px" });
    expect(rows[0].querySelector(".h-12.w-12.rounded-full")).not.toBeNull();
  });
});

describe("Cuentas", () => {
  it("cabecera real y tarjetas con la misma cuadrícula y forma que AccountCard", () => {
    render(<AccountsSkeleton />);
    expect(screen.getByRole("heading", { name: "Cuentas" })).toBeInTheDocument();
    expect(screen.getByText("Añadir cuenta")).toBeInTheDocument();
    const cards = screen.getAllByTestId("account-card-skeleton");
    expect(cards).toHaveLength(3);
    expect(cards[0].className).toContain("p-4 pl-5");
    expect(source("components/accounts/AccountCard.tsx")).toContain("card relative flex flex-col gap-3 overflow-hidden p-4 pl-5");
    expect(source("pages/AccountsPage.tsx")).toContain("minmax(min(340px, 100%), 1fr)");
  });

  it("la zona sola sí avisa «Cargando» (la usa la página real)", () => {
    render(<AccountsGridSkeleton />);
    expect(screen.getAllByRole("status", { name: "Cargando" })).toHaveLength(1);
  });
});

describe("Campañas", () => {
  it("cabecera con sus dos botones y filas con las cinco columnas de CampaignRow", () => {
    const { container } = render(<CampaignsSkeleton />);
    expect(screen.getByRole("heading", { name: "Campañas" })).toBeInTheDocument();
    expect(screen.getByText("Ritmo de la cuenta")).toBeInTheDocument();
    expect(screen.getByText("Nueva campaña")).toBeInTheDocument();
    expect(source("components/campaigns/CampaignRow.tsx")).toContain(CAMPAIGN_ROW_COLUMNS);
    expect(container.querySelector(`.${CSS.escape(CAMPAIGN_ROW_COLUMNS)}`)).not.toBeNull();
    expect(container.querySelector(".max-w-5xl")).not.toBeNull();
    expect(source("pages/CampaignsPage.tsx")).toContain("mx-auto flex max-w-5xl flex-col gap-2");
  });
});

describe("Destinos", () => {
  it("tarjetas de destino de 768 px máx. con punto, texto, estado y dos botones", () => {
    const { container } = render(<DestinationsSkeleton />);
    expect(screen.getByRole("heading", { name: "Destinos" })).toBeInTheDocument();
    expect(screen.getByText("Nuevo destino")).toBeInTheDocument();
    expect(container.querySelector(".max-w-3xl.flex-col.gap-3")).not.toBeNull();
    expect(source("pages/DestinationsPage.tsx")).toContain("mx-auto flex max-w-3xl flex-col gap-3");
    expect(source("components/destinations/DestinationCard.tsx")).toContain("card flex items-center gap-4 px-4 py-3");
    expect(container.querySelectorAll(".card.flex.items-center.gap-4.px-4.py-3").length).toBeGreaterThanOrEqual(4);
  });
});

describe("Programados", () => {
  it("pestañas de estado reales y tabla con las siete columnas de ScheduledTable", () => {
    const { container } = render(<ScheduledSkeleton />);
    expect(screen.getByRole("heading", { name: "Programados" })).toBeInTheDocument();
    expect(SCHEDULED_FILTER_LABELS).toEqual(FILTERS.map((f) => f.label));
    for (const f of FILTERS) expect(screen.getByText(f.label)).toBeInTheDocument();
    expect(SCHEDULED_GRID).toBe(`grid-cols-[${SCHEDULED_COLS.replaceAll(" ", "_")}]`);
    expect(source("components/scheduled/ScheduledTable.tsx")).toContain(SCHEDULED_COLS);
    for (const h of ["Fecha", "Cuenta", "Destino", "Tipo", "Contenido", "Estado"]) expect(screen.getByText(h)).toBeInTheDocument();
    expect(container.querySelectorAll(`.${CSS.escape(SCHEDULED_GRID)}`).length).toBe(9);
  });
});

describe("Contactos", () => {
  it("columna de listas de 260 px, cabecera real y tabla de cinco columnas con filas de 56 px", () => {
    render(<ContactsSkeleton />);
    expect(screen.getByRole("heading", { name: "Contactos" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Listas" })).toBeInTheDocument();
    expect(screen.getByText("Todos")).toBeInTheDocument();
    for (const t of ["Buscar", "Etiqueta", "Origen: todos", "Actualizar desde las cuentas", "Nuevo contacto", "Nombre", "Teléfono", "Canales", "Etiquetas"]) {
      expect(screen.getAllByText(t).length).toBeGreaterThan(0);
    }
    expect(screen.getByTestId("lists-sidebar-skeleton").className).toContain("w-[260px]");
    expect(ZONE_CONTACT_COLUMNS).toBe(CONTACT_COLUMNS);
    expect(CONTACT_ROW_HEIGHT).toBe(CONTACT_ROW);
    const src = source("pages/ContactsPage.tsx");
    expect(src).toContain("flex flex-wrap items-center gap-x-3 gap-y-2 border-b border-line px-6 py-3");
    expect(source("components/contacts/ListsSidebar.tsx")).toContain("flex w-[260px] shrink-0 flex-col border-r border-line bg-panel");
  });

  it("las filas de la tabla llevan avatar de 36 y las mismas columnas", () => {
    const { container } = render(<ContactRowsSkeleton rows={4} />);
    const rows = Array.from(container.querySelectorAll<HTMLElement>("[style*='grid-template-columns']"));
    expect(rows).toHaveLength(4);
    expect(rows[0].style.gridTemplateColumns.replaceAll(", ", ",")).toBe(CONTACT_COLUMNS);
    expect(rows[0].style.height).toBe("56px");
    expect(rows[0].querySelector(".h-9.w-9.rounded-full")).not.toBeNull();
  });

  it("las listas llevan punto de 12 px", () => {
    const { container } = render(<ListRowsSkeleton />);
    expect(container.querySelectorAll(".h-3.w-3.rounded-full")).toHaveLength(3);
  });
});

describe("Estadísticas", () => {
  it("cabecera real (pestañas y periodos) y las cifras y tarjetas reales de Conversaciones", () => {
    render(<StatsSkeleton />);
    expect(screen.getByRole("heading", { name: "Estadísticas" })).toBeInTheDocument();
    for (const t of STATS_TABS) expect(screen.getByText(t)).toBeInTheDocument();
    expect(STATS_PERIODS).toEqual([...PRESET_DAYS.map((d) => presetLabel(d)), presetLabel("custom")]);
    for (const k of CONVERSATION_KPIS) expect(screen.getByText(k)).toBeInTheDocument();
    for (const t of ["Mensajes por día", "Horas de más actividad", "Tiempos de respuesta", "Quién inicia", "Chats más activos", "Chats más frecuentes"]) {
      expect(screen.getByRole("heading", { name: t })).toBeInTheDocument();
    }
    expect(screen.queryByTestId("stats-accounts-skeleton")).toBeNull();
  });

  it("las cifras y los títulos coinciden con los de las pestañas reales", () => {
    const conv = source("components/stats/ConversationsTab.tsx");
    for (const k of CONVERSATION_KPIS) expect(conv).toContain(`label="${k}"`);
    for (const t of ["Mensajes por día", "Horas de más actividad", "Tiempos de respuesta", "Quién inicia", "Chats más activos", "Chats más frecuentes"]) {
      expect(conv).toContain(`<Card title="${t}">`);
    }
    expect(conv).toContain("grid grid-cols-2 gap-3 xl:grid-cols-4");
    expect(conv).toContain("h-56 w-full");
    const groups = source("components/stats/GroupsTab.tsx");
    for (const k of GROUP_KPIS) expect(groups).toContain(`label="${k}"`);
  });

  it("con varias cuentas se anticipa la fila de cuentas", () => {
    const acc = (id: string) => ({ account: { id, enabled: true } }) as never;
    useAccounts.setState({ views: { a: acc("a"), b: acc("b") }, order: ["a", "b"], loaded: true });
    render(<StatsSkeleton />);
    expect(screen.getByTestId("stats-accounts-skeleton")).toBeInTheDocument();
  });

  it("Grupos y estadísticas de estados tienen su propia forma", () => {
    const { unmount } = render(<GroupsStatsSkeleton />);
    for (const k of GROUP_KPIS) expect(screen.getByText(k)).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Personas más activas" })).toBeInTheDocument();
    unmount();
    render(<StatusStatsSkeleton />);
    for (const k of ["Publicados", "Vistas medias", "Alcance"]) expect(screen.getByText(k)).toBeInTheDocument();
    render(<ConversationsStatsSkeleton />);
  });
});

describe("Estados", () => {
  it("columna de 360 px con sus filtros reales, visor con pestañas reales y pantalla 9:16", () => {
    render(<StatusesSkeleton />);
    expect(screen.getByRole("heading", { name: "Estados" })).toBeInTheDocument();
    for (const t of STATUS_TABS) expect(screen.getAllByText(t).length).toBeGreaterThan(0);
    for (const t of ["Todos", "Ocultar vistos", "Nuevo estado", "Recientes", "Vistos"]) expect(screen.getAllByText(t).length).toBeGreaterThan(0);
    expect(screen.getByTestId("status-sidebar-skeleton").className).toContain("w-[360px]");
    expect(screen.getByTestId("story-skeleton").querySelector(".aspect-\\[9\\/16\\]")).not.toBeNull();
    const src = source("pages/StatusesPage.tsx");
    for (const t of STATUS_TABS) expect(src).toContain(`"${t}"]`);
    const side = source("components/status/StatusSidebar.tsx");
    expect(side).toContain("flex h-full w-[360px] shrink-0 flex-col border-r border-line bg-panel");
    expect(side).toContain("flex h-[62px] w-full items-center gap-3");
  });

  it("filas de 62 px con anillo de 50, y tarjetas de «Mis estados» en cuadrícula de 132 px", () => {
    const { container, unmount } = render(<StatusListSkeleton />);
    expect(container.querySelectorAll(".h-\\[62px\\]").length).toBe(7);
    expect(container.querySelector(".h-\\[50px\\].w-\\[50px\\].rounded-full")).not.toBeNull();
    unmount();
    const r = render(<MyStatusesGridSkeleton />);
    expect(r.container.querySelectorAll(".aspect-\\[9\\/16\\]")).toHaveLength(6);
    expect(source("components/status/MyStatusesGrid.tsx")).toContain("repeat(auto-fill, minmax(132px, 1fr))");
  });
});

describe("Ajustes", () => {
  it("cabecera, menú de secciones de 224 px (reales) y la tarjeta «General» con sus etiquetas reales", () => {
    render(<SettingsSkeleton />);
    expect(screen.getByRole("heading", { name: "Ajustes" })).toBeInTheDocument();
    const src = source("pages/SettingsPage.tsx");
    for (const s of SETTINGS_SECTIONS) {
      expect(src).toContain(`{ id: "${s.id}", label: "${s.label}" }`);
      expect(screen.getAllByText(s.label).length).toBeGreaterThan(0);
    }
    for (const r of GENERAL_ROWS) expect(screen.getByText(r.label)).toBeInTheDocument();
    expect(GENERAL_ROWS[3].label).toBe(AUTO_HISTORY_LABEL);
    expect(src).toContain("w-56 shrink-0 border-r border-line bg-panel p-3");
    expect(src).toContain("mx-auto max-w-2xl");
    const card = screen.getByLabelText("General");
    expect(within(card).getAllByText(/./).length).toBeGreaterThan(3);
  });

  it("las demás secciones muestran su título con filas genéricas", () => {
    render(<SettingsSectionSkeleton section="about" />);
    expect(screen.getByRole("heading", { name: "Acerca de" })).toBeInTheDocument();
  });
});
