# removarr — design

- **Date:** 2026-11-18
- **Status:** approved by the owner on 2026-11-18
- **Scope:** version 1

## 1. Purpose

removarr helps the owner of a self-hosted media server reclaim disk space. It looks at what has been watched and at what the download client is still holding, and proposes things to delete. It never deletes on its own: every deletion is selected and confirmed by a person.

It is built for one concrete stack first (Jellyfin, Sonarr, Radarr, qBittorrent on a Raspberry Pi 5 running DietPi) and published as open source so others with a similar stack can use it.

What sets it apart from existing cleanup tools: it combines watched-media cleanup and torrent cleanup in one place, behind a review-and-approve UI, instead of deleting on a schedule.

## 2. Goals and non-goals

### Goals for v1

- Suggest watched movies for deletion.
- Suggest watched series, or watched seasons, for deletion.
- Suggest orphan torrents for deletion.
- Let the user delete or ignore suggestions in bulk, and review ignored items.
- Show enough information per suggestion (size, who watched, seeding state) to decide safely.
- Run as one Docker container, on arm64 and amd64, with a small footprint.
- Keep the media server, media manager and download client behind interfaces so other products can be added later.

### Non-goals for v1

These are deliberately left out. Section 14 lists them as later work.

- Automatic or scheduled deletion.
- Authentication.
- "Never watched" and "abandoned series" lists.
- Detecting orphan files on disk.
- Notifications.
- Jellyseerr integration.
- More than one instance of each app (for example a second, 4K Radarr).
- Plex, Emby, usenet clients, or torrent clients other than qBittorrent.

## 3. Environment and constraints

- **Reference deployment:** Raspberry Pi 5 (8 GB), DietPi, with Sonarr, Radarr, Prowlarr, qBittorrent and Jellyfin.
- **Prowlarr is not used.** Indexer management plays no part in cleanup, so removarr does not connect to it.
- **Network:** trusted local network only. There is no login in v1.
- **Port:** 7272 by default, chosen to avoid the ports commonly used by media-server applications. It is configurable.
- **API-only:** removarr talks to the other apps over HTTP and never reads or writes the media or download directories. It needs no media mounts and works the same whether the other apps run natively on the host or in Docker.

Consequences of API-only:

- Sizes are what the apps report. removarr does not measure the disk.
- removarr cannot see hardlinks. It infers which torrent backs which library file from Sonarr/Radarr history.

## 4. Terms

| Term | Meaning |
|---|---|
| Media server | Where things are watched. Jellyfin in v1. |
| Media manager | Sonarr (series) and Radarr (movies). |
| Download client | qBittorrent in v1. |
| Scan | One on-demand run that reads all apps and produces the lists. |
| Candidate | One row in a list: something removarr suggests deleting. |
| Counting users | The Jellyfin users whose watch state decides whether something is watched. |
| Protected | Excluded from suggestions by a rule (favourite or tag). |
| Ignored | Hidden from a list by the user's choice. |
| Linked torrent | A torrent that at least one current library file was imported from. |
| Orphan torrent | A torrent whose hash no media manager knows. |
| Unlinked torrent | A torrent a media manager grabbed, but that no current library file came from. |
| Plan | The exact set of operations a deletion will perform, built from live data and shown for confirmation. |

## 5. Features

### 5.1 Lists

1. **Watched movies.**
2. **Watched series.** A setting chooses the unit: whole series, or per season.
3. **Orphan torrents.** Contains both orphan and unlinked torrents, each labelled with its reason.

### 5.2 Information per row

- **Movies and series:** title, year, size, which counting users watched it, the watched date, the number of torrents Delete would remove, and a seeding flag when any of those torrents is below the seeding minimum. A row can be expanded to show its torrents (name, tracker, ratio, seeding time).
- **Series rows** also show the scope (whole series, or season number) and a "still airing" label where it applies.
- **Torrents:** name, size, state, category, tracker, ratio, seeding time, date added, the reason (orphan or unlinked), and the seeding flag.

### 5.3 Actions

