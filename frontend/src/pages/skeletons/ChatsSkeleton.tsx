// Chats: lista de 360 px con su marco real (título, buscador, filtro de cuentas, pestañas) y el panel vacío de la derecha.
import { ChevronDown, MessageSquare, MessageSquarePlus, Search } from "lucide-react";
import { Skeleton } from "@/components/common/Skeleton";
import { PageRoot } from "./parts";
import { ChatRowsSkeleton } from "./zones";

export const CHAT_TABS = ["Todos", "No leídos", "Grupos", "Archivados"] as const;

/** Columna izquierda de Chats: mismo marco que `ChatSidebar`, con filas de chat a la espera. */
export function ChatSidebarSkeleton() {
  return (
    <div className="flex h-full w-[360px] shrink-0 flex-col border-r border-[var(--border)] bg-[var(--panel)]" data-testid="chat-sidebar-skeleton">
      <header className="flex h-[68px] shrink-0 items-center justify-between pr-4 pl-5">
        <h1 className="text-[22px] font-semibold tracking-[-0.2px]">Chats</h1>
        <span className="flex h-[38px] w-[38px] shrink-0 items-center justify-center rounded-xl bg-[var(--accent)] text-[#04201a]">
          <MessageSquarePlus size={20} aria-hidden="true" />
        </span>
      </header>
      <div className="shrink-0 px-4 pb-3">
        <div className="flex h-10 items-center gap-2.5 rounded-xl border border-transparent bg-[var(--panel-2)] px-3 text-[var(--muted)]">
          <Search size={16} aria-hidden="true" className="shrink-0" />
          <span className="min-w-0 flex-1 text-[14px]">Buscar chat</span>
        </div>
      </div>
      <div className="relative flex shrink-0 items-center gap-1 px-4 pb-3">
        <span className="inline-flex h-[30px] shrink-0 items-center rounded-[15px] border-[1.5px] border-transparent bg-[var(--badge-bg)] px-3.5 text-[12.5px] font-semibold whitespace-nowrap text-[var(--badge-fg)]">
          Todas
        </span>
        <Skeleton className="h-[30px] w-9 shrink-0 rounded-[15px]" />
        <Skeleton className="h-[30px] w-9 shrink-0 rounded-[15px]" />
        <span className="ml-auto flex h-[30px] shrink-0 items-center gap-1.5 rounded-[15px] border border-[var(--border)] bg-[var(--panel)] pr-[9px] pl-2.5 text-[12.5px] font-medium text-[var(--text)]">
          Cuentas
          <ChevronDown size={12} strokeWidth={2.2} aria-hidden="true" />
        </span>
      </div>
      <div className="flex h-[42px] shrink-0 gap-5 border-b border-[var(--border)] px-5">
        {CHAT_TABS.map((label, i) => (
          <span
            key={label}
            className={`-mb-px flex h-[42px] shrink-0 items-center gap-1.5 border-b-2 text-[13.5px] whitespace-nowrap ${
              i === 0 ? "border-[var(--accent)] font-semibold text-[var(--text)]" : "border-transparent font-medium text-[var(--muted)]"
            }`}
          >
            {label}
          </span>
        ))}
      </div>
      <ChatRowsSkeleton bare />
    </div>
  );
}

export default function ChatsSkeleton() {
  return (
    <PageRoot className="flex h-full min-w-0 bg-[var(--bg)] text-[var(--text)]" testId="chats-skeleton">
      <ChatSidebarSkeleton />
      <div className="flex min-w-0 flex-1 flex-col items-center justify-center gap-3 border-b-[6px] border-[var(--accent)] bg-[var(--panel-2)] text-[var(--muted)]">
        <MessageSquare size={64} strokeWidth={1.2} />
        <span className="text-[22px] font-light">dConnector</span>
      </div>
    </PageRoot>
  );
}
