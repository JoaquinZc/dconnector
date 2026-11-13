import { CircleDot, Eye, Plus } from "lucide-react";
import type { Status } from "@/api/types";
import EmptyState from "@/components/common/EmptyState";
import { MyStatusesGridSkeleton } from "@/pages/skeletons/zones";
import StatusPreview from "./StatusPreview";
import { formatAgo, formatExpiry } from "./statusModel";

interface Props {
  statuses: Status[];
  canPost: boolean;
  onOpen: (s: Status) => void;
  onAdd: () => void;
  /** Aún no llegaron: tarjetas de esqueleto en vez de «Sin estados». */
  loading?: boolean;
}

/** Mis estados vigentes como tarjetas con su contador de vistas. */
export default function MyStatusesGrid({ statuses, canPost, onOpen, onAdd, loading = false }: Props) {
  if (loading && statuses.length === 0) {
    return <MyStatusesGridSkeleton />;
  }
  if (statuses.length === 0) {
    return (
      <EmptyState
        icon={CircleDot}
        title="Sin estados"
        action={
          canPost ? (
            <button className="btn btn-primary" onClick={onAdd}>
              <Plus size={16} />
              Nuevo estado
            </button>
          ) : undefined
        }
      />
    );
  }
  return (
    <div className="grid gap-4" style={{ gridTemplateColumns: "repeat(auto-fill, minmax(132px, 1fr))" }}>
      {statuses.map((s) => (
        <button
          key={s.key}
          type="button"
          onClick={() => onOpen(s)}
          className="card overflow-hidden text-left transition-shadow hover:shadow-md"
          aria-label={`Estado ${formatAgo(s.postedAt)}`}
        >
          <div className="relative">
            <StatusPreview status={s} className="aspect-[9/16] w-full" />
            <span className="absolute bottom-1.5 left-1.5 inline-flex items-center gap-1 rounded-full bg-black/60 px-2 py-0.5 text-xs font-medium text-white">
              <Eye size={12} />
              {s.viewCount}
            </span>
          </div>
          <div className="px-2.5 py-2">
            <p className="text-xs">{formatAgo(s.postedAt)}</p>
            <p className="truncate text-[11px] text-muted" title={formatExpiry(s.expiresAt)}>
              {formatExpiry(s.expiresAt)}
            </p>
          </div>
        </button>
      ))}
    </div>
  );
}
