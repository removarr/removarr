import { useState, type ReactNode } from "react";
import { Link } from "react-router";
import { useCandidates, useStatus } from "@/api/queries";
import type { Candidate, ListKind } from "@/api/types";
import { CandidateList } from "@/components/list/CandidateList";
import { IgnoreActions } from "@/components/list/IgnoreActions";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { ignoredCard, ignoredColumns } from "@/components/rows/ignored";
import { mediaCard, mediaColumns, mediaSortKeys } from "@/components/rows/MediaRow";
import { TorrentDetails } from "@/components/rows/TorrentDetails";
import { torrentCard, torrentColumns, torrentSortKeys } from "@/components/rows/TorrentRow";

const TITLES: Record<ListKind, string> = { movies: "Movies", series: "Series", torrents: "Torrents" };

// bulkActions is the slot that the ignore and delete tasks fill. ignoredView tells it which
// view the rows come from.
export type BulkActions = (selected: Candidate[], clear: () => void, ignoredView: boolean) => ReactNode;

function Message({ children }: { children: ReactNode }) {
  return <p className="py-8 text-center text-sm text-muted-foreground">{children}</p>;
}

function Alert({ children }: { children: ReactNode }) {
  return (
    <div role="alert" className="rounded-md border border-destructive/50 bg-destructive/10 px-4 py-3 text-sm text-destructive">
      {children}
    </div>
  );
}

// ListPage loads one list and shows it, or says why there is nothing to show. The states
// come in a fixed order of precedence: status loading, no app configured, the list's error
// (no rows and no bulk actions, spec 6.8 rule 5), no scan yet, an empty list, the list.
export function ListPage({ list, bulkActions }: { list: ListKind; bulkActions?: BulkActions }) {
  const status = useStatus();
  const [ignored, setIgnored] = useState(false);
  const candidates = useCandidates(list, ignored);
  const title = (
    <div className="flex flex-wrap items-center justify-between gap-2">
      <h1 className="text-2xl font-semibold">{TITLES[list]}</h1>
      <div className="flex items-center gap-2">
        <Switch id="show-ignored" checked={ignored} onCheckedChange={setIgnored} />
        <Label htmlFor="show-ignored">Show ignored</Label>
      </div>
    </div>
  );

  let body: ReactNode;
  if (!status.data) {
    body = status.error ? <Alert>{status.error.message}</Alert> : <Message>Loading…</Message>;
  } else if (Object.values(status.data.connections).every((c) => !c.configured)) {
    body = (
      <div className="flex flex-col items-center gap-1 py-8 text-sm text-muted-foreground">
        <p>removarr is not connected to any app yet.</p>
        <Link to="/settings" className="underline">Open the settings</Link>
      </div>
    );
  } else if (candidates.error) {
    body = <Alert>{candidates.error.message}</Alert>;
  } else if (!candidates.data) {
    body = <Message>Loading…</Message>;
  } else if (candidates.data.error) {
    body = <Alert>{candidates.data.error}</Alert>;
  } else if (candidates.data.scannedAt === null) {
    body = <Message>No scan yet. Start one with Scan above.</Message>;
  } else {
    const rows = candidates.data.candidates ?? [];
    const torrents = list === "torrents";
    const columns = torrents ? torrentColumns : mediaColumns;
    const card = torrents ? torrentCard : mediaCard;
    body = (
      <CandidateList
        key={String(ignored)}
        rows={rows}
        columns={ignored ? ignoredColumns(columns) : columns}
        sortKeys={torrents ? torrentSortKeys : mediaSortKeys}
        renderCard={ignored ? ignoredCard(card) : card}
        renderExpanded={torrents ? undefined : (r) => <TorrentDetails row={r} />}
        actions={(selected, clear) => (
          <>
            <IgnoreActions selected={selected} clear={clear} ignoredView={ignored} />
            {ignored ? null : (bulkActions?.(selected, clear, false) ?? null)}
          </>
        )}
        emptyText={ignored ? "Nothing is ignored in this list." : "Nothing to suggest."}
      />
    );
  }

  return (
    <div className="flex flex-col gap-4">
      {title}
      {body}
    </div>
  );
}
