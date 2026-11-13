import {
  BarChart3,
  CalendarClock,
  CircleDot,
  Megaphone,
  MessagesSquare,
  Settings,
  Smartphone,
  Unplug,
  Users,
  type LucideIcon,
} from "lucide-react";
import { PAGE_ORDER, type Page } from "@/store/ui";

export interface PageMeta {
  id: Page;
  label: string;
  /** Etiqueta corta para espacios estrechos (opcional; el rail usa `label` si falta). */
  shortLabel?: string;
  icon: LucideIcon;
  /** Número del atajo Ctrl+N. */
  shortcut: number;
}

const META: Record<Page, Omit<PageMeta, "shortcut">> = {
  chats: { id: "chats", label: "Chats", icon: MessagesSquare },
  statuses: { id: "statuses", label: "Estados", icon: CircleDot },
  campaigns: { id: "campaigns", label: "Campañas", icon: Megaphone },
  scheduled: { id: "scheduled", label: "Programados", icon: CalendarClock },
  contacts: { id: "contacts", label: "Contactos", icon: Users },
  stats: { id: "stats", label: "Estadísticas", icon: BarChart3 },
  accounts: { id: "accounts", label: "Cuentas", icon: Smartphone },
  destinations: { id: "destinations", label: "Destinos", icon: Unplug },
  settings: { id: "settings", label: "Ajustes", icon: Settings },
};

/** Las 9 páginas en el orden de Ctrl+1…9. */
export const PAGES: PageMeta[] = PAGE_ORDER.map((id, i) => ({ ...META[id], shortcut: i + 1 }));

export function pageMeta(id: Page): PageMeta {
  return PAGES.find((p) => p.id === id) ?? PAGES[0];
}