- **Delete** (bulk): builds a plan, shows it for confirmation, then executes it.
- **Ignore** (bulk): hides the rows.
- **Show ignored:** a switch that shows the ignored rows instead. In that view the bulk action is **Un-ignore**. An ignored row must be un-ignored before it can be deleted.
- Lists can be sorted (size, watched date, title), filtered by text, and selected with "select all" over the filtered rows. The bulk bar shows the selected count and total size.

### 5.4 Scan

- A scan runs only when the user starts it. One scan runs at a time.
- Scan results are stored, so the lists load immediately on later visits.
- The header shows when the last scan ran, a summary (candidates per list, protected count, unmatched count, errors), and the free space of the library root folders as reported by Sonarr and Radarr.

### 5.5 History

A page listing every executed deletion: time, item, size, each step and its result.

### 5.6 Settings

| Setting | Default | Notes |
|---|---|---|
| Connections | none | URL and credentials per app, each with a test button. See section 9. |
| Counting users | none selected | Watched lists are not produced until at least one user is selected. |
| Watched mode | all | "all": every counting user must have watched. "any": one is enough. |
| Grace period | 0 days | Minimum time since the watched date. |
| Series unit | whole series | Or per season. |
| Protect favourites | on | See 6.4. |
| Protection tag | `keep` | Tag name looked up in Sonarr and Radarr. |
| Minimum ratio | off | Seeding flag, see 6.6. |
| Minimum seeding time | off | In days. Seeding flag, see 6.6. |
| Excluded categories and tags | none | qBittorrent categories and tags left out of orphan detection. |
| Add to exclusion list on delete | off | See 6.7. |

## 6. Behaviour rules

### 6.1 Watched

- A movie or episode is **watched** when Jellyfin marks it played for the counting users: all of them in "all" mode, at least one in "any" mode.
- Its **watched date** is the latest "last played" date among the counting users who played it. If Jellyfin gives no date, the item still counts as watched and the date shows as unknown.
- The **grace period** is satisfied when the watched date is at least that many days ago. An unknown date satisfies it.
- For a season or series, the watched date is the latest watched date among its episodes.
- With no counting users selected, the watched lists are not produced.

### 6.2 Matching between Jellyfin and the media managers

- **Movies:** by TMDB ID, falling back to IMDb ID.
- **Series:** by TVDB ID, falling back to IMDb ID, then TMDB ID.
- **Episodes:** within a matched series, by season number and episode number. A Jellyfin item that covers a range of episodes counts for each episode in the range.
- If several Jellyfin items match one movie or episode, it is watched for a user when any of them is played by that user.
- A movie with no Jellyfin match is never suggested. An episode with no Jellyfin match counts as unwatched. Both are counted as "unmatched" in the scan summary.

### 6.3 Candidate rules

Only episodes and movies that have a file are considered. "Regular seasons" means season 1 and up; season 0 holds specials.

**Movie.** A candidate when it has a file, is matched, is watched, satisfies the grace period, and is not protected.

**Season.** A regular season *qualifies* when it has at least one file and every episode with a file is watched and satisfies the grace period.

**Series, whole-series mode.** A candidate when every regular season that has files qualifies, there is at least one such season, and the series is not protected.

**Series, per-season mode.** Each qualifying regular season is a candidate row. If every regular season that has files qualifies, those rows are replaced by a single series row, the same as in whole-series mode. The rows are not merged while any of those seasons is individually ignored.

**Specials.** Specials never block a series from qualifying and never get their own row. They are deleted only when the series is removed from Sonarr.

**Still airing.** A series row is labelled "still airing" when Sonarr's status for the series is anything other than ended. This changes what Delete does (6.7).

**Finished airing.** A season has finished airing when the series has ended, or when a later regular season exists and every episode in the season has an air date in the past. The newest season of a series that has not ended is never treated as finished.

### 6.4 Protection

Protected items are not candidates and do not appear in any list. The scan summary shows how many were protected.

- **Favourites** (when enabled): a movie is protected when any counting user has marked it a favourite. A series is protected when any counting user has marked the series, or any season or episode in it, a favourite.
- **Tag:** a movie or series is protected when it carries the protection tag in Radarr or Sonarr.

A protected series produces no series row and no season rows.

