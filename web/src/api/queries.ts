import { useEffect, useRef } from "react";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, rowsBody } from "./client";
import type {
  ActionsPage, AppName, CandidatesResponse, ExecuteResult, ListKind, MediaUser, Plan, Rules, Settings, Status,
  TestResult,
} from "./types";

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

// useActions pages through the history, newest first. The server's `next` is the id to pass
// as `before`; a page without it is the last (spec 5.5).
export function useActions() {
  return useInfiniteQuery({
    queryKey: keys.actions,
    initialPageParam: undefined as number | undefined,
    queryFn: ({ pageParam }) =>
      api<ActionsPage>(`/api/actions?limit=50${pageParam === undefined ? "" : `&before=${pageParam}`}`),
    getNextPageParam: (last) => last.next,
  });
}

// useScanFinishedRefresh reloads the lists and the status when a scan finishes, so the page
// shows the new lists without a manual reload. A scan has finished when the status goes from
// running to not running, or when the last scan changes: a quick scan can be over before the
// status is read again, so the status never shows it running. lastScanId is undefined until
// the status is known and null while there is no scan.
export function useScanFinishedRefresh(running: boolean | undefined, lastScanId: number | null | undefined) {
  const qc = useQueryClient();
  const was = useRef(running);
  const wasLast = useRef(lastScanId);
  useEffect(() => {
    const stopped = was.current === true && running === false;
    const newScan = wasLast.current !== undefined && lastScanId !== undefined && lastScanId !== wasLast.current;
    if (stopped || newScan) {
      qc.invalidateQueries({ queryKey: keys.candidates });
      qc.invalidateQueries({ queryKey: keys.status });
    }
    was.current = running;
    wasLast.current = lastScanId;
  }, [running, lastScanId, qc]);
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
// plan runs once, and a second request would be refused anyway. When it settles, whatever the
// answer, it reloads what an execution changes; this runs also when the page that started it
// is gone, so the lists never show rows that were deleted.
export function useExecutePlan() {
  const refresh = useRefreshAfterDelete();
  return useMutation({
    mutationFn: (id: string) =>
      api<ExecuteResult>(`/api/plans/${encodeURIComponent(id)}/execute`, { method: "POST", body: {} }),
    onSettled: refresh,
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

// useSettings loads the rules and where each connection value comes from (spec 5.6). A secret
// never comes back.
export function useSettings() {
  return useQuery({ queryKey: keys.settings, queryFn: () => api<Settings>("/api/settings") });
}

// SettingsChange is the body of PUT /api/settings: only the fields that changed. A
// connection field that is named here is changed, and null clears it (spec 9.2).
export interface SettingsChange {
  rules?: Partial<Rules>;
  connections?: Partial<Record<AppName, Record<string, string | null>>>;
}

// useSaveSettings saves the changed fields. The answer is the new settings; the status is
// reloaded because the stale notice depends on them, and the users because Jellyfin may now
// be connected.
export function useSaveSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (change: SettingsChange) => api<Settings>("/api/settings", { method: "PUT", body: change }),
    onSuccess: (data) => {
      qc.setQueryData(keys.settings, data);
      qc.invalidateQueries({ queryKey: keys.status });
      qc.invalidateQueries({ queryKey: keys.users });
    },
  });
}

// useUsers lists the media server's users. It only asks when Jellyfin is configured.
export function useUsers(enabled: boolean) {
  return useQuery({ queryKey: keys.users, queryFn: () => api<MediaUser[]>("/api/media-server/users"), enabled });
}

// useTestConnection tests the SAVED values of one app.
export function useTestConnection(app: AppName) {
  return useMutation({
    mutationFn: () => api<TestResult>(`/api/connections/${app}/test`, { method: "POST", body: {} }),
  });
}
