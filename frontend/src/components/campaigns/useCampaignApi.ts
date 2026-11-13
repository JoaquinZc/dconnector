// Costuras locales de Campañas con la api y los enlaces: lista en vivo, revisión con retraso y pie de baja.
import { useCallback, useEffect, useRef, useState } from "react";
import { api, errorMessage, onEvent } from "@/api";
import { rpc } from "@/api/transport";
import type { AppSettings, Campaign, CampaignReview, PacingSettings } from "@/api/types";
import {
  applyProgress,
  campaignFromForm,
  reviewKey,
  type CampaignForm,
  type CampaignLive,
  type CampaignProgressData,
} from "./campaignModel";
import { DEFAULT_PACING } from "./pacingModel";

/** Revisión sin aviso de error: se pide al teclear y el error se pinta dentro del panel. */
export async function reviewCampaign(cp: Campaign): Promise<CampaignReview> {
  return await rpc<CampaignReview>("Campaigns", "Review", [cp]);
}

/** Ritmo de la cuenta sin aviso de error (solo se muestra de resumen); si falla, los valores seguros. */
export async function fetchPacing(accountId: string): Promise<PacingSettings> {
  try {
    return await rpc<PacingSettings>("Campaigns", "GetPacing", [accountId]);
  } catch {
    return DEFAULT_PACING;
  }
}

/** Texto del pie de baja de Ajustes (vacío si no se puede leer). */
export async function fetchFooterText(): Promise<string> {
  try {
    const s = await rpc<AppSettings>("Settings", "Get", []);
    return String(s?.optOutFooterText ?? "");
  } catch {
    return "";
  }
}

/** Campañas de la lista y, por campaña, lo último que avisó el motor (`campaign:progress`). */
export function useCampaigns() {
  const [campaigns, setCampaigns] = useState<Campaign[]>([]);
  const [live, setLive] = useState<Record<string, CampaignLive>>({});
  const [loaded, setLoaded] = useState(false);
  const [failed, setFailed] = useState(false);
  const ref = useRef<Campaign[]>([]);

  const reload = useCallback(async () => {
    try {
      const list = await api.campaigns.List();
      ref.current = list;
      setCampaigns(list);
      setFailed(false);
    } catch {
      setFailed(true); // el aviso ya salió; se conserva lo que hubiera
    } finally {
      setLoaded(true);
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  // Tras reconectar el servidor no reenvía lo perdido: se vuelve a pedir la lista.
  useEffect(() => onEvent("connection", (ev) => ev.reconnected && ev.state === "connected" && void reload()), [reload]);

  useEffect(() => {
    return onEvent<CampaignProgressData>("campaign:progress", (p) => {
      const r = applyProgress(ref.current, p);
      if (!r) {
        void reload(); // campaña nueva
        return;
      }
      ref.current = r.list;
      setCampaigns(r.list);
      setLive((l) => ({ ...l, [String(p.campaignId)]: r.live }));
    });
  }, [reload]);

  return { campaigns, live, loaded, failed, reload };
}

interface ReviewState {
  review: CampaignReview | null;
  loading: boolean;
  error: string;
}

/**
 * Revisa la campaña del formulario con un pequeño retraso al escribir. Solo vuelve a pedir si
 * cambia algo que influye (no el nombre). Un resultado viejo nunca pisa a uno nuevo.
 */
export function useReview(
  form: CampaignForm,
  pacing: PacingSettings | null,
  base: Campaign | null,
  enabled: boolean,
  delay = 600,
): ReviewState {
  const [state, setState] = useState<ReviewState>({ review: null, loading: false, error: "" });
  const latest = useRef({ form, pacing, base });
  latest.current = { form, pacing, base };
  const key = reviewKey(form, pacing);
  const hasAudience = form.listIds.length + form.contactIds.length > 0;
  const ready = enabled && !!form.accountId && hasAudience;

  useEffect(() => {
    if (!ready) {
      setState({ review: null, loading: false, error: "" });
      return;
    }
    let alive = true;
    setState((s) => ({ ...s, loading: true, error: "" }));
    const t = setTimeout(async () => {
      const { form: f, pacing: p, base: b } = latest.current;
      try {
        const review = await reviewCampaign(campaignFromForm(f, p ?? DEFAULT_PACING, b));
        if (alive) setState({ review, loading: false, error: "" });
      } catch (e) {
        if (alive) setState({ review: null, loading: false, error: errorMessage(e) });
      }
    }, delay);
    return () => {
      alive = false;
      clearTimeout(t);
    };
  }, [key, ready, delay]);

  return state;
}
