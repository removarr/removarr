// The JSON of removarr's HTTP API. Each type mirrors a Go type; the comment names it.

export type ListKind = "movies" | "series" | "torrents"; // domain.ListKind
export type AppName = "jellyfin" | "sonarr" | "radarr" | "qbittorrent"; // config.Apps

// domain.RowRef
export interface RowRef { list: ListKind; key: string; title: string; season?: number }

// domain.TorrentInfo
export interface TorrentInfo {
  id: string; name: string; size: number; state: string; category: string; tracker: string;
  ratio: number | null; seedingDays: number | null; addedAt: string; belowSeedMin: boolean;
  outsideLibrary?: number; // bytes not in the library; only set when 50 MB or more (spec 6.7)
}

// domain.Candidate
export interface Candidate {
  list: ListKind; key: string; title: string; year?: number; size: number;
  scope?: "series" | "season"; season?: number; stillAiring?: boolean;
  watchedBy?: string[]; watchedAt?: string; reason?: "orphan" | "unlinked";
  torrents: TorrentInfo[] | null; belowSeedMin: boolean; tiedWith?: RowRef[];
  waiting?: boolean; waitReason?: string; ignoredAt?: string; notInScan?: boolean; merged?: boolean;
}

// api.candidatesResponse
export interface CandidatesResponse { list: ListKind; scannedAt: string | null; error?: string; candidates: Candidate[] | null }

// domain.ListSummary, domain.RootFolder, domain.ScanSummary, store.ScanRecord, scanner.Status
export interface ListSummary { candidates: number; protected: number; unmatched: number; waiting: number; error?: string }
export interface RootFolder { path: string; freeBytes: number }
export interface ScanSummary { lists: Partial<Record<ListKind, ListSummary>>; rootFolders: RootFolder[] | null; settingsFingerprint?: string }
export interface ScanRecord { id: number; startedAt: string; finishedAt: string | null; status: string; summary: ScanSummary }
export interface ScanStatus { running: boolean; phase?: string; startedAt?: string; lastError?: string }
// api.statusResponse
export interface Status {
  dryRun: boolean; connections: Record<AppName, { configured: boolean }>;
  scan: ScanStatus; lastScan: ScanRecord | null; stale: boolean;
}
// api.scanResponse
export interface ScanCurrent { status: ScanStatus; last: ScanRecord | null }

// domain.ManagerStep, domain.PlanRow, domain.PlanTorrent, domain.KeptTorrent, domain.PlanItem,
// domain.SkippedRow, domain.Plan
export type StepAction = "remove-movie" | "remove-series" | "delete-episode-files" | "unmonitor-episodes" | "unmonitor-seasons";
export interface ManagerStep {
  manager: "movies" | "series"; action: StepAction; rowKey: string; itemId: number;
  fileIds?: number[]; episodeIds?: number[]; seasons?: number[]; addExclusion?: boolean;
}
export interface PlanRow { row: Candidate; added?: boolean; addedWith?: RowRef; shares?: string[]; notInScan?: boolean }
export interface PlanTorrent { torrent: TorrentInfo; keepData?: boolean; linkedBySize?: boolean }
export interface KeptTorrent { torrent: TorrentInfo; reason: "queued" | "backs-library" }
export interface PlanItem { rows: PlanRow[]; steps: ManagerStep[] | null; torrents: PlanTorrent[] | null; kept: KeptTorrent[] | null }
export interface SkippedRow { row: RowRef; reason: string }
export interface Plan { id: string; createdAt: string; expiresAt: string; items: PlanItem[] | null; skipped: SkippedRow[] | null }

// domain.Action, domain.ActionRow, domain.StepResult, executor.Result, the body of GET /api/actions
export type Outcome = "done" | "failed" | "dry-run";
export type StepResultKind = "done" | "failed" | "skipped" | "gone" | "dry-run";
export interface ActionRow { list: ListKind; key: string; title: string; season?: number; size: number; added?: boolean }
export interface StepResult { manager?: ManagerStep; torrent?: TorrentInfo; keepData?: boolean; result: StepResultKind; error?: string }
export interface Action { id: number; planId: string; at: string; dryRun: boolean; outcome: Outcome; size: number; rows: ActionRow[]; steps: StepResult[] }
export interface ExecuteResult { planId: string; dryRun: boolean; actions: Action[] }
export interface ActionsPage { actions: Action[]; next?: number }

// config.Source, config.FieldView, domain.Rules, api.settingsResponse, domain.User,
// api.testResponse
export type Source = "ui" | "env" | "none";
export interface FieldView { value?: string; set: boolean; source: Source }
export interface Rules {
  countingUsers: string[]; watchedMode: "all" | "any"; graceDays: number; seriesUnit: "series" | "season";
  protectFavorites: boolean; protectionTag: string; minRatio: number; minSeedDays: number;
  excludedCategories: string[]; excludedTags: string[]; addExclusionOnDelete: boolean;
}
export interface Settings { rules: Rules; connections: Record<AppName, Record<string, FieldView>>; dryRun: boolean }
export interface MediaUser { id: string; name: string }
export interface TestResult { ok: boolean; detail?: string; error?: string }
