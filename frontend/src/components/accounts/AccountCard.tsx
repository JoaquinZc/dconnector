import { useEffect, useRef, useState, type DragEvent } from "react";
import { GripVertical, Link2, LogOut, MoreHorizontal, Pencil, Plug, PlugZap, Trash2 } from "lucide-react";
import type { AccountView } from "@/api/types";
import ChannelIcon from "@/components/common/ChannelIcon";
import { TONE_COLOR, accountStateText } from "@/lib/accountStatus";
import { channelName } from "@/lib/channels";
import { cx } from "@/lib/cx";
import HistorySection from "./HistorySection";

/** Aviso fijo de Messenger cuando los chats cifrados no se pueden mostrar (docs/09 §1.3). */
const E2EE_NOTICE = "Los chats cifrados de extremo a extremo aún no se muestran";

export interface AccountCardActions {
  onConnect: (v: AccountView) => Promise<void> | void;
  onDisconnect: (v: AccountView) => Promise<void> | void;
  onEdit: (v: AccountView) => void;
  onRelink: (v: AccountView) => void;
  onLogout: (v: AccountView) => Promise<void> | void;
  onDelete: (v: AccountView) => Promise<void> | void;
}

interface Props extends AccountCardActions {
  view: AccountView;
  dragging?: boolean;
  onDragStart: (id: string) => void;
  onDragEnter: (id: string) => void;
  onDragEnd: () => void;
  /** Reordenar con el teclado desde el asa (↑/↓). */
  onMove: (id: string, delta: -1 | 1) => void;
}

