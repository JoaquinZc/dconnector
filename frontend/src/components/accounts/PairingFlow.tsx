// Paso de vinculación (11 §6.8 · 3): según el canal y `AccountStatus.Need`, escuchando `account:status`
// (el store de cuentas ya recibe esos eventos). Se usa en «Añadir cuenta» y en «Volver a vincular».
import { useEffect, useRef, useState, type ReactNode } from "react";
import { QRCodeSVG } from "qrcode.react";
import { ChevronDown, Loader2, RefreshCw } from "lucide-react";
import { api } from "@/api";
import type { AccountStatus } from "@/api/types";
import { cx } from "@/lib/cx";
import { useAccounts } from "@/store/accounts";

interface Props {
  accountId: string;
  channel: string;
  /** Se llama una vez cuando la cuenta queda conectada. */
  onConnected?: () => void;
}

type Effective = "wait" | "scan_qr" | "enter_code" | "phone" | "login_code" | "password" | "cookies";

/** Lo que hay que pedir ahora. Si el adaptador aún no dice nada se parte del primer paso del canal. */
function effectiveNeed(channel: string, status: AccountStatus | undefined): Effective {
  const state = String(status?.state ?? "");
  if (state === "connected") return "wait";
  const need = String(status?.need ?? "");
  if (need) return need as Effective;
  if (state === "connecting") return "wait";
  if (channel === "telegram") return "phone";
  if (channel === "messenger" || channel === "instagram") return "cookies";
  return "scan_qr";
}

function Tabs<T extends string>({
  tabs,
  value,
  onChange,
}: {
  tabs: Array<{ id: T; label: string }>;
  value: T;
  onChange: (t: T) => void;
}) {
  return (
    <div className="mb-4 flex gap-1 rounded-lg bg-panel2 p-1" role="tablist">
      {tabs.map((t) => (
        <button
          key={t.id}
          role="tab"
          aria-selected={value === t.id}
          onClick={() => onChange(t.id)}
          className={cx(
            "flex-1 rounded-md px-3 py-1.5 text-sm font-medium transition-colors",
            value === t.id ? "bg-panel text-ink shadow-sm" : "text-muted hover:text-ink",
          )}
        >
          {t.label}
        </button>
      ))}
    </div>
  );
}

function Waiting({ text }: { text: string }) {
  return (
    <div className="flex flex-col items-center gap-3 py-10 text-muted">
      <Loader2 className="animate-spin" size={28} />
      <p>{text}</p>
    </div>
  );
}

function Detail({ text }: { text?: string }) {
  if (!text) return null;
  return (
    <p className="mt-2 break-words text-sm text-danger" role="alert">
      {text}
    </p>
  );
}

interface TextStepProps {
  label: string;
  placeholder?: string;
  submitLabel: string;
  type?: "text" | "password" | "tel";
  inputMode?: "numeric" | "tel" | "text";
  detail?: string;
  onSubmit: (value: string) => Promise<void>;
  big?: boolean;
  hint?: ReactNode;
}

function TextStep({ label, placeholder, submitLabel, type = "text", inputMode, detail, onSubmit, big, hint }: TextStepProps) {
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);
  const go = async () => {
    if (!value.trim() || busy) return;
    setBusy(true);
    try {
      await onSubmit(value.trim());
    } catch {
      /* el aviso ya salió */
    } finally {
      setBusy(false);
    }
  };
  return (
    <div>
      <label className="label" htmlFor="pair-input">
        {label}
      </label>
      <input
        id="pair-input"
        className={cx("input", big && "!h-14 text-center font-mono text-2xl tracking-[0.3em]")}
        type={type}
        inputMode={inputMode}
        placeholder={placeholder}
        value={value}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => e.key === "Enter" && void go()}
        autoComplete="off"
        data-autofocus
      />
      {hint}
      <Detail text={detail} />
      <div className="mt-4 flex justify-end">
        <button className="btn btn-primary" disabled={!value.trim() || busy} onClick={() => void go()}>
          {busy && <Loader2 size={15} className="animate-spin" />}
          {submitLabel}
        </button>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------- WhatsApp

