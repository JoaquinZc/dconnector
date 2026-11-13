import { useCallback, useEffect, useState } from "react";
import { BellRing, ExternalLink, Loader2 } from "lucide-react";
import { api } from "@/api";
import type { NotifyStateId, NotifyStatus, QuietStateId } from "@/api/types";
import { useUI } from "@/store/ui";
import { SettingRow, SettingsCard } from "./SettingRow";
import type { SettingsForm } from "./settingsForm";
import Switch from "./Switch";

interface Props {
  form: SettingsForm;
  patch: (p: Partial<SettingsForm>) => void;
}

export const NOTIFICATIONS_HINT =
  "Si las notificaciones están apagadas o bloqueadas, solo sonará un aviso. Los chats silenciados no avisan.";

/** Cada cuánto se vuelve a leer el estado de Windows mientras la sección está abierta. */
export const NOTIFY_REFRESH_MS = 15_000;

type Tone = "ok" | "warn" | "danger" | "muted";
const TONE_COLOR: Record<Tone, string> = {
  ok: "var(--ok)",
  warn: "var(--warn)",
  danger: "var(--danger)",
  muted: "var(--muted)",
};

interface BadgeInfo {
  label: string;
  tone: Tone;
}

const BADGES: Record<NotifyStateId, BadgeInfo> = {
  granted: { label: "Concedido", tone: "ok" },
  denied: { label: "Bloqueado en Windows", tone: "danger" },
  unknown: { label: "Desconocido", tone: "warn" },
  unsupported: { label: "No disponible", tone: "muted" },
};

const QUIET_BADGES: Record<QuietStateId, BadgeInfo> = {
  active: { label: "Activado", tone: "warn" },
  inactive: { label: "Desactivado", tone: "ok" },
  unknown: { label: "Desconocido", tone: "muted" },
};

/**
 * Estado del permiso → insignia; un estado que la interfaz no conoce se ve como «Desconocido». Con permiso
 * concedido pero los avisos suprimidos (No molestar, pantalla completa…) el tono es de advertencia: «Concedido»
 * no significa «todo activo».
 */
export function badgeFor(state: string, suppressed = false): BadgeInfo {
  const b = BADGES[state as NotifyStateId] ?? BADGES.unknown;
  return state === "granted" && suppressed ? { ...b, tone: "warn" } : b;
}

/** Estado de «No molestar» → insignia (un valor ausente o raro es «Desconocido»). */
export function quietBadgeFor(quiet?: string): BadgeInfo {
  return QUIET_BADGES[quiet as QuietStateId] ?? QUIET_BADGES.unknown;
}

function Pill({ info, testId, suppressed }: { info: BadgeInfo; testId: string; suppressed?: boolean }) {
  const color = TONE_COLOR[info.tone];
  return (
    <span
      data-testid={testId}
      data-tone={info.tone}
      data-suppressed={suppressed ? "true" : undefined}
      className="inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium"
      style={{ color, background: `color-mix(in srgb, ${color} 14%, transparent)` }}
    >
      {info.label}
    </span>
  );
}

