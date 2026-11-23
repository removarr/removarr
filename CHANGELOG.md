# Changelog

What changed in removarr, written for the person who runs it.

**Format.** Changes are collected under "Unreleased" and move under a version heading with its date when a release is cut. Every entry has three parts:

- **Scope:** what is new, changed, fixed or removed, in terms a user or operator can notice.
- **Deployment:** what to do when updating. Database migrations, new or changed environment variables, settings to review, and any manual step. Write "Nothing to do" when that is the case.
- **Breaking changes:** anything that worked before and no longer does, or behaves differently, with what to do about it. Write "None" when that is the case.

## Unreleased

### Stage 2b: ignore and un-ignore (issue #3)

Rows you do not want suggested can be hidden and brought back. Still read-only towards the apps: nothing is deleted yet.

#### Scope

- **Ignore:** `POST /api/ignores` with `{"rows":[{"list":"movies","key":"tmdb:603"}]}` hides rows, in bulk. Only rows of the latest scan can be ignored; the request is saved whole or not at all.
- **Un-ignore:** `DELETE /api/ignores` with the same body brings rows back. An ignore whose row is gone can still be removed.
- **Ignored view:** `GET /api/candidates?list=…&ignored=true` lists one entry per ignore, with `ignoredAt`. An ignore that the latest scan has no row for shows its key and title with `notInScan`; a row the scan holds back shows `waiting`.
- **Takes effect at once:** ignoring and un-ignoring change the lists and the counts in `/api/status` and `/api/scans/current` without a new scan. A row tied to an ignored row is not listed and counts as `waiting`, because the torrent or file they share now stays.
- **Series and seasons:** ignoring a series hides its season rows too. Ignoring a season hides only that season's row and has no effect in whole-series mode. In per-season mode, seasons are not merged into one series row while one of them is ignored; un-ignoring that season merges them at the next scan. A series row that a scan merged while you ignored one of its seasons, because the scan was still running, is marked `merged` and counts as `waiting` until the next scan, together with every row tied to it.
- Ignores persist across scans until they are removed.

#### Deployment

- Database migration `0002_ignores` runs on start and adds the `ignores` table.
- Scan again after updating. Scans now also store the rows the whole-torrent rule holds back; a scan from before the update has none, so the ignored view cannot show them until the next scan.

#### Breaking changes

- None. `ignored` on `GET /api/candidates` accepts only `true` and `false`; any other value is a 400.

### Stage 2a: whole torrents, whole files and episode ranges (pull request #9)

Rules that follow from two principles: free disk space without removing anything unwatched, and never delete only part of what a torrent holds. Still read-only: nothing is deleted yet.

#### Scope

- **Whole torrents:** a movie, series or season is listed only when every torrent that backs its files would go with it. A row whose torrent also holds something that stays is not listed, and the scan summary counts it as `waiting`. Examples: a multi-season pack with an unwatched season, a collection pack with a protected movie, a download that is still in a media manager's queue.
- **Whole files:** the same for library files. A file that also holds an episode that stays keeps its row waiting.
- **Tied rows:** rows that share a torrent or a file and can all go are listed with `tiedWith`, naming each other. They are meant to be deleted together.
- **Episode ranges:** a media server item that covers several episodes counts for a further episode only when Sonarr holds it in the same file as the first.
- **Data outside the library:** each torrent on a movie, series or season row states `outsideLibrary`, the bytes it holds that are not in the library, when that is 50 MB or more.
- **List dependencies:** with both Sonarr and Radarr configured, each watched list needs both to answer.
- **API additions:** `tiedWith` on rows, `outsideLibrary` on a row's torrents, `waiting` in each list's summary.
- **Fixed:** a file that spans two seasons was counted twice in the size of a series row.

#### Deployment

- No database migration and no new environment variable.
- Scan again after updating. The stored lists were computed with the old rules.

#### Breaking changes

- With both Sonarr and Radarr configured, neither watched list is produced while one of them cannot be read. Before, the other list was still shown.
- Rows can leave the watched lists. A row that would delete only part of a torrent or of a file now waits, and an episode covered only by a range that Sonarr does not confirm counts as unwatched.
- A row can name more torrents than before: tied rows each name the torrent they share.

### Stage 1: read-only core (pull request #1)

The first working version. It scans Jellyfin, Sonarr, Radarr and qBittorrent on demand and serves three suggestion lists over a JSON API. It changes nothing in those apps: there is no deletion, no ignore and no web UI yet.

#### Scope

- **Three lists:** watched movies, watched series (whole series or per season) and orphan torrents, each row with its size and the torrents a deletion would remove.
- **Rules:** counting users, watched mode "all" or "any", grace period, protection by favourite and by tag, seeding minimums, excluded categories and tags.
- **Linking torrents to library files:** by Sonarr and Radarr history, and where history says nothing, by content. A torrent backs a library file when one of its files has exactly the same size and that size is at least 50 MB.
- **No guessing:** a list is produced only when every app it depends on answered completely. Otherwise it shows the error.
- **API:**
  - `GET /api/health`, `GET /api/status`
  - `GET /api/settings`, `PUT /api/settings`
  - `POST /api/connections/{app}/test`, `GET /api/media-server/users`
  - `POST /api/scans`, `GET /api/scans/current`
  - `GET /api/candidates?list=movies|series|torrents`
- **Settings:** connections and rules are saved through the API and take priority over the environment variables. API keys and the qBittorrent password are never returned.
- **Security:**
  - No login: for a trusted local network only.
  - The server answers only when reached by an IP address, as `localhost`, or by a host name listed in `REMOVARR_ALLOWED_HOSTS`. The health check answers under any name.
  - A request that changes something must be JSON, carry the header `X-Removarr-Request`, and come from the server's own address.
  - Connections to the apps do not follow a redirect to another host or port, or from https to http.

#### Deployment

- **Requirements:** Go 1.27 to build. There is no container image yet.
- **Database:** one SQLite file, `removarr.db`, in the config directory, created on first start and readable by its owner only. Migration `0001_init` creates the tables `settings`, `scans` and `candidates`. Migrations run by themselves at start.
- **Environment variables:**

  | Variable | Default | Purpose |
  |---|---|---|
  | `REMOVARR_PORT` | `7272` | Port the server listens on |
  | `REMOVARR_CONFIG_DIR` | `/config` | Directory of the database |
  | `REMOVARR_DRY_RUN` | `false` | Reserved for deletions, which are not built yet |
  | `REMOVARR_ALLOWED_HOSTS` | none | Host names removarr answers to, besides IP addresses and `localhost`; separated by commas, without scheme or port |
  | `REMOVARR_JELLYFIN_URL`, `REMOVARR_JELLYFIN_API_KEY` | none | Jellyfin connection |
  | `REMOVARR_SONARR_URL`, `REMOVARR_SONARR_API_KEY` | none | Sonarr connection |
  | `REMOVARR_RADARR_URL`, `REMOVARR_RADARR_API_KEY` | none | Radarr connection |
  | `REMOVARR_QBITTORRENT_URL`, `REMOVARR_QBITTORRENT_USERNAME`, `REMOVARR_QBITTORRENT_PASSWORD` | none | qBittorrent connection |

- **After the first start:** select the counting users (`PUT /api/settings`), then start a scan. Without counting users the two watched lists are not produced.
- **Reaching it by name:** to open removarr as `pi.local` or through a reverse proxy, list that name in `REMOVARR_ALLOWED_HOSTS`. A reverse proxy must pass the Host header on unchanged.

#### Breaking changes

None. This is the first version.
