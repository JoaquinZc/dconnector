import { useMemo, useState } from "react";
import { Gauge, Megaphone, Plus } from "lucide-react";
import type { Campaign } from "@/api/types";
import CampaignDetail from "@/components/campaigns/CampaignDetail";
import CampaignRow from "@/components/campaigns/CampaignRow";
import CampaignWizard from "@/components/campaigns/CampaignWizard";
import PacingModal from "@/components/campaigns/PacingModal";
import { sortCampaigns } from "@/components/campaigns/campaignModel";
import { useCampaigns } from "@/components/campaigns/useCampaignApi";
import EmptyState from "@/components/common/EmptyState";
import LoadError from "@/components/common/LoadError";
import { CampaignRowsSkeleton } from "@/pages/skeletons/zones";
import { useAccounts } from "@/store/accounts";

export default function CampaignsPage() {
  const views = useAccounts((s) => s.views);
  const { campaigns, live, loaded, failed, reload } = useCampaigns();

  const [detailId, setDetailId] = useState<string | null>(null);
  // `undefined` = cerrado, `null` = nueva, otro = editar.
  const [wizard, setWizard] = useState<Campaign | null | undefined>(undefined);
  const [pacing, setPacing] = useState(false);

  const sorted = useMemo(() => sortCampaigns(campaigns), [campaigns]);
  const detail = detailId ? campaigns.find((c) => c.id === detailId) : undefined;

  const wizardModal =
    wizard !== undefined ? (
      <CampaignWizard
        key={wizard?.id ?? "new"}
        campaign={wizard}
        onClose={() => setWizard(undefined)}
        onDone={(kind, saved) => {
          void reload();
          if (kind === "launched") setDetailId(saved.id);
        }}
      />
    ) : null;

  if (detail) {
    return (
      <>
        <CampaignDetail
          campaign={detail}
          live={live[detail.id]}
          account={views[String(detail.accountId)]}
          onBack={() => setDetailId(null)}
          onEdit={() => setWizard(detail)}
          onChanged={(deleted) => {
            if (deleted) setDetailId(null);
            void reload();
          }}
        />
        {wizardModal}
      </>
    );
  }

  return (
    <div className="flex h-full flex-col">
      <header className="flex items-center gap-3 border-b border-line bg-panel px-6 py-3">
        <h1 className="flex-1 text-lg font-semibold">Campañas</h1>
        <button className="btn" onClick={() => setPacing(true)}>
          <Gauge size={16} />
          Ritmo de la cuenta
        </button>
        <button className="btn btn-primary" onClick={() => setWizard(null)}>
          <Plus size={16} />
          Nueva campaña
        </button>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto p-6">
        {!loaded ? (
          <CampaignRowsSkeleton />
        ) : failed && sorted.length === 0 ? (
          <LoadError what="las campañas" onRetry={() => void reload()} />
        ) : sorted.length === 0 ? (
          <EmptyState
            icon={Megaphone}
            title="Sin campañas"
            action={
              <button className="btn btn-primary" onClick={() => setWizard(null)}>
                <Plus size={16} />
                Nueva campaña
              </button>
            }
          />
        ) : (
          <div className="mx-auto flex max-w-5xl flex-col gap-2" data-testid="campaign-list">
            {sorted.map((c) => (
              <CampaignRow
                key={c.id}
                campaign={c}
                live={live[c.id]}
                account={views[String(c.accountId)]}
                onOpen={() => setDetailId(c.id)}
              />
            ))}
          </div>
        )}
      </div>

      {wizardModal}
      {pacing && <PacingModal onClose={() => setPacing(false)} />}
    </div>
  );
}