/** Hora local de una decisión (milisegundos Unix). */
export function formatDecisionTime(ms: number): string {
  return new Date(ms).toLocaleTimeString("es", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

/**
 * Notificaciones: interruptores (los guarda el botón «Guardar» de la página) y el estado real de Windows,
 * que se lee del sistema y no forma parte del formulario: el permiso, el modo «No molestar» (que oculta y
 * silencia los avisos aunque el permiso esté concedido) y la última decisión del servicio.
 */
export default function NotificationsSection({ form, patch }: Props) {
  const toast = useUI((s) => s.toast);
  const [status, setStatus] = useState<NotifyStatus | null>(null);
  const [failed, setFailed] = useState(false);
  const [testing, setTesting] = useState(false);
  const [testMessage, setTestMessage] = useState("");

  const refresh = useCallback(async () => {
    try {
      setStatus(await api.notifications.Status());
      setFailed(false);
    } catch {
      setFailed(true);
    }
  }, []);

  // Al abrir la sección, al volver a la ventana (por ejemplo, desde la Configuración de Windows) y cada 15 s.
  useEffect(() => {
    void refresh();
    const on = () => void refresh();
    window.addEventListener("focus", on);
    const timer = window.setInterval(on, NOTIFY_REFRESH_MS);
    return () => {
      window.removeEventListener("focus", on);
      window.clearInterval(timer);
    };
  }, [refresh]);

  const test = async () => {
    setTesting(true);
    setTestMessage("");
    try {
      const st = await api.notifications.SendTest();
      setStatus(st);
      const msg = st.testMessage || (st.testVisible ? "Aviso enviado" : st.reason);
      setTestMessage(msg);
      toast(st.testVisible ? "success" : "info", msg);
    } catch {
      /* el aviso ya salió */
    } finally {
      setTesting(false);
    }
  };

  const openSystem = async () => {
    try {
      await api.notifications.OpenSystemSettings();
    } catch {
      /* el aviso ya salió */
    }
  };

  const suppressed = status?.suppressed === true;
  const badge = status ? badgeFor(status.state, suppressed) : null;
  const quiet = status ? quietBadgeFor(status.quiet) : null;
  const quietActive = status?.quiet === "active";
  const denied = status?.state === "denied";

  return (
    <SettingsCard title="Notificaciones">
      <SettingRow label="Notificaciones de escritorio" hint="Muestra un aviso en Windows cuando llega un mensaje nuevo que no estás viendo.">
        <Switch
          label="Notificaciones de escritorio"
          checked={form.notifications}
          onChange={(v) => patch({ notifications: v })}
        />
      </SettingRow>
      <SettingRow label="Sonido" hint="Suena aunque el aviso no se vea (por ejemplo, con «No molestar» de Windows).">
        <Switch label="Sonido" checked={form.notificationsSound} onChange={(v) => patch({ notificationsSound: v })} />
      </SettingRow>
      <SettingRow label="Mostrar el texto del mensaje" hint="Apagado, el aviso solo dice «Mensaje nuevo».">
        <Switch
          label="Mostrar el texto del mensaje"
          checked={form.notificationsPreview}
          onChange={(v) => patch({ notificationsPreview: v })}
        />
      </SettingRow>

      <div className="flex items-start gap-3 px-5 py-3" data-testid="notify-status">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span>Permiso de Windows</span>
            {badge ? (
              <Pill info={badge} testId="notify-badge" suppressed={suppressed && status?.state === "granted"} />
            ) : failed ? (
              <span className="text-sm text-muted">No se pudo leer</span>
            ) : (
              <Loader2 size={14} className="animate-spin text-muted" aria-label="Leyendo el estado" />
            )}
          </div>
          {status?.reason && (
            <p className="mt-0.5 text-sm text-muted" data-testid="notify-reason">
              {status.reason}
            </p>
          )}
        </div>
        <div className="flex shrink-0 flex-wrap justify-end gap-2">
          <button className="btn btn-sm" onClick={() => void test()} disabled={testing}>
            {testing ? <Loader2 size={14} className="animate-spin" /> : <BellRing size={14} />}
            Probar notificación
          </button>
        </div>
      </div>

      <div className="flex items-start gap-3 px-5 py-3" data-testid="notify-quiet">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span>Modo No molestar de Windows</span>
            {quiet && <Pill info={quiet} testId="notify-quiet-badge" />}
          </div>
          {status && quietActive && (
            <p className="mt-0.5 text-sm" style={{ color: "var(--warn)" }} data-testid="notify-quiet-reason" role="status">
              {status.quietReason ||
                "Windows está suprimiendo los avisos: no aparecen en pantalla (se guardan en el centro de notificaciones). Los sonidos de dConnector siguen sonando."}
            </p>
          )}
          {status && status.quiet === "inactive" && (
            <p className="mt-0.5 text-sm text-muted" data-testid="notify-quiet-reason">
              Windows no está suprimiendo los avisos.
            </p>
          )}
          {status && !quietActive && status.quiet !== "inactive" && (
            <p className="mt-0.5 text-sm text-muted" data-testid="notify-quiet-reason">
              No se pudo comprobar si Windows está en «No molestar». Si no ves los avisos, revisa esa opción.
            </p>
          )}
        </div>
        <div className="flex shrink-0 flex-wrap justify-end gap-2">
          {(quietActive || denied) && (
            <button className="btn btn-sm" onClick={() => void openSystem()} data-testid="notify-open-quiet">
              <ExternalLink size={14} />
              Abrir ajustes de Windows
            </button>
          )}
        </div>
      </div>

      {testMessage && (
        <p className="px-5 py-3 text-sm" role="status" data-testid="notify-test-result">
          {testMessage}
        </p>
      )}

      <div className="px-5 py-3" data-testid="notify-last-decision">
        <div className="flex flex-wrap items-baseline gap-2">
          <span>Última decisión</span>
          {status && status.lastDecisionAt > 0 && (
            <span className="text-xs text-muted">{formatDecisionTime(status.lastDecisionAt)}</span>
          )}
        </div>
        <p className="mt-0.5 text-sm text-muted" data-testid="notify-last-decision-text">
          {status?.lastDecision || "Todavía no ha llegado ningún mensaje desde que se abrió dConnector."}
        </p>
      </div>

      <p className="px-5 py-3 text-sm text-muted">{NOTIFICATIONS_HINT}</p>
    </SettingsCard>
  );
}
