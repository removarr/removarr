import { ChevronDown, TriangleAlert } from "lucide-react";
import { Fragment, useId, useState } from "react";
import { useStatus } from "@/api/queries";
import type { ListKind, ListSummary } from "@/api/types";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { readOpen, writeOpen } from "@/lib/choice";
import { formatDateTime, formatSize } from "@/lib/format";
import { cn } from "@/lib/utils";

// Whether the scan details are shown or collapsed to their first line, remembered per browser.
const DETAILS_KEY = "removarr.scanDetails";

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

// ScanDetails is the strip below the header bar: what the last scan found. Its first line,
// "Last scan …" with a mark when something needs attention, is one button that collapses the
// rest to that line and brings it back; the choice is kept in the browser. Collapsing never
// hides a problem: the mark names every one the details would show.
export function ScanDetails() {
  const { data: status } = useStatus();
  const base = useId();
  const [expanded, setExpanded] = useState(() => readOpen(DETAILS_KEY));
  if (!status) return null;

  const toggle = () => {
    setExpanded(!expanded);
    writeOpen(DETAILS_KEY, !expanded);
  };
  const last = status.lastScan ?? null;
  const lists = last?.summary.lists ?? {};
  const shown = LISTS.filter(({ kind }) => lists[kind]);
  const sum = (field: "protected" | "unmatched" | "waiting") =>
    Object.values(lists).reduce((total, l) => total + (l?.[field] ?? 0), 0);
  const roots = last?.summary.rootFolders ?? [];
  const lineText = status.scan.running
    ? "Scanning…"
    : last
      ? `Last scan ${last.finishedAt ? formatDateTime(last.finishedAt) : "unfinished"}`
      : "No scan yet";
  const problems = [
    status.stale && "lists out of date",
    Object.values(lists).some((l) => l?.error) && "a list could not be produced",
    Boolean(status.scan.lastError) && "the last scan failed",
  ].filter((p): p is string => typeof p === "string");
  const bodyId = `${base}-details`;

  return (
    <div data-testid="scan-details" className="rounded-md border bg-muted/50 p-1">
      {/* The whole line is the toggle, so its name is what it says; the chevron is last. */}
      <button
        type="button"
        data-testid="scan-summary"
        aria-expanded={expanded}
        aria-controls={bodyId}
        onClick={toggle}
        className="flex min-h-8 w-full flex-wrap items-center gap-x-3 gap-y-1 rounded-md px-2 py-1 text-left text-sm outline-none hover:bg-muted focus-visible:ring-[3px] focus-visible:ring-ring/50"
      >
        <span>{lineText}</span>
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
        <ChevronDown
          aria-hidden="true"
          className={cn(
            "ml-auto size-4 shrink-0 transition-transform duration-200 motion-reduce:transition-none",
            !expanded && "-rotate-90",
          )}
        />
      </button>
      {/* Collapsed, the details stay in the document for aria-controls but out of reach: inert
          and hidden from assistive technology at once, and out of sight once the rows have
          closed, because visibility is part of the collapsing transition only. */}
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
        <div className="flex min-h-0 min-w-0 flex-wrap items-start gap-x-6 gap-y-2 overflow-hidden px-2">
          {last && (
            <div className="flex min-w-0 flex-col gap-0.5 pb-1 text-sm">
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
            </div>
          )}
          {status.scan.lastError && <p className="pb-1 text-sm text-destructive">{status.scan.lastError}</p>}
          {roots.length > 0 && (
            <ul aria-label="Free space" className="pb-1 text-sm text-muted-foreground">
              {roots.map((r) => (
                <li key={r.path} className="flex gap-2">
                  <span>{r.path}</span>
                  <span className="font-medium text-foreground">{formatSize(r.freeBytes)}</span>
                </li>
              ))}
            </ul>
          )}
          {status.stale && (
            <p role="status" className="pb-1 text-sm text-amber-700 dark:text-amber-400">
              Settings changed since the last scan. Scan again to update the lists.
            </p>
          )}
        </div>
      </div>
    </div>
  );
}
