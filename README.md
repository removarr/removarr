# removarr

removarr suggests what to delete from a self-hosted media server: watched movies, watched series and orphan torrents. It reads Jellyfin, Sonarr, Radarr and qBittorrent over their APIs. It never deletes anything on its own: you select the rows, check the plan, and confirm.

This is version 1. removarr scans the four apps, shows the three suggestion lists in a web UI, and deletes what you select after you confirm the plan, recording each deletion in a history. It runs as a container image for `linux/arm64` and `linux/amd64`. The design is in `docs/superpowers/specs/2026-11-18-removarr-design.md`.

## Do not expose it to the internet

removarr has no login. Run it only on a network you trust. It listens on every interface of its container, so whoever can reach its port can delete your media. To keep it on the host only, publish the port on the loopback address: `127.0.0.1:7272:7272` under `ports`.

## Installing

With Docker Compose, using the example file `docker-compose.yml` from this repository:

    docker compose up -d

The same with `docker run`:

    docker run -d --name removarr --restart unless-stopped \
      -p 7272:7272 \
      -v removarr-config:/config \
      --add-host host.docker.internal:host-gateway \
      --stop-timeout 130 \
      -e REMOVARR_DRY_RUN=true \
      ghcr.io/removarr/removarr:latest

The image is `ghcr.io/removarr/removarr`, tagged `latest` and with each version, for example `ghcr.io/removarr/removarr:1.0.0`. It is built for `linux/arm64` and `linux/amd64`; the arm64 image is about 16 MB.

To update, pull the new image and start the container again:

    docker compose pull && docker compose up -d

The database migrates itself and stays in the volume.

## First steps

1. Open `http://<address>:7272` in a browser.
2. Under Settings, Connections, enter the URL and credentials of each app and test them.
3. Choose the counting users: whose watch state counts. Without one, no watched list is produced.
4. Start a scan from the header, then review the lists.
5. Keep dry run on until the history of a dry-run execution matches its plan. Then remove `REMOVARR_DRY_RUN` and recreate the container.

With dry run on, executing a plan sends no call that changes an app, and the history records every step as a dry run.

## Configuration

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

In the image, `REMOVARR_CONFIG_DIR` is `/config`, the volume. Leave it so. The port, the config directory, dry run and the allowed host names are set only through the environment. For each connection setting, a value saved in the UI, or through the API, wins over the variable; clearing it in the UI falls back to the variable. The Settings page shows where each value comes from.

The other settings, such as the watched mode, the grace period and the protection tag, are set in the UI.

### Keeping the config in a directory

A named volume needs nothing: it takes over the owner of the image's `/config`, the user with uid and gid 65532. To keep the config in a directory of the host instead, the container's user must be able to write to it. Either give the directory to uid 65532:

    sudo chown 65532:65532 ./config

or run the container as the user who owns the directory, with `user: "1000:1000"` under the service in the compose file (`--user 1000:1000` for `docker run`). Then mount it: `./config:/config`.

If the user cannot write to the directory, removarr stops at start with an error about the database, with an error about opening the database.

To back up, stop removarr and copy `removarr.db` from the config directory.

## Reaching apps on the host

Apps installed natively on the same machine are reached from the container as `http://host.docker.internal:<port>`. The `extra_hosts` line in the compose example, or `--add-host` for `docker run`, provides that name on Linux; Docker Desktop has it built in.

The app has to listen on an address the container can reach, not only on `127.0.0.1`. Settings to check, depending on the app:

- qBittorrent's "Bypass authentication for clients on localhost" does not apply to the container. Give removarr a user name and password. Adding the Docker network to qBittorrent's whitelist is the alternative.
- Sonarr and Radarr: the "Bind Address" should be `*`.
- Jellyfin listens on all addresses by default.
- qBittorrent: the Web UI address should be `*`.
- If qBittorrent's host header validation is on, add `host.docker.internal` to its server domains.

Binding an app to every address, or whitelisting the Docker network, widens who can reach that app. Do it only on a network you trust.

Alternatives: use the host's LAN address, or set `network_mode: host`. With `network_mode: host`, `ports` is ignored and removarr listens on port 7272 of every interface of the host, and the `127.0.0.1:7272:7272` mapping no longer applies. A firewall or the host's own setup must then limit who can reach port 7272.

Apps that run in other containers: put removarr on the same Docker network and use the service name, for example `http://sonarr:8989`.

## Reaching removarr by name

removarr answers when it is reached by an IP address or as `localhost`. To reach it by a host name, list the name. In the compose file:

    REMOVARR_ALLOWED_HOSTS: pi.local,removarr.example.lan

or, as an environment variable:

    REMOVARR_ALLOWED_HOSTS=pi.local,removarr.example.lan

List names only, without `http://` and without a port. A request under any other name gets a 403, except the health check at `/api/health`, which answers under any name, so a monitor can use the container name. This stops a website from pointing its own domain name at removarr's address and then reading and changing everything through your browser. In a browser, a refused name gets a short page that says so.

### Behind a reverse proxy

