import type { Action, ExecuteResult, Outcome, StepResult } from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { formatSize } from "@/lib/format";
import { describeStep, refLabel } from "./describe";

const OUTCOME: Record<Outcome, string> = { done: "Done", failed: "Failed", "dry-run": "Dry run" };

function resultText(s: StepResult): string {
  switch (s.result) {
    case "done":
      return "done";
    case "failed":
      return s.error ? `failed: ${s.error}` : "failed";
    case "skipped":
      return "skipped";
    case "gone":
      return "already gone";
    case "dry-run":
      return "dry run";
  }
}

function stepText(s: StepResult, action: Action): string {
  if (s.manager) {
    const row = action.rows.find((r) => r.key === s.manager!.rowKey);
    return describeStep(s.manager, row?.title ?? s.manager.rowKey);
  }
  return s.torrent ? s.torrent.name : "Unknown step";
}

// OutcomeView shows what an execution did (spec 7.4, step 6): per item its rows, its outcome
// and every step with its result, failures with the app's error.
export function OutcomeView({ result }: { result: ExecuteResult }) {
  const n = (o: Outcome) => result.actions.filter((a) => a.outcome === o).length;
  const dry = n("dry-run");
  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm font-medium tabular-nums">
        {`${n("done")} done · ${n("failed")} failed${dry > 0 ? ` · ${dry} dry run` : ""}`}
      </p>
      <ul className="flex flex-col gap-4">
        {result.actions.map((a) => (
          <li key={a.id} className="flex flex-col gap-1 rounded-md border px-3 py-2">
            <div className="flex items-start justify-between gap-2">
              <ul className="flex flex-col text-sm">
                {a.rows.map((r) => (
                  <li key={`${r.list}:${r.key}`}>
                    <span className="font-medium">{refLabel(r)}</span>{" "}
                    <span className="text-muted-foreground tabular-nums">{formatSize(r.size)}</span>
                    {r.added && <span className="text-xs text-muted-foreground"> (added)</span>}
                  </li>
                ))}
              </ul>
              <Badge variant={a.outcome === "failed" ? "destructive" : a.outcome === "done" ? "default" : "secondary"}>
                {OUTCOME[a.outcome]}
              </Badge>
            </div>
            <ol className="flex list-decimal flex-col gap-0.5 pl-5 text-sm">
              {a.steps.map((s, i) => (
                <li key={i} className={s.result === "failed" ? "text-destructive" : undefined}>
                  <span className="break-all">{stepText(s, a)}</span>
                  {" — "}
                  <span>{resultText(s)}</span>
                  {s.torrent && s.keepData && (
                    <>
                      {", "}
                      <span>kept its data</span>
                    </>
                  )}
                </li>
              ))}
            </ol>
          </li>
        ))}
      </ul>
    </div>
  );
}
