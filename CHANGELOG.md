# Changelog

What changed in removarr, written for the person who runs it.

**Format.** Changes are collected under "Unreleased" and move under a version heading with its date when a release is cut. Every entry has three parts:

- **Scope:** what is new, changed, fixed or removed, in terms a user or operator can notice.
- **Deployment:** what to do when updating. Database migrations, new or changed environment variables, settings to review, and any manual step. Write "Nothing to do" when that is the case.
- **Breaking changes:** anything that worked before and no longer does, or behaves differently, with what to do about it. Write "None" when that is the case.

## Unreleased

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
