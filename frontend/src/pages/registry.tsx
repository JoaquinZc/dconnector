// Registro de páginas. Solo las que ya existen se importan (un import() de un archivo que no existe
// rompe `tsc`); el resto muestra «Próximamente». El orquestador enlaza las demás al cerrar las
// oleadas 2 y 3: basta con añadir una línea al mapa `PAGE_COMPONENTS`.
import { Suspense, lazy, useEffect, useState, type ComponentType, type LazyExoticComponent } from "react";
import { pageMeta } from "@/components/layout/pages";
import { PageActiveContext } from "@/lib/pageActive";
import { PAGE_SKELETONS } from "./skeletons";
import type { Page } from "@/store/ui";

type PageComponent = LazyExoticComponent<ComponentType> | ComponentType;

// Cada página es un trozo de código aparte (se descarga al entrar). Los cargadores viven en un mapa para poder
// precargarlos todos en segundo plano (`preloadPages`): así, al cambiar de página no hay que esperar la descarga.
const LOADERS: Partial<Record<Page, () => Promise<{ default: ComponentType }>>> = {
  chats: () => import("./ChatsPage"),
  accounts: () => import("./AccountsPage"),
  statuses: () => import("./StatusesPage"),
  campaigns: () => import("./CampaignsPage"),
  scheduled: () => import("./ScheduledPage"),
  contacts: () => import("./ContactsPage"),
  stats: () => import("./StatsPage"),
  destinations: () => import("./DestinationsPage"),
  settings: () => import("./SettingsPage"),
};

/** Páginas reales (cargadas bajo demanda). */
export const PAGE_COMPONENTS: Partial<Record<Page, PageComponent>> = Object.fromEntries(
  Object.entries(LOADERS).map(([page, load]) => [page, lazy(load)]),
);

/**
 * Descarga el código de todas las páginas justo después del primer pintado. Solo trae JavaScript: no llama al
 * servidor ni consulta datos. Es idempotente.
 */
let preloaded = false;
export function preloadPages(): void {
  if (preloaded) return;
  preloaded = true;
  // Tras el primer pintado y en paralelo: el código está en el propio programa, así que es rápido, y así
  // ninguna pestaña espera al navegador «libre».
  window.setTimeout(() => {
    for (const load of Object.values(LOADERS)) void load().catch(() => undefined);
  }, 0);
}

/** Páginas que consultan al servidor por su cuenta con un temporizador: se desmontan al salir (no se mantienen). */
const NO_KEEP_ALIVE: ReadonlySet<Page> = new Set<Page>(["scheduled", "settings"]);

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

/** Esqueleto a la medida de la página mientras llega su código; solo aparece si la espera pasa de 120 ms (sin destellos). */
function Loading({ page }: { page: Page }) {
  const [show, setShow] = useState(false);
  useEffect(() => {
    const t = window.setTimeout(() => setShow(true), 120);
    return () => window.clearTimeout(t);
  }, []);
  if (!show) return null;
  const Skeleton = PAGE_SKELETONS[page];
  return <Skeleton />;
}

function PageBody({ page }: { page: Page }) {
  const Component = PAGE_COMPONENTS[page];
  if (!Component) return <Placeholder page={page} />;
  return (
    <Suspense fallback={<Loading page={page} />}>
      <Component />
    </Suspense>
  );
}

/**
 * La página activa. Las que ya se visitaron se mantienen montadas pero ocultas, así volver a ellas es instantáneo y
 * conservan su desplazamiento y su estado; las páginas aún no visitadas no se montan (no consultan nada). Las que
 * tienen temporizador propio (`NO_KEEP_ALIVE`) se desmontan al salir.
 */
export function PageView({ page }: { page: Page }) {
  const [visited, setVisited] = useState<Page[]>([page]);
  useEffect(() => {
    setVisited((v) => (v.includes(page) ? v : [...v, page]));
  }, [page]);
  const mounted = (visited.includes(page) ? visited : [...visited, page]).filter(
    (p) => p === page || !NO_KEEP_ALIVE.has(p),
  );
  return (
    <>
      {mounted.map((p) => (
        <div key={p} className="h-full" hidden={p !== page} aria-hidden={p !== page}>
          <PageActiveContext.Provider value={p === page}>
            <PageBody page={p} />
          </PageActiveContext.Provider>
        </div>
      ))}
    </>
  );
}