### 6.5 Ignore

- Ignores are stored by stable ID: TMDB ID for a movie, TVDB ID for a series, TVDB ID plus season number for a season, hash for a torrent.
- A series ignore hides the series row and all of its season rows. A season ignore hides only that season's row, and has no effect in whole-series mode.
- Ignored rows are left out of counts and totals.
- Ignores persist across scans until the user removes them.

### 6.6 Torrents

**Linking.** removarr reads each media manager's history and queue to learn:

- which media items each torrent hash was grabbed for, and
- which torrent each current library file was imported from.

A torrent is **associated** with a movie or episode when that media manager's history records it as grabbed for it. A torrent **backs** a library file when that file was imported from it.

**Classification** of each torrent in the download client:

- **In queue:** the hash is in a media manager's queue. Never listed.
- **Linked:** at least one current library file was imported from it. Not listed as an orphan; shown under its movie or series.
- **Unlinked:** a media manager's history knows the hash, but no current library file came from it. Typical cause: the release was replaced by an upgrade. Listed.
- **Orphan:** no media manager knows the hash. Typical cause: the movie or series was deleted from the media manager and the torrent was left behind. Listed.

Torrents in an excluded category or with an excluded tag are left out of orphan detection.

**Seeding flag.** A torrent is flagged "below seeding minimum" when:

- both minimums are set, and its ratio is below the minimum ratio *and* its seeding time is below the minimum time; or
- only one minimum is set, and the torrent is below it.

With both minimums off, nothing is flagged. The flag is information only. It appears on the row and again in the confirmation dialog, and never blocks a deletion.

### 6.7 What Delete does

| Candidate | Media manager step |
|---|---|
| Movie | Remove the movie from Radarr, with its files. |
| Series row, series ended | Remove the series from Sonarr, with its files (specials included). |
| Series row, still airing | Delete the episode files in regular seasons, all of which are watched. Unmonitor those episodes. Unmonitor each season that has finished airing. The series stays, and upcoming episodes still download. |
| Season row | Delete the season's episode files. Unmonitor those episodes. Unmonitor the season if it has finished airing. The series stays. |
| Orphan or unlinked torrent | None. |

**Download client step.** The same rule applies to every movie, series and season row: after the media manager step, removarr removes, with data, each torrent that

- is associated with a deleted movie or episode,
- is not in a media manager's queue, and
- no longer backs any library file.

For an orphan or unlinked torrent row, the torrent itself is removed, with data.

Notes:

- A torrent that still backs other library files stays. Examples: a multi-season pack stays until all of its seasons are deleted, and a collection pack stays until all of its movies are deleted. The plan says so.
- **Exclusion list:** when the setting is on, removing a movie from Radarr or a series from Sonarr also adds it to that app's import exclusion list, so an import list does not add it back.
- **Shared data:** if a torrent being removed has the same content path as a torrent that is not being removed, removarr removes the torrent entry and keeps the data.
- Deleting an orphan or unlinked torrent never changes anything in Sonarr or Radarr.

### 6.8 Safety rules

1. **Nothing is deleted without a confirmed plan.** Selecting Delete asks the server for a plan. The server builds it from live data, not from the stored scan: each selected item is re-read from the apps and re-evaluated with the same rules as the scan. Items that no longer qualify are listed in the plan as skipped, with the reason. The confirmation dialog shows the plan. Only a confirmed plan is executed.
2. **Plans expire** ten minutes after they are built. An expired plan cannot be executed; the user rebuilds it.
3. **Order per item:** media manager first, then torrents. If the media manager step fails, the torrent steps for that item are skipped.
4. **Failures are contained.** A failed item is recorded with its error and the remaining items continue. Anything left behind, such as a torrent whose media was already removed, appears as an orphan in the next scan.
5. **No guessing when an app is down.** A list is produced only when every app it depends on answered completely:
   - Watched movies: Jellyfin, Radarr, qBittorrent.
   - Watched series: Jellyfin, Sonarr, qBittorrent.
   - Orphan torrents: qBittorrent and every configured media manager, of which there must be at least one.

   Otherwise the list shows an error instead of results. In particular, torrents are never called orphans while a configured media manager is unreachable.
