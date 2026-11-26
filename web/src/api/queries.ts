import { useEffect, useRef } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import type { CandidatesResponse, ListKind, Status } from "./types";

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
    // A refusal usually means a job runs: read the status once (not a retry of the start).
  });
}
