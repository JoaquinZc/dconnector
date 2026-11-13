import { useEffect, useRef, useState, type CSSProperties } from "react";
import { ChevronDown, ChevronRight, EyeOff, Plus } from "lucide-react";
import type { StatusAuthor } from "@/api/types";
import Avatar from "@/components/common/Avatar";
import { StatusListSkeleton } from "@/pages/skeletons/zones";
import { channelName } from "@/lib/channels";
import { cx } from "@/lib/cx";
import { ChannelBadge, ChannelGlyph, channelVar } from "./ChannelGlyph";
import { authorKey, countText, formatAgo, newText, type AccountMeta, type ChannelBlock } from "./statusModel";
import StatusRing from "./StatusRing";

interface Props {
  /** Lista ya filtrada: canal → cuenta → recientes y vistos. */
  sections: ChannelBlock[];
  /** Cuentas con estados (para «Mis estados»). */
  accounts: AccountMeta[];
  /** Canales con cuentas con estados: un chip cada uno. */
  channels: string[];
  channelFilter: string | null;
  onChannelFilter: (channel: string | null) => void;
  hideSeen: boolean;
  onHideSeen: (v: boolean) => void;
  /** Mis estados vigentes en todas las cuentas. */
  mineActive: number;
  /** El panel de la derecha muestra «Mis estados». */
  mineSelected: boolean;
  onMine: () => void;
  canPost: boolean;
  onAdd: () => void;
  selectedKey: string | null;
  onSelect: (a: StatusAuthor) => void;
  /** Canales sin estados en dConnector (Instagram, Messenger) de los que hay cuentas. */
  unsupported: string[];
  /** Los autores aún no llegaron: esqueleto en vez de «Sin estados». */
  loading?: boolean;
}

function AuthorRow({ a, selected, onClick }: { a: StatusAuthor; selected: boolean; onClick: () => void }) {
  const fresh = a.unviewed > 0;
  const ref = useRef<HTMLButtonElement>(null);
  // La fila elegida siempre queda a la vista, también si cambia de sección al verse.
  useEffect(() => {
    if (selected) ref.current?.scrollIntoView?.({ block: "nearest" });
  }, [selected]);
  return (
    <button
      ref={ref}
      type="button"
      onClick={onClick}
      aria-current={selected ? "true" : undefined}
      data-selected={selected ? "1" : "0"}
      className={cx(
        "relative flex h-[62px] w-full items-center gap-3 border-0 px-4 text-left",
        selected ? "bg-[var(--sel)]" : "bg-panel hover:bg-[var(--hover)]",
      )}
    >
      {selected && <span aria-hidden className="absolute bottom-3 left-0 top-3 w-[3px] rounded-r-[3px] bg-accent" />}
      <StatusRing count={a.count} unviewed={a.unviewed} name={a.authorName} src={a.avatarUrl} colorKey={a.authorId} />
      <span className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className={cx("truncate text-[14.5px]", fresh ? "font-semibold" : "font-medium")} title={a.authorName}>
          {a.authorName}
        </span>
        <span className="truncate text-[12.5px] text-muted">
          {countText(a.count)}
          {fresh && (
            <>
              {" · "}
              <em className="font-semibold not-italic text-[var(--accent-text)]">{newText(a.unviewed)}</em>
            </>
          )}
          {" · "}
          {formatAgo(a.lastAt)}
        </span>
      </span>
    </button>
  );
}

function Label({ children }: { children: string }) {
  return <div className="px-4 pb-0.5 pt-1.5 text-[11.5px] font-semibold uppercase tracking-[0.4px] text-muted">{children}</div>;
}

