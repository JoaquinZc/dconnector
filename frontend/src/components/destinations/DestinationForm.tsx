import { useEffect, useMemo, useRef, useState } from "react";
import { AlertTriangle, Eye, EyeOff, Loader2, PlugZap } from "lucide-react";
import { api } from "@/api";
import type { ContactList, Destination } from "@/api/types";
import Modal from "@/components/common/Modal";
import Switch from "@/components/settings/Switch";
import { CHANNEL_ORDER, channelName } from "@/lib/channels";
import { clearDraft, loadDraft, saveDraft } from "@/lib/draft";
import { useAccounts } from "@/store/accounts";
import { useUI } from "@/store/ui";
import ChipSelect from "./ChipSelect";
import {
  ALL_EVENT_NAMES,
  PROTOCOL_EVENTS,
  contactScopeSummary,
  draftOfDest,
  emptyDestForm,
  formFromDraft,
  hasErrors,
  indexListNames,
  toDestination,
  toForm,
  validateDestForm,
  type DestDraft,
  type DestErrors,
  type DestForm,
} from "./destinationModel";
import ProtocolLink from "./ProtocolLink";

interface Props {
  /** `null` = destino nuevo. */
  destination: Destination | null;
  onClose: () => void;
  /** Tras guardar (también al probar la conexión de un destino nuevo). */
  onSaved: (d: Destination) => void;
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-4">
      <span>{label}</span>
      {children}
    </div>
  );
}