function WhatsAppPairing({ accountId, status }: { accountId: string; status: AccountStatus | undefined }) {
  const [tab, setTab] = useState<"qr" | "phone">("qr");
  const [requested, setRequested] = useState(false);
  const startedFor = useRef<string | null>(null);

  const begin = async (req: { method: string; phone?: string }) => {
    try {
      await api.accounts.BeginPairing(accountId, req);
    } catch {
      /* el aviso ya salió */
    }
  };

  useEffect(() => {
    if (tab !== "qr") {
      startedFor.current = null;
      return;
    }
    if (startedFor.current === "qr") return;
    startedFor.current = "qr";
    void begin({ method: "qr" });
  }, [tab]); // eslint-disable-line react-hooks/exhaustive-deps

  const need = String(status?.need ?? "");
  const detail = status?.detail;

  return (
    <div>
      <Tabs
        tabs={[
          { id: "qr", label: "Código QR" },
          { id: "phone", label: "Con número" },
        ]}
        value={tab}
        onChange={(t) => {
          setTab(t);
          setRequested(false);
        }}
      />

      {tab === "qr" && (
        <div className="flex flex-col items-center gap-3">
          {need === "scan_qr" && status?.qrCode ? (
            <div className="rounded-xl bg-white p-4" data-testid="qr">
              <QRCodeSVG value={status.qrCode} size={280} level="M" bgColor="#ffffff" fgColor="#111b21" />
            </div>
          ) : (
            <div className="flex h-[312px] w-[312px] items-center justify-center rounded-xl bg-panel2 text-muted">
              <Loader2 className="animate-spin" size={30} />
            </div>
          )}
          <p className="text-center text-sm text-muted">
            WhatsApp › Dispositivos vinculados › Vincular un dispositivo
          </p>
          {detail && (
            <div className="flex flex-col items-center gap-2">
              <p className="text-sm text-danger" role="alert">
                {detail}
              </p>
              <button className="btn btn-sm" onClick={() => void begin({ method: "qr" })}>
                <RefreshCw size={14} />
                Nuevo código QR
              </button>
            </div>
          )}
        </div>
      )}

      {tab === "phone" &&
        (requested && need === "enter_code" && status?.pairCode ? (
          <div className="flex flex-col items-center gap-5">
            <div
              className="rounded-xl border border-line bg-panel2 px-6 py-4 font-mono text-4xl font-semibold tracking-[0.18em]"
              data-testid="pair-code"
            >
              {status.pairCode}
            </div>
            <ol className="w-full list-decimal space-y-1.5 pl-5 text-sm">
              <li>Abre WhatsApp en tu teléfono.</li>
              <li>Toca Ajustes › Dispositivos vinculados › Vincular un dispositivo.</li>
              <li>Elige «Vincular con el número de teléfono» y escribe este código.</li>
            </ol>
            <button className="btn btn-sm btn-ghost" onClick={() => setRequested(false)}>
              Usar otro número
            </button>
            <Detail text={detail} />
          </div>
        ) : requested ? (
          <Waiting text="Generando código…" />
        ) : (
          <TextStep
            label="Número de teléfono"
            placeholder="+593 99 123 4567"
            type="tel"
            inputMode="tel"
            submitLabel="Obtener código"
            detail={detail}
            onSubmit={async (phone) => {
              await api.accounts.BeginPairing(accountId, { method: "phone", phone });
              setRequested(true);
            }}
          />
        ))}
    </div>
  );
}

// ---------------------------------------------------------------- Meta

function CookiesHelp({ channel }: { channel: string }) {
  const instagram = channel === "instagram";
  const site = instagram ? "instagram.com" : "messenger.com";
  const needed = instagram ? "sessionid, ds_user_id, csrftoken" : "c_user, xs, datr";
  return (
    <details className="group mt-3 rounded-lg border border-line px-3 py-2 text-sm">
      <summary className="flex cursor-pointer list-none items-center gap-1.5 font-medium">
        <ChevronDown size={15} className="transition-transform group-open:rotate-180" />
        Cómo copiar las cookies
      </summary>
      <ol className="mt-2 list-decimal space-y-1.5 pl-5 text-muted">
        <li>
          Abre <strong className="text-ink">{site}</strong> en tu navegador e inicia sesión con la cuenta.
        </li>
        <li>
          Pulsa <kbd className="rounded border border-line px-1">F12</kbd> y entra en Aplicación › Cookies (en Firefox: Almacenamiento).
        </li>
        <li>
          Copia estas cookies: <strong className="text-ink">{needed}</strong>. También sirve una extensión como Cookie-Editor con «Exportar › JSON».
        </li>
        <li>Pega el resultado en la caja. Acepta JSON o el formato nombre=valor; nombre2=valor2.</li>
      </ol>
    </details>
  );
}

