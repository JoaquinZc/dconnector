import { MessagesSquare } from "lucide-react";
import {
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Legend,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { api } from "@/api";
import type { Range } from "@/api/types";
import EmptyState from "@/components/common/EmptyState";
import LoadError from "@/components/common/LoadError";
import { ConversationsStatsSkeleton } from "@/pages/skeletons/zones";
import { useUI } from "@/store/ui";
import RankList, { type RankItem } from "./RankList";
import {
  AXIS_TICK,
  CHART_TOOLTIP,
  Card,
  Heatmap,
  Kpi,
  MixBar,
  NoData,
  RECEIVED_COLOR,
  SENT_COLOR,
  n,
} from "./StatsParts";
import {
  formatBucket,
  formatFraction,
  formatSeconds,
  readConversationOverview,
  type ChatRankView,
  type ConversationView,
} from "./statsShape";
import { useStatsLoad } from "./useStatsLoad";

interface Props {
  range: Range;
  /** Vacío = todas las cuentas. */
  accountIds: string[];
}

const openChat = (key: string) => () => useUI.getState().openChat(key);

function rankItems(rows: ChatRankView[], value: (r: ChatRankView) => number, metric: (r: ChatRankView) => string): RankItem[] {
  return rows.map((r) => ({
    key: r.chatKey,
    title: r.title,
    avatarUrl: r.avatarUrl,
    channel: r.channel,
    value: value(r),
    metric: metric(r),
    onOpen: r.chatKey ? openChat(r.chatKey) : undefined,
  }));
}

/** Pestaña «Conversaciones»: KPIs con comparación, mensajes por día, mapa de calor, tiempos, quién inicia y rankings. */
export default function ConversationsTab({ range, accountIds }: Props) {
  const { data, loading, failed, retry } = useStatsLoad<ConversationView>(
    () => api.stats.Conversations(accountIds.length ? accountIds : null, range),
    readConversationOverview,
    [range.days, range.from, range.to, accountIds.join(",")],
  );

  if (!data) {
    if (failed) return <LoadError what="las estadísticas de conversaciones" onRetry={retry} className="flex-1" />;
    return <ConversationsStatsSkeleton />;
  }
  if (!data.hasData) return <EmptyState icon={MessagesSquare} title="Sin mensajes en este periodo" className="flex-1" />;

  const c = data.current;
  const ch = data.changes;
  const r = c.response;
  const starts = [
    { name: "Tú", value: c.startedByMe, color: SENT_COLOR },
    { name: "Ellos", value: c.startedByThem, color: RECEIVED_COLOR },
  ];
  const times = [
    { name: "Tú", sec: r.mineMedianSec },
    { name: "Ellos", sec: r.theirsMedianSec },
  ];
  const hasTimes = times.some((t) => t.sec !== null);

  return (
    <div className={`flex flex-col gap-4 ${loading ? "opacity-70" : ""}`} data-testid="conversations">
      <div className="grid grid-cols-2 gap-3 xl:grid-cols-4">
        <Kpi label="Enviados" value={n(c.sent)} change={ch.sent} />
        <Kpi label="Recibidos" value={n(c.received)} change={ch.received} />
        <Kpi label="Chats con actividad" value={n(c.chats)} change={ch.chats} hint="Chats individuales con algún mensaje" />
        <Kpi
          label="Tu tiempo de respuesta"
          value={formatSeconds(r.mineMedianSec)}
          change={ch.mineMedianSec}
          lowerIsBetter
          hint="Mediana, contando las 24 h"
        />
        <Kpi label="Respondes en menos de 1 h" value={formatFraction(r.mineUnderHour)} change={ch.mineUnderHour} hint={`${n(r.mineCount)} respuestas tuyas`} />
        <Kpi
          label="Te responden en"
          value={formatSeconds(r.theirsMedianSec)}
          change={ch.theirsMedianSec}
          lowerIsBetter
          hint="Mediana de lo que tardan en responderte"
        />
        <Kpi label="Conversaciones que inicias" value={n(c.startedByMe)} change={ch.startedByMe} hint="Primer mensaje tras 8 h o más sin hablar" />
        <Kpi label="Conversaciones que inician" value={n(c.startedByThem)} change={ch.startedByThem} hint="Te escriben primero tras 8 h o más sin hablar" />
      </div>

      <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
        <Card title="Mensajes por día">
          {data.perDay.length === 0 ? (
            <NoData />
          ) : (
            <div className="h-56 w-full" data-testid="per-day">
              <ResponsiveContainer width="100%" height="100%">
                <BarChart data={data.perDay} margin={{ top: 8, right: 8, left: -16, bottom: 0 }}>
                  <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="var(--border)" />
                  <XAxis dataKey="day" tickFormatter={formatBucket} tick={AXIS_TICK} tickLine={false} axisLine={false} minTickGap={12} />
                  <YAxis allowDecimals={false} tick={AXIS_TICK} tickLine={false} axisLine={false} />
                  <Tooltip
                    {...CHART_TOOLTIP}
                    labelFormatter={(d) => formatBucket(String(d))}
                    formatter={(v, name) => [String(v), name === "sent" ? "Enviados" : "Recibidos"]}
                  />
                  <Legend formatter={(v) => (v === "sent" ? "Enviados" : "Recibidos")} />
                  <Bar dataKey="received" stackId="a" fill={RECEIVED_COLOR} />
                  <Bar dataKey="sent" stackId="a" fill={SENT_COLOR} radius={[4, 4, 0, 0]} />
                </BarChart>
              </ResponsiveContainer>
            </div>
          )}
          {data.groupSent + data.groupReceived > 0 && (
            <p className="mt-1 text-xs text-muted">
              Incluye grupos: {n(data.groupSent)} enviados · {n(data.groupReceived)} recibidos
            </p>
          )}
        </Card>

        <Card title="Horas de más actividad">
          <Heatmap grid={data.heatmap} />
          <h3 className="label !mb-2 mt-4">Qué se envía</h3>
          <MixBar mix={c.mix} />
        </Card>
      </div>

      <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
        <Card title="Tiempos de respuesta">
          {!hasTimes ? (
            <NoData />
          ) : (
            <div className="h-44 w-full" data-testid="response-times">
              <ResponsiveContainer width="100%" height="100%">
                <BarChart data={times.map((t) => ({ name: t.name, sec: t.sec ?? 0 }))} layout="vertical" margin={{ top: 4, right: 16, left: 8, bottom: 0 }}>
                  <CartesianGrid strokeDasharray="3 3" horizontal={false} stroke="var(--border)" />
                  <XAxis type="number" tickFormatter={(s) => formatSeconds(Number(s))} tick={AXIS_TICK} tickLine={false} axisLine={false} />
                  <YAxis type="category" dataKey="name" tick={AXIS_TICK} tickLine={false} axisLine={false} width={48} />
                  <Tooltip {...CHART_TOOLTIP} formatter={(v) => [formatSeconds(Number(v)), "Mediana"]} />
                  <Bar dataKey="sec" radius={[0, 4, 4, 0]}>
                    <Cell fill={SENT_COLOR} />
                    <Cell fill={RECEIVED_COLOR} />
                  </Bar>
                </BarChart>
              </ResponsiveContainer>
            </div>
          )}
        </Card>

        <Card title="Quién inicia">
          {c.startedByMe + c.startedByThem === 0 ? (
            <NoData />
          ) : (
            <div className="h-44 w-full" data-testid="who-starts">
              <ResponsiveContainer width="100%" height="100%">
                <PieChart>
                  <Pie data={starts} dataKey="value" nameKey="name" innerRadius="55%" outerRadius="85%" paddingAngle={2} stroke="none">
                    {starts.map((s) => (
                      <Cell key={s.name} fill={s.color} />
                    ))}
                  </Pie>
                  <Tooltip {...CHART_TOOLTIP} />
                  <Legend />
                </PieChart>
              </ResponsiveContainer>
            </div>
          )}
        </Card>
      </div>

      <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
        <Card title="Chats más activos">
          <RankList
            label="Chats más activos"
            items={rankItems(data.topChats, (x) => x.total, (x) => `${n(x.total)} (${n(x.sent)}↑ ${n(x.received)}↓)`)}
          />
        </Card>
        <Card title="Chats más frecuentes">
          <RankList
            label="Chats más frecuentes"
            items={rankItems(data.frequent, (x) => x.activeDays, (x) => `${n(x.activeDays)} ${x.activeDays === 1 ? "día" : "días"}`)}
          />
        </Card>
      </div>
    </div>
  );
}
