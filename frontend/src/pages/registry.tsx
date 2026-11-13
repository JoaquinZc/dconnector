// Registro de páginas. Solo las que ya existen se importan (un import() de un archivo que no existe
// rompe `tsc`); el resto muestra «Próximamente». El orquestador enlaza las demás al cerrar las
// oleadas 2 y 3: basta con añadir una línea al mapa `PAGE_COMPONENTS`.
import { Suspense, lazy, type ComponentType, type LazyExoticComponent } from "react";
import { Loader2 } from "lucide-react";
import { pageMeta } from "@/components/layout/pages";
import type { Page } from "@/store/ui";

type PageComponent = LazyExoticComponent<ComponentType> | ComponentType;

const AccountsPage = lazy(() => import("./AccountsPage"));
const ChatsPage = lazy(() => import("./ChatsPage")); // de A5

/** Páginas reales. Añadir aquí: `chats: lazy(() => import("./ChatsPage"))`, etc. */
export const PAGE_COMPONENTS: Partial<Record<Page, PageComponent>> = {
  chats: ChatsPage,
  accounts: AccountsPage,
  statuses: lazy(() => import("./StatusesPage")),
  campaigns: lazy(() => import("./CampaignsPage")),
  scheduled: lazy(() => import("./ScheduledPage")),
  contacts: lazy(() => import("./ContactsPage")),
  stats: lazy(() => import("./StatsPage")),
  destinations: lazy(() => import("./DestinationsPage")),
  settings: lazy(() => import("./SettingsPage")),
};

export function Placeholder({ page }: { page: Page }) {
  const meta = pageMeta(page);
  const Icon = meta.icon;
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 text-muted">
      <Icon size={44} strokeWidth={1.3} />
      <p className="text-lg font-medium text-ink">{meta.label}</p>
      <p>Próximamente</p>
    </div>
  );
}

function Loading() {
  return (
    <div className="flex h-full items-center justify-center text-muted">
      <Loader2 className="animate-spin" size={24} />
    </div>
  );
}

/** La página activa (o su marcador). */
export function PageView({ page }: { page: Page }) {
  const Component = PAGE_COMPONENTS[page];
  if (!Component) return <Placeholder page={page} />;
  return (
    <Suspense fallback={<Loading />}>
      <Component />
    </Suspense>
  );
}
