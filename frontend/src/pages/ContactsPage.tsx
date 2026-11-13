import { useCallback, useEffect, useMemo, useState } from "react";
import { useReloadOnReconnect } from "@/lib/useReconnect";
import { FileSpreadsheet, GitMerge, Loader2, Plus, RefreshCw, Search, Smartphone, Users, X } from "lucide-react";
import { api } from "@/api";
import type { Contact, ContactList } from "@/api/types";
import EmptyState from "@/components/common/EmptyState";
import ContactPanel from "@/components/contacts/ContactPanel";
import ContactsTable from "@/components/contacts/ContactsTable";
import ImportCsvModal from "@/components/contacts/ImportCsvModal";
import ImportFromAccountModal from "@/components/contacts/ImportFromAccountModal";
import KebabMenu from "@/components/contacts/KebabMenu";
import ListModal from "@/components/contacts/ListModal";
import ListsSidebar from "@/components/contacts/ListsSidebar";
import MergeModal from "@/components/contacts/MergeModal";
import NewContactModal from "@/components/contacts/NewContactModal";
import SuggestMergesModal from "@/components/contacts/SuggestMergesModal";
import {
  SOURCE_OPTIONS,
  countLabel,
  filterBySource,
  hasActiveFilter,
  knownTags,
  normalizeContact,
  syncSummary,
  type SourceFilter,
} from "@/components/contacts/contactModel";
import { useContactTable } from "@/components/contacts/useContactTable";
import { useAccounts } from "@/store/accounts";
import { useUI } from "@/store/ui";

type ModalState =
  | { kind: "none" }
  | { kind: "new" }
  | { kind: "list"; list: ContactList | null }
  | { kind: "csv" }
  | { kind: "account" }
  | { kind: "suggest" }
  | { kind: "merge"; contact: Contact };

