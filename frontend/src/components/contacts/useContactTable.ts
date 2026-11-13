// Carga de la tabla de contactos: primera página, páginas siguientes y recarga. Una respuesta tardía
// de una consulta anterior se descarta (cada petición lleva un número).
import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "@/api";
import type { Contact } from "@/api/types";
import {
  EMPTY_TABLE,
  appendPage,
  applyMerge,
  buildFilter,
  firstPage,
  hasMore,
  nextOffset,
  removeContact,
  replaceContact,
  type ContactQuery,
  type TableState,
} from "./contactModel";

export interface ContactTable {
  rows: Contact[];
  total: number;
  loading: boolean;
  loaded: boolean;
  more: boolean;
  loadMore: () => void;
  reload: () => Promise<void>;
  /** Cambios locales sin volver a pedir (guardado, borrado, unión). */
  patch: (c: Contact) => void;
  drop: (id: string) => void;
  merged: (kept: Contact, absorbedId: string) => void;
}

export function useContactTable(query: ContactQuery): ContactTable {
  const [state, setState] = useState<TableState>(EMPTY_TABLE);
  const [loading, setLoading] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const seq = useRef(0);
  const stateRef = useRef(state);
  stateRef.current = state;
  const loadingRef = useRef(false);

  const fetchFirst = useCallback(async () => {
    const mine = ++seq.current;
    loadingRef.current = true;
    setLoading(true);
    try {
      const page = await api.contacts.List(buildFilter(query, 0));
      if (mine !== seq.current) return;
      setState(firstPage(page?.contacts, page?.total ?? 0));
    } catch {
      /* el aviso ya salió */
    } finally {
      if (mine === seq.current) {
        loadingRef.current = false;
        setLoading(false);
        setLoaded(true);
      }
    }
  }, [query]);

  useEffect(() => {
    void fetchFirst();
  }, [fetchFirst]);

  const loadMore = useCallback(() => {
    if (loadingRef.current || !hasMore(stateRef.current)) return;
    const mine = seq.current;
    loadingRef.current = true;
    setLoading(true);
    api.contacts
      .List(buildFilter(query, nextOffset(stateRef.current)))
      .then((page) => {
        if (mine !== seq.current) return;
        setState((s) => appendPage(s, page?.contacts, page?.total ?? s.total));
      })
      .catch(() => undefined)
      .finally(() => {
        if (mine === seq.current) {
          loadingRef.current = false;
          setLoading(false);
        }
      });
  }, [query]);

  return {
    rows: state.rows,
    total: state.total,
    loading,
    loaded,
    more: hasMore(state),
    loadMore,
    reload: fetchFirst,
    patch: (c) => setState((s) => replaceContact(s, c)),
    drop: (id) => setState((s) => removeContact(s, id)),
    merged: (kept, absorbedId) => setState((s) => applyMerge(s, kept, absorbedId)),
  };
}
