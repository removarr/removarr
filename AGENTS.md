# removarr: instructions for coding agents

removarr suggests what to delete from a self-hosted media server: watched movies, watched series and orphan torrents. It reads Jellyfin, Sonarr, Radarr and qBittorrent over their APIs. A person selects and confirms every deletion.

## Read first

- **The spec is binding:** `docs/superpowers/specs/2026-11-18-removarr-design.md`. If the code and the spec disagree, fix the code, or amend the spec and add the change to the "Amended" line in its header. Never let them drift apart silently.
- **Two principles decide what the rules leave open** (spec section 2):
  - Free disk space, but never remove a movie or episode that has not been watched. Unsure means unwatched.
  - A torrent goes as a whole or stays as a whole. Never delete only part of what a torrent holds.
- **`LEARNINGS.md`** before you touch an app client, the linking rules or the store. It records what was already found out the hard way.
- **Work packages** are GitHub issues: stage 2 in four parts, the web UI, packaging, and review follow-ups. Each names what it depends on. Implementation plans are in `docs/superpowers/plans/`.
- **`WORKFLOW.md`** when you take an issue. It gives the order of work, from picking the issue to the reviewed pull request, and says when to stop and ask.

## Layout

| Path | Role |
|---|---|
| `cmd/removarr` | Entry point |
| `internal/domain` | Product-neutral types |
| `internal/scan` | Every rule about what is a candidate, as pure functions. No I/O |
| `internal/scanner` | Runs one scan: reads the apps, calls `scan`, stores the result |
| `internal/providers/*`, `internal/httpjson` | One client per app. They translate an API into `domain` types and decide nothing |
| `internal/store` | SQLite: settings, scans, candidates. Migrations in `internal/store/migrations` |
| `internal/config` | Environment variables and saved settings; saved settings take priority |
| `internal/app`, `internal/api` | Provider wiring and the HTTP API |
| `docs/superpowers` | Spec and implementation plans |
| `scripts` | Helpers for the review steps of `WORKFLOW.md`. They need `gh` |

## Commands

The checks. All of them must pass:

```
gofmt -l .        # must print nothing
go vet ./...
go test ./...
go test -race ./internal/api/ ./internal/config/ ./internal/store/ ./internal/scanner/ ./internal/scan/
go build ./...
```

CI runs `go vet` and `go test` on every pull request.

To start the server from the source tree. It runs until it is stopped, so it is not a check:

```
REMOVARR_CONFIG_DIR=./config go run ./cmd/removarr
```

## Rules for the code

- **Dependencies:** Go 1.27 and the standard library. The only third-party module is `modernc.org/sqlite`. Adding one is the owner's decision.
- **No deleting call yet.** Until issue #5, removarr sends only GET requests to the apps, plus the qBittorrent login. Do not add a call that changes anything in an app outside that issue.
- **No guessing from a partial answer** (spec 6.8, rule 5). If an app's answer is incomplete or inconsistent, return an error so the list shows it. Do not fill in a default.
- **Secrets are write-only.** API keys and the qBittorrent password are never returned, logged or put into an error. An error about a URL never repeats the URL.
- **Outbound requests** use an HTTP client with `httpjson.SameHostRedirects`.
- **Settings** are written only through `config.Service.Apply`. Read an app's connection through `App` or `ConnView`, never field by field.
- **The store has one database connection.** Inside a transaction use the transaction, never `s.db`: that call would wait forever.
- **Every API route goes through `guard`** in `internal/api/server.go`.

## Tests

- Write the test first and see it fail for the reason you expect. A fix comes with a test that fails without it.
- One test per rule in spec section 6. Name a test after what must hold, for example `TestRunFailsEveryListWhenFilesCannotBeRead`.
- `scan`: table-driven tests over hand-built snapshots. Providers: against a local HTTP server. Store: against a temporary SQLite file. API: through the harness in `internal/api/server_test.go`.
- When you add glue code, break it on purpose once and check that a test notices.

## Real data

- Never commit or print URLs, keys, user names, file names or titles from anyone's library. Record measurements as counts and sizes only.
- `.env` in the repository root holds the owner's connection details. It is git-ignored. Read it by path when a check needs it; never print or copy it.
- Checking against the real apps is done with the owner: build the binary, hand over the command, compare the result. See "Checking against real apps" in `LEARNINGS.md`.

## Git and pull requests

- Branch from `main`. Never push to `main` and never force-push. One pull request per work package.
- Commit subject: `type: what changed`, in lower case, with one of `feat`, `fix`, `docs`, `test`, `chore`. The body says why.
- Before a pull request: the checks pass and `CHANGELOG.md` is updated.

## Files to keep up to date

| File | Update it when |
|---|---|
| `CHANGELOG.md` | A user or operator can notice the change. Add it under "Unreleased" with its scope, its deployment notes and any breaking change. The file explains the format. |
| `LEARNINGS.md` | You found out something that is not obvious from the code and that the next person would otherwise find out again. |
| The spec | Behaviour changes. |
| `README.md` | How to run or configure removarr changes. |
