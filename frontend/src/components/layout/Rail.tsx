import { useEffect, useMemo } from "react";
import { Plug } from "lucide-react";
import { useConnection } from "@/api/connection";
import type { CapSet } from "@/api/types";
import { useAccounts, type Health } from "@/store/accounts";
import { useInbox } from "@/store/inbox";
import { useStatuses } from "@/store/statuses";
import { useUI, type Page } from "@/store/ui";
import { cx } from "@/lib/cx";
import { pageMeta } from "./pages";

const TOP: Page[] = ["chats", "statuses", "campaigns", "scheduled", "contacts", "stats", "destinations"];
const BOTTOM: Page[] = ["accounts", "settings"];

const HEALTH_COLOR: Record<Exclude<Health, "none">, string> = {
  green: "var(--ok)",
  amber: "var(--warn)",
  red: "var(--danger)",
};

const CONN_TEXT: Record<Exclude<Health, "none">, string> = {
  green: "Conectado",
  amber: "Reconectando",
  red: "Sin conexión",
};

/** Texto de una insignia: máximo «99+». */
export function badgeText(n: number): string {
  return n > 99 ? "99+" : String(n);
}

interface ItemProps {
  page: Page;
  badge?: number;
  dot?: Health;
}

function RailItem({ page, badge, dot }: ItemProps) {
  const current = useUI((s) => s.page);
  const setPage = useUI((s) => s.setPage);
  const meta = pageMeta(page);
  const active = current === page;
  const Icon = meta.icon;
  const label = `${meta.label} (Ctrl+${meta.shortcut})`;
  return (
    <button
      type="button"
      title={label}
      aria-label={meta.label}
      aria-current={active ? "page" : undefined}
      onClick={() => setPage(page)}
      className={cx(
        "relative flex h-[58px] w-[72px] shrink-0 flex-col items-center justify-center gap-1 rounded-[14px] p-0 text-[10.5px] transition-colors max-[700px]:h-[52px]",
        active
          ? "bg-[var(--sel)] font-semibold text-[var(--accent-text)]"
          : "font-medium text-muted hover:bg-[var(--hover)] hover:text-ink",
      )}
    >
      <Icon size={22} strokeWidth={active ? 2.2 : 1.8} aria-hidden="true" />
      <span className="block max-w-[70px] truncate leading-[14px]" data-testid="rail-label">
        {meta.shortLabel ?? meta.label}
      </span>
      {!!badge && badge > 0 && (
        <span
          className="absolute right-[6px] top-[3px] box-content h-[18px] min-w-[18px] rounded-full border-2 border-panel bg-[var(--badge-bg)] px-[5px] text-center text-[11px] font-bold leading-[18px] text-[var(--badge-fg)]"
          data-testid="rail-badge"
          data-page={page}
        >
          {badgeText(badge)}
        </span>
      )}
      {dot && dot !== "none" && (
        <span
          className="absolute right-[14px] top-[8px] h-2.5 w-2.5 rounded-full border-2 border-panel"
          style={{ background: HEALTH_COLOR[dot] }}
          data-testid="rail-dot"
          data-health={dot}
        />
      )}
    </button>
  );
}

/** Indicador de conexión: el peor estado de las cuentas activas. No sale si no hay cuentas. */
function ConnectionStatus({ health }: { health: Health }) {
  if (health === "none") return null;
  return (
    <div
      role="status"
      className="mb-1 flex flex-col items-center gap-1 text-[11px] leading-[14px] text-muted"
      data-testid="rail-conn"
      data-health={health}
    >
      <i
        className="h-[9px] w-[9px] rounded-full"
        style={{ background: HEALTH_COLOR[health] }}
        aria-hidden="true"
        data-testid="rail-conn-dot"
      />
      <span className="max-w-[72px] truncate">{CONN_TEXT[health]}</span>
    </div>
  );
}

/** Barra lateral de 76 px: marca, páginas, indicador de conexión, cuentas y ajustes. */
export default function Rail() {
  const unread = useInbox((s) => s.unread.total);
  // Se suscribe a `views` para que el indicador cambie con cada estado.
  const accountsHealth = useAccounts((s) => s.worstState());
  // El enlace con el servidor manda sobre el de las cuentas: sin servidor no hay cuentas que mirar.
  const link = useConnection((s) => s.link);
  const health: Health = link === "offline" ? "red" : link === "reconnecting" ? "amber" : accountsHealth;
  const views = useAccounts((s) => s.views);
  const order = useAccounts((s) => s.order);
  const loaded = useAccounts((s) => s.loaded);
  const authors = useStatuses((s) => s.authors);
  const loadAuthors = useStatuses((s) => s.loadAuthors);

  // Cuentas habilitadas con estados: las mismas que usa la página de Estados.
  const statusIds = useMemo(
    () =>
      order
        .map((id) => views[id])
        .filter((v) => v && v.account.enabled && (v.capabilities as CapSet | undefined)?.statuses)
        .map((v) => v.account.id),
    [order, views],
  );
  const idsKey = statusIds.join("|");

  // Carga inicial y cuando cambian las cuentas con estados; `status:upsert` lo recarga el store.
  useEffect(() => {
    if (!loaded || !idsKey) return;
    void loadAuthors(idsKey.split("|"));
  }, [loaded, idsKey, loadAuthors]);

  const unviewed = useMemo(() => authors.reduce((n, a) => n + Math.max(0, a.unviewed || 0), 0), [authors]);

  const badgeOf = (p: Page) => (p === "chats" ? unread : p === "statuses" ? unviewed : undefined);

  return (
    <nav
      className="flex min-h-0 w-[80px] shrink-0 flex-col items-center border-r border-line bg-panel pb-3 pt-3.5 max-[700px]:pt-2.5"
      aria-label="Páginas"
    >
      <div
        className="mb-3 flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-accent text-[#04201a] max-[700px]:mb-2"
        aria-hidden="true"
        data-testid="rail-logo"
      >
        <Plug size={22} strokeWidth={2} />
      </div>
      <div className="flex min-h-0 flex-1 flex-col items-center gap-1 overflow-y-auto overflow-x-hidden max-[700px]:gap-0.5">
        {TOP.map((p) => (
          <RailItem key={p} page={p} badge={badgeOf(p)} />
        ))}
      </div>
      <div className="flex shrink-0 flex-col items-center gap-1 pt-1 max-[700px]:gap-0.5">
        <ConnectionStatus health={health} />
        {BOTTOM.map((p) => (
          <RailItem key={p} page={p} dot={p === "accounts" ? health : undefined} />
        ))}
      </div>
    </nav>
  );
}
