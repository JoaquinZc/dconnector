import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { CircleDot, Plus } from "lucide-react";
import { api, onEvent } from "@/api";
import type { CapSet, Status, StatusAuthor } from "@/api/types";
import EmptyState from "@/components/common/EmptyState";
import MyStatusDetail from "@/components/status/MyStatusDetail";
import MyStatusesGrid from "@/components/status/MyStatusesGrid";
import PostStatusModal from "@/components/status/PostStatusModal";
import StatusSidebar from "@/components/status/StatusSidebar";
import StatusStatsPanel from "@/components/status/StatusStatsPanel";
import StoryViewer, { type ViewerAccount } from "@/components/status/StoryViewer";
import {
  authorKey,
  buildSections,
  flattenSections,
  liveSorted,
  stableOrder,
  type AccountMeta,
} from "@/components/status/statusModel";
import { CHANNEL_ORDER } from "@/lib/channels";
import { cx } from "@/lib/cx";
import { useAccounts } from "@/store/accounts";
import { useStatuses } from "@/store/statuses";

const ACCOUNT_KEY = "dc:statuses:account";
const HIDE_SEEN_KEY = "dc:statuses:hideSeen";
const DAY = 24 * 60 * 60 * 1000;
const NO_STATUS_CHANNELS = ["instagram", "messenger"];

function readStored(key: string): string {
  try {
    return localStorage.getItem(key) ?? "";
  } catch {
    return "";
  }
}

function writeStored(key: string, value: string) {
  try {
    localStorage.setItem(key, value);
  } catch {
    /* sin almacenamiento */
  }
}

type Tab = "view" | "mine" | "stats";

const TABS: ReadonlyArray<readonly [Tab, string]> = [
  ["view", "Ver estados"],
  ["mine", "Mis estados"],
  ["stats", "Estadísticas"],
];

