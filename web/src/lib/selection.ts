import type { Candidate } from "@/api/types";

// The selection rules of a list (spec 5.3). A selection holds row keys. It only ever holds
// keys of rows the user can see: the list prunes it whenever the filter or the rows change,
// so that a bulk action never names a row that is hidden or gone.
export type Selection = ReadonlySet<string>;

// rowLabel names a row, or a reference to one, wherever the UI names it. Season rows of one
// series carry the series title, so a row with a season adds it; otherwise two rows would
// share one name.
export function rowLabel(row: { title: string; season?: number }): string {
  return row.season === undefined ? row.title : `${row.title}, season ${row.season}`;
}

// selectAll selects every visible row, and nothing else.
export function selectAll(visible: Candidate[]): Selection {
  return new Set(visible.map((r) => r.key));
}

// toggle selects a row that is not selected and deselects one that is.
export function toggle(sel: Selection, key: string): Selection {
  const next = new Set(sel);
  if (next.has(key)) next.delete(key);
  else next.add(key);
  return next;
}

// prune keeps only the keys of visible rows. It returns the same selection when it drops
// nothing, so that pruning an unchanged selection does not make React render again.
export function prune(sel: Selection, visible: Candidate[]): Selection {
  const keys = new Set(visible.map((r) => r.key));
  const next = new Set([...sel].filter((k) => keys.has(k)));
  return next.size === sel.size ? sel : next;
}

// selectedRows returns the selected rows among the visible ones, in their order.
export function selectedRows(sel: Selection, visible: Candidate[]): Candidate[] {
  return visible.filter((r) => sel.has(r.key));
}

// totalSize adds up the size of the rows.
export function totalSize(rows: Candidate[]): number {
  return rows.reduce((sum, r) => sum + r.size, 0);
}
