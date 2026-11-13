import { lazy, Suspense } from "react";
import type { EmojiPanelProps } from "./EmojiPickerLazy";

const Panel = lazy(() => import("./EmojiPickerLazy"));

export function isDarkTheme(): boolean {
  return typeof document !== "undefined" && document.documentElement.classList.contains("dark");
}

/** Selector de emojis con carga bajo demanda. */
export default function LazyEmojiPicker(props: Omit<EmojiPanelProps, "dark"> & { dark?: boolean }) {
  return (
    <Suspense fallback={<div className="flex h-24 w-[320px] items-center justify-center text-xs text-[var(--muted)]">Cargando…</div>}>
      <Panel {...props} dark={props.dark ?? isDarkTheme()} />
    </Suspense>
  );
}
