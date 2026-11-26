import type { ReactNode, Ref } from "react";
import { Button } from "@/components/ui/button";
import { formatSize } from "@/lib/format";

export interface BulkBarProps { count: number; size: number; children: ReactNode; onClear: () => void; ref?: Ref<HTMLDivElement> }

// BulkBar is fixed to the bottom of the window while rows are selected. It shows how many and
// their size (spec 6.9 calls it size, not space freed) and holds the list's bulk actions.
//
// It spans the content area: its left edge is --content-left, which the shell sets to the
// width of the side panel when the panel is shown beside the content. It sits above the list
// and below the drawer and the dialogs (z-50).
export function BulkBar({ count, size, children, onClear, ref }: BulkBarProps) {
  return (
    <div
      ref={ref}
      role="region"
      aria-label="Selected rows"
      className="fixed right-0 bottom-0 left-[var(--content-left,0px)] z-30 border-t bg-background/95 px-4 py-2 pb-[max(0.5rem,env(safe-area-inset-bottom))] backdrop-blur"
    >
      <div className="mx-auto flex max-w-7xl flex-wrap items-center gap-2">
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
    </div>
  );
}
