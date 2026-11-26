import type { ReactNode } from "react";
import type { Candidate } from "@/api/types";
import type { ColumnSpec } from "@/components/list/CandidateList";
import { formatDate } from "@/lib/format";

function ignoredNote(r: Candidate): ReactNode {
  return (
    <div className="flex flex-col text-xs text-muted-foreground">
      <span>Ignored {formatDate(r.ignoredAt)}</span>
      {r.waiting && <span>{r.waitReason ? `Waiting: ${r.waitReason}` : "Waiting"}</span>}
    </div>
  );
}

function notInScan(r: Candidate): ReactNode {
  return (
    <div className="flex flex-col gap-1">
      <span className="font-medium break-all">{r.title}</span>
      <span className="text-xs text-muted-foreground">Not in the latest scan</span>
    </div>
  );
}

// ignoredColumns adds the ignore to each row of the ignored view (spec 6.5). A row that is not
// in the latest scan has only its key and title, so it shows the title and that, nothing else.
export function ignoredColumns(columns: ColumnSpec[]): ColumnSpec[] {
  const [first, ...rest] = columns;
  return [
    { ...first, cell: (r) => (r.notInScan ? notInScan(r) : first.cell(r)) },
    ...rest.map((c) => ({ ...c, cell: (r: Candidate) => (r.notInScan ? null : c.cell(r)) })),
    { id: "ignored", header: "Ignored", cell: ignoredNote, className: "whitespace-nowrap" },
  ];
}

export function ignoredCard(card: (r: Candidate) => ReactNode): (r: Candidate) => ReactNode {
  return (r) => (
    <div className="flex flex-col gap-1">
      {r.notInScan ? notInScan(r) : card(r)}
      {ignoredNote(r)}
    </div>
  );
}