function MetaPairing({ accountId, channel, status }: { accountId: string; channel: string; status: AccountStatus | undefined }) {
  // Lo fácil primero: usuario y contraseña; las cookies quedan como alternativa.
  const [tab, setTab] = useState<"cookies" | "login">("login");
  const [loginUnsupported, setLoginUnsupported] = useState(false);
  const [cookies, setCookies] = useState("");
  const [user, setUser] = useState("");
  const [pass, setPass] = useState("");
  const [busy, setBusy] = useState(false);
  const detail = status?.detail;
  // «Usuario y contraseña» existe en Messenger e Instagram si el adaptador lo admite (09 §4). Si Meta
  // pide un paso extra (captcha, verificación), el aviso manda a «Pegar cookies».
  const canLogin = (channel === "messenger" || channel === "instagram") && !loginUnsupported;

  const sendCookies = async () => {
    if (!cookies.trim() || busy) return;
    setBusy(true);
    try {
      await api.accounts.SubmitPairing(accountId, { cookies: cookies.trim() });
    } catch {
      /* el aviso ya salió */
    } finally {
      setBusy(false);
    }
  };

  const sendLogin = async () => {
    if (!user.trim() || !pass || busy) return;
    setBusy(true);
    try {
      try {
        await api.accounts.BeginPairing(accountId, { method: "password_login" });
      } catch {
        setLoginUnsupported(true);
        setTab("cookies");
        return;
      }
      await api.accounts.SubmitPairing(accountId, { username: user.trim(), password: pass });
    } catch {
      /* el aviso ya salió */
    } finally {
      setBusy(false);
    }
  };

  return (
    <div>
      {canLogin && (
        <Tabs
          tabs={[
            { id: "cookies", label: "Pegar cookies" },
            { id: "login", label: "Usuario y contraseña" },
          ]}
          value={tab}
          onChange={setTab}
        />
      )}
      {tab === "cookies" || !canLogin ? (
        <div>
          <label className="label" htmlFor="pair-cookies">
            Cookies
          </label>
          <textarea
            id="pair-cookies"
            className="input !min-h-[170px] font-mono text-xs"
            placeholder="Pega aquí las cookies"
            value={cookies}
            onChange={(e) => setCookies(e.target.value)}
            spellCheck={false}
            autoComplete="off"
            data-autofocus
          />
          <CookiesHelp channel={channel} />
          <Detail text={detail} />
          <div className="mt-4 flex justify-end">
            <button className="btn btn-primary" disabled={!cookies.trim() || busy} onClick={() => void sendCookies()}>
              {busy && <Loader2 size={15} className="animate-spin" />}
              Vincular
            </button>
          </div>
        </div>
      ) : (
        <div className="flex flex-col gap-3">
          <div>
            <label className="label" htmlFor="pair-user">
              Usuario
            </label>
            <input id="pair-user" className="input" value={user} onChange={(e) => setUser(e.target.value)} autoComplete="off" />
          </div>
          <div>
            <label className="label" htmlFor="pair-pass">
              Contraseña
            </label>
            <input
              id="pair-pass"
              className="input"
              type="password"
              value={pass}
              onChange={(e) => setPass(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && void sendLogin()}
              autoComplete="off"
            />
          </div>
          <Detail text={detail} />
          <div className="flex justify-end">
            <button className="btn btn-primary" disabled={!user.trim() || !pass || busy} onClick={() => void sendLogin()}>
              {busy && <Loader2 size={15} className="animate-spin" />}
              Iniciar sesión
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------- común

export default function PairingFlow({ accountId, channel, onConnected }: Props) {
  const status = useAccounts((s) => s.views[accountId]?.status);
  const connected = String(status?.state) === "connected";
  const notified = useRef(false);

  useEffect(() => {
    if (connected && !notified.current) {
      notified.current = true;
      onConnected?.();
    }
  }, [connected, onConnected]);

  const submit = (input: { code?: string; password?: string }) => async () => {
    await api.accounts.SubmitPairing(accountId, input);
  };

  if (connected) return <Waiting text="Conectada" />;

  const need = effectiveNeed(channel, status);
  const detail = status?.detail;
  const isWA = channel === "whatsapp";
  const isMeta = channel === "messenger" || channel === "instagram";

  // Los pasos de código y contraseña son comunes (Telegram, y Meta si pide verificación).
  if (need === "login_code") {
    return (
      <TextStep
        key="code"
        label="Código recibido"
        placeholder="12345"
        inputMode="numeric"
        big
        submitLabel="Continuar"
        detail={detail}
        onSubmit={(code) => submit({ code })()}
      />
    );
  }
  if (need === "password") {
    return (
      <TextStep
        key="password"
        label="Contraseña"
        type="password"
        submitLabel="Continuar"
        detail={detail}
        onSubmit={(password) => submit({ password })()}
      />
    );
  }

  if (isWA) return <WhatsAppPairing accountId={accountId} status={status} />;
  if (isMeta) return <MetaPairing accountId={accountId} channel={channel} status={status} />;

  if (channel === "telegram") {
    if (need === "phone") {
      return (
        <TextStep
          key="phone"
          label="Número de teléfono"
          placeholder="+593 99 123 4567"
          type="tel"
          inputMode="tel"
          submitLabel="Enviar código"
          detail={detail}
          onSubmit={async (phone) => {
            await api.accounts.BeginPairing(accountId, { method: "login", phone });
          }}
        />
      );
    }
  }
  return <Waiting text={detail || "Conectando…"} />;
}
