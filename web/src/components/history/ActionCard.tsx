import type { Action } from "@/api/types";
import { OutcomeBadge } from "@/components/delete/OutcomeBadge";
import { StepLine } from "@/components/delete/StepLine";
import { Badge } from "@/components/ui/badge";
import { formatDateTime, formatSize } from "@/lib/format";
import { rowLabel } from "@/lib/selection";

// ActionCard is one executed deletion: when, its rows, the total size, the outcome and
// every step with its result (spec 5.5).
export function ActionCard({ action: a }: { action: Action }) {
  const when = formatDateTime(a.at);
  return (
    <li aria-label={`Deletion ${when}`} className="flex flex-col gap-2 rounded-md border px-3 py-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="text-sm font-medium">{when}</span>
        <span className="flex items-center gap-2">
          <span className="text-sm text-muted-foreground tabular-nums">{`size ${formatSize(a.size)}`}</span>
          {a.dryRun && a.outcome !== "dry-run" && <Badge variant="outline">Dry run</Badge>}
          <OutcomeBadge outcome={a.outcome} />
        </span>
      </div>
      <ul className="flex flex-col text-sm">
        {a.rows.map((r) => (
          <li key={`${r.list}:${r.key}`}>
            <span className="font-medium">{rowLabel(r)}</span>{" "}
            <span className="text-muted-foreground tabular-nums">{formatSize(r.size)}</span>
            {r.added && <span className="text-xs text-muted-foreground"> (added)</span>}
          </li>
        ))}
      </ul>
      <ol className="flex list-decimal flex-col gap-0.5 pl-5 text-sm">
        {a.steps.map((s, i) => (
          <StepLine key={i} step={s} rows={a.rows} />
        ))}
      </ol>
    </li>
  );
}
