import { useEffect, useMemo, useState } from "react";
import ConversationsTab from "@/components/stats/ConversationsTab";
import GroupsTab from "@/components/stats/GroupsTab";
import PeriodSelector from "@/components/stats/PeriodSelector";
import StatsAccountPicker from "@/components/stats/StatsAccountPicker";
import { defaultPeriod, isValidPeriod, toRange, type PeriodState } from "@/components/stats/periodModel";
import { cx } from "@/lib/cx";
import { useAccounts } from "@/store/accounts";

const ACCOUNTS_KEY = "dc:stats:accounts";

type Tab = "conversations" | "groups";

function readStoredAccounts(): string[] {
  try {
    const v = JSON.parse(localStorage.getItem(ACCOUNTS_KEY) ?? "[]");
    return Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [];
  } catch {
    return [];
  }
}

export default function StatsPage() {
  const views = useAccounts((s) => s.views);
  const order = useAccounts((s) => s.order);
  const accounts = useMemo(() => order.map((id) => views[id]).filter((v) => v && v.account.enabled), [order, views]);

  const [tab, setTab] = useState<Tab>("conversations");
  const [period, setPeriod] = useState<PeriodState>(() => defaultPeriod());
  const [stored, setStored] = useState<string[]>(readStoredAccounts);

  // Solo cuentas que existen (una cuenta borrada no deja el filtro vacío para siempre).
  const selected = useMemo(() => stored.filter((id) => accounts.some((v) => v.account.id === id)), [stored, accounts]);

  const chooseAccounts = (ids: string[]) => {
    setStored(ids);
    try {
      localStorage.setItem(ACCOUNTS_KEY, JSON.stringify(ids));
    } catch {
      /* sin almacenamiento */
    }
  };

  // El rango solo cambia cuando el periodo es válido (un rango a medio escribir no pide nada).
  const [range, setRange] = useState(() => toRange(period));
  useEffect(() => {
    if (!isValidPeriod(period)) return;
    const next = toRange(period);
    setRange((prev) => (prev.days === next.days && prev.from === next.from && prev.to === next.to ? prev : next));
  }, [period]);

  return (
    <div className="flex h-full flex-col">
      <header className="flex flex-wrap items-center gap-x-6 gap-y-2 border-b border-line bg-panel px-6 py-3">
        <h1 className="text-lg font-semibold">Estadísticas</h1>
        <div role="tablist" className="inline-flex rounded-lg bg-panel2 p-0.5">
          {(
            [
              ["conversations", "Conversaciones"],
              ["groups", "Grupos"],
            ] as const
          ).map(([id, label]) => (
            <button
              key={id}
              role="tab"
              aria-selected={tab === id}
              onClick={() => setTab(id)}
              className={cx(
                "rounded-md px-4 py-1.5 text-sm font-medium",
                tab === id ? "bg-panel shadow-sm" : "text-muted hover:text-ink",
              )}
            >
              {label}
            </button>
          ))}
        </div>
        <span className="flex-1" />
        <PeriodSelector value={period} onChange={setPeriod} />
      </header>
      {accounts.length > 1 && (
        <div className="border-b border-line bg-panel px-6 py-2">
          <StatsAccountPicker accounts={accounts} selected={selected} onChange={chooseAccounts} />
        </div>
      )}

      <div className="min-h-0 flex-1 overflow-y-auto bg-bg p-6">
        {tab === "conversations" ? (
          <ConversationsTab range={range} accountIds={selected} />
        ) : (
          <GroupsTab range={range} accountIds={selected} />
        )}
      </div>
    </div>
  );
}