export default function ContactsPage() {
  const confirm = useUI((s) => s.confirm);
  const toast = useUI((s) => s.toast);
  const loadAccounts = useAccounts((s) => s.load);
  const accountsLoaded = useAccounts((s) => s.loaded);

  const [lists, setLists] = useState<ContactList[]>([]);
  const [listId, setListId] = useState("");
  const [tag, setTag] = useState("");
  const [source, setSource] = useState<SourceFilter>("all");
  const [syncing, setSyncing] = useState(false);
  const [search, setSearch] = useState("");
  const [debounced, setDebounced] = useState("");
  const [selected, setSelected] = useState<Contact | null>(null);
  const [modal, setModal] = useState<ModalState>({ kind: "none" });

  useEffect(() => {
    const t = setTimeout(() => setDebounced(search), 250);
    return () => clearTimeout(t);
  }, [search]);

  useEffect(() => {
    if (!accountsLoaded) void loadAccounts();
  }, [accountsLoaded, loadAccounts]);

  const query = useMemo(() => ({ query: debounced, listId, tag }), [debounced, listId, tag]);
  const table = useContactTable(query);

  const loadLists = useCallback(async () => {
    try {
      setLists(await api.contacts.Lists());
    } catch {
      /* el aviso ya salió */
    }
  }, []);

  useEffect(() => {
    void loadLists();
  }, [loadLists]);

  const reloadTable = table.reload;
  useReloadOnReconnect(() => {
    void loadLists();
    void reloadTable();
  });

  const tags = useMemo(() => knownTags(table.rows, tag ? [tag] : []), [table.rows, tag]);
  // «Origen» se filtra aquí: el filtro de Go no lo admite.
  const rows = useMemo(() => filterBySource(table.rows, source), [table.rows, source]);

  // Con un origen elegido las páginas siguientes se piden solas hasta tenerlas todas (si no, el filtro
  // sería engañoso: solo vería la parte ya cargada).
  const { more: tableMore, loading: tableLoading, loadMore: tableLoadMore } = table;
  useEffect(() => {
    if (source !== "all" && tableMore && !tableLoading) tableLoadMore();
  }, [source, tableMore, tableLoading, tableLoadMore, table.rows.length]);
  const close = () => setModal({ kind: "none" });

  const changed = (c: Contact) => {
    const n = normalizeContact(c);
    table.patch(n);
    setSelected(n);
    void loadLists();
  };

  const merged = (kept: Contact, absorbedId: string) => {
    table.merged(kept, absorbedId);
    setSelected((s) => (s && (s.id === absorbedId || s.id === kept.id) ? kept : s));
    void loadLists();
  };

  const saveList = async (l: ContactList) => {
    await api.contacts.SaveList(l);
    close();
    await loadLists();
    if (l.id) void table.reload(); // el nombre o el color cambian lo que se ve en las filas
  };

  const deleteList = async (l: ContactList) => {
    const ok = await confirm({
      title: "Eliminar lista",
      message: `Se borra «${l.name}». Los contactos no se borran.`,
      confirmLabel: "Eliminar",
      danger: true,
    });
    if (!ok) return;
    try {
      await api.contacts.DeleteList(l.id);
      if (listId === l.id) setListId("");
      toast("success", "Lista eliminada");
      await loadLists();
      void table.reload();
    } catch {
      /* el aviso ya salió */
    }
  };

  const syncAll = async () => {
    if (syncing) return;
    setSyncing(true);
    try {
      const created = await api.contacts.SyncAll();
      toast("success", syncSummary(created));
      if (created > 0) {
        await table.reload();
        void loadLists();
      }
    } catch {
      /* el aviso ya salió */
    } finally {
      setSyncing(false);
    }
  };

  const activeFilter = hasActiveFilter(query) || source !== "all";
  const totalText = source === "all" ? countLabel(table.total) : `${countLabel(rows.length)}${table.more ? "…" : ""}`;
  const activeList = lists.find((l) => l.id === listId);

  return (
    <div className="flex h-full">
      <ListsSidebar
        lists={lists}
        selected={listId}
        onSelect={setListId}
        onCreate={() => setModal({ kind: "list", list: null })}
        onEdit={(l) => setModal({ kind: "list", list: l })}
        onDelete={(l) => void deleteList(l)}
      />

      <main className="flex min-w-0 flex-1 flex-col bg-panel">
        <header className="flex flex-wrap items-center gap-x-3 gap-y-2 border-b border-line px-6 py-3">
          <h1 className="text-lg font-semibold">Contactos</h1>
          <span className="text-sm text-muted" data-testid="contacts-total" aria-live="polite">
            {totalText}
          </span>
          <div className="relative ml-4 min-w-[180px] max-w-sm flex-1">
            <Search size={16} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-muted" />
            <input
              className="input !pl-9"
              placeholder="Buscar"
              aria-label="Buscar contactos"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
          </div>
          <div className="w-44">
            <input
              className="input"
              placeholder="Etiqueta"
              aria-label="Filtrar por etiqueta"
              list="contact-tag-options"
              value={tag}
              onChange={(e) => setTag(e.target.value)}
            />
            <datalist id="contact-tag-options">
              {tags.map((t) => (
                <option key={t} value={t} />
              ))}
            </datalist>
          </div>
          <select
            className="input w-40"
            aria-label="Filtrar por origen"
            value={source}
            onChange={(e) => setSource(e.target.value as SourceFilter)}
          >
            {SOURCE_OPTIONS.map((o) => (
              <option key={o.id} value={o.id}>
                {o.id === "all" ? "Origen: todos" : o.label}
              </option>
            ))}
          </select>
          <span className="flex-1" />
          <button
            className="btn"
            onClick={() => void syncAll()}
            disabled={syncing}
            title="Agrega los contactos que falten con las personas de tus cuentas"
          >
            {syncing ? <Loader2 size={16} className="animate-spin" /> : <RefreshCw size={16} />}
            Actualizar desde las cuentas
          </button>
          <button className="btn btn-primary" onClick={() => setModal({ kind: "new" })}>
            <Plus size={16} />
            Nuevo contacto
          </button>
          <KebabMenu
            items={[
              { label: "Importar de una cuenta", icon: <Smartphone size={16} />, onClick: () => setModal({ kind: "account" }) },
              { label: "Importar CSV", icon: <FileSpreadsheet size={16} />, onClick: () => setModal({ kind: "csv" }) },
              { label: "Sugerencias de unión", icon: <GitMerge size={16} />, onClick: () => setModal({ kind: "suggest" }) },
            ]}
          />
        </header>

        {(activeList || tag) && (
          <div className="flex items-center gap-2 border-b border-line px-6 py-2 text-sm">
            {activeList && (
              <span className="inline-flex items-center gap-1.5 rounded-full bg-panel2 py-0.5 pl-2.5 pr-1">
                <span className="h-2 w-2 rounded-full" style={{ background: activeList.color }} />
                {activeList.name}
                <button className="rounded-full p-0.5 text-muted hover:text-ink" aria-label="Quitar filtro de lista" onClick={() => setListId("")}>
                  <X size={12} />
                </button>
              </span>
            )}
            {tag && (
              <span className="inline-flex items-center gap-1.5 rounded-full bg-panel2 py-0.5 pl-2.5 pr-1">
                {tag}
                <button className="rounded-full p-0.5 text-muted hover:text-ink" aria-label="Quitar filtro de etiqueta" onClick={() => setTag("")}>
                  <X size={12} />
                </button>
              </span>
            )}
          </div>
        )}

        {table.loaded && rows.length === 0 && (source === "all" || !table.more) ? (
          <EmptyState
            icon={Users}
            title={activeFilter ? "Sin resultados" : "Sin contactos"}
            description={
              activeFilter
                ? source !== "all"
                  ? "Ningún contacto tiene ese origen con los filtros actuales."
                  : undefined
                : "Los contactos se agregan solos al vincular tus cuentas: cada persona con la que chateas aparece aquí. Si acabas de vincular una, pulsa «Actualizar desde las cuentas»."
            }
            className="flex-1"
            action={
              !activeFilter && (
                <div className="flex flex-wrap items-center justify-center gap-2">
                  <button className="btn" onClick={() => void syncAll()} disabled={syncing}>
                    {syncing ? <Loader2 size={16} className="animate-spin" /> : <RefreshCw size={16} />}
                    Actualizar desde las cuentas
                  </button>
                  <button className="btn btn-primary" onClick={() => setModal({ kind: "new" })}>
                    <Plus size={16} />
                    Nuevo contacto
                  </button>
                </div>
              )
            }
          />
        ) : (
          <ContactsTable
            rows={rows}
            lists={lists}
            selectedId={selected?.id ?? null}
            loading={table.loading}
            more={table.more}
            onLoadMore={table.loadMore}
            onOpen={(c) => setSelected(c)}
            onTag={setTag}
          />
        )}
      </main>

      {selected && (
        <ContactPanel
          key={selected.id}
          contact={selected}
          lists={lists}
          onClose={() => setSelected(null)}
          onChanged={changed}
          onDeleted={(id) => {
            table.drop(id);
            setSelected(null);
            void loadLists();
          }}
          onMerge={(c) => setModal({ kind: "merge", contact: c })}
          onSplit={() => void table.reload()}
        />
      )}

      {modal.kind === "new" && (
        <NewContactModal
          onClose={close}
          onCreated={(c) => {
            close();
            void table.reload();
            void loadLists();
            setSelected(normalizeContact(c));
          }}
        />
      )}
      {modal.kind === "list" && (
        <ListModal list={modal.list} lists={lists} onClose={close} onSave={saveList} />
      )}
      {modal.kind === "csv" && (
        <ImportCsvModal
          lists={lists}
          onClose={close}
          onDone={() => {
            void table.reload();
            void loadLists();
          }}
        />
      )}
      {modal.kind === "account" && (
        <ImportFromAccountModal
          onClose={close}
          onDone={() => {
            void table.reload();
            void loadLists();
          }}
        />
      )}
      {modal.kind === "suggest" && <SuggestMergesModal onClose={close} onMerged={merged} />}
      {modal.kind === "merge" && (
        <MergeModal
          contact={modal.contact}
          onClose={close}
          onMerged={(kept, absorbedId) => {
            merged(kept, absorbedId);
            close();
          }}
        />
      )}
    </div>
  );
}
