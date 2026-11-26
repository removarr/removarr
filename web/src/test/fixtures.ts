import type { Candidate, ExecuteResult, Plan, Status, TorrentInfo } from "@/api/types";

// Made-up data for the tests of the delete flow. No title or name here comes from a library.

const GiB = 1024 ** 3;

const c = { configured: true };

// statusFixture is a status with every app configured, no scan running and dry run off.
export const statusFixture: Status = {
  dryRun: false,
  connections: { jellyfin: c, sonarr: c, radarr: c, qbittorrent: c },
  scan: { running: false },
  lastScan: null,
  stale: false,
};

// SCAN_FINISHED is when the scan of scannedStatus finished.
export const SCAN_FINISHED = "2026-11-20T10:30:00Z";

// scannedStatus is a status after one finished scan of three lists, for the header and the
// scan details.
export function scannedStatus(over: Partial<Status> = {}): Status {
  return {
    ...statusFixture,
    lastScan: {
      id: 1, startedAt: "2026-11-20T10:29:00Z", finishedAt: SCAN_FINISHED, status: "done",
      summary: {
        lists: {
          movies: { candidates: 12, protected: 1, unmatched: 2, waiting: 3 },
          series: { candidates: 4, protected: 2, unmatched: 0, waiting: 1 },
          torrents: { candidates: 9, protected: 0, unmatched: 1, waiting: 0 },
        },
        rootFolders: [{ path: "/media/movies", freeBytes: 2 * 1024 ** 3 }],
      },
    },
    ...over,
  };
}

export function torrentFixture(over: Partial<TorrentInfo> & { id: string; name: string }): TorrentInfo {
  return {
    size: GiB, state: "stalledUP", category: "movies", tracker: "tracker.example", ratio: 1.5, seedingDays: 30,
    addedAt: "2026-01-01T00:00:00Z", belowSeedMin: false, ...over,
  };
}

export function movieFixture(key: string, title: string, size: number): Candidate {
  return { list: "movies", key, title, size, torrents: [], belowSeedMin: false };
}

// planFixture covers every field the confirmation shows: a movie item with a selected row,
// two added rows (one sharing a torrent, one sharing a library file and not in the latest
// scan), removed torrents with each flag, a kept torrent of each reason, a season item with
// every Sonarr step, an orphan torrent item, and a skipped row.
export const planFixture: Plan = {
  id: "plan-1",
  createdAt: "2026-03-01T10:00:00Z",
  expiresAt: "2026-03-01T10:10:00Z",
  items: [
    {
      rows: [
        { row: movieFixture("tmdb:1", "Alpha", 4 * GiB) },
        {
          row: movieFixture("tmdb:3", "Beta", 2 * GiB),
          added: true,
          addedWith: { list: "movies", key: "tmdb:1", title: "Alpha" },
          shares: ["h1"],
        },
        {
          row: movieFixture("tmdb:4", "Delta", 1 * GiB),
          added: true,
          addedWith: { list: "movies", key: "tmdb:1", title: "Alpha" },
          shares: [],
          notInScan: true,
        },
      ],
      steps: [
        { manager: "movies", action: "remove-movie", rowKey: "tmdb:1", itemId: 11, addExclusion: true },
        { manager: "movies", action: "remove-movie", rowKey: "tmdb:3", itemId: 13 },
        { manager: "movies", action: "remove-movie", rowKey: "tmdb:4", itemId: 14 },
      ],
      torrents: [
        { torrent: torrentFixture({ id: "h1", name: "Alpha.Beta.Pack", size: 12 * GiB, belowSeedMin: true, outsideLibrary: 6 * GiB }) },
        { torrent: torrentFixture({ id: "h2", name: "Alpha.Remux", size: 3 * GiB }), keepData: true, linkedBySize: true },
      ],
      kept: [{ torrent: torrentFixture({ id: "h3", name: "Alpha.Queued" }), reason: "queued" }],
    },
    {
      rows: [
        {
          row: {
            list: "series", key: "tvdb:7:2", title: "Show", season: 2, scope: "season", size: 5 * GiB,
            torrents: [], belowSeedMin: false,
          },
        },
      ],
      steps: [
        { manager: "series", action: "unmonitor-episodes", rowKey: "tvdb:7:2", itemId: 7, episodeIds: [1, 2, 3] },
        { manager: "series", action: "unmonitor-seasons", rowKey: "tvdb:7:2", itemId: 7, seasons: [2] },
        { manager: "series", action: "delete-episode-files", rowKey: "tvdb:7:2", itemId: 7, fileIds: [5, 6, 7] },
      ],
      torrents: [],
      kept: [{ torrent: torrentFixture({ id: "h4", name: "Show.S02.Pack", category: "tv" }), reason: "backs-library" }],
    },
    {
      rows: [
        {
          row: {
            list: "torrents", key: "h5", title: "Orphan.Torrent", size: 1 * GiB, reason: "orphan",
            torrents: null, belowSeedMin: false,
          },
        },
      ],
      steps: null,
      torrents: [{ torrent: torrentFixture({ id: "h5", name: "Orphan.Torrent" }) }],
      kept: null,
    },
  ],
  skipped: [{ row: { list: "movies", key: "tmdb:2", title: "Gamma" }, reason: "It no longer qualifies." }],
};

// executeFixture is the answer to executing planFixture with one item done and one failed.
export const executeFixture: ExecuteResult = {
  planId: "plan-1",
  dryRun: false,
  actions: [
    {
      id: 1, planId: "plan-1", at: "2026-03-01T10:01:00Z", dryRun: false, outcome: "done", size: 7 * GiB,
      rows: [
        { list: "movies", key: "tmdb:1", title: "Alpha", size: 4 * GiB },
        { list: "movies", key: "tmdb:3", title: "Beta", size: 2 * GiB, added: true },
      ],
      steps: [
        { manager: { manager: "movies", action: "remove-movie", rowKey: "tmdb:1", itemId: 11, addExclusion: true }, result: "done" },
        { manager: { manager: "movies", action: "remove-movie", rowKey: "tmdb:3", itemId: 13 }, result: "done" },
        { torrent: torrentFixture({ id: "h1", name: "Alpha.Beta.Pack" }), keepData: false, result: "done" },
        { torrent: torrentFixture({ id: "h2", name: "Alpha.Remux" }), keepData: true, result: "gone" },
      ],
    },
    {
      id: 2, planId: "plan-1", at: "2026-03-01T10:02:00Z", dryRun: false, outcome: "failed", size: 5 * GiB,
      rows: [{ list: "series", key: "tvdb:7:2", title: "Show", season: 2, size: 5 * GiB }],
      steps: [
        { manager: { manager: "series", action: "unmonitor-episodes", rowKey: "tvdb:7:2", itemId: 7, episodeIds: [1, 2, 3] }, result: "done" },
        { manager: { manager: "series", action: "delete-episode-files", rowKey: "tvdb:7:2", itemId: 7, fileIds: [5, 6, 7] }, result: "failed", error: "Sonarr answered 500." },
        { torrent: torrentFixture({ id: "h6", name: "Show.S02.Extra" }), result: "skipped" },
      ],
    },
  ],
};
