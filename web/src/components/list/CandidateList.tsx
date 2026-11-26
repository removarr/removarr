import { ArrowDownWideNarrow, ArrowUpNarrowWide, ChevronRight } from "lucide-react";
import { Fragment, useMemo, useState, type ReactNode } from "react";
import type { Candidate } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { prune, selectAll, selectedRows, toggle, totalSize, type Selection } from "@/lib/selection";
import { useMediaQuery } from "@/lib/useMediaQuery";
import { cn } from "@/lib/utils";
import { BulkBar } from "./BulkBar";

export interface ColumnSpec { id: string; header: string; cell: (row: Candidate) => ReactNode; className?: string }

type SortKey = "size" | "watchedAt" | "addedAt" | "title";
type Direction = "asc" | "desc";

export interface CandidateListProps {
  rows: Candidate[];
  columns: ColumnSpec[];
  sortKeys: { id: SortKey; label: string }[];
  renderCard: (row: Candidate) => ReactNode;
  renderExpanded?: (row: Candidate) => ReactNode;
  actions: (selected: Candidate[], clear: () => void) => ReactNode;
  emptyText: string;
}

// matches reports whether a row contains the lower-case text in its title, its year or the
// names of its torrents; a torrent row also matches on its torrents' category and tracker.
function matches(row: Candidate, needle: string): boolean {
  const parts = [row.title, row.year === undefined ? "" : String(row.year)];
  for (const t of row.torrents ?? []) {
    parts.push(t.name);
    if (row.list === "torrents") parts.push(t.category, t.tracker);
  }
  return parts.some((p) => p.toLowerCase().includes(needle));
}

// time reads a date for sorting; an absent date sorts as the oldest.
function time(iso?: string): number {
  const t = iso ? Date.parse(iso) : NaN;
  return Number.isNaN(t) ? -Infinity : t;
}

function compareBy(key: SortKey, a: Candidate, b: Candidate): number {
  if (key === "title") return a.title.localeCompare(b.title);
  const value = (r: Candidate) =>
    key === "size" ? r.size : key === "watchedAt" ? time(r.watchedAt) : time(r.torrents?.[0]?.addedAt);
  const x = value(a), y = value(b);
  return x < y ? -1 : x > y ? 1 : 0;
}