export default function DestinationForm({ destination, onClose, onSaved }: Props) {
  const toast = useUI((s) => s.toast);
  const views = useAccounts((s) => s.views);
  const order = useAccounts((s) => s.order);

  const [saved, setSaved] = useState<Destination | null>(destination);
  // Clave del borrador: el id del destino, o «new» hasta que se guarde por primera vez.
  const draftKey = useRef(destination?.id || "new");
  // Lo guardado, para no dejar borrador cuando el formulario no difiere de él.
  const baseline = useRef(JSON.stringify(draftOfDest(destination ? toForm(destination) : emptyDestForm())));
  const [form, setForm] = useState<DestForm>(() => {
    if (destination) {
      const f = toForm(destination);
      const d = loadDraft<DestDraft>("destinations", destination.id);
      return d ? { ...formFromDraft(d), token: f.token } : f;
    }
    return formFromDraft(loadDraft<DestDraft>("destinations", "new"));
  });
  const [errors, setErrors] = useState<DestErrors>({});
  const [showToken, setShowToken] = useState(false);
  const [busy, setBusy] = useState<"save" | "test" | null>(null);
  const first = useRef(true);
  // Listas de contactos (`null` hasta que cargan, o si falla).
  const [lists, setLists] = useState<ContactList[] | null>(null);

  useEffect(() => {
    let alive = true;
    api.contacts
      .Lists()
      .then((all) => {
        if (!alive) return;
        setLists(all);
        // Una lista que ya no existe no se puede ver ni quitar: se descarta del formulario.
        const exist = new Set(all.map((l) => l.id));
        setForm((f) => {
          const inc = f.includeListIds.filter((id) => exist.has(id));
          const exc = f.excludeListIds.filter((id) => exist.has(id));
          if (inc.length === f.includeListIds.length && exc.length === f.excludeListIds.length) return f;
          return { ...f, includeListIds: inc, excludeListIds: exc };
        });
      })
      .catch(() => {
        /* el aviso ya salió; sin listas el bloque «Contactos» queda en «Todos» */
      });
    return () => {
      alive = false;
    };
  }, []);

  const patch = (p: Partial<DestForm>) => {
    setForm((f) => ({ ...f, ...p }));
    setErrors({});
  };

  // Borrador (sin el token).
  useEffect(() => {
    if (first.current) {
      first.current = false;
      return;
    }
    const d = draftOfDest(form);
    if (JSON.stringify(d) === baseline.current) clearDraft("destinations", draftKey.current);
    else saveDraft("destinations", draftKey.current, d);
  }, [form]);

  const accountOptions = useMemo(
    () =>
      order
        .map((id) => views[id]?.account)
        .filter(Boolean)
        .map((a) => ({ id: a.id, label: a.label, color: a.color })),
    [order, views],
  );
  const channelOptions = CHANNEL_ORDER.map((c) => ({ id: c, label: channelName(c) }));
  const listNames = useMemo(() => (lists ? indexListNames(lists) : null), [lists]);
  // Una lista elegida en un lado se deshabilita en el otro.
  const includeOptions = (lists ?? []).map((l) => ({
    id: l.id,
    label: l.name,
    color: l.color,
    disabled: form.excludeListIds.includes(l.id),
    hint: form.excludeListIds.includes(l.id) ? `${l.name}: ya está en «Excluir estas listas»` : l.name,
  }));
  const excludeOptions = (lists ?? []).map((l) => {
    const inInclude = form.contactMode === "lists" && form.includeListIds.includes(l.id);
    return { id: l.id, label: l.name, color: l.color, disabled: inInclude, hint: inInclude ? `${l.name}: ya está en «Incluir estas listas»` : l.name };
  });

  const toggleEvent = (name: string) =>
    patch({ events: form.events.includes(name) ? form.events.filter((e) => e !== name) : [...form.events, name] });

  const validate = (): boolean => {
    const e = validateDestForm(form);
    setErrors(e);
    return !hasErrors(e);
  };

  /** Guarda (ya validado); devuelve el destino guardado o `null`. */
  const persist = async (): Promise<Destination | null> => {
    try {
      const out = await api.destinations.Save(toDestination(form, saved));
      setSaved(out);
      // Con el token que generó el servicio (campo vacío al crear).
      setForm((f) => ({ ...f, token: out.token }));
      clearDraft("destinations", draftKey.current);
      clearDraft("destinations", out.id);
      draftKey.current = out.id;
      baseline.current = JSON.stringify(draftOfDest(form));
      onSaved(out);
      return out;
    } catch {
      return null; // el aviso ya salió
    }
  };

  const save = async () => {
    if (!validate()) return;
    setBusy("save");
    const out = await persist();
    setBusy(null);
    if (out) {
      toast("success", "Destino guardado");
      onClose();
    }
  };

  // «Probar conexión» necesita un destino guardado: guarda primero lo que haya en el formulario.
  const test = async () => {
    if (!validate()) return;
    setBusy("test");
    try {
      const out = await persist();
      if (!out) return;
      await api.destinations.Test(out.id);
      toast("success", "Conexión correcta");
    } catch {
      /* el aviso ya salió */
    } finally {
      setBusy(null);
    }
  };

  return (
    <Modal
      open
      onClose={onClose}
      size="lg"
      title={destination ? "Editar destino" : "Nuevo destino"}
      footer={
        <>
          <span className="mr-auto">
            <ProtocolLink />
          </span>
          <button className="btn" onClick={() => void test()} disabled={busy !== null}>
            {busy === "test" ? <Loader2 size={15} className="animate-spin" /> : <PlugZap size={15} />}
            Probar conexión
          </button>
          <button className="btn" onClick={onClose} disabled={busy !== null}>
            Cancelar
          </button>
          <button className="btn btn-primary" onClick={() => void save()} disabled={busy !== null}>
            {busy === "save" && <Loader2 size={15} className="animate-spin" />}
            Guardar
          </button>
        </>
      }
    >
      <div className="flex flex-col gap-5">
        <div className="grid grid-cols-2 gap-4">
          <div>
            <label className="label" htmlFor="dest-name">
              Nombre
            </label>
            <input
              id="dest-name"
              data-autofocus
              className="input"
              value={form.name}
              onChange={(e) => patch({ name: e.target.value })}
              aria-invalid={!!errors.name}
            />
            {errors.name && <p className="mt-1 text-xs text-danger">{errors.name}</p>}
          </div>
          <div>
            <label className="label" htmlFor="dest-token">
              Token
            </label>
            <div className="relative">
              <input
                id="dest-token"
                className="input !pr-10 font-mono"
                type={showToken ? "text" : "password"}
                autoComplete="off"
                placeholder="Se genera solo"
                value={form.token}
                onChange={(e) => patch({ token: e.target.value })}
              />
              <button
                type="button"
                className="icon-btn absolute right-0.5 top-0.5"
                aria-label={showToken ? "Ocultar token" : "Mostrar token"}
                title={showToken ? "Ocultar token" : "Mostrar token"}
                onClick={() => setShowToken((v) => !v)}
              >
                {showToken ? <EyeOff size={16} /> : <Eye size={16} />}
              </button>
            </div>
          </div>
          <div className="col-span-2">
            <label className="label" htmlFor="dest-url">
              Dirección
            </label>
            <input
              id="dest-url"
              className="input font-mono"
              placeholder="wss://"
              spellCheck={false}
              value={form.url}
              onChange={(e) => patch({ url: e.target.value })}
              aria-invalid={!!errors.url}
            />
            {errors.url && <p className="mt-1 text-xs text-danger">{errors.url}</p>}
          </div>
        </div>

        <div>
          <span className="label">Cuentas</span>
          <ChipSelect
            ariaLabel="Cuentas"
            allLabel="Todas"
            options={accountOptions}
            value={form.accountIds}
            onChange={(v) => patch({ accountIds: v })}
          />
        </div>

        <div>
          <span className="label">Canales</span>
          <ChipSelect
            ariaLabel="Canales"
            allLabel="Todos"
            options={channelOptions}
            value={form.channels}
            onChange={(v) => patch({ channels: v })}
          />
        </div>

        <div className="flex flex-col gap-3 rounded-lg border border-line p-3" data-testid="contact-scope">
          <div className="flex items-center justify-between gap-4">
            <label className="label !mb-0" htmlFor="dest-contacts">
              Contactos
            </label>
            <select
              id="dest-contacts"
              className="input !w-auto"
              value={form.contactMode}
              onChange={(e) => patch({ contactMode: e.target.value === "lists" ? "lists" : "all" })}
            >
              <option value="all">Todos los contactos</option>
              <option value="lists">Solo estas listas</option>
            </select>
          </div>
          {lists !== null && lists.length === 0 && (
            <p className="text-xs text-muted">Aún no hay listas de contactos. Créalas en Contactos para poder elegirlas aquí.</p>
          )}
          {form.contactMode === "lists" && (
            <div>
              <span className="label">Incluir estas listas</span>
              <ChipSelect
                ariaLabel="Incluir estas listas"
                allLabel=""
                hideAll
                options={includeOptions}
                value={form.includeListIds}
                onChange={(v) => patch({ includeListIds: v })}
              />
            </div>
          )}
          <div>
            <span className="label">Excluir estas listas</span>
            <ChipSelect
              ariaLabel="Excluir estas listas"
              allLabel=""
              hideAll
              options={excludeOptions}
              value={form.excludeListIds}
              onChange={(v) => patch({ excludeListIds: v })}
            />
          </div>
          <p className={errors.contacts ? "text-sm text-danger" : "text-sm text-muted"} data-testid="contact-scope-summary" role={errors.contacts ? "alert" : undefined}>
            {errors.contacts ?? contactScopeSummary(form, listNames)}
          </p>
        </div>

        <div>
          <div className="mb-1 flex items-center gap-3">
            <span className="label !mb-0">Eventos</span>
            <button type="button" className="text-xs font-medium text-accent hover:underline" onClick={() => patch({ events: [...ALL_EVENT_NAMES] })}>
              Todos
            </button>
            <button type="button" className="text-xs font-medium text-accent hover:underline" onClick={() => patch({ events: [] })}>
              Ninguno
            </button>
          </div>
          <div className="grid grid-cols-2 gap-x-4 gap-y-0.5 rounded-lg border border-line bg-panel2 p-2" role="group" aria-label="Eventos">
            {PROTOCOL_EVENTS.map((ev) => (
              <label key={ev.name} className="flex cursor-pointer items-center gap-2 rounded px-2 py-1 hover:bg-[var(--hover)]">
                <input
                  type="checkbox"
                  className="accent-[var(--accent)]"
                  checked={form.events.includes(ev.name)}
                  onChange={() => toggleEvent(ev.name)}
                />
                <span className="min-w-0 flex-1 truncate" title={ev.label}>
                  {ev.label}
                </span>
                <code className="text-[11px] text-muted">{ev.name}</code>
              </label>
            ))}
          </div>
          {errors.events && <p className="mt-1 text-xs text-danger">{errors.events}</p>}
        </div>

        <div className="flex flex-col gap-3 rounded-lg border border-line p-3">
          <Row label="Activo">
            <Switch label="Activo" checked={form.enabled} onChange={(v) => patch({ enabled: v })} />
          </Row>
          <Row label="Incluir los míos">
            <Switch label="Incluir los míos" checked={form.includeFromMe} onChange={(v) => patch({ includeFromMe: v })} />
          </Row>
          <p className="-mt-1 text-xs text-muted">
            Envía también los mensajes que escribes tú, desde dConnector o desde el teléfono, para que el servidor tenga la conversación completa.
          </p>
          <Row label="Incluir grupos">
            <Switch label="Incluir grupos" checked={form.includeGroups} onChange={(v) => patch({ includeGroups: v })} />
          </Row>
          <Row label="Permitir comandos">
            <Switch label="Permitir comandos" checked={form.allowCommands} onChange={(v) => patch({ allowCommands: v })} />
          </Row>
          {form.allowCommands && (
            <p className="flex items-center gap-2 rounded-lg bg-[color-mix(in_srgb,var(--warn)_16%,transparent)] px-3 py-2 text-sm text-warn" role="note">
              <AlertTriangle size={16} className="shrink-0" />
              El servidor podrá enviar mensajes en tu nombre
            </p>
          )}
        </div>
      </div>
    </Modal>
  );
}
