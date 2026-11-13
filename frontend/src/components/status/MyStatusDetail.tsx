import { useCallback, useEffect, useState } from "react";
import { Eye, Loader2, Trash2 } from "lucide-react";
import { api, onEvent } from "@/api";
import type { CapSet, Status, StatusView } from "@/api/types";
import Avatar from "@/components/common/Avatar";
import Modal from "@/components/common/Modal";
import { formatClock } from "@/lib/format";
import { useUI } from "@/store/ui";
import StatusPreview from "./StatusPreview";
import { formatAgo, formatExpiry } from "./statusModel";

interface Props {
  status: Status;
  accountId: string;
  caps?: CapSet | null;
  onClose: () => void;
  onDeleted?: () => void;
}

/** Vistas de uno de mis estados: cuántas, quién (con hora y reacción) y borrar. */
export default function MyStatusDetail({ status, accountId, caps, onClose, onDeleted }: Props) {
  const confirm = useUI((s) => s.confirm);
  const toast = useUI((s) => s.toast);
  const [viewers, setViewers] = useState<StatusView[] | null>(null);
  const [busy, setBusy] = useState(false);
  const canList = !!caps?.statusViewers;

  const load = useCallback(() => {
    if (!canList) return;
    api.statuses
      .Viewers(accountId, status.nativeId)
      .then((v) => setViewers([...v].sort((a, b) => b.at - a.at)))
      .catch(() => setViewers((cur) => cur ?? []));
  }, [accountId, status.nativeId, canList]);

  useEffect(() => {
    load();
    return onEvent("status:view", (v) => {
      if (v.statusKey === status.key) load();
    });
  }, [load, status.key]);

  const total = Math.max(status.viewCount, viewers?.length ?? 0);

  const remove = async () => {
    const ok = await confirm({
      title: "Borrar estado",
      message: "Se borra para todos los que lo pueden ver.",
      confirmLabel: "Borrar",
      danger: true,
    });
    if (!ok) return;
    setBusy(true);
    try {
      await api.statuses.Delete(accountId, status.nativeId);
      toast("success", "Estado borrado");
      onDeleted?.();
      onClose();
    } catch {
      /* el aviso ya salió */
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal
      open
      onClose={onClose}
      size="sm"
      title="Mi estado"
      footer={
        <>
          <button className="btn btn-danger mr-auto" onClick={() => void remove()} disabled={busy}>
            <Trash2 size={15} />
            Borrar
          </button>
          <button className="btn" onClick={onClose}>
            Cerrar
          </button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <div className="flex items-center gap-3">
          <StatusPreview status={status} className="h-24 w-[54px] shrink-0 rounded-lg" />
          <div className="min-w-0">
            <p className="text-sm">{formatAgo(status.postedAt)}</p>
            <p className="text-xs text-muted">{formatExpiry(status.expiresAt)}</p>
            <p className="mt-2 inline-flex items-center gap-1.5 text-base font-semibold" data-testid="view-count">
              <Eye size={16} className="text-muted" />
              {total}
            </p>
          </div>
        </div>

        {canList && (
          <div>
            <span className="label">Vistas</span>
            {viewers === null ? (
              <div className="flex justify-center p-4 text-muted">
                <Loader2 className="animate-spin" size={18} />
              </div>
            ) : viewers.length === 0 ? (
              <p className="py-3 text-center text-sm text-muted">Sin vistas</p>
            ) : (
              <ul className="flex max-h-72 flex-col overflow-y-auto">
                {viewers.map((v) => (
                  <li key={`${v.viewerId}-${v.at}`} className="flex items-center gap-3 py-1.5">
                    <Avatar name={v.viewerName || v.viewerId} size={32} colorKey={v.viewerId} />
                    <span className="min-w-0 flex-1 truncate" title={v.viewerName || v.viewerId}>
                      {v.viewerName || v.viewerId}
                    </span>
                    {v.reaction && <span className="text-lg">{v.reaction}</span>}
                    <span className="text-xs text-muted">{formatClock(v.at)}</span>
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}
      </div>
    </Modal>
  );
}
