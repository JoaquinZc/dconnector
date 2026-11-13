// Asistente de «Importar CSV». Limitación conocida: no hay un método del servicio que lea el archivo,
// así que no hay vista previa de filas; la persona indica el NÚMERO de cada columna (1, 2, 3…).
import { useState } from "react";
import { FileSpreadsheet } from "lucide-react";
import { api } from "@/api";
import type { ContactList, ImportResult, PickedFile } from "@/api/types";
import Modal from "@/components/common/Modal";
import { formatFileSize } from "@/lib/format";
import { clearDraft, loadDraft, saveDraft } from "@/lib/draft";
import {
  DEFAULT_CSV_WIZARD,
  importSummary,
  repeatedColumns,
  sortLists,
  toCsvMapping,
  validateCsvWizard,
  type CsvWizard,
} from "./contactModel";

interface Props {
  lists: ContactList[];
  onClose: () => void;
  onDone: (r: ImportResult) => void;
}

const COLUMN_OPTIONS = Array.from({ length: 20 }, (_, i) => i + 1);

function ColumnSelect({ id, label, value, onChange }: { id: string; label: string; value: number; onChange: (n: number) => void }) {
  return (
    <div>
      <label className="label" htmlFor={id}>
        {label}
      </label>
      <select id={id} className="input" value={value} onChange={(e) => onChange(Number(e.target.value))}>
        <option value={0}>No importar</option>
        {COLUMN_OPTIONS.map((n) => (
          <option key={n} value={n}>
            Columna {n}
          </option>
        ))}
      </select>
    </div>
  );
}

/** Elegir archivo, indicar las columnas y la lista de destino, e importar. */
export default function ImportCsvModal({ lists, onClose, onDone }: Props) {
  const [file, setFile] = useState<PickedFile | null>(null);
  const [wiz, setWiz] = useState<CsvWizard>(() => {
    const d = { ...DEFAULT_CSV_WIZARD, ...(loadDraft<Partial<CsvWizard>>("contacts", "csv") ?? {}) };
    return lists.some((l) => l.id === d.listId) ? d : { ...d, listId: "" };
  });
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<ImportResult | null>(null);
  const [touched, setTouched] = useState(false);

  const set = (p: Partial<CsvWizard>) => {
    const next = { ...wiz, ...p };
    setWiz(next);
    saveDraft("contacts", "csv", next);
  };

  const pick = async () => {
    try {
      const files = await api.files.PickFiles("csv");
      if (files[0]) setFile(files[0]);
    } catch {
      /* el aviso ya salió */
    }
  };

  const error = validateCsvWizard(wiz, !!file);

  const run = async () => {
    setTouched(true);
    if (error || !file || busy) return;
    setBusy(true);
    try {
      const r = await api.contacts.ImportCSV(file.path, toCsvMapping(wiz));
      clearDraft("contacts", "csv");
      setResult(r);
      onDone(r);
    } catch {
      /* el aviso ya salió */
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal
      open
      size="md"
      title="Importar CSV"
      onClose={onClose}
      footer={
        result ? (
          <button className="btn btn-primary" onClick={onClose}>
            Listo
          </button>
        ) : (
          <>
            <button className="btn" onClick={onClose}>
              Cancelar
            </button>
            <button className="btn btn-primary" disabled={busy} onClick={() => void run()}>
              {busy ? "Importando…" : "Importar"}
            </button>
          </>
        )
      }
    >
      {result ? (
        <p className="text-base" data-testid="import-result">
          {importSummary(result)}
        </p>
      ) : (
        <div className="flex flex-col gap-4">
          <div className="flex items-center gap-3 rounded-lg border border-line p-3">
            <FileSpreadsheet size={28} className="shrink-0 text-muted" />
            <div className="min-w-0 flex-1">
              {file ? (
                <>
                  <p className="truncate font-medium" title={file.path}>
                    {file.name}
                  </p>
                  <p className="text-sm text-muted">{formatFileSize(file.size)}</p>
                </>
              ) : (
                <p className="text-muted">Sin archivo</p>
              )}
            </div>
            <button className="btn" onClick={() => void pick()}>
              {file ? "Cambiar" : "Elegir archivo"}
            </button>
          </div>

          <label className="flex items-center gap-2">
            <input type="checkbox" checked={wiz.hasHeader} onChange={(e) => set({ hasHeader: e.target.checked })} />
            Tiene encabezado
          </label>

          <div className="grid grid-cols-2 gap-3">
            <ColumnSelect id="csv-name" label="Nombre" value={wiz.name} onChange={(n) => set({ name: n })} />
            <ColumnSelect id="csv-phone" label="Teléfono" value={wiz.phone} onChange={(n) => set({ phone: n })} />
            <ColumnSelect id="csv-tags" label="Etiquetas" value={wiz.tags} onChange={(n) => set({ tags: n })} />
            <ColumnSelect id="csv-notes" label="Notas" value={wiz.notes} onChange={(n) => set({ notes: n })} />
          </div>
          {repeatedColumns(wiz) && <p className="text-xs text-warn">Hay columnas repetidas</p>}

          <div>
            <label className="label" htmlFor="csv-list">
              Añadir a la lista
            </label>
            <select id="csv-list" className="input" value={wiz.listId} onChange={(e) => set({ listId: e.target.value })}>
              <option value="">Ninguna</option>
              {sortLists(lists).map((l) => (
                <option key={l.id} value={l.id}>
                  {l.name}
                </option>
              ))}
            </select>
          </div>

          {touched && error && <p className="text-sm text-danger">{error}</p>}
        </div>
      )}
    </Modal>
  );
}
