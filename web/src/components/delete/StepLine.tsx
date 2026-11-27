import type { ActionRow, StepResult } from "@/api/types";
import { rowLabel } from "@/lib/selection";
import { describeStep } from "./describe";

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

function stepText(s: StepResult, rows: ActionRow[]): string {
  if (s.manager) {
    const row = rows.find((r) => r.key === s.manager!.rowKey);
    return describeStep(s.manager, row ? rowLabel(row) : s.manager.rowKey);
  }
  return s.torrent ? s.torrent.name : "Unknown step";
}

// StepLine is one step of an executed deletion with its result, the app's error for a
// failure and "kept its data" for a torrent that was removed without its files.
export function StepLine({ step, rows }: { step: StepResult; rows: ActionRow[] }) {
  return (
    <li className={step.result === "failed" ? "text-destructive" : undefined}>
      <span className="break-all">{stepText(step, rows)}</span>
      {" — "}
      <span>{resultText(step)}</span>
      {step.torrent && step.keepData && (
        <>
          {", "}
          <span>kept its data</span>
        </>
      )}
    </li>
  );
}
