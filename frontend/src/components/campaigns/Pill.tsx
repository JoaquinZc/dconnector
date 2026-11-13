import type { ReactNode } from "react";
import type { Tone } from "./campaignModel";

export const TONE_COLORS: Record<Tone, string> = {
  ok: "var(--ok)",
  warn: "var(--warn)",
  danger: "var(--danger)",
  info: "#0ea5e9",
  muted: "var(--muted)",
};

interface Props {
  tone: Tone;
  children: ReactNode;
  title?: string;
  className?: string;
}

/** Píldora de color por tono (estado de campaña, estado de programado, nivel de revisión). */
export default function Pill({ tone, children, title, className }: Props) {
  const color = TONE_COLORS[tone];
  return (
    <span
      title={title}
      data-tone={tone}
      className={`inline-flex max-w-full items-center whitespace-nowrap rounded-full px-2 py-px text-xs font-medium leading-5 ${className ?? ""}`}
      style={{ background: `color-mix(in srgb, ${color} 16%, transparent)`, color }}
    >
      <span className="min-w-0 overflow-hidden text-ellipsis">{children}</span>
    </span>
  );
}
