import type { Candidate } from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { formatDays, formatRatio, formatSize } from "@/lib/format";

// TorrentDetails lists the torrents behind a movie or a season. The server sets
// outsideLibrary only when it is 50 MB or more (spec 6.7), so it is shown whenever present.
export function TorrentDetails({ row }: { row: Candidate }) {
  const torrents = row.torrents ?? [];
  if (torrents.length === 0) return <p className="text-sm text-muted-foreground">No torrents.</p>;
  return (
    <ul className="flex flex-col gap-2 text-sm">
      {torrents.map((t) => (
        <li key={t.id} className="flex flex-col gap-0.5">
          <span className="font-medium break-all">{t.name}</span>
          <span className="text-muted-foreground">
            {t.tracker || "no tracker"} · ratio {formatRatio(t.ratio)} · seeded {formatDays(t.seedingDays)}
          </span>
          <div className="flex flex-wrap gap-1">
            {t.belowSeedMin && <Badge variant="secondary">Below seeding minimum</Badge>}
            {t.outsideLibrary !== undefined && <Badge variant="outline">Outside the library: {formatSize(t.outsideLibrary)}</Badge>}
          </div>
        </li>
      ))}
    </ul>
  );
}
