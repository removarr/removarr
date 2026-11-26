import { useEffect, type ReactNode } from "react";
import { useScanFinishedRefresh, useStartScan, useStatus } from "@/api/queries";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";

// Header is the top bar's content: the lead (the menu button and the app's name) on the left,
// the dry run badge and the Scan button at the right end. What the last scan found is a strip
// of its own below the bar (ScanDetails).
export function Header({ lead }: { lead?: ReactNode }) {
  const { data: status } = useStatus();
  const start = useStartScan();
  useScanFinishedRefresh(status?.scan.running, status ? (status.lastScan?.id ?? null) : undefined);

  const running = status?.scan.running ?? false;
  // A refusal message is about the state it was shown in: drop it when that state changes.
  const { reset } = start;
  useEffect(() => reset(), [running, reset]);

  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3">
      {lead && <div className="flex items-center gap-2">{lead}</div>}

      {/* Scan is the last control, at the right end on every screen. What it says about itself
          (the phase, a refusal) goes below it, so it never moves. */}
      <div data-testid="scan-controls" className="ml-auto flex flex-col items-end gap-1">
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
