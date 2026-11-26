import type { ReactNode } from "react";
import { Button } from "@/components/ui/button";
import { formatSize } from "@/lib/format";

export interface BulkBarProps { count: number; size: number; children: ReactNode; onClear: () => void }

// BulkBar stays at the bottom of the screen while rows are selected. It shows how many and
// their size (spec 6.9 calls it size, not space freed) and holds the list's bulk actions.
export function BulkBar({ count, size, children, onClear }: BulkBarProps) {
  return (
    <div
      role="region"
      aria-label="Selected rows"
      className="sticky bottom-0 z-10 flex flex-wrap items-center gap-2 border-t bg-background/95 px-3 py-2 pb-[max(0.5rem,env(safe-area-inset-bottom))] backdrop-blur"
    >
      <span className="text-sm font-medium tabular-nums">
        {count} selected · <span title="Total size of the selected rows">{formatSize(size)}</span>
      </span>
      <div className="ml-auto flex flex-wrap items-center gap-2">
        {children}
        <Button type="button" variant="ghost" size="sm" onClick={onClear}>
          Clear selection
        </Button>
      </div>
    </div>
  );
}
