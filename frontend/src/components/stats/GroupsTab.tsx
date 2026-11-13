import { UsersRound } from "lucide-react";
import { api } from "@/api";
import type { Range } from "@/api/types";
import EmptyState from "@/components/common/EmptyState";
import LoadError from "@/components/common/LoadError";
import { GroupsStatsSkeleton } from "@/pages/skeletons/zones";
import { formatListTime } from "@/lib/format";
import { useUI } from "@/store/ui";
import RankList, { type RankItem } from "./RankList";
import { Card, Kpi, n } from "./StatsParts";
import { readGroupsOverview, type GroupsView } from "./statsShape";
import { useStatsLoad } from "./useStatsLoad";

interface Props {
  range: Range;
  /** Vacío = todas las cuentas. */
  accountIds: string[];
}

/** Items de «Grupos más activos» (clic = abrir el grupo). */
export function groupItems(groups: GroupsView["groups"]): RankItem[] {
  return groups.map((g) => ({
    key: g.chatKey || `${g.accountId}|${g.nativeId}`,
    title: g.title,
    avatarUrl: g.avatarUrl,
    channel: g.channel,
    value: g.total,
    metric: `${n(g.total)} · ${n(g.authors)} ${g.authors === 1 ? "persona" : "personas"}`,
    onOpen: g.chatKey ? () => useUI.getState().openChat(g.chatKey) : undefined,
  }));
}

/** Items de «Personas más activas» (sin clic: no hay chat directo seguro). */
export function peopleItems(people: GroupsView["people"]): RankItem[] {
  return people.map((p) => ({
    key: `${p.accountId}|${p.senderId}`,
    title: p.name || p.senderId,
    avatarUrl: p.avatarUrl,
    channel: p.channel,
    value: p.total,
    metric: `${n(p.total)} · ${n(p.groups)} ${p.groups === 1 ? "grupo" : "grupos"}${p.lastAt ? ` · ${formatListTime(p.lastAt)}` : ""}`,
  }));
}

/** Pestaña «Grupos»: totales, grupos más activos y personas más activas. */
export default function GroupsTab({ range, accountIds }: Props) {
  const { data, loading, failed, retry } = useStatsLoad<GroupsView>(
    () => api.stats.Groups(accountIds.length ? accountIds : null, range),
    readGroupsOverview,
    [range.days, range.from, range.to, accountIds.join(",")],
  );

  if (!data) {
    if (failed) return <LoadError what="las estadísticas de grupos" onRetry={retry} className="flex-1" />;
    return <GroupsStatsSkeleton />;
  }
  if (!data.hasData) return <EmptyState icon={UsersRound} title="Sin actividad en grupos" className="flex-1" />;

  return (
    <div className={`flex flex-col gap-4 ${loading ? "opacity-70" : ""}`} data-testid="groups">
      <div className="grid grid-cols-2 gap-3 xl:grid-cols-4">
        <Kpi label="Mensajes en grupos" value={n(data.groupSent + data.groupReceived)} />
        <Kpi label="Enviados" value={n(data.groupSent)} />
        <Kpi label="Recibidos" value={n(data.groupReceived)} />
        <Kpi label="Grupos activos" value={n(data.groups.length)} />
      </div>
      <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
        <Card title="Grupos más activos">
          <RankList label="Grupos más activos" items={groupItems(data.groups)} />
        </Card>
        <Card title="Personas más activas">
          <RankList label="Personas más activas" items={peopleItems(data.people)} />
        </Card>
      </div>
    </div>
  );
}
