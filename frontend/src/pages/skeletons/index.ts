// Esqueleto a la medida de cada pestaña. El fallback de carga de `registry.tsx` usa este mapa.
import type { ComponentType } from "react";
import type { Page } from "@/store/ui";
import AccountsSkeleton from "./AccountsSkeleton";
import CampaignsSkeleton from "./CampaignsSkeleton";
import ChatsSkeleton from "./ChatsSkeleton";
import ContactsSkeleton from "./ContactsSkeleton";
import DestinationsSkeleton from "./DestinationsSkeleton";
import ScheduledSkeleton from "./ScheduledSkeleton";
import SettingsSkeleton from "./SettingsSkeleton";
import StatsSkeleton from "./StatsSkeleton";
import StatusesSkeleton from "./StatusesSkeleton";

export const PAGE_SKELETONS: Record<Page, ComponentType> = {
  chats: ChatsSkeleton,
  statuses: StatusesSkeleton,
  campaigns: CampaignsSkeleton,
  scheduled: ScheduledSkeleton,
  contacts: ContactsSkeleton,
  stats: StatsSkeleton,
  accounts: AccountsSkeleton,
  destinations: DestinationsSkeleton,
  settings: SettingsSkeleton,
};
