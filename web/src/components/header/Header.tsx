import { ChevronDown, TriangleAlert } from "lucide-react";
import { Fragment, useEffect, useId, useState, type ReactNode } from "react";
import { useScanFinishedRefresh, useStartScan, useStatus } from "@/api/queries";
import type { ListKind, ListSummary } from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { readOpen, writeOpen } from "@/lib/choice";
import { formatDateTime, formatSize } from "@/lib/format";
import { cn } from "@/lib/utils";

// Whether the scan details are shown or collapsed to their summary, remembered per browser.
const DETAILS_KEY = "removarr.scanDetails";

const lastScanText = (finishedAt?: string | null) => `Last scan ${finishedAt ? formatDateTime(finishedAt) : "unfinished"}`;

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

  const [expanded, setExpanded] = useState(() => readOpen(DETAILS_KEY));
  const bodyId = `${base}-details`;
  const toggleDetails = () => {
    setExpanded(!expanded);
    writeOpen(DETAILS_KEY, !expanded);
  };
  const summaryText = running ? "Scanning…" : last ? lastScanText(last.finishedAt) : "No scan yet";
  // Collapsing never hides a problem: the summary names every one the details would show.
  const problems = status
    ? [
        status.stale && "lists out of date",
        Object.values(lists).some((l) => l?.error) && "a list could not be produced",
        Boolean(status.scan.lastError) && "the last scan failed",
      ].filter((p): p is string => typeof p === "string")
    : [];

  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3">
      {lead && <div className="order-1 flex items-center gap-2">{lead}</div>}

      {/* The details read before the controls; on a narrow screen CSS moves them to a row
          of their own below the controls, on a wide one they fill the middle. They sit on a
          surface of their own and collapse to a one-line summary. */}
      <div
        data-testid="scan-details"
        className="order-3 flex min-w-0 basis-full items-start gap-1 rounded-md border bg-muted/50 px-1.5 py-1 md:order-2 md:flex-1"
      >
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label={expanded ? "Hide scan details" : "Show scan details"}
          aria-expanded={expanded}
          aria-controls={bodyId}
          onClick={toggleDetails}
        >
          <ChevronDown
            aria-hidden="true"
            className={cn("transition-transform duration-200 motion-reduce:transition-none", !expanded && "-rotate-90")}
          />
        </Button>
        <div className="min-w-0 flex-1">
          {!expanded && status && (
            <p data-testid="scan-summary" className="flex min-h-8 flex-wrap items-center gap-x-3 text-sm">
              <span>{summaryText}</span>
              {problems.length > 0 && (
                <span
                  data-testid="attention"
                  title={`Needs attention: ${problems.join(", ")}`}
                  className="inline-flex items-center gap-1 text-amber-700 dark:text-amber-400"
                >
                  <TriangleAlert aria-hidden="true" className="size-4 shrink-0" />
                  <span>Needs attention</span>
                  <span className="sr-only">{`: ${problems.join(", ")}`}</span>
                </span>
              )}
            </p>
          )}
          {/* Collapsed, the details stay in the document for aria-controls but out of reach:
              inert and hidden from assistive technology at once, and out of sight once the
              rows have closed, because visibility is part of the collapsing transition only. */}
          <div
            id={bodyId}
            inert={!expanded}
            aria-hidden={expanded ? undefined : true}
            className={cn(
              "grid duration-200 ease-out motion-reduce:transition-none",
              expanded
                ? "visible grid-rows-[1fr] transition-[grid-template-rows]"
                : "invisible grid-rows-[0fr] transition-[grid-template-rows,visibility]",
            )}
          >
            <div className="flex min-h-0 min-w-0 flex-wrap items-center gap-x-4 gap-y-2 overflow-hidden px-1">
              {status && (
                <div className="flex min-w-0 flex-col gap-0.5 text-sm">
                  {last ? (
                    <>
                      <span>{lastScanText(last.finishedAt)}</span>
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
          </div>
        </div>
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
