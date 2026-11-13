import type { ReactNode } from "react";
import type { LucideIcon } from "lucide-react";

interface Props {
  icon?: LucideIcon;
  /** Frase corta: «Sin cuentas». */
  title: string;
  /** Una o dos frases que explican por qué está vacío y qué hacer. */
  description?: ReactNode;
  action?: ReactNode;
  className?: string;
}

export default function EmptyState({ icon: Icon, title, description, action, className }: Props) {
  return (
    <div className={`flex flex-col items-center justify-center gap-3 p-8 text-center text-muted ${className ?? ""}`}>
      {Icon && <Icon size={40} strokeWidth={1.4} />}
      <p className="text-base">{title}</p>
      {description && <p className="max-w-md text-sm">{description}</p>}
      {action}
    </div>
  );
}
