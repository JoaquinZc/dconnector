// Piezas mínimas de los esqueletos a la medida: la raíz accesible, una línea de texto y anchos que varían.
import type { ReactNode } from "react";
import { Skeleton } from "@/components/common/Skeleton";

/** Un esqueleto de zona lleva `role="status"`; dentro de un esqueleto de página (`bare`) lo lleva solo la raíz. */
export interface ZoneProps {
  bare?: boolean;
}

export const statusProps = (bare?: boolean) => (bare ? {} : ({ role: "status", "aria-label": "Cargando" } as const));

/** Raíz de un esqueleto de página: ocupa lo mismo que la página y avisa a los lectores de pantalla. */
export function PageRoot({ children, className = "flex h-full flex-col", testId }: { children: ReactNode; className?: string; testId?: string }) {
  return (
    <div role="status" aria-label="Cargando" data-testid={testId} className={className}>
      {children}
    </div>
  );
}

/** Ancho porcentual que varía con el índice (sin azar: el esqueleto no cambia entre pintados). */
export function pct(i: number, base: number, span: number, step = 17): string {
  return `${base + ((i * step) % span)}%`;
}

/**
 * Una línea de texto: una caja de la altura de línea `lh` (la que tendría el texto real) con una barra centrada.
 * Así el esqueleto ocupa lo mismo que el texto y al llegar los datos nada se mueve.
 */
export function Line({ lh = 20, bar = 12, w = "60%", className = "" }: { lh?: number; bar?: number; w?: string | number; className?: string }) {
  return (
    <div className={`flex items-center ${className}`} style={{ height: lh }}>
      <Skeleton style={{ height: bar, width: w }} />
    </div>
  );
}

/** Rellena un hueco con tantos elementos como se pidan. */
export const times = <T,>(n: number, f: (i: number) => T): T[] => Array.from({ length: n }, (_, i) => f(i));