// CandidateList is the list every page renders: a filter, a sort, a table on wide screens and
// one card per row on narrow ones, and the bulk bar. What is in a row comes from the props.
//
// The selection holds only rows the user can see (spec 5.3). Whenever the shown rows change,
// because the filter changed or the list was reloaded, the selection is pruned to them before
// anything renders, so a bulk action never names a hidden row or one the list no longer holds.
export function CandidateList({ rows, columns, sortKeys, renderCard, renderExpanded, actions, emptyText }: CandidateListProps) {
  const wide = useMediaQuery("(min-width: 768px)");
  const [query, setQuery] = useState("");
  const [sortKey, setSortKey] = useState<SortKey>("size");
  const [direction, setDirection] = useState<Direction>("desc");
  const [selection, setSelection] = useState<Selection>(() => new Set());
  const [expanded, setExpanded] = useState<Selection>(() => new Set());

  const visible = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return needle === "" ? rows : rows.filter((r) => matches(r, needle));
  }, [rows, query]);

  // Prune during render rather than in an effect, so that no render, and no click, ever sees
  // a selection that holds a row which is not shown.
  const [prunedFor, setPrunedFor] = useState(visible);
  let sel = selection;
  if (prunedFor !== visible) {
    sel = prune(selection, visible);
    setPrunedFor(visible);
    setSelection(sel);
  }

  const shown = useMemo(() => {
    const sign = direction === "asc" ? 1 : -1;
    return [...visible].sort(
      (a, b) => sign * compareBy(sortKey, a, b) || a.title.localeCompare(b.title) || a.key.localeCompare(b.key),
    );
  }, [visible, sortKey, direction]);

  const selected = selectedRows(sel, shown);
  const allSelected = shown.length > 0 && selected.length === shown.length;
  const headerState = allSelected ? true : selected.length > 0 ? "indeterminate" : false;
  const clear = () => setSelection(new Set());
  const toggleAll = () => setSelection(allSelected ? new Set() : selectAll(shown));
  const toggleRow = (key: string) => setSelection(toggle(sel, key));
  const toggleExpanded = (key: string) => setExpanded((e) => toggle(e, key));

  if (rows.length === 0) return <p className="py-8 text-center text-sm text-muted-foreground">{emptyText}</p>;

  const rowBox = (row: Candidate, className?: string) => (
    <Checkbox
      aria-label={`Select ${row.title}`}
      checked={sel.has(row.key)}
      onCheckedChange={() => toggleRow(row.key)}
      className={className}
    />
  );
  const headerBox = (
    <Checkbox aria-label="Select all shown rows" checked={headerState} onCheckedChange={toggleAll} disabled={shown.length === 0} />
  );
  const expandButton = (row: Candidate) => (
    <Button
      type="button"
      variant="ghost"
      size="icon-sm"
      aria-label="Show torrents"
      aria-expanded={expanded.has(row.key)}
      onClick={() => toggleExpanded(row.key)}
    >
      <ChevronRight className={cn("transition-transform", expanded.has(row.key) && "rotate-90")} />
    </Button>
  );

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <Input
          type="search"
          aria-label="Filter"
          placeholder="Filter"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          className="w-full sm:max-w-xs"
        />
        <Select value={sortKey} onValueChange={(v) => setSortKey(v as SortKey)}>
          <SelectTrigger aria-label="Sort by">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {sortKeys.map((k) => (
              <SelectItem key={k.id} value={k.id}>{k.label}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button type="button" variant="outline" onClick={() => setDirection((d) => (d === "asc" ? "desc" : "asc"))}>
          {direction === "asc" ? <ArrowUpNarrowWide /> : <ArrowDownWideNarrow />}
          {direction === "asc" ? "Ascending" : "Descending"}
        </Button>
      </div>

      {shown.length === 0 ? (
        <p className="py-8 text-center text-sm text-muted-foreground">No rows match the filter.</p>
      ) : wide ? (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-8">{headerBox}</TableHead>
              {columns.map((c) => (
                <TableHead key={c.id} className={c.className}>{c.header}</TableHead>
              ))}
              {renderExpanded && <TableHead className="w-10"><span className="sr-only">Details</span></TableHead>}
            </TableRow>
          </TableHeader>
          <TableBody>
            {shown.map((row) => (
              <Fragment key={row.key}>
                <TableRow data-state={sel.has(row.key) ? "selected" : undefined}>
                  <TableCell>{rowBox(row)}</TableCell>
                  {columns.map((c) => (
                    <TableCell key={c.id} className={c.className}>{c.cell(row)}</TableCell>
                  ))}
                  {renderExpanded && <TableCell>{expandButton(row)}</TableCell>}
                </TableRow>
                {renderExpanded && expanded.has(row.key) && (
                  <TableRow>
                    <TableCell colSpan={columns.length + 2} className="bg-muted/30">{renderExpanded(row)}</TableCell>
                  </TableRow>
                )}
              </Fragment>
            ))}
          </TableBody>
        </Table>
      ) : (
        <>
          <label className="flex items-center gap-3 px-4 text-sm text-muted-foreground">
            {headerBox}
            <span aria-hidden="true">Select all</span>
          </label>
          <ul className="flex flex-col gap-2">
            {shown.map((row) => (
              <li key={row.key}>
                <Card className="gap-0 py-3" data-state={sel.has(row.key) ? "selected" : undefined}>
                  <div className="flex items-start gap-3 px-4">
                    {rowBox(row, "mt-1")}
                    <div className="min-w-0 flex-1">{renderCard(row)}</div>
                    {renderExpanded && expandButton(row)}
                  </div>
                  {renderExpanded && expanded.has(row.key) && (
                    <div className="mt-3 border-t px-4 pt-3">{renderExpanded(row)}</div>
                  )}
                </Card>
              </li>
            ))}
          </ul>
        </>
      )}

      {selected.length > 0 && (
        <BulkBar count={selected.length} size={totalSize(selected)} onClear={clear}>
          {actions(selected, clear)}
        </BulkBar>
      )}
    </div>
  );
}
