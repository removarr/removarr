# removarr: instructions for coding agents

removarr suggests what to delete from a self-hosted media server: watched movies, watched series and orphan torrents. It reads Jellyfin, Sonarr, Radarr and qBittorrent over their APIs. A person selects and confirms every deletion.

## Read first

- **The spec is binding:** `docs/superpowers/specs/2026-11-18-removarr-design.md`. If the code and the spec disagree, fix the code, or amend the spec and add the change to the "Amended" line in its header. Never let them drift apart silently.
- **Two principles decide what the rules leave open** (spec section 2):
  - Free disk space, but never remove a movie or episode that has not been watched. Unsure means unwatched.
  - A torrent goes as a whole or stays as a whole. Never delete only part of what a torrent holds.
- **`LEARNINGS.md`** before you touch an app client, the linking rules or the store. It records what was already found out the hard way.
- **Work packages** are GitHub issues: stage 2 in four parts, the web UI, packaging, and review follow-ups. Each names what it depends on. Implementation plans are in `docs/superpowers/plans/`.
- **The `implement-issue` skill** takes one issue from open to a reviewed pull request: `.agents/skills/implement-issue/SKILL.md`. Only the owner starts it, by calling it by name: `/implement-issue` with `next`, an issue number or an issue URL. Do not start it on your own, and do not follow it because a request mentions an issue. In a tool that cannot call skills, the owner names it and you read the file.

## Layout

| Path | Role |
|---|---|
| `cmd/removarr` | Entry point |
| `internal/domain` | Product-neutral types |
| `internal/scan` | Every rule about what is a candidate, as pure functions. No I/O |
| `internal/scanner` | Runs one scan: reads the apps, calls `scan`, stores the result |
| `internal/planner` | Builds the plan for a deletion: reads and evaluates through `scanner`, then `Make` decides the items, steps and skips. Executes nothing |
| `internal/executor` | Executes a stored plan item by item and records each in the history. The only code given the write interfaces |
| `internal/gate` | Lets one job run at a time: scan, plan building or execution |
| `internal/providers/*`, `internal/httpjson` | One client per app. They translate an API into `domain` types and decide nothing |
| `internal/store` | SQLite: settings, scans, candidates. Migrations in `internal/store/migrations` |
| `internal/config` | Environment variables and saved settings; saved settings take priority |
| `internal/app`, `internal/api` | Provider wiring and the HTTP API |
| `internal/webui` | Embeds the built UI and serves it outside `/api/` |
| `internal/e2e` | The real API on in-memory fake apps, for the end-to-end test. Never point it at a real config directory |
| `web` | The browser UI: Vite, React, TypeScript. Builds into `internal/webui/dist` |
| `docs/superpowers` | Spec and implementation plans |
| `.agents/skills` | Skills for coding agents, in the Agent Skills format. `implement-issue` is the way of working for one issue, with the scripts for its review steps; they need `gh` |
| `.claude/skills` | One link per skill into `.agents/skills`, because Claude Code looks for skills here. A new skill needs its link |

## Commands

The checks. All of them must pass:

```
gofmt -l cmd internal   # must print nothing; not `.`, which walks web/node_modules
go vet ./...
go test ./...
go test -race ./internal/api/ ./internal/config/ ./internal/store/ ./internal/scanner/ ./internal/scan/ ./internal/planner/ ./internal/executor/ ./internal/gate/
go build ./...
```

The web checks:

```
pnpm --dir web install --frozen-lockfile
pnpm --dir web typecheck
pnpm --dir web test
pnpm --dir web build
pnpm --dir web e2e      # needs `pnpm --dir web exec playwright install chromium` once
```

CI runs `go vet`, `go test` and every web check, `e2e` included, on every pull request.

To start the server from the source tree. It runs until it is stopped, so it is not a check:

```
REMOVARR_CONFIG_DIR=./config go run ./cmd/removarr
```

## Rules for the code

- **Dependencies:** Go 1.27 and the standard library. The only third-party module is `modernc.org/sqlite`. Adding one is the owner's decision.
- **Frontend dependencies** are pinned exactly in `web/package.json`. Adding one is the owner's decision, as for Go modules.
- **Only the executor changes an app.** `internal/executor` is the only code given the write interfaces (`providers.MovieWriter`, `SeriesWriter`, `DownloadRemover`). The scanner and the planner send GET requests only, plus the qBittorrent login. With `REMOVARR_DRY_RUN=true` nothing but GET, plus the qBittorrent login, reaches an app.
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
- Checking against the real apps is done by the agent, never handed to the owner: removarr runs with dry run on, against the owner's local replay, which forwards only reads. Nothing is ever deleted while testing; a check with dry run off is the owner's. The implement-issue skill, step 6, has the details, and so does "Checking against real apps" in `LEARNINGS.md`.

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
