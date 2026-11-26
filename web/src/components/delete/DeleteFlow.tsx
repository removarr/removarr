import { useRef, useState } from "react";
import { ApiError } from "@/api/client";
import { useBuildPlan, useExecutePlan, useRefreshAfterDelete, useStatus } from "@/api/queries";
import type { Candidate, ExecuteResult, ListKind, Plan } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { OutcomeView } from "./OutcomeView";
import { PlanView } from "./PlanView";

type RowKey = { list: ListKind; key: string };

// The states of a deletion. Only "built" offers to execute, and it executes the plan it holds:
// nothing runs without a press for that very plan (spec 6.8, rule 1).
type State =
  | { kind: "closed" }
  | { kind: "building" }
  | { kind: "built"; plan: Plan }
  | { kind: "executing" }
  | { kind: "done"; result: ExecuteResult }
  | { kind: "expired"; message: string }
  | { kind: "error"; message: string; executed: boolean };

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

export type DeleteFlowState = ReturnType<typeof useDeleteFlow>;

// useDeleteFlow holds the deletion's state. It lives in the page, above the list, so that the
// dialog stays open when the list reloads under it while a plan runs and its rows go away.
export function useDeleteFlow() {
  const [state, setState] = useState<State>({ kind: "closed" });
  const rows = useRef<RowKey[]>([]);
  const clear = useRef<() => void>(() => {});
  // run numbers each request; an answer applies only while it is the latest one.
  const run = useRef(0);
  // confirmed holds the plan already sent for execution, so a second press cannot send it again.
  const confirmed = useRef<Plan | null>(null);
  const build = useBuildPlan();
  const execute = useExecutePlan();
  const refresh = useRefreshAfterDelete();

  const buildPlan = () => {
    const n = ++run.current;
    setState({ kind: "building" });
    build.mutateAsync(rows.current).then(
      (plan) => n === run.current && setState({ kind: "built", plan }),
      (e) => n === run.current && setState({ kind: "error", message: message(e), executed: false }),
    );
  };

  return {
    state,
    // start builds the plan for the selected rows. clearSelection is called when the
    // deletion has run.
    start(selected: Candidate[], clearSelection: () => void) {
      if (state.kind !== "closed") return;
      rows.current = selected.map(({ list, key }) => ({ list, key }));
      clear.current = clearSelection;
      buildPlan();
    },
    // rebuild builds a new plan from the same rows; it needs a new confirmation of its own.
    rebuild() {
      if (state.kind === "expired") buildPlan();
    },
    confirm(plan: Plan) {
      if (state.kind !== "built" || state.plan !== plan || confirmed.current === plan) return;
      confirmed.current = plan;
      const n = ++run.current;
      setState({ kind: "executing" });
      execute.mutateAsync(plan.id).then(
        (result) => n === run.current && setState({ kind: "done", result }),
        (e) => {
          if (n !== run.current) return;
          if (e instanceof ApiError && (e.status === 410 || e.status === 404)) setState({ kind: "expired", message: e.message });
          // A 409 means another job runs and nothing of this plan did.
          else setState({ kind: "error", message: message(e), executed: !(e instanceof ApiError && e.status === 409) });
        },
      );
    },
    close() {
      if (state.kind === "building" || state.kind === "executing") return;
      run.current++;
      if (state.kind === "done") {
        refresh();
        clear.current();
      } else if (state.kind === "error" && state.executed) {
        // The execution may have run in part: read what it changed.
        refresh();
      }
      setState({ kind: "closed" });
    },
  };
}

const TITLES: Record<Exclude<State["kind"], "closed">, string> = {
  building: "Building the plan",
  built: "Confirm the deletion",
  executing: "Deleting",
  done: "Outcome",
  expired: "The plan cannot run",
  error: "Error",
};

// DeleteFlow is the dialog of a deletion: the plan for confirmation, then the outcome. It
// cannot be closed while it waits on the server, so nothing suggests the request stopped.
export function DeleteFlow({ flow }: { flow: DeleteFlowState }) {
  const status = useStatus();
  const dryRun = status.data?.dryRun ?? false;
  const { state } = flow;
  const busy = state.kind === "building" || state.kind === "executing";
  const keep = (e: Event) => {
    if (busy) e.preventDefault();
  };

  let body;
  let buttons;
  switch (state.kind) {
    case "closed":
      return null;
    case "building":
      body = <p className="text-sm">Building the plan… This reads every app and takes about as long as a scan.</p>;
      break;
    case "built": {
      const { plan } = state;
      const runnable = (plan.items ?? []).length > 0;
      body = <PlanView plan={plan} dryRun={dryRun} />;
      buttons = runnable ? (
        <>
          <Button type="button" variant="outline" onClick={flow.close}>Cancel</Button>
          <Button type="button" variant="destructive" onClick={() => flow.confirm(plan)}>
            {dryRun ? "Run as dry run" : "Delete"}
          </Button>
        </>
      ) : (
        <Button type="button" variant="outline" onClick={flow.close}>Close</Button>
      );
      break;
    }
    case "executing":
      body = (
        <p className="text-sm">
          Deleting… This continues on the server if you close the page; the History page shows the result.
        </p>
      );
      break;
    case "done":
      body = <OutcomeView result={state.result} />;
      buttons = <Button type="button" onClick={flow.close}>Close</Button>;
      break;
    case "expired":
      body = <p role="alert" className="text-sm">{state.message}</p>;
      buttons = (
        <>
          <Button type="button" variant="outline" onClick={flow.close}>Cancel</Button>
          <Button type="button" onClick={flow.rebuild}>Build the plan again</Button>
        </>
      );
      break;
    case "error":
      body = (
        <div className="flex flex-col gap-2 text-sm">
          <p role="alert" className="text-destructive">{state.message}</p>
          {state.executed && <p>The plan may have run in part on the server; the History page shows what it did.</p>}
        </div>
      );
      buttons = <Button type="button" variant="outline" onClick={flow.close}>Close</Button>;
      break;
  }

  return (
    <Dialog open onOpenChange={(open) => !open && flow.close()}>
      <DialogContent
        showCloseButton={false}
        onEscapeKeyDown={keep}
        onPointerDownOutside={keep}
        onInteractOutside={keep}
        className="max-h-[90vh] overflow-y-auto sm:max-w-2xl"
      >
        <DialogHeader>
          <DialogTitle>{TITLES[state.kind]}</DialogTitle>
          <DialogDescription className="sr-only">Delete the selected rows</DialogDescription>
        </DialogHeader>
        {body}
        {buttons && <DialogFooter>{buttons}</DialogFooter>}
      </DialogContent>
    </Dialog>
  );
}
