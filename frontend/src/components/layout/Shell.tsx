import type { ReactNode } from "react";
import ConfirmDialog from "@/components/common/ConfirmDialog";
import Toasts from "@/components/common/Toasts";
import { useUI } from "@/store/ui";
import Rail from "./Rail";

/** Marco de la app: rail a la izquierda, la página a la derecha, avisos y confirmación global. */
export default function Shell({ children }: { children: ReactNode }) {
  const confirmReq = useUI((s) => s.confirmReq);
  const resolveConfirm = useUI((s) => s.resolveConfirm);
  const o = confirmReq?.options;
  return (
    <div className="flex h-full w-full bg-bg text-ink">
      <Rail />
      <main className="min-w-0 flex-1 overflow-hidden">{children}</main>
      <Toasts />
      <ConfirmDialog
        open={!!confirmReq}
        title={o?.title ?? ""}
        message={o?.message}
        confirmLabel={o?.confirmLabel}
        cancelLabel={o?.cancelLabel}
        danger={o?.danger}
        requireText={o?.requireText}
        onConfirm={() => resolveConfirm(true)}
        onCancel={() => resolveConfirm(false)}
      />
    </div>
  );
}