6. **Dry run.** With `REMOVARR_DRY_RUN=true`, plans execute as no-ops: no deleting call is sent, and the history records the action as a dry run.

### 6.9 Sizes

- A movie or series row's size is the sum of the library file sizes, as reported by the media manager, for the files Delete would remove.
- A torrent row's size is its size as reported by the download client.
- Sizes are labelled as sizes, not as space that will be freed. Where a plan keeps a torrent that backs the deleted files, the plan notes that the space is not freed until that torrent is removed.

## 7. Architecture

```
Browser UI (React, TypeScript, shadcn)
        │  JSON over HTTP
   ┌────┴─────────────────────────────┐
   │ Go server                        │
   │   HTTP API ── scan engine ───┐   │
   │            ── planner ───────┤   │
   │            ── executor ──────┼── providers ── Jellyfin, Sonarr, Radarr, qBittorrent
   │            ── store (SQLite) ┘   │
   └──────────────────────────────────┘
```

The UI is compiled to static files and embedded in the Go binary, so the whole app is one process and one container.

### 7.1 Units

| Unit | Responsibility | Depends on |
|---|---|---|
| `domain` | Product-neutral types: media items, files, watch state, downloads, candidates, plans. | nothing |
| `providers` | The three interfaces below. | `domain` |
| `providers/jellyfin`, `sonarr`, `radarr`, `qbittorrent` | One implementation each. Translate an app's API into `domain` types. | `providers`, `domain` |
| `httpjson` | JSON request helper and timestamp parsing shared by the provider implementations. | nothing |
| `scan` | The rules of section 6.1 to 6.6, as pure functions from snapshots to candidates. No I/O. | `domain` |
| `scanner` | Runs one scan: reads every configured app, calls `scan`, and stores the result. | `providers`, `scan`, `store` |
| `planner` | Builds a plan for selected items from live data (6.7, 6.8). | `providers`, `scan`, `domain` |
| `executor` | Runs a plan step by step and records results. | `providers`, `store` |
| `store` | SQLite persistence. | `domain` |
| `config` | Environment variables and the UI-over-env precedence. | `store` |
| `app` | Builds provider implementations from the current settings. | `config`, `providers` |
| `api` | HTTP handlers; serves the embedded UI. | all of the above |
| `web` | The browser UI. | the HTTP API only |

`scan` contains every decision about what is a candidate. Because it does no I/O, each rule can be tested with plain data.

### 7.2 Provider interfaces

Illustrative shape, not final signatures.

- **MediaServer**
  - list users;
  - return a library snapshot for a set of users: movies and episodes with their external IDs, and per user the played flag, last played date and favourite flag.
- **MovieManager** (Radarr) and **SeriesManager** (Sonarr)
  - return a snapshot: items, files and sizes, tags, series status and air dates, torrent links from history, queue hashes, root folder free space;
  - re-read a single item (for planning);
  - remove a movie or series, delete episode files, unmonitor episodes and seasons.
- **DownloadClient**
  - list downloads: ID, name, size, state, category, tags, content path, date added, and optionally ratio, seeding time and tracker;
  - remove a download, with or without its data.

The seeding fields are optional so that a usenet client, which does not seed, can implement the same interface later.

Adding Plex, Emby or another download client means adding one implementation of one interface. The scan engine, planner, executor and UI do not change.

### 7.3 Scan flow

1. The user starts a scan. The server refuses if one is already running.
2. The server takes a snapshot from every configured app.
3. For each list whose dependencies all answered (6.8, rule 5), `scan` computes the candidates.
4. The store replaces the previous scan's candidates with the new ones and saves the summary.
5. The UI polls the scan status and reloads the lists when it finishes.

### 7.4 Delete flow

1. The user selects rows and chooses Delete.
2. The server builds a plan from live data and stores it with an expiry.
3. The UI shows the plan grouped by app: what is removed, which torrents are below the seeding minimum, which torrents stay and why, and which items were skipped.
4. The user confirms. The server executes the plan item by item, in order.
5. The server records each item in the history. It removes the deleted candidates from the stored scan, along with any torrent rows whose torrents were removed, so the lists are correct without a rescan.
6. The UI shows the outcome, including any failures.