The proxy must pass the Host header on unchanged, and the name it is reached by must be listed in `REMOVARR_ALLOWED_HOSTS`. removarr refuses a request that changes something unless it comes from the address the request names, so a proxy that rewrites Host to removarr's own address breaks every Save, Scan and Delete in the UI.

With nginx, add this to the location that forwards to removarr:

    proxy_set_header Host $host;

Caddy and Traefik pass the Host header on by default.

## Health check

The image checks itself every 30 seconds with `removarr healthcheck`, and `docker ps` shows `healthy` once it answers. A monitor can call `GET /api/health`.

## Stopping

When removarr is stopped during an execution, it starts no further item and waits up to two minutes for the one it is running to finish and be recorded. Give it more than that before it is killed: Docker waits 10 seconds by default. The compose example sets `stop_grace_period: 130s`; for `docker run`, use `--stop-timeout 130`. With systemd, use `TimeoutStopSec=130`.

## What the lists mean

**Movies.** A movie is listed when the counting users have watched it (all of them, or any one, as the watched mode says), the grace period has passed, and it is not protected. A movie with no match in Jellyfin is never listed.

**Series.** A series is listed when every regular season that has files qualifies: each episode with a file is watched and past the grace period. A setting chooses the unit, the whole series or one row per season. A series or season that is still airing is listed, with the label "still airing". Deleting it unmonitors only the seasons that have finished airing and keeps the series in Sonarr, so upcoming episodes still download. Specials never block a series and never get their own row; they go only when the whole series is removed from Sonarr.

**Torrents.** A torrent is listed when it is in qBittorrent but no longer backs any library file. An orphan is a torrent that no media manager knows. An unlinked torrent is one that a media manager's history knows, but that backs no current library file, for example after an upgrade replaced the release. A torrent that is in a media manager's queue is never listed, and torrents in an excluded category or with an excluded tag are left out of orphan detection. Each row shows the reason and a seeding flag.

Protected items are never listed: favourites of a counting user (when "Protect favourites" is on, as it is by default), and anything that carries the protection tag (`keep` by default) in Sonarr or Radarr. An item that is not clearly watched counts as unwatched, and an item whose torrent is shared with something that stays waits instead of being listed.

### Limits of linking torrents to files

removarr links a torrent to library files through the history and the queue of Sonarr and Radarr. Where the history says nothing, it compares file sizes: a torrent file and a library file are linked when they have exactly the same size in bytes and that size is at least 50 MB. It never reads your disk, so it cannot see hardlinks.

- A link made by file size alone is marked in the plan, so you can check it before you confirm.
- A file you imported by hand has no history. It is linked only by its size.
- Of two torrents that hold the same file, such as cross-seeds, only one is linked. The other stays listed.
- Two unrelated files of exactly the same size would be linked.

A torrent is removed as a whole or stays as a whole. removarr never deletes part of what a torrent holds. When unsure, it counts a movie or episode as unwatched, and it keeps a torrent's data.

## What Delete does

Delete first builds a plan from live data and shows it. Nothing runs before you confirm the plan, and a plan expires after ten minutes. For each item, the media manager step comes first and the torrents after it. If a step fails, the steps after it for that item are skipped, and the other items continue.

- **Movie:** removed from Radarr, with its files.
- **Series, ended:** removed from Sonarr, with its files.
- **Series, still airing, or a season:** the episodes that have files are unmonitored and their files deleted. A season is unmonitored only if it has finished airing. The series stays in Sonarr.
- **Torrents:** after that, each torrent tied to the deleted items that is not in a media manager's queue and no longer backs any library file is removed from qBittorrent, with its data. A torrent is removed as a whole.
- **Orphan or unlinked torrent:** the torrent is removed, with its data. Nothing changes in Sonarr or Radarr.

A torrent keeps its data, and only its entry is removed, when another torrent that stays has the same content path or one inside the other, or when its content path is not known.

With dry run on, every step is recorded in the history as it would have run, and nothing that changes an app is sent.

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

To start the server from the source tree:

    REMOVARR_CONFIG_DIR=./config go run ./cmd/removarr

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

## Building the image

    docker build -t removarr .
    scripts/smoke-image.sh removarr

The smoke test starts the image, waits until it is healthy, checks that it runs as a non-root user, and checks that a saved setting survives a restart. With podman, build with `--format docker`, so the health check is kept:

    podman build --format docker -t removarr .

## Releasing

For maintainers.

1. On `main`, move "Unreleased" in `CHANGELOG.md` under the version, with its date.
2. Publish a GitHub release with the tag `vX.Y.Z`.
3. The `image` workflow builds the image, runs the smoke test, and pushes it as `X.Y.Z`, `X.Y`, `X` and `latest`. A pre-release gets only its own version tag.
4. Check that `ghcr.io/removarr/removarr:X.Y.Z` pulls on both architectures.

Before the first release: do the manual pass on the reference stack, with dry run on and then off. Make the repository public right before publishing the release, and make the package public on ghcr.io after the first push.

## License

MIT. See `LICENSE`.