export default function AccountCard({
  view,
  dragging,
  onDragStart,
  onDragEnter,
  onDragEnd,
  onMove,
  onConnect,
  onDisconnect,
  onEdit,
  onRelink,
  onLogout,
  onDelete,
}: Props) {
  const { account, status } = view;
  const state = String(status?.state);
  const st = accountStateText(view);
  const [busy, setBusy] = useState(false);
  const [menu, setMenu] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!menu) return;
    const close = (e: MouseEvent) => {
      if (!menuRef.current?.contains(e.target as Node)) setMenu(false);
    };
    window.addEventListener("mousedown", close);
    return () => window.removeEventListener("mousedown", close);
  }, [menu]);

  const run = async (fn: () => Promise<void> | void) => {
    setBusy(true);
    setMenu(false);
    try {
      await fn();
    } finally {
      setBusy(false);
    }
  };

  const online = state === "connected" || state === "connecting";
  const needsRelink = state === "logged_out" || state === "pairing";
  const who = [account.selfName, account.selfHandle].filter(Boolean).join(" · ");

  const handleDragStart = (e: DragEvent<HTMLElement>) => {
    e.dataTransfer.effectAllowed = "move";
    e.dataTransfer.setData("text/plain", account.id);
    if (rootRef.current) e.dataTransfer.setDragImage(rootRef.current, 24, 24);
    onDragStart(account.id);
  };

  return (
    <div
      ref={rootRef}
      data-testid="account-card"
      data-account-id={account.id}
      onDragEnter={() => onDragEnter(account.id)}
      onDragOver={(e) => e.preventDefault()}
      onDrop={(e) => e.preventDefault()}
      className={cx(
        "card relative flex flex-col gap-3 overflow-hidden p-4 pl-5 transition-opacity",
        dragging && "opacity-40",
        !account.enabled && "opacity-70",
      )}
    >
      <span className="absolute inset-y-0 left-0 w-1.5" style={{ background: account.color }} />

      <div className="flex items-start gap-3">
        <button
          type="button"
          draggable
          onDragStart={handleDragStart}
          onDragEnd={onDragEnd}
          onKeyDown={(e) => {
            if (e.key === "ArrowUp") {
              e.preventDefault();
              onMove(account.id, -1);
            } else if (e.key === "ArrowDown") {
              e.preventDefault();
              onMove(account.id, 1);
            }
          }}
          className="icon-btn !h-8 !w-6 shrink-0 cursor-grab active:cursor-grabbing"
          title="Arrastrar para ordenar"
          aria-label="Arrastrar para ordenar"
        >
          <GripVertical size={16} />
        </button>
        <ChannelIcon channel={String(account.channel)} size={36} className="shrink-0" />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className="h-2.5 w-2.5 shrink-0 rounded-full" style={{ background: account.color }} />
            <h3 className="min-w-0 truncate text-base font-semibold" title={account.label}>
              {account.label}
            </h3>
          </div>
          <p className="truncate text-muted" title={`${channelName(account.channel)}${who ? ` · ${who}` : ""}`}>
            {channelName(account.channel)}
            {who ? ` · ${who}` : ""}
          </p>
        </div>
      </div>

      <div className="min-h-[38px]">
        <p className="flex items-center gap-2 font-medium" data-testid="account-state">
          <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: TONE_COLOR[st.tone] }} />
          {st.label}
          {view.unread > 0 && <span className="ml-auto rounded-full bg-panel2 px-2 text-xs text-muted">{view.unread} sin leer</span>}
        </p>
        {st.detail && (
          <p className="mt-0.5 truncate text-sm text-muted" title={st.detail}>
            {st.detail}
          </p>
        )}
        {String(account.channel) === "messenger" &&
          (view.capabilities as { e2eeChats?: boolean } | undefined)?.e2eeChats === false && (
            <p className="mt-0.5 truncate text-sm text-muted" title={E2EE_NOTICE} data-testid="e2ee-notice">
              {E2EE_NOTICE}
            </p>
          )}
      </div>

      <HistorySection view={view} />

      <div className="flex items-center gap-2">
        {needsRelink ? (
          <button className="btn btn-primary btn-sm" disabled={busy} onClick={() => onRelink(view)}>
            <Link2 size={15} />
            Volver a vincular
          </button>
        ) : online ? (
          <button className="btn btn-sm" disabled={busy} onClick={() => void run(() => onDisconnect(view))}>
            <PlugZap size={15} />
            Desconectar
          </button>
        ) : (
          <button className="btn btn-primary btn-sm" disabled={busy || !account.enabled} onClick={() => void run(() => onConnect(view))}>
            <Plug size={15} />
            Conectar
          </button>
        )}
        <button className="btn btn-sm" disabled={busy} onClick={() => onEdit(view)}>
          <Pencil size={14} />
          Editar
        </button>

        <div className="relative ml-auto" ref={menuRef}>
          <button
            className="icon-btn"
            aria-label="Más acciones"
            title="Más acciones"
            aria-haspopup="menu"
            aria-expanded={menu}
            onClick={() => setMenu((m) => !m)}
          >
            <MoreHorizontal size={18} />
          </button>
          {menu && (
            <div
              role="menu"
              className="anim-pop absolute bottom-full right-0 z-20 mb-1 w-52 overflow-hidden rounded-lg border border-line bg-panel py-1"
              style={{ boxShadow: "var(--shadow)" }}
            >
              {!needsRelink && (
                <MenuItem
                  icon={<Link2 size={15} />}
                  label="Volver a vincular"
                  onClick={() => {
                    setMenu(false);
                    onRelink(view);
                  }}
                />
              )}
              <MenuItem
                icon={<LogOut size={15} />}
                label="Cerrar sesión"
                disabled={state === "logged_out"}
                onClick={() => void run(() => onLogout(view))}
              />
              <MenuItem icon={<Trash2 size={15} />} label="Eliminar" danger onClick={() => void run(() => onDelete(view))} />
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function MenuItem({
  icon,
  label,
  onClick,
  danger,
  disabled,
}: {
  icon: React.ReactNode;
  label: string;
  onClick: () => void;
  danger?: boolean;
  disabled?: boolean;
}) {
  return (
    <button
      role="menuitem"
      disabled={disabled}
      onClick={onClick}
      className={cx(
        "flex w-full items-center gap-2.5 px-3 py-2 text-left hover:bg-[var(--hover)] disabled:cursor-not-allowed disabled:opacity-40",
        danger && "text-danger",
      )}
    >
      {icon}
      {label}
    </button>
  );
}
