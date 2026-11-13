// Filtro de canal y cuentas en una sola fila, sin desplazamiento: «Todas», un chip cuadrado por canal
// presente y, a la derecha, «Cuentas ▾» con un menú para marcar cuentas sueltas. Selección múltiple.
import { useEffect, useId, useRef, useState } from "react";
import { Check, ChevronDown } from "lucide-react";
import {
  channelsPresent,
  isEmptySelection,
  toggleAccountChecked,
  toggleSelection,
  visibleAccountIds,
  type AccountLite,
  type AccountSelection,
} from "./accountSelection";
import { channelColorStyle, ChannelGlyph } from "./channelUi";
import { channelInfo } from "./parts";

const FOCUS = "focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-[var(--accent)]";

export default function AccountFilter({
  accounts,
  selection,
  onChange,
}: {
  accounts: AccountLite[];
  selection: AccountSelection;
  onChange: (s: AccountSelection) => void;
}) {
  const [open, setOpen] = useState(false);
  const wrapRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const menuId = useId();

  // el menú se cierra con Escape y con un clic fuera de él (o de su botón)
  useEffect(() => {
    if (!open) return;
    const down = (e: MouseEvent) => {
      if (wrapRef.current && !wrapRef.current.contains(e.target as Node)) setOpen(false);
    };
    const key = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      setOpen(false);
      buttonRef.current?.focus();
    };
    document.addEventListener("mousedown", down);
    document.addEventListener("keydown", key);
    return () => {
      document.removeEventListener("mousedown", down);
      document.removeEventListener("keydown", key);
    };
  }, [open]);

  if (accounts.length === 0) return null;

  const channels = channelsPresent(accounts);
  const all = isEmptySelection(selection);
  const visible = new Set(visibleAccountIds(selection, accounts));
  const dots = accounts.filter((a) => visible.has(a.id)).slice(0, 3);

  return (
    <div ref={wrapRef} className="relative flex shrink-0 items-center gap-1 px-4 pb-3" role="group" aria-label="Filtrar por canal">
      <button
        type="button"
        aria-pressed={all}
        onClick={() => onChange(toggleSelection(selection, { kind: "all" }))}
        className={`h-[30px] shrink-0 rounded-[15px] border-[1.5px] border-transparent px-3.5 text-[12.5px] whitespace-nowrap transition-colors ${FOCUS} ${
          all
            ? "bg-[var(--badge-bg)] font-semibold text-[var(--badge-fg)]"
            : "bg-[var(--panel-2)] font-medium text-[var(--muted)] hover:bg-[var(--hover)]"
        }`}
      >
        Todas
      </button>
      {channels.map((ch) => {
        const name = channelInfo(ch).name;
        const active = selection.channels.includes(ch);
        return (
          <button
            key={ch}
            type="button"
            title={name}
            aria-label={name}
            aria-pressed={active}
            onClick={() => onChange(toggleSelection(selection, { kind: "channel", id: ch }))}
            style={channelColorStyle(ch)}
            className={`flex h-[30px] w-9 shrink-0 items-center justify-center rounded-[15px] border-[1.5px] text-[var(--c)] transition-colors ${FOCUS} ${
              active
                ? "border-[var(--c)] bg-[color-mix(in_srgb,var(--c)_18%,var(--panel))]"
                : "border-transparent bg-[var(--panel-2)] hover:bg-[var(--hover)]"
            }`}
          >
            <ChannelGlyph channel={ch} size={16} />
          </button>
        );
      })}
      <button
        ref={buttonRef}
        type="button"
        title="Elegir cuentas"
        aria-haspopup="true"
        aria-expanded={open}
        aria-controls={open ? menuId : undefined}
        onClick={() => setOpen((v) => !v)}
        className={`ml-auto flex h-[30px] shrink-0 items-center gap-1.5 rounded-[15px] border border-[var(--border)] bg-[var(--panel)] pr-[9px] pl-2.5 text-[12.5px] font-medium text-[var(--text)] hover:bg-[var(--hover)] aria-expanded:border-[var(--accent)] aria-expanded:bg-[var(--sel)] ${FOCUS}`}
      >
        <span className="flex" aria-hidden="true">
          {dots.map((a, i) => (
            <i
              key={a.id}
              className={`h-[9px] w-[9px] rounded-full border-[1.5px] border-[var(--panel)] ${i > 0 ? "-ml-[3px]" : ""}`}
              style={{ background: a.color }}
            />
          ))}
        </span>
        Cuentas
        <ChevronDown size={12} strokeWidth={2.2} aria-hidden="true" />
      </button>

      {open && (
        <div
          id={menuId}
          role="group"
          aria-label="Elegir cuentas"
          className="absolute top-[calc(100%-6px)] right-3 z-30 w-[300px] rounded-[14px] border border-[var(--border)] bg-[var(--panel)] p-2 shadow-[var(--shadow)]"
        >
          <div className="px-2 pt-1.5 pb-2 text-[12px] font-semibold tracking-[0.3px] text-[var(--muted)] uppercase">Mostrar chats de</div>
          <div className="max-h-[min(50vh,360px)] overflow-y-auto">
            {channels.map((ch) => {
              const name = channelInfo(ch).name;
              return (
                <div key={ch} role="group" aria-label={name}>
                  <div className="px-2 pt-1.5 pb-0.5 text-[12px] text-[var(--muted)]">{name}</div>
                  {accounts
                    .filter((a) => a.channel === ch)
                    .map((a) => {
                      const checked = visible.has(a.id);
                      return (
                        <button
                          key={a.id}
                          type="button"
                          role="checkbox"
                          aria-checked={checked}
                          aria-label={`${a.label} (${name})`}
                          onClick={() => {
                            const next = toggleAccountChecked(selection, accounts, a.id);
                            if (next !== selection) onChange(next);
                          }}
                          className={`flex h-[38px] w-full items-center gap-2.5 rounded-[9px] px-2 text-left text-[14px] hover:bg-[var(--hover)] ${FOCUS}`}
                        >
                          <span
                            className={`flex h-[18px] w-[18px] shrink-0 items-center justify-center rounded-[5px] border-[1.5px] text-[var(--badge-fg)] ${
                              checked ? "border-[var(--badge-bg)] bg-[var(--badge-bg)]" : "border-[var(--muted)]"
                            }`}
                            aria-hidden="true"
                          >
                            {checked && <Check size={12} strokeWidth={2.2} />}
                          </span>
                          <i className="h-2 w-2 shrink-0 rounded-full" style={{ background: a.color }} aria-hidden="true" />
                          <span className="min-w-0 flex-1 truncate">{a.label}</span>
                        </button>
                      );
                    })}
                </div>
              );
            })}
          </div>
          <div className="flex justify-end px-1 pt-2 pb-0.5">
            <button
              type="button"
              onClick={() => {
                setOpen(false);
                buttonRef.current?.focus();
              }}
              className={`h-[34px] rounded-[10px] bg-[var(--badge-bg)] px-4 text-[13px] font-semibold text-[var(--badge-fg)] hover:opacity-90 ${FOCUS}`}
            >
              Listo
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