### 7.5 Storage

One SQLite file in the config directory.

| Table | Contents |
|---|---|
| `settings` | Key and JSON value. Holds rules and connection values saved in the UI. |
| `ignores` | Kind, stable key, title at the time of ignoring, created time. |
| `scans` | Start and end time, status, summary. The last ten are kept. |
| `candidates` | The latest scan's rows: list, stable key, row data as JSON. |
| `plans` | Pending plans with their expiry. |
| `actions` | History: time, list, key, title, size, outcome, per-step results. |

### 7.6 HTTP API

| Method and path | Purpose |
|---|---|
| `GET /api/health` | Liveness, for the container health check. |
| `GET /api/status` | Connection health, last scan summary, free space. |
| `POST /api/scans` | Start a scan. |
| `GET /api/scans/current` | Scan progress and result. |
| `GET /api/candidates?list=…&ignored=…` | Rows of one list. |
| `POST /api/ignores`, `DELETE /api/ignores` | Ignore and un-ignore rows. |
| `POST /api/plans` | Build a plan for the selected rows. |
| `POST /api/plans/{id}/execute` | Execute a confirmed plan. |
| `GET /api/actions` | History, paged. |
| `GET /api/settings`, `PUT /api/settings` | Read and change settings. |
| `POST /api/connections/{app}/test` | Test one connection. |
| `GET /api/media-server/users` | Users for the counting-users picker. |

### 7.7 Web UI

- **Pages:** Movies, Series, Torrents, History, Settings.
- **Header:** Scan button, last scan time and summary, free space.
- **Lists:** a table on wide screens and cards on narrow ones, with a bulk bar that stays visible while rows are selected.
- **First run:** with no connections configured, the lists point the user to Settings.
- **Theme:** follows the system light or dark setting.

## 8. Error handling

- **App unreachable or returning errors during a scan:** the lists that depend on it show the error; other lists are still produced.
- **App unreachable while planning:** the affected items are skipped in the plan with the reason.
- **Step fails while executing:** see 6.8, rules 3 and 4.
- **Scan already running:** a second start request is refused with a clear message.
- **Expired plan:** execution is refused and the UI offers to rebuild it.
- **Invalid settings:** rejected on save with a message naming the field.

## 9. Configuration

### 9.1 Environment variables

| Variable | Default | Purpose |
|---|---|---|
| `REMOVARR_PORT` | `7272` | Port the web server listens on. |
| `REMOVARR_CONFIG_DIR` | `/config` | Directory holding the SQLite file. |
| `REMOVARR_DRY_RUN` | `false` | See 6.8, rule 6. |
| `REMOVARR_JELLYFIN_URL`, `REMOVARR_JELLYFIN_API_KEY` | none | Jellyfin connection. |
| `REMOVARR_SONARR_URL`, `REMOVARR_SONARR_API_KEY` | none | Sonarr connection. |
| `REMOVARR_RADARR_URL`, `REMOVARR_RADARR_API_KEY` | none | Radarr connection. |
| `REMOVARR_QBITTORRENT_URL`, `REMOVARR_QBITTORRENT_USERNAME`, `REMOVARR_QBITTORRENT_PASSWORD` | none | qBittorrent connection. |

### 9.2 Precedence

- For each connection field, a value saved in the UI wins. If none is saved, the environment variable is used. If neither exists, the field is unset.
- Clearing a field in the UI falls back to the environment variable.
- The settings page shows where each value comes from (UI, environment, or unset).
- Port, config directory and dry run are environment-only. The rules in 5.6 are UI-only.

## 10. Security

- No login in v1. removarr is meant for a trusted local network, and the README says not to expose it to the internet.
- API keys and the qBittorrent password are write-only: the API never returns them, only whether each is set and where it comes from.
- Secrets saved in the UI are stored in the SQLite file, which is created readable by its owner only.
- Requests that change anything must be JSON and carry a custom header, and the server rejects requests whose origin does not match the address it was reached on. This stops another website open in the user's browser from triggering deletions.
- removarr contacts only the apps configured in its settings.

