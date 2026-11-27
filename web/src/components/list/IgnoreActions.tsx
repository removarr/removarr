import type { Candidate } from "@/api/types";
import { useIgnore, useUnignore } from "@/api/queries";
import { Button } from "@/components/ui/button";

// IgnoreActions is the ignore button of the bulk bar: Ignore in the normal view, Un-ignore
// in the ignored view. On success the list reloads and the selection is cleared; on failure
// the server's message shows and the selection stays.
export function IgnoreActions({ selected, clear, ignoredView }: { selected: Candidate[]; clear: () => void; ignoredView: boolean }) {
  const ignore = useIgnore();
  const unignore = useUnignore();
  const m = ignoredView ? unignore : ignore;
  return (
    <>
      {m.error && <span role="alert" className="text-sm text-destructive">{m.error.message}</span>}
      <Button
        type="button"
        variant="outline"
        size="sm"
        disabled={m.isPending}
        onClick={() => m.mutate(selected, { onSuccess: clear })}
      >
        {ignoredView ? "Un-ignore" : "Ignore"}
      </Button>
    </>
  );
}
