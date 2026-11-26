import type { ReactNode } from "react";
import type { Candidate } from "@/api/types";
import { Badge } from "@/components/ui/badge";
import type { ColumnSpec, CandidateListProps } from "@/components/list/CandidateList";
import { formatDate, formatSize } from "@/lib/format";
import { rowLabel } from "@/lib/selection";

export const mediaSortKeys: CandidateListProps["sortKeys"] = [
  { id: "size", label: "Size" },
  { id: "watchedAt", label: "Watched" },
  { id: "title", label: "Title" },
];

function titleOf(row: Candidate): string {
  return row.year === undefined ? row.title : `${row.title} (${row.year})`;
}

function scopeOf(row: Candidate): string | null {
  if (row.scope === "season") return `Season ${row.season}`;
  if (row.scope === "series") return "Whole series";
  return null;
}

function watchedBy(row: Candidate): string {
  return row.watchedBy && row.watchedBy.length > 0 ? row.watchedBy.join(", ") : "—";
}

// Badges shows the flags of a row: a season of a series that still airs, and torrents that
// have not seeded long enough.
function Badges({ row }: { row: Candidate }): ReactNode {
  return (
    <>
      {row.stillAiring && <Badge variant="outline">Still airing</Badge>}
      {row.belowSeedMin && <Badge variant="secondary">Below seeding minimum</Badge>}
    </>
  );
}

// Tied names the rows that are deleted together with this one (spec 6.3, 6.7). A season row
// carries its series' title, so the label names the season too; a label holds a comma, so
// the labels are separated by semicolons.
function Tied({ row }: { row: Candidate }): ReactNode {
  if (!row.tiedWith || row.tiedWith.length === 0) return null;
  return (
    <p className="text-xs text-muted-foreground">
      Deleted together with: {row.tiedWith.map(rowLabel).join("; ")}
    </p>
  );
}

export const mediaColumns: ColumnSpec[] = [
  {
    id: "title",
    header: "Title",
    cell: (r) => (
      <div className="flex flex-col gap-1">
        <span className="font-medium">{titleOf(r)}</span>
        {scopeOf(r) && <span className="text-xs text-muted-foreground">{scopeOf(r)}</span>}
        {r.stillAiring && <Badge variant="outline" className="self-start">Still airing</Badge>}
        <Tied row={r} />
      </div>
    ),
  },
  { id: "size", header: "Size", cell: (r) => formatSize(r.size), className: "whitespace-nowrap" },
  { id: "watchedBy", header: "Watched by", cell: watchedBy },
  { id: "watchedAt", header: "Watched", cell: (r) => formatDate(r.watchedAt), className: "whitespace-nowrap" },
  {
    id: "torrents",
    header: "Torrents",
    cell: (r) => (
      <div className="flex flex-wrap items-center gap-1">
        <span>{r.torrents?.length ?? 0}</span>
        {r.belowSeedMin && <Badge variant="secondary">Below seeding minimum</Badge>}
      </div>
    ),
  },
];

// mediaCard is the row of a movie or a season on a narrow screen.
export function mediaCard(r: Candidate): ReactNode {
  return (
    <div className="flex flex-col gap-1 text-sm">
      <span className="font-medium">{titleOf(r)}</span>
      {scopeOf(r) && <span className="text-xs text-muted-foreground">{scopeOf(r)}</span>}
      <span>{formatSize(r.size)}</span>
      <span className="text-muted-foreground">Watched by {watchedBy(r)}, {formatDate(r.watchedAt)}</span>
      <span className="text-muted-foreground">Torrents: {r.torrents?.length ?? 0}</span>
      <div className="flex flex-wrap gap-1">
        <Badges row={r} />
      </div>
      <Tied row={r} />
    </div>
  );
}
