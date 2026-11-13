// Formularios cortos de adjuntos: ubicación, contacto y encuesta. Devuelven un `Outgoing` parcial.
import { useState } from "react";
import { Plus, Trash2 } from "lucide-react";
import type { Outgoing } from "../../api/core/model/models.js";
import { Overlay, OverlayHeader } from "./parts";

export type Payload = Pick<Outgoing, "kind" | "text" | "location" | "contact" | "poll">;

const kindOf = (k: string) => k as unknown as Outgoing["kind"];

/** Número con coma o punto decimal; NaN si no es válido. */
export function parseCoord(s: string): number {
  const t = String(s).trim().replace(",", ".");
  return t === "" ? NaN : Number(t);
}

export function buildLocation(latS: string, lngS: string, name: string, address: string): Payload | null {
  const lat = parseCoord(latS);
  const lng = parseCoord(lngS);
  if (!Number.isFinite(lat) || !Number.isFinite(lng) || Math.abs(lat) > 90 || Math.abs(lng) > 180) return null;
  return {
    kind: kindOf("location"),
    text: "",
    location: { lat, lng, name: name.trim(), address: address.trim(), live: false },
  };
}

export function buildContact(name: string, phone: string): Payload | null {
  const n = name.trim();
  const p = phone.trim();
  if (!n || !p) return null;
  const vcard = `BEGIN:VCARD\nVERSION:3.0\nFN:${n}\nTEL;type=CELL:${p}\nEND:VCARD`;
  return { kind: kindOf("contact"), text: "", contact: { name: n, phone: p, vcard } };
}

export function buildPoll(question: string, options: string[], multi: boolean): Payload | null {
  const q = question.trim();
  const opts = options.map((o) => o.trim()).filter(Boolean);
  if (!q || new Set(opts).size < 2) return null;
  return {
    kind: kindOf("poll"),
    text: "",
    poll: { question: q, options: Array.from(new Set(opts)).map((name) => ({ name, voters: [] })), multi },
  };
}

const input =
  "w-full rounded-lg border border-[var(--border)] bg-[var(--panel-2)] px-3 py-2 text-[14px] outline-none focus:border-[var(--accent)]";

function Actions({ ok, onClose, onSend, label = "Enviar" }: { ok: boolean; onClose: () => void; onSend: () => void; label?: string }) {
  return (
    <div className="flex justify-end gap-2 border-t border-[var(--border)] px-4 py-3">
      <button type="button" onClick={onClose} className="rounded-full px-4 py-1.5 text-[14px] font-medium text-[var(--muted)] hover:bg-black/5 dark:hover:bg-white/10">
        Cancelar
      </button>
      <button
        type="button"
        disabled={!ok}
        onClick={onSend}
        className="rounded-full bg-[var(--accent)] px-5 py-1.5 text-[14px] font-medium text-white disabled:opacity-40"
      >
        {label}
      </button>
    </div>
  );
}

export function LocationForm({ onClose, onSend }: { onClose: () => void; onSend: (p: Payload) => void }) {
  const [lat, setLat] = useState("");
  const [lng, setLng] = useState("");
  const [name, setName] = useState("");
  const [address, setAddress] = useState("");
  const payload = buildLocation(lat, lng, name, address);
  return (
    <Overlay onClose={onClose} width={400} label="Enviar ubicación">
      <OverlayHeader title="Ubicación" onClose={onClose} />
      <div className="flex flex-col gap-2.5 p-4">
        <div className="flex gap-2">
          <input className={input} placeholder="Latitud" value={lat} onChange={(e) => setLat(e.target.value)} inputMode="decimal" autoFocus />
          <input className={input} placeholder="Longitud" value={lng} onChange={(e) => setLng(e.target.value)} inputMode="decimal" />
        </div>
        <input className={input} placeholder="Nombre del lugar" value={name} onChange={(e) => setName(e.target.value)} />
        <input className={input} placeholder="Dirección" value={address} onChange={(e) => setAddress(e.target.value)} />
      </div>
      <Actions ok={!!payload} onClose={onClose} onSend={() => payload && onSend(payload)} />
    </Overlay>
  );
}

export function ContactForm({ onClose, onSend }: { onClose: () => void; onSend: (p: Payload) => void }) {
  const [name, setName] = useState("");
  const [phone, setPhone] = useState("");
  const payload = buildContact(name, phone);
  return (
    <Overlay onClose={onClose} width={400} label="Enviar contacto">
      <OverlayHeader title="Contacto" onClose={onClose} />
      <div className="flex flex-col gap-2.5 p-4">
        <input className={input} placeholder="Nombre" value={name} onChange={(e) => setName(e.target.value)} autoFocus />
        <input className={input} placeholder="Teléfono (+593…)" value={phone} onChange={(e) => setPhone(e.target.value)} inputMode="tel" />
      </div>
      <Actions ok={!!payload} onClose={onClose} onSend={() => payload && onSend(payload)} />
    </Overlay>
  );
}

export function PollForm({ onClose, onSend }: { onClose: () => void; onSend: (p: Payload) => void }) {
  const [question, setQuestion] = useState("");
  const [options, setOptions] = useState(["", ""]);
  const [multi, setMulti] = useState(false);
  const payload = buildPoll(question, options, multi);
  return (
    <Overlay onClose={onClose} width={420} label="Crear encuesta">
      <OverlayHeader title="Encuesta" onClose={onClose} />
      <div className="flex flex-col gap-2.5 overflow-y-auto p-4">
        <input className={input} placeholder="Pregunta" value={question} onChange={(e) => setQuestion(e.target.value)} autoFocus />
        {options.map((o, i) => (
          <div key={i} className="flex items-center gap-2">
            <input
              className={input}
              placeholder={`Opción ${i + 1}`}
              value={o}
              onChange={(e) => setOptions(options.map((x, j) => (j === i ? e.target.value : x)))}
            />
            {options.length > 2 && (
              <button type="button" title="Quitar opción" onClick={() => setOptions(options.filter((_, j) => j !== i))} className="text-[var(--muted)] hover:text-[var(--danger)]">
                <Trash2 size={16} />
              </button>
            )}
          </div>
        ))}
        {options.length < 12 && (
          <button type="button" onClick={() => setOptions([...options, ""])} className="flex items-center gap-1.5 self-start text-[13px] font-medium text-[var(--accent)]">
            <Plus size={14} /> Añadir opción
          </button>
        )}
        <label className="flex items-center gap-2 text-[13.5px]">
          <input type="checkbox" checked={multi} onChange={(e) => setMulti(e.target.checked)} className="accent-[var(--accent)]" />
          Permitir varias respuestas
        </label>
      </div>
      <Actions ok={!!payload} onClose={onClose} onSend={() => payload && onSend(payload)} />
    </Overlay>
  );
}
