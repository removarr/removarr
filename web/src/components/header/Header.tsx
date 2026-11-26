import { Fragment, useEffect, useId, type ReactNode } from "react";
import { useScanFinishedRefresh, useStartScan, useStatus } from "@/api/queries";
import type { ListKind, ListSummary } from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { formatDateTime, formatSize } from "@/lib/format";

const LISTS: { kind: ListKind; label: string }[] = [
  { kind: "movies", label: "Movies" },
  { kind: "series", label: "Series" },
  { kind: "torrents", label: "Torrents" },
];

// ListCount shows one list's count. A list that could not be produced shows "error", with
// the reason in a tooltip and as the accessible description.
function ListCount({ label, summary, describedBy }: { label: string; summary: ListSummary; describedBy: string }) {
  if (!summary.error) return <span>{`${label} ${summary.candidates}`}</span>;
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span tabIndex={0} aria-describedby={describedBy} className="text-destructive underline decoration-dotted">
          {`${label}: error`}
        </span>
      </TooltipTrigger>
      <TooltipContent>{summary.error}</TooltipContent>
    </Tooltip>
  );
}

// Header is the top bar's content: the lead (the menu button and the app's name), the
// last scan with its summary, the free space and the notice that the lists are out of date,
// then the dry run badge and the Scan button at the right end.
export function Header({ lead }: { lead?: ReactNode }) {
  const { data: status } = useStatus();
  const start = useStartScan();
  const base = useId();
  useScanFinishedRefresh(status?.scan.running, status ? (status.lastScan?.id ?? null) : undefined);

  const running = status?.scan.running ?? false;
  // A refusal message is about the state it was shown in: drop it when that state changes.
  const { reset } = start;
  useEffect(() => reset(), [running, reset]);
  const last = status?.lastScan ?? null;
  const lists = last?.summary.lists ?? {};
  const shown = LISTS.filter(({ kind }) => lists[kind]);
  const sum = (field: "protected" | "unmatched" | "waiting") =>
    Object.values(lists).reduce((total, l) => total + (l?.[field] ?? 0), 0);
  const roots = last?.summary.rootFolders ?? [];

  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3">
      {lead && <div className="order-1 flex items-center gap-2">{lead}</div>}

      {/* The details read before the controls; on a narrow screen CSS moves them to a row
          of their own below the controls, on a wide one they fill the middle. */}
      <div
        data-testid="scan-details"
        className="order-3 flex min-w-0 basis-full flex-wrap items-center gap-x-4 gap-y-2 md:order-2 md:flex-1"
      >
        {status && (
          <div className="flex min-w-0 flex-col gap-0.5 text-sm">
            {last ? (
              <>
                <span>{`Last scan ${last.finishedAt ? formatDateTime(last.finishedAt) : "unfinished"}`}</span>
                <p data-testid="counts" className="text-muted-foreground">
                  {shown.map(({ kind, label }, i) => (
                    <Fragment key={kind}>
                      {i > 0 && " · "}
                      <ListCount label={label} summary={lists[kind]!} describedBy={`${base}-${kind}`} />
                    </Fragment>
                  ))}
                </p>
                {shown.map(({ kind }) =>
                  lists[kind]!.error ? (
                    <span key={kind} id={`${base}-${kind}`} className="sr-only">
                      {lists[kind]!.error}
                    </span>
                  ) : null,
                )}
                <p className="text-muted-foreground">
                  <span>{`${sum("protected")} protected`}</span>
                  {" · "}
                  <span>{`${sum("unmatched")} unmatched`}</span>
                  {" · "}
                  <span>{`${sum("waiting")} waiting`}</span>
                </p>
              </>
            ) : (
              <span>No scan yet</span>
            )}
            {status.scan.lastError && <span className="text-destructive">{status.scan.lastError}</span>}
          </div>
        )}

        {roots.length > 0 && (
          <ul aria-label="Free space" className="text-sm text-muted-foreground">
            {roots.map((r) => (
              <li key={r.path} className="flex gap-2">
                <span>{r.path}</span>
                <span className="font-medium text-foreground">{formatSize(r.freeBytes)}</span>
              </li>
            ))}
          </ul>
        )}

        {status?.stale && (
          <p role="status" className="text-sm text-amber-700 dark:text-amber-400">
            Settings changed since the last scan. Scan again to update the lists.
          </p>
        )}
      </div>

      {/* Scan is the last control, at the right end of the first row on every screen. What it
          says about itself (the phase, a refusal) goes below it, so it never moves. */}
      <div data-testid="scan-controls" className="order-2 ml-auto flex flex-col items-end gap-1 md:order-3">
        <div className="flex items-center gap-2">
          {status?.dryRun && <Badge variant="secondary">Dry run</Badge>}
          <Button onClick={() => start.mutate()} disabled={running || start.isPending}>
            {running ? "Scanning…" : "Scan"}
          </Button>
        </div>
        {running && status?.scan.phase && <span className="text-sm text-muted-foreground">{status.scan.phase}</span>}
        {start.error && (
          <span role="alert" className="max-w-xs text-right text-sm text-destructive">
            {start.error.message}
          </span>
        )}
      </div>
    </div>
  );
}
