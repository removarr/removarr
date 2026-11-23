# removarr

removarr suggests what to delete from a self-hosted media server: watched movies, watched series and orphan torrents. It never deletes anything on its own.

Status: early development. The server can scan Jellyfin, Sonarr, Radarr and qBittorrent and serve the three suggestion lists over a JSON API. It cannot delete anything yet, and there is no web UI yet. The design is in `docs/superpowers/specs/2026-11-18-removarr-design.md`.

## Development

Requires Go 1.27.

    go test ./...

## Running

    REMOVARR_CONFIG_DIR=./config go run ./cmd/removarr

| Variable | Default | Purpose |
|---|---|---|
| `REMOVARR_PORT` | `7272` | Port the server listens on. |
| `REMOVARR_CONFIG_DIR` | `/config` | Directory holding the SQLite database. |
| `REMOVARR_DRY_RUN` | `false` | Reserved for deletions, which are not built yet. |
| `REMOVARR_ALLOWED_HOSTS` | | Host names removarr answers to, separated by commas. See below. |
| `REMOVARR_JELLYFIN_URL`, `REMOVARR_JELLYFIN_API_KEY` | | Jellyfin connection. |
| `REMOVARR_SONARR_URL`, `REMOVARR_SONARR_API_KEY` | | Sonarr connection. |
| `REMOVARR_RADARR_URL`, `REMOVARR_RADARR_API_KEY` | | Radarr connection. |
| `REMOVARR_QBITTORRENT_URL`, `REMOVARR_QBITTORRENT_USERNAME`, `REMOVARR_QBITTORRENT_PASSWORD` | | qBittorrent connection. |

Connection settings saved through the API take priority over these variables.

## Trying the API

Requests that change something need two headers:

    curl -X POST -H 'Content-Type: application/json' -H 'X-Removarr-Request: 1' localhost:7272/api/scans

Choose whose watch state counts, start a scan, then read the lists:

    curl localhost:7272/api/media-server/users
    curl -X PUT -H 'Content-Type: application/json' -H 'X-Removarr-Request: 1' \
      -d '{"rules":{"countingUsers":["<user id>"]}}' localhost:7272/api/settings
    curl -X POST -H 'Content-Type: application/json' -H 'X-Removarr-Request: 1' localhost:7272/api/scans
    curl localhost:7272/api/scans/current
    curl 'localhost:7272/api/candidates?list=movies'
    curl 'localhost:7272/api/candidates?list=series'
    curl 'localhost:7272/api/candidates?list=torrents'
    curl -X POST -H 'Content-Type: application/json' -H 'X-Removarr-Request: 1' \
      -d '{"rows":[{"list":"movies","key":"tmdb:603"}]}' localhost:7272/api/ignores
    curl 'localhost:7272/api/candidates?list=movies&ignored=true'
    curl -X DELETE -H 'Content-Type: application/json' -H 'X-Removarr-Request: 1' \
      -d '{"rows":[{"list":"movies","key":"tmdb:603"}]}' localhost:7272/api/ignores

removarr has no login. Run it only on a network you trust, and do not expose it to the internet.

## Reaching removarr by name

removarr answers when it is reached by an IP address or as `localhost`. To reach it by a host name, list the name:

    REMOVARR_ALLOWED_HOSTS=pi.local,removarr.lan

List names only, without `http://` and without a port. The same applies to the name a reverse proxy passes on. A request under any other name gets a 403, except the health check at `/api/health`. This stops a website from pointing its own domain name at removarr's address and then reading and changing everything through your browser.