/** Columna izquierda: filtros por canal, «Mis estados» y la lista por canal y cuenta. */
export default function StatusSidebar({
  sections,
  accounts,
  channels,
  channelFilter,
  onChannelFilter,
  hideSeen,
  onHideSeen,
  mineActive,
  mineSelected,
  onMine,
  canPost,
  onAdd,
  selectedKey,
  onSelect,
  unsupported,
  loading = false,
}: Props) {
  const [closed, setClosed] = useState<Record<string, boolean>>({});

  return (
    <aside className="flex h-full w-[360px] shrink-0 flex-col border-r border-line bg-panel" aria-label="Estados">
      <header className="flex h-[68px] shrink-0 items-center justify-between pl-5 pr-4">
        <h1 className="m-0 text-[22px] font-semibold tracking-[-0.2px]">Estados</h1>
        {canPost && (
          <button
            type="button"
            aria-label="Nuevo estado"
            title="Nuevo estado"
            onClick={onAdd}
            className="flex h-[38px] w-[38px] items-center justify-center rounded-xl bg-accent text-[#04201a] hover:brightness-110 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
          >
            <Plus size={20} strokeWidth={1.8} aria-hidden />
          </button>
        )}
      </header>

      <div className="flex shrink-0 flex-wrap items-center gap-x-1 gap-y-1.5 px-4 pb-3" role="group" aria-label="Filtrar por canal">
        <button
          type="button"
          aria-pressed={channelFilter === null}
          onClick={() => onChannelFilter(null)}
          className={cx(
            "inline-flex h-[30px] items-center justify-center rounded-[15px] border-[1.5px] border-transparent px-3 text-[12.5px]",
            channelFilter === null
              ? "bg-[var(--badge-bg)] font-semibold text-[var(--badge-fg)]"
              : "bg-panel2 font-medium text-muted hover:bg-[var(--hover)]",
          )}
        >
          Todos
        </button>
        {channels.map((ch) => {
          const on = channelFilter === ch;
          return (
            <button
              key={ch}
              type="button"
              aria-pressed={on}
              onClick={() => onChannelFilter(on ? null : ch)}
              style={
                {
                  "--c": channelVar(ch),
                  ...(on ? { background: "color-mix(in srgb, var(--c) 18%, var(--panel))", borderColor: "var(--c)" } : null),
                } as CSSProperties
              }
              className={cx(
                "inline-flex h-[30px] items-center justify-center gap-1.5 rounded-[15px] border-[1.5px] border-transparent px-3 text-[12.5px] text-ink",
                on ? "font-semibold" : "bg-panel2 font-medium hover:bg-[var(--hover)]",
              )}
            >
              <span style={{ color: "var(--c)" }} className="inline-flex">
                <ChannelGlyph channel={ch} size={16} />
              </span>
              {channelName(ch)}
            </button>
          );
        })}
        <button
          type="button"
          aria-pressed={hideSeen}
          onClick={() => onHideSeen(!hideSeen)}
          className={cx(
            "ml-auto inline-flex h-[30px] items-center gap-1.5 rounded-[15px] border px-[11px] text-[12.5px] font-medium text-ink hover:bg-[var(--hover)]",
            hideSeen ? "border-accent bg-[var(--sel)]" : "border-line bg-panel",
          )}
        >
          <EyeOff size={16} aria-hidden />
          Ocultar vistos
        </button>
      </div>

      <div className="flex shrink-0 items-center gap-3 border-y border-line bg-panel px-4 py-2.5">
        <button
          type="button"
          onClick={onMine}
          aria-label="Mis estados"
          aria-pressed={mineSelected}
          className="flex min-w-0 flex-1 items-center gap-3 text-left"
        >
          <span className="flex shrink-0" aria-hidden>
            {accounts.slice(0, 4).map((a, i) => (
              <span key={a.id} className={cx("rounded-full border-2 border-panel", i > 0 && "-ml-3")}>
                <Avatar name={a.label} size={30} colorKey={a.id} />
              </span>
            ))}
          </span>
          <span className="min-w-0 flex-1">
            <b className="block text-[15px] font-semibold">Mis estados</b>
            <span className="text-[12.5px] text-muted">
              {accounts.length === 1 ? "1 cuenta" : `${accounts.length} cuentas`} · {mineActive === 1 ? "1 activo" : `${mineActive} activos`}
            </span>
          </span>
        </button>
        {canPost && (
          <button
            type="button"
            aria-label="Publicar mi estado"
            title="Publicar mi estado"
            onClick={onAdd}
            className="flex h-[34px] w-[34px] shrink-0 items-center justify-center rounded-full bg-accent text-[#04201a] hover:brightness-110"
          >
            <Plus size={16} strokeWidth={1.8} aria-hidden />
          </button>
        )}
      </div>

      <div className="relative min-h-0 flex-1 overflow-y-auto" data-testid="status-list">
        {loading && sections.length === 0 && <StatusListSkeleton />}
        {!loading && sections.length === 0 && <p className="px-4 py-8 text-center text-sm text-muted">Sin estados</p>}
        {sections.map((c) => {
          const open = !closed[c.channel];
          const Chev = open ? ChevronDown : ChevronRight;
          return (
            <section key={c.channel} aria-label={channelName(c.channel)}>
              <button
                type="button"
                aria-expanded={open}
                onClick={() => setClosed((p) => ({ ...p, [c.channel]: open }))}
                className="sticky top-0 z-[1] flex h-[42px] w-full items-center gap-2.5 border-b border-line bg-panel px-4 text-left hover:bg-[var(--hover)]"
              >
                <ChannelBadge channel={c.channel} />
                <b className="flex-1 text-sm font-semibold">{channelName(c.channel)}</b>
                {c.newCount > 0 && (
                  <span
                    className="h-5 min-w-5 rounded-[10px] bg-[var(--badge-bg)] px-1.5 text-center text-[11.5px] font-bold leading-5 text-[var(--badge-fg)]"
                    aria-label={`${newText(c.newCount)} en ${channelName(c.channel)}`}
                  >
                    {c.newCount}
                  </span>
                )}
                <Chev size={16} className="text-muted" aria-hidden />
              </button>
              {open &&
                c.accounts.map((b) => (
                  <div key={b.account.id} role="group" aria-label={b.account.label}>
                    <div className="flex items-center gap-[7px] px-4 pb-1 pt-3 text-[12.5px] font-semibold">
                      <i className="h-2 w-2 rounded-full" style={{ background: b.account.color }} aria-hidden />
                      <span className="truncate">{b.account.label}</span>
                    </div>
                    {b.recent.length > 0 && (
                      <>
                        <Label>Recientes</Label>
                        {b.recent.map((a) => (
                          <AuthorRow key={authorKey(a)} a={a} selected={authorKey(a) === selectedKey} onClick={() => onSelect(a)} />
                        ))}
                      </>
                    )}
                    {b.seen.length > 0 && (
                      <>
                        <Label>Vistos</Label>
                        {b.seen.map((a) => (
                          <AuthorRow key={authorKey(a)} a={a} selected={authorKey(a) === selectedKey} onClick={() => onSelect(a)} />
                        ))}
                      </>
                    )}
                  </div>
                ))}
            </section>
          );
        })}
      </div>

      {unsupported.length > 0 && (
        <div
          role="note"
          className="mx-3 mb-3 mt-2 flex shrink-0 items-center gap-2.5 rounded-xl bg-panel2 px-3 py-2.5 text-[12.5px] leading-[1.35] text-muted"
        >
          <span className="flex shrink-0" aria-hidden>
            {unsupported.map((ch, i) => (
              <ChannelBadge key={ch} channel={ch} size={22} className={cx("border-2 border-panel2", i > 0 && "-ml-1.5")} />
            ))}
          </span>
          <span>Instagram y Messenger no permiten ver historias desde dConnector.</span>
        </div>
      )}
    </aside>
  );
}
