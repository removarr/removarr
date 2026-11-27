import type { Outcome } from "@/api/types";
import { Badge } from "@/components/ui/badge";

const LABEL: Record<Outcome, string> = { done: "Done", failed: "Failed", "dry-run": "Dry run" };
const VARIANT: Record<Outcome, "default" | "destructive" | "secondary"> = {
  done: "default",
  failed: "destructive",
  "dry-run": "secondary",
};

// OutcomeBadge shows how an executed item ended, in the outcome view and in the history.
export function OutcomeBadge({ outcome }: { outcome: Outcome }) {
  return <Badge variant={VARIANT[outcome]}>{LABEL[outcome]}</Badge>;
}
