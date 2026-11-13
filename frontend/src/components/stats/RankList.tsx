import type { ReactNode } from "react";
import Avatar from "@/components/common/Avatar";
import ChannelIcon from "@/components/common/ChannelIcon";
import { barShare, maxOf } from "./statsShape";
import { NoData, RankBar } from "./StatsParts";

export interface RankItem {
  key: string;
  title: string;
  avatarUrl?: string;
  channel?: string;
  /** Valor con el que se ordena y se dibuja la barra. */
  value: number;
  /** Texto de la derecha. */
  metric: ReactNode;
  /** Clic en la fila (abrir el chat). */
  onOpen?: () => void;
}

/** Lista numerada con barra proporcional al valor más alto. */
export default function RankList({ items, label }: { items: RankItem[]; label: string }) {
  if (items.length === 0) return <NoData className="!py-4" />;
  const max = maxOf(items.map((i) => i.value));
  return (
    <ol className="flex flex-col" aria-label={label}>
      {items.map((it, i) => {
        const inner = (
          <>
            <span className="w-5 shrink-0 text-xs font-semibold tabular-nums text-muted">{i + 1}</span>
            <span className="relative shrink-0">
              <Avatar src={it.avatarUrl} name={it.title || "?"} colorKey={it.key} size={32} />
              {it.channel && (
                <span className="absolute -bottom-0.5 -right-0.5">
                  <ChannelIcon channel={it.channel} size={13} ring />
                </span>
              )}
            </span>
            <span className="min-w-0 flex-1">
              <span className="block truncate text-sm" title={it.title}>
                {it.title || "Sin nombre"}
              </span>
              <RankBar share={barShare(it.value, max)} />
            </span>
            <span className="shrink-0 text-right text-xs font-semibold tabular-nums">{it.metric}</span>
          </>
        );
        return (
          <li key={it.key} className="border-b border-line last:border-b-0">
            {it.onOpen ? (
              <button
                type="button"
                onClick={it.onOpen}
                title="Abrir el chat"
                className="flex w-full items-center gap-3 rounded-lg px-1 py-2 text-left hover:bg-[var(--hover)]"
              >
                {inner}
              </button>
            ) : (
              <div className="flex items-center gap-3 px-1 py-2">{inner}</div>
            )}
          </li>
        );
      })}
    </ol>
  );
}
