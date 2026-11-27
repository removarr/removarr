# removarr

removarr suggests what to delete from a self-hosted media server: watched movies, watched series and orphan torrents. It never deletes anything on its own.

Status: early development. removarr scans Jellyfin, Sonarr, Radarr and qBittorrent, shows the three suggestion lists in a web UI, and deletes what you select after you confirm the plan, recording each deletion in a history. There is no container image yet. The design is in `docs/superpowers/specs/2026-11-18-removarr-design.md`.

## Development

Requires Go 1.27, and Node 24 with pnpm for the web UI.

    go test ./...

## Building

The removarr binary embeds the web UI, so build the UI first:

    pnpm --dir web install --frozen-lockfile
    pnpm --dir web build
    go build ./cmd/removarr

A binary built without the UI answers every page with a note that the UI is not built; the API works either way.

## Developing the UI

The web UI is in `web/` and needs Node 24 and pnpm.

    pnpm --dir web install

Start removarr on port 7272, or the fake server, which serves the real API on a small fake library and never connects to an app:

    go run ./internal/e2e/fakeserver -port 7272 -dir ./config-fake

Never give the fake server removarr's real config directory as `-dir`: it opens the database there, overwrites the saved rules and replaces the stored scans with its fake library.

Then start the development server, which forwards `/api` to port 7272:

    pnpm --dir web dev

The removarr binary embeds `internal/webui/dist`, so build the UI with `pnpm --dir web build` before `go build`. The checks of the UI are `pnpm --dir web test`, `pnpm --dir web typecheck` and the end-to-end test, which runs the fake server on port 7373 and needs the UI built and Chromium installed once:

    pnpm --dir web exec playwright install chromium
    pnpm --dir web build
    pnpm --dir web e2e

When port 7373 is taken, for example by a fake server you are looking at, run the end-to-end test on another port with `REMOVARR_E2E_PORT`:

    REMOVARR_E2E_PORT=7374 pnpm --dir web e2e

## Running

    REMOVARR_CONFIG_DIR=./config go run ./cmd/removarr

Then open `http://<address>:7272` in a browser. Set up the connections and the counting users under Settings, and start a scan from the header.

| Variable | Default | Purpose |
|---|---|---|
| `REMOVARR_PORT` | `7272` | Port the server listens on. |
| `REMOVARR_CONFIG_DIR` | `/config` | Directory holding the SQLite database. |
| `REMOVARR_DRY_RUN` | `false` | With `true`, executing a plan sends nothing that changes an app and records a dry run in the history. |
| `REMOVARR_ALLOWED_HOSTS` | | Host names removarr answers to, separated by commas. See below. |
| `REMOVARR_JELLYFIN_URL`, `REMOVARR_JELLYFIN_API_KEY` | | Jellyfin connection. |
| `REMOVARR_SONARR_URL`, `REMOVARR_SONARR_API_KEY` | | Sonarr connection. |
| `REMOVARR_RADARR_URL`, `REMOVARR_RADARR_API_KEY` | | Radarr connection. |
| `REMOVARR_QBITTORRENT_URL`, `REMOVARR_QBITTORRENT_USERNAME`, `REMOVARR_QBITTORRENT_PASSWORD` | | qBittorrent connection. |

Connection settings saved in the UI, or through the API, take priority over these variables.

When removarr is stopped during an execution, it starts no further item and waits up to two minutes for the one it is running to finish and be recorded. Give it more than that before it is killed: Docker waits 10 seconds by default (`docker stop -t 130`), systemd 90 seconds (`TimeoutStopSec=130`).

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

See what deleting rows would do. This builds a plan from live data and deletes nothing:

    curl -X POST -H 'Content-Type: application/json' -H 'X-Removarr-Request: 1' \
      -d '{"rows":[{"list":"movies","key":"tmdb:603"}]}' localhost:7272/api/plans

Carry out a plan, using the plan's `id` from the answer above. This deletes what the plan lists, so start with `REMOVARR_DRY_RUN=true` and compare the history with the plan first. A plan can be executed once, within ten minutes of being built, and executing it discards every other plan. A scan, a plan being built and an execution refuse each other with a 409:

    curl -X POST -H 'Content-Type: application/json' -H 'X-Removarr-Request: 1' \
      -d '{}' localhost:7272/api/plans/<plan id>/execute

List the history of what was done, newest first. `limit` is from 1 to 200 (default 50); when the answer has a `next`, pass it as `before` for the following page:

    curl 'localhost:7272/api/actions?limit=20'

removarr has no login. Run it only on a network you trust, and do not expose it to the internet.

## Reaching removarr by name

removarr answers when it is reached by an IP address or as `localhost`. To reach it by a host name, list the name:

    REMOVARR_ALLOWED_HOSTS=pi.local,removarr.lan

List names only, without `http://` and without a port. A request under any other name gets a 403, except the health check at `/api/health`. This stops a website from pointing its own domain name at removarr's address and then reading and changing everything through your browser. In a browser, a refused name gets a short page that says so.

### Behind a reverse proxy

The proxy must pass the Host header on unchanged, and the name it is reached by must be listed in `REMOVARR_ALLOWED_HOSTS`. removarr refuses a request that changes something unless it comes from the address the request names, so a proxy that rewrites Host to removarr's own address breaks every Save, Scan and Delete in the UI.