## 11. Packaging and deployment

- One container image for `linux/arm64` and `linux/amd64`, published as `ghcr.io/removarr/removarr`.
- Runs as a non-root user. The only volume is the config directory.
- The repository ships a `docker-compose.yml` example and a health check on `/api/health`.
- The README explains how to reach apps that run natively on the host from inside the container.

## 12. Testing

- **`scan`:** table-driven tests over hand-built snapshots, one per rule in sections 6.1 to 6.6, including specials, still-airing series, season merging, ignored seasons, both watched modes, grace period, protection, and each torrent classification.
- **Providers:** each implementation tested against a local HTTP server replaying recorded responses from the real app.
- **`planner` and `executor`:** tested with fake providers: re-evaluation and skips, step order, failure containment, the shared-data rule, plan expiry, dry run.
- **`store`:** tested against a temporary SQLite file.
- **`api`:** handler tests, including the write-only secrets and the request checks in section 10.
- **`web`:** component tests for selection and the bulk bar, and one end-to-end test against a server running with fake providers.
- **Before a release:** a manual pass on the reference stack with dry run on, then with it off.

## 13. Build order

v1 is one product, built in four stages. Each stage ends with something that can be checked on the reference stack before the next begins.

1. **Read-only core.** Providers, the scan engine, the store and the scan and candidate endpoints. Check: a scan against the real apps produces three correct lists. The assumptions in section 16 are verified here.
2. **Ignore and delete.** Ignores, the planner, the executor and the history. Check: plans are correct with dry run on, then real deletions work.
3. **Web UI.** All pages, on top of the finished API.
4. **Packaging.** The multi-architecture image, the compose example and the README.

## 14. Later work

Not part of v1, in no fixed order:

- "Never watched" list (added long ago, never played) and "abandoned series" list.
- Orphan files on disk, which needs access to the media and download directories.
- Scheduled scans and notifications.
- Jellyseerr: clear the request when its media is deleted.
- Grouping cross-seeded torrents with the torrent they duplicate.
- Multiple instances per app.
- Plex and Emby; other torrent clients; usenet clients.
- Authentication.

## 15. Technology choices

Approved by the owner on 2026-11-18. Versions are pinned in the implementation plan to the stable releases current at that time.

| Area | Choice | Reason |
|---|---|---|
| Server language | Go | Small footprint on the Pi; one static binary. |
| HTTP and logging | Go standard library only | About a dozen endpoints; no framework needed. |
| SQLite | `modernc.org/sqlite`, plain SQL, embedded migration files | Pure Go, so arm64 and amd64 builds need no C toolchain. |
| App clients | Thin hand-written clients for Jellyfin, Sonarr, Radarr and qBittorrent | Only a few endpoints each, and exact control over the fields the rules depend on. |
| Frontend base | Vite, React, TypeScript, Tailwind CSS, shadcn/ui | Chosen by the owner. |
| Frontend libraries | TanStack Query, TanStack Table, React Router | shadcn's data table is built on TanStack Table. |
| Tooling and tests | pnpm; Go `testing`; Vitest; Playwright for the end-to-end test | Common, well-known defaults. |
| Container | Multi-stage build, distroless base image, non-root user | Small image with nothing extra inside. |
| CI and releases | GitHub Actions; image on `ghcr.io/removarr/removarr` | Free for public repositories and lives with the code. |
| Repository | `github.com/removarr/removarr` | The owner's GitHub organisation. |
| License | MIT | Simplest for others to reuse. |

## 16. Assumptions to verify early in implementation

Each of these is expected to hold but has not been checked against the running apps. The plan verifies them first, because the torrent rules depend on them.

1. Sonarr and Radarr history expose the torrent hash on grab and import events, and make it possible to tell which download a current file was imported from.
2. Sonarr's API can unmonitor individual episodes and a season. If unmonitoring a season does not also unmonitor its episodes, removarr does both explicitly.
3. Jellyfin reports the played flag, last played date and favourite flag per user, and the external IDs, for movies and episodes.
4. qBittorrent reports content path, ratio, seeding time, tracker, category and tags per torrent.
