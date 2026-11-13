// «Cargar mensajes anteriores»: pide al canal más historial de UN chat (HistoryService.LoadMore) y dice
// «Cargando…» mientras el servicio le tenga una tanda. Los mensajes llegan por los eventos de siempre y
// la lista conserva la posición del scroll (MessageList).
import { Loader2, History } from "lucide-react";
import { errorMessage } from "@/api";
import type { Chat } from "../../api/core/model/models.js";
import { isChatLoading, useHistory } from "@/store/history";
import { useUI } from "@/store/ui";

export default function LoadEarlier({ chat }: { chat: Pick<Chat, "accountId" | "nativeId" | "title"> }) {
  const loading = useHistory((s) => isChatLoading(s.pending, chat.accountId, chat.nativeId));
  const loadMore = useHistory((s) => s.loadMore);
  const toast = useUI((s) => s.toast);

  const click = () => {
    if (loading) return;
    loadMore(chat.accountId, chat.nativeId, chat.title).catch((e: unknown) => toast("error", errorMessage(e)));
  };

  return (
    <button
      type="button"
      onClick={click}
      disabled={loading}
      aria-busy={loading}
      data-testid="load-earlier"
      className="pointer-events-auto inline-flex items-center gap-1.5 rounded-full border border-[var(--border)] bg-[var(--panel)] px-3.5 py-1.5 text-[12.5px] font-medium text-[var(--muted)] shadow-md transition-colors hover:text-[var(--text)] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--accent)] disabled:cursor-default disabled:opacity-90"
    >
      {loading ? (
        <>
          <Loader2 size={14} className="animate-spin" aria-hidden="true" />
          Cargando…
        </>
      ) : (
        <>
          <History size={14} aria-hidden="true" />
          Cargar mensajes anteriores
        </>
      )}
    </button>
  );
}