export default function StatusesPage() {
  const views = useAccounts((s) => s.views);
  const order = useAccounts((s) => s.order);
  const authors = useStatuses((s) => s.authors);
  const loadAuthors = useStatuses((s) => s.loadAuthors);

  // Todas las cuentas habilitadas con estados (WhatsApp, Telegram…): se ven juntas.
  const accounts = useMemo(
    () => order.map((id) => views[id]).filter((v) => v && v.account.enabled && (v.capabilities as CapSet | undefined)?.statuses),
    [order, views],
  );
  const ids = useMemo(() => accounts.map((v) => v.account.id), [accounts]);
  const idsKey = ids.join("|");
  const idsRef = useRef<string[]>(ids);
  idsRef.current = ids;

  const metas = useMemo<AccountMeta[]>(
    () =>
      accounts.map((v) => ({
        id: v.account.id,
        channel: String(v.account.channel),
        label: v.account.label,
        color: v.account.color,
        selfId: v.account.selfId,
      })),
    [accounts],
  );
  const viewerAccounts = useMemo(() => {
    const m: Record<string, ViewerAccount> = {};
    for (const v of accounts) {
      m[v.account.id] = { label: v.account.label, channel: String(v.account.channel), caps: v.capabilities as CapSet | undefined };
    }
    return m;
  }, [accounts]);
  const channels = useMemo(() => CHANNEL_ORDER.filter((c) => metas.some((m) => m.channel === c)), [metas]);
  // Instagram y Messenger no tienen estados: solo se avisa si hay cuentas de esos canales.
  const unsupported = useMemo(
    () =>
      NO_STATUS_CHANNELS.filter((ch) =>
        order.some((id) => {
          const v = views[id];
          return !!v && v.account.enabled && String(v.account.channel) === ch && !(v.capabilities as CapSet | undefined)?.statuses;
        }),
      ),
    [order, views],
  );

  const [tab, setTab] = useState<Tab>("view");
  const [channelFilter, setChannelFilter] = useState<string | null>(null);
  const [hideSeen, setHideSeen] = useState(() => readStored(HIDE_SEEN_KEY) === "1");
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const [posting, setPosting] = useState(false);
  const [detail, setDetail] = useState<Status | null>(null);

  // Cuenta de «Mis estados» y «Estadísticas».
  const [pref, setPref] = useState(() => readStored(ACCOUNT_KEY));
  const mineView = accounts.find((v) => v.account.id === pref) ?? accounts[0];
  const mineId = mineView?.account.id ?? "";
  const mineCaps = mineView?.capabilities as CapSet | undefined;
  const chooseAccount = (id: string) => {
    setPref(id);
    writeStored(ACCOUNT_KEY, id);
  };

  // Cuentas donde sí se puede publicar.
  const postable = useMemo(() => accounts.filter((v) => (v.capabilities as CapSet | undefined)?.postStatus), [accounts]);
  const [target, setTarget] = useState("");
  const targetView = postable.find((v) => v.account.id === target) ?? postable[0];

  // ---- lista

  const activeFilter = channelFilter && (channels as string[]).includes(channelFilter) ? channelFilter : null;
  const byKey = useMemo(() => new Map(authors.map((a) => [authorKey(a), a] as const)), [authors]);
  const activeKey = selectedKey && byKey.has(selectedKey) ? selectedKey : null;
  const sections = useMemo(
    () => buildSections(authors, metas, { channel: activeFilter, hideSeen, keepKey: activeKey }),
    [authors, metas, activeFilter, hideSeen, activeKey],
  );
  const displayKeys = useMemo(() => flattenSections(sections).map(authorKey), [sections]);

  // Orden del visor: el de la lista al elegir a alguien, y estable mientras la lista se recarga.
  const [navKeys, setNavKeys] = useState<string[]>([]);
  const resetNav = useRef(true);
  const displayStr = displayKeys.join("\n");
  useEffect(() => {
    const reset = resetNav.current;
    resetNav.current = false;
    setNavKeys((prev) => stableOrder(reset ? [] : prev, displayStr ? displayStr.split("\n") : [], activeKey));
  }, [displayStr, activeKey]);
  const navAuthors = useMemo(
    () => navKeys.map((k) => byKey.get(k)).filter((a): a is StatusAuthor => !!a),
    [navKeys, byKey],
  );

  const pick = (a: StatusAuthor) => {
    resetNav.current = true;
    setSelectedKey(authorKey(a));
    setTab("view");
  };
  const pickChannel = (ch: string | null) => {
    resetNav.current = true;
    setChannelFilter(ch);
  };
  const toggleHide = (v: boolean) => {
    resetNav.current = true;
    setHideSeen(v);
    writeStored(HIDE_SEEN_KEY, v ? "1" : "0");
  };

  // ---- datos

  useEffect(() => {
    if (idsKey) void loadAuthors(idsKey.split("|"));
  }, [idsKey, loadAuthors]);

  // Tras ver un estado la lista se refresca (junta los avisos seguidos de varios estados).
  const refreshTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const refreshAuthors = useCallback(() => {
    if (refreshTimer.current) clearTimeout(refreshTimer.current);
    refreshTimer.current = setTimeout(() => {
      refreshTimer.current = null;
      if (idsRef.current.length > 0) void loadAuthors(idsRef.current);
    }, 300);
  }, [loadAuthors]);
  useEffect(
    () => () => {
      if (refreshTimer.current) clearTimeout(refreshTimer.current);
    },
    [],
  );

  // Mis estados vigentes, por cuenta.
  const [mine, setMine] = useState<Record<string, Status[]>>({});
  const loadMine = useCallback(async (only?: string) => {
    const targets = only ? [only] : idsRef.current;
    const now = Date.now();
    const res = await Promise.all(
      targets.map(async (id) => {
        try {
          return [id, liveSorted(await api.statuses.Mine(id, now - DAY, now + 60 * 60 * 1000))] as const;
        } catch {
          return null; // el aviso ya salió
        }
      }),
    );
    setMine((prev) => {
      const next = { ...prev };
      for (const r of res) if (r) next[r[0]] = r[1];
      return next;
    });
  }, []);

  useEffect(() => {
    if (idsKey) void loadMine();
  }, [idsKey, loadMine]);

  // Lo mío cambia con estados nuevos y con vistas.
  useEffect(() => {
    const offUp = onEvent("status:upsert", (s) => {
      if (s.fromMe && idsRef.current.includes(String(s.accountId))) void loadMine(String(s.accountId));
    });
    const offView = onEvent("status:view", () => void loadMine());
    return () => {
      offUp();
      offView();
    };
  }, [loadMine]);

  const mineList = mine[mineId] ?? [];
  const mineActive = ids.reduce((n, id) => n + (mine[id]?.length ?? 0), 0);

  const openPost = () => {
    const preferred = tab === "view" && activeKey ? String(byKey.get(activeKey)?.accountId ?? "") : mineId;
    setTarget(postable.some((v) => v.account.id === preferred) ? preferred : (postable[0]?.account.id ?? ""));
    setPosting(true);
  };

  if (accounts.length === 0) {
    return (
      <div className="flex h-full flex-col">
        <header className="border-b border-line bg-panel px-6 py-3">
          <h1 className="text-lg font-semibold">Estados</h1>
        </header>
        <EmptyState icon={CircleDot} title="Sin cuentas con estados" className="flex-1" />
      </div>
    );
  }

  const accountChips =
    accounts.length > 1 ? (
      <div className="flex flex-wrap gap-1.5 pb-4" role="radiogroup" aria-label="Cuenta">
        {accounts.map((v) => {
          const on = v.account.id === mineId;
          return (
            <button
              key={v.account.id}
              type="button"
              role="radio"
              aria-checked={on}
              onClick={() => chooseAccount(v.account.id)}
              title={v.account.label}
              className={cx(
                "inline-flex max-w-[180px] items-center gap-1.5 rounded-full border px-3 py-1 text-xs font-medium",
                on ? "border-transparent text-white" : "border-line text-ink hover:bg-[var(--hover)]",
              )}
              style={on ? { background: v.account.color } : undefined}
            >
              {!on && <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: v.account.color }} />}
              <span className="truncate">{v.account.label}</span>
            </button>
          );
        })}
      </div>
    ) : null;

  return (
    <div className="flex h-full">
      <StatusSidebar
        sections={sections}
        accounts={metas}
        channels={channels}
        channelFilter={activeFilter}
        onChannelFilter={pickChannel}
        hideSeen={hideSeen}
        onHideSeen={toggleHide}
        mineActive={mineActive}
        mineSelected={tab === "mine"}
        onMine={() => setTab("mine")}
        canPost={postable.length > 0}
        onAdd={openPost}
        selectedKey={activeKey}
        onSelect={pick}
        unsupported={unsupported}
      />

      <main className="flex min-w-0 flex-1 flex-col bg-bg" aria-label="Visor de estados">
        <header className="flex h-[68px] shrink-0 items-center gap-3 border-b border-line bg-panel px-5">
          <div role="tablist" aria-label="Vista" className="inline-flex rounded-[10px] bg-panel2 p-[3px]">
            {TABS.map(([id, label]) => (
              <button
                key={id}
                type="button"
                role="tab"
                aria-selected={tab === id}
                onClick={() => setTab(id)}
                className={cx(
                  "h-8 rounded-lg px-4 text-[13.5px]",
                  tab === id ? "bg-panel font-semibold text-ink shadow-[0_1px_2px_rgba(11,20,26,0.12)]" : "font-medium text-muted hover:text-ink",
                )}
              >
                {label}
              </button>
            ))}
          </div>
          <span className="flex-1" />
          {postable.length > 0 && (
            <button type="button" className="btn btn-primary !h-[38px] !rounded-xl !px-4 !font-semibold" onClick={openPost}>
              <Plus size={16} aria-hidden />
              Nuevo estado
            </button>
          )}
        </header>

        {tab === "view" ? (
          <div role="tabpanel" aria-label="Ver estados" className="min-h-0 flex-1 bg-chat">
            {activeKey && navAuthors.length > 0 ? (
              <StoryViewer
                inline
                authors={navAuthors}
                accounts={viewerAccounts}
                selectedKey={activeKey}
                onSelect={setSelectedKey}
                onViewed={refreshAuthors}
                paused={posting || !!detail}
              />
            ) : (
              <EmptyState
                icon={CircleDot}
                title="Sin estados por ver"
                description={displayKeys.length > 0 ? "Elige a alguien de la lista para ver sus estados." : undefined}
                className="h-full"
              />
            )}
          </div>
        ) : (
          <div role="tabpanel" aria-label={tab === "mine" ? "Mis estados" : "Estadísticas"} className="min-h-0 flex-1 overflow-y-auto p-6">
            {accountChips}
            {tab === "mine" ? (
              <MyStatusesGrid statuses={mineList} canPost={!!mineCaps?.postStatus} onOpen={setDetail} onAdd={openPost} />
            ) : (
              <StatusStatsPanel key={mineId} accountId={mineId} />
            )}
          </div>
        )}
      </main>

      {posting && (
        <PostStatusModal
          key={targetView?.account.id ?? mineId}
          accountId={targetView?.account.id ?? mineId}
          caps={(targetView?.capabilities as CapSet | undefined) ?? mineCaps}
          targets={postable.map((v) => ({ id: v.account.id, label: v.account.label, channel: String(v.account.channel) }))}
          onTarget={setTarget}
          onClose={() => setPosting(false)}
          onDone={(k) => {
            if (k === "posted") void loadMine();
          }}
        />
      )}
      {detail && (
        <MyStatusDetail
          status={detail}
          accountId={mineId}
          caps={mineCaps}
          onClose={() => setDetail(null)}
          onDeleted={() => void loadMine(mineId)}
        />
      )}
    </div>
  );
}
