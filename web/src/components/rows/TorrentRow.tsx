import type { ReactNode } from "react";
import type { Candidate } from "@/api/types";
import type { CandidateListProps, ColumnSpec } from "@/components/list/CandidateList";
import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { formatDate, formatDays, formatRatio, formatSize } from "@/lib/format";

export const torrentSortKeys: CandidateListProps["sortKeys"] = [
  { id: "size", label: "Size" },
  { id: "addedAt", label: "Added" },
  { id: "title", label: "Name" },
];

const REASONS = {
  orphan: { label: "Orphan", tip: "No media manager knows this torrent." },
  unlinked: { label: "Unlinked", tip: "A media manager grabbed it, but it backs no library file now." },
} as const;

function Reason({ row }: { row: Candidate }): ReactNode {
  if (!row.reason) return null;
  const { label, tip } = REASONS[row.reason];
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Badge variant="outline" tabIndex={0}>{label}</Badge>
      </TooltipTrigger>
      <TooltipContent>{tip}</TooltipContent>
    </Tooltip>
  );
}

// the torrent of a torrent row; the list builds exactly one per row.
const t = (r: Candidate) => r.torrents?.[0];

export const torrentColumns: ColumnSpec[] = [
  { id: "name", header: "Name", cell: (r) => <span className="font-medium break-all">{r.title}</span> },
  { id: "size", header: "Size", cell: (r) => formatSize(r.size), className: "whitespace-nowrap" },
  { id: "state", header: "State", cell: (r) => t(r)?.state ?? "—" },
  { id: "category", header: "Category", cell: (r) => t(r)?.category || "—" },
  { id: "tracker", header: "Tracker", cell: (r) => t(r)?.tracker || "—" },
  { id: "ratio", header: "Ratio", cell: (r) => formatRatio(t(r)?.ratio ?? null) },
  { id: "seeding", header: "Seeding time", cell: (r) => formatDays(t(r)?.seedingDays ?? null), className: "whitespace-nowrap" },
  { id: "added", header: "Added", cell: (r) => formatDate(t(r)?.addedAt), className: "whitespace-nowrap" },
  {
    id: "reason",
    header: "Reason",
    cell: (r) => (
      <div className="flex flex-wrap items-center gap-1">
        <Reason row={r} />
        {r.belowSeedMin && <Badge variant="secondary">Below seeding minimum</Badge>}
      </div>
    ),
  },
];

// torrentCard is the row of a torrent on a narrow screen.
export function torrentCard(r: Candidate): ReactNode {
  const x = t(r);
  return (
    <div className="flex flex-col gap-1 text-sm">
      <span className="font-medium break-all">{r.title}</span>
      <span>{formatSize(r.size)} · {x?.state ?? "—"}</span>
      <span className="text-muted-foreground">
        {x?.category || "no category"} · {x?.tracker || "no tracker"} · ratio {formatRatio(x?.ratio ?? null)} · seeded {formatDays(x?.seedingDays ?? null)}
      </span>
      <span className="text-muted-foreground">Added {formatDate(x?.addedAt)}</span>
      <div className="flex flex-wrap gap-1">
        <Reason row={r} />
        {r.belowSeedMin && <Badge variant="secondary">Below seeding minimum</Badge>}
      </div>
    </div>
  );
}
