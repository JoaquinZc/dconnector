import { useEffect, useState } from "react";
import { desktopPhaseText, useConnection } from "@/api/connection";

/** Segundos que faltan para el próximo intento (0 si ya pasó o no hay). */
export function secondsLeft(retryInMs: number, at: number, now: number): number {
  if (!retryInMs || retryInMs <= 0) return 0;
  return Math.max(0, Math.ceil((at + retryInMs - now) / 1000));
}

/** Estado del servidor según el escritorio, con «Reintentando en N s». No pinta nada si está listo o no hay datos. */
export default function DesktopStatusLine({ className = "" }: { className?: string }) {
  const d = useConnection((s) => s.desktop);
  const [now, setNow] = useState(() => Date.now());
  const counting = !!d && d.state !== "ready" && (d.retryInMs ?? 0) > 0;
  useEffect(() => {
    if (!counting) return;
    setNow(Date.now());
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [counting, d?.at]);
  if (!d || d.state === "ready") return null;
  const secs = secondsLeft(d.retryInMs ?? 0, d.at, now);
  return (
    <span className={className} data-testid="desktop-status">
      {desktopPhaseText(d)}
      {secs > 0 ? ` Reintentando en ${secs} s.` : ""}
    </span>
  );
}
