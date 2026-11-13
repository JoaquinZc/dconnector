// Esqueletos de zona: cada uno copia la forma de lo que sustituye (columnas, alturas, tamaños de avatar, márgenes) y
// lo usan tanto las páginas, mientras cargan sus datos, como los esqueletos de página (`*Skeleton.tsx`).
// Regla: las clases de maquetación son las mismas que las del componente real; si cambias aquellas, cambia estas.
import { ChevronRight, History } from "lucide-react";
import { Card } from "@/components/stats/StatsParts";
import { Skeleton } from "@/components/common/Skeleton";
import { SettingRow, SettingsCard } from "@/components/settings/SettingRow";
import { Line, pct, statusProps, times, type ZoneProps } from "./parts";

const CELL_GREY = "bg-[color-mix(in_srgb,var(--muted)_20%,transparent)]";

// ---------------------------------------------------------------- Chats

export const CHAT_ROW_HEIGHT = 72;

/** Lista de chats: filas de 72 px con avatar de 48, nombre y hora, vista previa e insignia (como `ChatRow`). */
export function ChatRowsSkeleton({ rows = 12, bare }: ZoneProps & { rows?: number }) {
  return (
    <div {...statusProps(bare)} data-testid="chat-list-skeleton" className="min-h-0 flex-1 overflow-hidden">
      {times(rows, (i) => (
        <div key={i} data-testid="chat-row-skeleton" className="flex items-center gap-3 px-4" style={{ height: CHAT_ROW_HEIGHT }}>
          <Skeleton className="h-12 w-12 shrink-0 rounded-full" />
          <div className="flex min-w-0 flex-1 flex-col justify-center gap-[3px] border-b border-[var(--border)]" style={{ height: CHAT_ROW_HEIGHT }}>
            <div className="flex min-w-0 items-center gap-2">
              <div className="min-w-0 flex-1">
                <Line lh={22} bar={14} w={pct(i, 38, 40)} />
              </div>
              <Skeleton className="h-3 w-9 shrink-0" />
            </div>
            <div className="flex min-w-0 items-center gap-2">
              <div className="min-w-0 flex-1">
                <Line lh={20} bar={12} w={pct(i, 48, 42, 23)} />
              </div>
              {i % 3 === 0 && <Skeleton className="h-5 w-5 shrink-0 rounded-full" />}
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------- Cuentas

/** Tarjeta de cuenta: franja de color, asa, icono de 36, nombre, estado, historial y botones (como `AccountCard`). */
export function AccountCardSkeleton({ i = 0 }: { i?: number }) {
  return (
    <div data-testid="account-card-skeleton" className="card relative flex flex-col gap-3 overflow-hidden p-4 pl-5">
      <span aria-hidden="true" className={`absolute inset-y-0 left-0 w-1.5 ${CELL_GREY}`} />
      <div className="flex items-start gap-3">
        <Skeleton className="h-8 w-6 shrink-0" />
        <Skeleton className="h-9 w-9 shrink-0 rounded-full" />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <Skeleton className="h-2.5 w-2.5 shrink-0 rounded-full" />
            <Line lh={24} bar={14} w={pct(i, 40, 30)} className="min-w-0 flex-1" />
          </div>
          <Line lh={21} bar={12} w={pct(i, 50, 25, 13)} />
        </div>
      </div>
      <div className="min-h-[38px]">
        <div className="flex items-center gap-2" style={{ height: 24 }}>
          <Skeleton className="h-2 w-2 shrink-0 rounded-full" />
          <Skeleton className="h-3.5 w-24" />
        </div>
      </div>
      <div className="rounded-lg border border-line bg-panel2/40" aria-hidden="true">
        <div className="flex w-full items-center gap-2 px-3 py-2 text-sm font-medium">
          <ChevronRight size={15} />
          <History size={15} className="text-muted" />
          <span className="flex-1">Historial de mensajes</span>
        </div>
      </div>
      <div className="flex items-center gap-2">
        <Skeleton className="h-7 w-28 rounded-lg" />
        <Skeleton className="h-7 w-20 rounded-lg" />
        <Skeleton className="ml-auto h-8 w-8 rounded-lg" />
      </div>
    </div>
  );
}

/** Cuadrícula de tarjetas de cuenta (mismas columnas que la página: `auto-fill` de 340 px). */
export function AccountsGridSkeleton({ count = 3, bare }: ZoneProps & { count?: number }) {
  return (
    <div
      {...statusProps(bare)}
      className="grid gap-4"
      style={{ gridTemplateColumns: "repeat(auto-fill, minmax(min(340px, 100%), 1fr))" }}
    >
      {times(count, (i) => (
        <AccountCardSkeleton key={i} i={i} />
      ))}
    </div>
  );
}

// ---------------------------------------------------------------- Campañas

/** Columnas de una fila de campaña (las mismas que `CampaignRow`). */
export const CAMPAIGN_ROW_COLUMNS = "grid-cols-[minmax(0,1.4fr)_auto_minmax(120px,1fr)_minmax(0,1.2fr)_auto]";

/** Lista de campañas: tarjeta con nombre y cuenta, estado, progreso, línea de situación y flecha. */
export function CampaignRowsSkeleton({ count = 6, bare }: ZoneProps & { count?: number }) {
  return (
    <div {...statusProps(bare)} className="mx-auto flex max-w-5xl flex-col gap-2" data-testid="campaign-list-skeleton">
      {times(count, (i) => (
        <div key={i} className={`card grid w-full ${CAMPAIGN_ROW_COLUMNS} items-center gap-4 px-4 py-3`}>
          <div className="min-w-0">
            <Line lh={21} bar={14} w={pct(i, 45, 35)} />
            <div className="mt-0.5 flex" style={{ height: 18 }}>
              <Skeleton className="h-[18px] w-20 rounded-full" />
            </div>
          </div>
          <Skeleton className="h-[22px] w-14 rounded-full" />
          <div className="min-w-0">
            <Line lh={20} bar={12} w={36} className="mb-1" />
            <Skeleton className="h-1.5 w-full rounded-full" />
          </div>
          <Line lh={20} bar={12} w={pct(i, 50, 40, 11)} />
          <Skeleton className="h-4 w-4" />
        </div>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------- Destinos

/** Lista de destinos: punto de estado, nombre, URL y alcance, estado de conexión y dos botones (como `DestinationCard`). */
export function DestinationCardsSkeleton({ count = 5, bare }: ZoneProps & { count?: number }) {
  return (
    <div {...statusProps(bare)} className="mx-auto flex max-w-3xl flex-col gap-3" data-testid="destination-list-skeleton">
      {times(count, (i) => (
        <div key={i} className="card flex items-center gap-4 px-4 py-3">
          <Skeleton className="h-2.5 w-2.5 shrink-0 rounded-full" />
          <div className="min-w-0 flex-1">
            <Line lh={21} bar={14} w={pct(i, 30, 30)} />
            <Line lh={16} bar={10} w={pct(i, 50, 35, 13)} />
            <Line lh={16} bar={10} w={pct(i, 35, 30, 11)} />
          </div>
          <div className="flex min-w-0 max-w-[260px] flex-col items-end">
            <Line lh={20} bar={12} w={90} />
          </div>
          <Skeleton className="h-8 w-8 shrink-0 rounded-lg" />
          <Skeleton className="h-8 w-8 shrink-0 rounded-lg" />
        </div>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------- Programados

/** Mismas columnas que `ScheduledTable`. */
export const SCHEDULED_GRID = "grid-cols-[150px_130px_minmax(0,1fr)_90px_minmax(0,1.6fr)_110px_168px]";
export const SCHEDULED_HEADERS = ["Fecha", "Cuenta", "Destino", "Tipo", "Contenido", "Estado"] as const;

/** Tabla de programados: cabecera real y filas con sus siete columnas. */
export function ScheduledTableSkeleton({ rows = 8, bare }: ZoneProps & { rows?: number }) {
  return (
    <div {...statusProps(bare)} className="card overflow-hidden" data-testid="scheduled-table-skeleton">
      <div className={`grid ${SCHEDULED_GRID} items-center gap-3 border-b border-line bg-panel2 px-4 py-2 text-xs font-semibold text-muted`}>
        {SCHEDULED_HEADERS.map((h) => (
          <span key={h}>{h}</span>
        ))}
        <span />
      </div>
      {times(rows, (i) => (
        <div key={i} className={`grid ${SCHEDULED_GRID} items-center gap-3 border-b border-line px-4 py-2 text-sm last:border-b-0`}>
          <Line lh={20} bar={12} w={110} />
          <Skeleton className="h-[18px] w-16 rounded-full" />
          <Line lh={20} bar={12} w={pct(i, 45, 40)} />
          <Line lh={20} bar={12} w={44} />
          <div className="min-w-0">
            <Line lh={20} bar={12} w={pct(i, 50, 40, 11)} />
          </div>
          <Skeleton className="h-[22px] w-20 rounded-full" />
          <div className="flex justify-end gap-0.5">
            <Skeleton className="h-8 w-8 rounded-lg" />
            <Skeleton className="h-8 w-8 rounded-lg" />
          </div>
        </div>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------- Contactos

/** Mismas columnas que `ContactsTable`. */
export const CONTACT_COLUMNS = "minmax(200px,2.2fr) 150px 110px minmax(120px,1.6fr) minmax(120px,1.6fr)";
export const CONTACT_ROW_HEIGHT = 56;

/** Cabecera de la tabla de contactos (texto real, no cambia). */
export function ContactsTableHead() {
  return (
    <div
      className="grid items-center gap-3 border-b border-line bg-panel px-4 py-2 text-xs font-semibold text-muted"
      style={{ gridTemplateColumns: CONTACT_COLUMNS }}
    >
      <span>Nombre</span>
      <span>Teléfono</span>
      <span>Canales</span>
      <span>Listas</span>
      <span>Etiquetas</span>
    </div>
  );
}

/** Filas de la tabla de contactos: avatar de 36 con nombre, teléfono, iconos de canal y chips de listas y etiquetas. */
export function ContactRowsSkeleton({ rows = 12, bare }: ZoneProps & { rows?: number }) {
  return (
    <div {...statusProps(bare)} className="flex flex-col" data-testid="contact-rows-skeleton">
      {times(rows, (i) => (
        <div
          key={i}
          className="grid items-center gap-3 border-b border-line px-4"
          style={{ gridTemplateColumns: CONTACT_COLUMNS, height: CONTACT_ROW_HEIGHT }}
        >
          <div className="flex min-w-0 items-center gap-3">
            <Skeleton className="h-9 w-9 shrink-0 rounded-full" />
            <Skeleton className="h-3.5" style={{ width: pct(i, 35, 45) }} />
          </div>
          <Skeleton className="h-3.5 w-28" />
          <div className="flex items-center gap-1">
            <Skeleton className="h-[18px] w-[18px] rounded-full" />
            {i % 3 === 0 && <Skeleton className="h-[18px] w-[18px] rounded-full" />}
          </div>
          <div className="flex min-w-0 gap-1">{i % 2 === 0 && <Skeleton className="h-6 w-16 rounded-full" />}</div>
          <div className="flex min-w-0 gap-1">
            {i % 3 !== 2 && <Skeleton className="h-6 w-14 rounded-full" />}
            {i % 4 === 0 && <Skeleton className="h-6 w-12 rounded-full" />}
          </div>
        </div>
      ))}
    </div>
  );
}

/** Filas de las listas de contactos: punto de color, nombre y cuenta (como `ListsSidebar`). */
export function ListRowsSkeleton({ rows = 3, bare }: ZoneProps & { rows?: number }) {
  return (
    <div {...statusProps(bare)} data-testid="lists-skeleton">
      {times(rows, (i) => (
        <div key={i} className="flex items-center gap-2.5 px-3 py-2">
          <Skeleton className="h-3 w-3 shrink-0 rounded-full" />
          <Line lh={24} bar={14} w={pct(i, 45, 35)} className="min-w-0 flex-1" />
          <Skeleton className="h-3 w-4 shrink-0" />
        </div>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------- Estadísticas

/** Una cifra: la etiqueta real y el número pendiente (como `Kpi`). */
export function KpiSkeleton({ label, className = "card px-4 py-3" }: { label: string; className?: string }) {
  return (
    <div className={className}>
      <p className="truncate text-xs font-semibold text-muted">{label}</p>
      <div className="mt-1 flex h-8 items-center">
        <Skeleton className="h-6 w-16" />
      </div>
    </div>
  );
}

const DAYS = ["Lun", "Mar", "Mié", "Jue", "Vie", "Sáb", "Dom"];

/** Mapa de calor 7 × 24 con las mismas celdas (16 px, hueco de 2) que el real. */
function HeatmapSkeleton() {
  return (
    <div className="flex flex-col gap-2">
      <div className="overflow-x-auto">
        <table className="animate-pulse border-separate motion-reduce:animate-none" style={{ borderSpacing: 2 }} aria-hidden="true">
          <tbody>
            {DAYS.map((d) => (
              <tr key={d}>
                <th scope="row" className="pr-1 text-left text-[10px] font-semibold text-muted">
                  {d}
                </th>
                {times(24, (h) => (
                  <td key={h} className={`h-4 w-4 rounded-sm ${CELL_GREY}`} />
                ))}
              </tr>
            ))}
            <tr>
              <td />
              {times(24, (h) => (
                <td key={h} className="text-center text-[9px] text-muted">
                  {h % 3 === 0 ? h : ""}
                </td>
              ))}
            </tr>
          </tbody>
        </table>
      </div>
      <Line lh={16} bar={10} w="45%" />
    </div>
  );
}

/** Barras verticales de un gráfico de 224 px (`h-56`). */
function BarsChartSkeleton() {
  const heights = [40, 65, 30, 80, 55, 70, 35, 60, 45, 75, 50, 38];
  return (
    <div className="flex h-56 w-full items-end gap-2 px-2 pb-6" aria-hidden="true">
      {heights.map((h, i) => (
        <Skeleton key={i} className="flex-1 rounded-b-none" style={{ height: `${h}%` }} />
      ))}
    </div>
  );
}

/** Lista numerada con avatar de 32 y barra (como `RankList`). */
export function RankListSkeleton({ rows = 6 }: { rows?: number }) {
  return (
    <ol className="flex flex-col" aria-hidden="true">
      {times(rows, (i) => (
        <li key={i} className="border-b border-line last:border-b-0">
          <div className="flex items-center gap-3 px-1 py-2">
            <span className="w-5 shrink-0 text-xs font-semibold tabular-nums text-muted">{i + 1}</span>
            <Skeleton className="h-8 w-8 shrink-0 rounded-full" />
            <span className="min-w-0 flex-1">
              <Line lh={20} bar={12} w={pct(i, 35, 40)} />
              <Skeleton className="block h-1.5 rounded-full" style={{ width: `${Math.max(20, 100 - i * 14)}%` }} />
            </span>
            <Skeleton className="h-3 w-16 shrink-0" />
          </div>
        </li>
      ))}
    </ol>
  );
}

export const CONVERSATION_KPIS = [
  "Enviados",
  "Recibidos",
  "Chats con actividad",
  "Tu tiempo de respuesta",
  "Respondes en menos de 1 h",
  "Te responden en",
  "Conversaciones que inicias",
  "Conversaciones que inician",
] as const;
export const GROUP_KPIS = ["Mensajes en grupos", "Enviados", "Recibidos", "Grupos activos"] as const;

/** Pestaña «Conversaciones»: ocho cifras, mensajes por día, mapa de calor, tiempos, quién inicia y dos rankings. */
export function ConversationsStatsSkeleton({ bare }: ZoneProps) {
  return (
    <div {...statusProps(bare)} className="flex flex-col gap-4" data-testid="conversations-skeleton">
      <div className="grid grid-cols-2 gap-3 xl:grid-cols-4">
        {CONVERSATION_KPIS.map((k) => (
          <KpiSkeleton key={k} label={k} />
        ))}
      </div>
      <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
        <Card title="Mensajes por día">
          <BarsChartSkeleton />
        </Card>
        <Card title="Horas de más actividad">
          <HeatmapSkeleton />
          <h3 className="label !mb-2 mt-4">Qué se envía</h3>
          <div className="flex flex-col gap-2">
            <Skeleton className="h-3 w-full rounded-full" />
            <Line lh={16} bar={10} w="70%" />
          </div>
        </Card>
      </div>
      <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
        <Card title="Tiempos de respuesta">
          <div className="flex h-44 w-full flex-col justify-center gap-6 pl-2 pr-4" aria-hidden="true">
            <Skeleton className="h-9 w-4/5" />
            <Skeleton className="h-9 w-3/5" />
          </div>
        </Card>
        <Card title="Quién inicia">
          <div className="flex h-44 w-full items-center justify-center" aria-hidden="true">
            <Skeleton className="h-36 w-36 rounded-full" />
          </div>
        </Card>
      </div>
      <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
        <Card title="Chats más activos">
          <RankListSkeleton />
        </Card>
        <Card title="Chats más frecuentes">
          <RankListSkeleton />
        </Card>
      </div>
    </div>
  );
}

/** Pestaña «Grupos»: cuatro cifras y dos rankings. */
export function GroupsStatsSkeleton({ bare }: ZoneProps) {
  return (
    <div {...statusProps(bare)} className="flex flex-col gap-4" data-testid="groups-skeleton">
      <div className="grid grid-cols-2 gap-3 xl:grid-cols-4">
        {GROUP_KPIS.map((k) => (
          <KpiSkeleton key={k} label={k} />
        ))}
      </div>
      <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
        <Card title="Grupos más activos">
          <RankListSkeleton />
        </Card>
        <Card title="Personas más activas">
          <RankListSkeleton />
        </Card>
      </div>
    </div>
  );
}

/** Estadísticas de mis estados: tres cifras y la curva de vistas (como `StatusStatsPanel`). */
export function StatusStatsSkeleton({ bare }: ZoneProps) {
  return (
    <div {...statusProps(bare)} className="flex flex-col gap-4" data-testid="status-stats-skeleton">
      <div className="flex gap-3">
        {["Publicados", "Vistas medias", "Alcance"].map((k) => (
          <KpiSkeleton key={k} label={k} className="card flex-1 px-4 py-3" />
        ))}
      </div>
      <div className="card p-4">
        <p className="label">Vistas</p>
        <BarsChartSkeleton />
      </div>
    </div>
  );
}

// ---------------------------------------------------------------- Estados

/** Lista de la columna de estados: sección de canal, cuenta, «Recientes» y «Vistos» con filas de 62 px y anillo de 50. */
export function StatusListSkeleton({ bare }: ZoneProps) {
  const label = "px-4 pb-0.5 pt-1.5 text-[11.5px] font-semibold uppercase tracking-[0.4px] text-muted";
  const row = (i: number) => (
    <div key={i} className="flex h-[62px] w-full items-center gap-3 px-4">
      <Skeleton className="h-[50px] w-[50px] shrink-0 rounded-full" />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <Line lh={20} bar={13} w={pct(i, 40, 35)} />
        <Line lh={18} bar={11} w={pct(i, 55, 30, 11)} />
      </div>
    </div>
  );
  return (
    <div {...statusProps(bare)} data-testid="status-list-skeleton">
      <div className="flex h-[42px] w-full items-center gap-2.5 border-b border-line bg-panel px-4">
        <Skeleton className="h-6 w-6 shrink-0 rounded-full" />
        <Skeleton className="h-3.5 w-24 flex-1" style={{ maxWidth: 96 }} />
        <span className="flex-1" />
        <Skeleton className="h-4 w-4" />
      </div>
      <div className="flex items-center gap-[7px] px-4 pb-1 pt-3">
        <Skeleton className="h-2 w-2 rounded-full" />
        <Line lh={19} bar={11} w={112} />
      </div>
      <div className={label}>Recientes</div>
      {times(4, row)}
      <div className={label}>Vistos</div>
      {times(3, (i) => row(i + 4))}
    </div>
  );
}

/** Tarjetas de mis estados: miniatura 9:16 con dos líneas (cuadrícula de 132 px mínimo, como `MyStatusesGrid`). */
export function MyStatusesGridSkeleton({ count = 6, bare }: ZoneProps & { count?: number }) {
  return (
    <div {...statusProps(bare)} className="grid gap-4" style={{ gridTemplateColumns: "repeat(auto-fill, minmax(132px, 1fr))" }}>
      {times(count, (i) => (
        <div key={i} className="card overflow-hidden">
          <Skeleton className="aspect-[9/16] w-full rounded-none" />
          <div className="flex flex-col gap-1.5 px-2.5 py-2">
            <Skeleton className="h-3 w-2/3" />
            <Skeleton className="h-2.5 w-1/2" />
          </div>
        </div>
      ))}
    </div>
  );
}

/** Visor de estados: la pantalla vertical 9:16 centrada en el fondo del chat. */
export function StoryViewerSkeleton({ bare }: ZoneProps) {
  return (
    <div {...statusProps(bare)} className="flex h-full items-center justify-center" data-testid="story-skeleton">
      <Skeleton className="aspect-[9/16] h-[70%] max-h-[640px] max-w-[90%] rounded-xl" />
    </div>
  );
}

// ---------------------------------------------------------------- Ajustes

/** Filas de la sección «General» (etiquetas reales; los interruptores y el tema esperan). */
export const GENERAL_ROWS = [
  { label: "Cerrar a la bandeja", control: "switch" },
  { label: "Iniciar con Windows", control: "switch" },
  { label: "Iniciar minimizado", control: "switch" },
  {
    label: "Guardar el historial automáticamente (despacio, en segundo plano)",
    hint: "Pide en segundo plano los mensajes anteriores de tus chats, poco a poco, para no arriesgar bloqueos en tus cuentas.",
    control: "switch",
  },
  { label: "Tema", control: "theme" },
] as const;

export const SETTINGS_SECTIONS = [
  { id: "general", label: "General" },
  { id: "connection", label: "Conexión" },
  { id: "updates", label: "Actualizaciones" },
  { id: "notifications", label: "Notificaciones" },
  { id: "downloads", label: "Descargas automáticas" },
  { id: "campaigns", label: "Campañas" },
  { id: "data", label: "Datos" },
  { id: "about", label: "Acerca de" },
] as const;
export type SettingsSectionId = (typeof SETTINGS_SECTIONS)[number]["id"];

/** Una sección de Ajustes: la tarjeta con su título real y filas; «General» con sus etiquetas reales. */
export function SettingsSectionSkeleton({ section = "general", bare }: ZoneProps & { section?: SettingsSectionId }) {
  const title = SETTINGS_SECTIONS.find((s) => s.id === section)?.label ?? "General";
  return (
    <div {...statusProps(bare)} data-testid="settings-section-skeleton">
      <SettingsCard title={title}>
        {section === "general"
          ? GENERAL_ROWS.map((r) => (
              <SettingRow key={r.label} label={r.label} hint={"hint" in r ? r.hint : undefined}>
                {r.control === "switch" ? (
                  <div className="flex h-[25px] items-center">
                    <Skeleton className="h-5 w-9 rounded-full" />
                  </div>
                ) : (
                  <Skeleton className="h-[33px] w-52 rounded-lg" />
                )}
              </SettingRow>
            ))
          : times(4, (i) => (
              <div key={i} className="flex items-center justify-between gap-6 px-5 py-3">
                <div className="min-w-0 flex-1">
                  <Line lh={21} bar={14} w={pct(i, 30, 35)} />
                </div>
                <Skeleton className="h-8 w-36 shrink-0 rounded-lg" />
              </div>
            ))}
      </SettingsCard>
    </div>
  );
}
