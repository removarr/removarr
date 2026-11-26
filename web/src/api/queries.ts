import { useEffect, useRef } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, rowsBody } from "./client";
import type { CandidatesResponse, ExecuteResult, ListKind, Plan, Status } from "./types";

// The query keys. A list's candidates are ["candidates", list, ignored].
export const keys = {
  status: ["status"] as const,
  candidates: ["candidates"] as const,
  actions: ["actions"] as const,
  settings: ["settings"] as const,
  users: ["users"] as const,
};

// useStatus polls every 2 seconds while a scan runs and every 15 seconds otherwise.
export function useStatus() {
  return useQuery({
    queryKey: keys.status,
    queryFn: () => api<Status>("/api/status"),
    refetchInterval: (q) => (q.state.data?.scan.running ? 2000 : 15000),
  });
}

// useCandidates loads one list. ignored selects the ignored view (spec 5.2).
export function useCandidates(list: ListKind, ignored: boolean) {
  return useQuery({
    queryKey: [...keys.candidates, list, ignored] as const,
    queryFn: () => api<CandidatesResponse>(`/api/candidates?list=${list}&ignored=${ignored}`),
  });
}

// useScanFinishedRefresh reloads the lists and the status when a scan goes from running to
// finished, so the page shows the new lists without a manual reload.
export function useScanFinishedRefresh(running: boolean | undefined) {
  const qc = useQueryClient();
  const was = useRef(running);
  useEffect(() => {
    if (was.current === true && running === false) {
      qc.invalidateQueries({ queryKey: keys.candidates });
      qc.invalidateQueries({ queryKey: keys.status });
    }
    was.current = running;
  }, [running, qc]);
}

// useStartScan starts a scan. It is never retried on its own: a 409 means another job runs.
export function useStartScan() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api("/api/scans", { method: "POST", body: {} }),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.status }),
    // A refusal usually means a job runs: read the status once (not a retry of the start).
    onError: () => qc.invalidateQueries({ queryKey: keys.status }),
  });
}

type RowRefs = { list: ListKind; key: string }[];

function useIgnoreMutation(method: "POST" | "DELETE") {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (rows: RowRefs) => api("/api/ignores", { method, body: rowsBody(rows) }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: keys.candidates });
      qc.invalidateQueries({ queryKey: keys.status });
    },
  });
}

// useIgnore ignores rows of the latest scan (spec 6.5).
export function useIgnore() {
  return useIgnoreMutation("POST");
}

// useUnignore removes the ignore of rows.
export function useUnignore() {
  return useIgnoreMutation("DELETE");
}

// useBuildPlan asks the server for the plan that deletes the rows (spec 6.8, rule 1; 6.10).
// Building reads every app. It is never retried on its own.
export function useBuildPlan() {
  return useMutation({
    mutationFn: (rows: RowRefs) => api<Plan>("/api/plans", { method: "POST", body: rowsBody(rows) }),
  });
}

// useExecutePlan executes one confirmed plan (spec 6.11). It is never retried on its own: a
// plan runs once, and a second request would be refused anyway.
export function useExecutePlan() {
  return useMutation({
    mutationFn: (id: string) =>
      api<ExecuteResult>(`/api/plans/${encodeURIComponent(id)}/execute`, { method: "POST", body: {} }),
  });
}

// useRefreshAfterDelete returns a function that reloads what an execution changes: the lists,
// the status and the history.
export function useRefreshAfterDelete() {
  const qc = useQueryClient();
  return () => {
    qc.invalidateQueries({ queryKey: keys.candidates });
    qc.invalidateQueries({ queryKey: keys.status });
    qc.invalidateQueries({ queryKey: keys.actions });
  };
}
