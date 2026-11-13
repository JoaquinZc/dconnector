import { useState } from "react";
import { BookOpen } from "lucide-react";
import ProtocolDocDialog from "./ProtocolDocDialog";

/**
 * Enlace «Ver el protocolo»: abre el documento completo del protocolo v1 (el que viaja dentro del
 * programa) en un diálogo grande, con búsqueda y botones para copiarlo.
 */
export default function ProtocolLink() {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button
        type="button"
        className="inline-flex items-center gap-1.5 text-sm font-medium text-accent hover:underline"
        onClick={() => setOpen(true)}
      >
        <BookOpen size={14} />
        Ver el protocolo
      </button>
      {open && <ProtocolDocDialog open onClose={() => setOpen(false)} />}
    </>
  );
}
