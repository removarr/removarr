# Stage 4: packaging and first release, implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make removarr installable from a container image on `linux/arm64` and `linux/amd64`, with a compose example, a README for operators, CI that checks the image on every pull request, and a workflow that publishes the image for a release (issue #7).

**Architecture:** The binary gains a `healthcheck` command, because the distroless base has no shell or HTTP client. A multi-stage Dockerfile builds the UI and cross-compiles the binary on the build machine's architecture, and copies only the binary and an owned `/config` into the distroless nonroot base. One script, `scripts/smoke-image.sh`, checks a built image (healthy, non-root, a saved setting survives a restart). A new workflow, `image.yml`, builds both architectures and runs the smoke test on every pull request and push to `main`, and also pushes to `ghcr.io/removarr/removarr` when a release is published. `ci.yml` gains `gofmt`, the race detector and the build.

**Tech Stack:** Go 1.27 standard library; Docker/BuildKit Dockerfile syntax (built locally with podman 5.7 using `--format docker`); GitHub Actions `docker/setup-buildx-action@v4`, `docker/login-action@v4`, `docker/metadata-action@v6`, `docker/build-push-action@v7`, `actions/checkout@v7`, `actions/setup-go@v7`; bash and curl in the smoke test.

**Spec:** `docs/superpowers/specs/2026-11-18-removarr-design.md`, sections 9, 10, 11 (amended 2026-11-27), 12 and 15. Read `AGENTS.md` and `LEARNINGS.md` first.

## Global Constraints

- Image name: `ghcr.io/removarr/removarr`. Platforms: `linux/arm64` and `linux/amd64`.
- Base: `gcr.io/distroless/static-debian13:nonroot` (uid and gid 65532). Builders: `golang:1.27-trixie`, `node:24-trixie-slim`. By tag, never by digest (spec 11, base images).
- The only volume is `/config`; it belongs to 65532:65532. `REMOVARR_CONFIG_DIR=/config`. Port 7272.
- Health check: `removarr healthcheck` asks `http://127.0.0.1:<REMOVARR_PORT>/api/health`, exits 0 only on 200 with `{"status":"ok"}`. Any other argument is an error (exit 2).
- The build context lists what it may use: `cmd`, `internal`, `web`, `go.mod`, `go.sum`. No `.env`, `node_modules` or built UI from the host.
- Only a published GitHub release pushes an image. Tags from `vX.Y.Z`: `X.Y.Z`, `X.Y`, `X`, `latest`; a pre-release only its own version. The smoke test runs before any push.
- Compose: named volume for `/config`, `stop_grace_period: 130s`, `extra_hosts: host.docker.internal:host-gateway`.
- No new Go module. No new frontend dependency.
- Never commit or print URLs, keys, user names, titles from the owner's library. `.env` is never read for this work.
- Commit subjects `type: what changed` in lower case (`feat`, `fix`, `docs`, `test`, `chore`); the body says why; end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Locally, build with `docker build --format docker` (the `docker` CLI is podman here): podman's default OCI format drops `HEALTHCHECK`.

## Review Focus

1. **A bind-mounted `/config` owned by root.** Expected: removarr exits at start with an error that names the config directory or database, the container does not report healthy, and the README says how to fix it. Pinned in Task 2, step 7 (manual run, result recorded in the task report).
2. **`REMOVARR_PORT` set to something other than 7272.** Expected: the health check follows it. Pinned in Task 1 (`TestHealthcheckUsesTheConfiguredPort`).
3. **A server that hangs or answers something else on the port.** Expected: the check fails within its timeout instead of reporting healthy. Pinned in Task 1 (timeout, non-200, wrong body tests).
4. **A `.env` or `node_modules` in the working tree at build time.** Expected: neither reaches any build stage. Pinned in Task 2, step 6 (context listing).
5. **The amd64 image under emulation starts slowly.** Expected: the smoke test waits long enough (up to 120 seconds for healthy) and the health check's start period covers start-up. Pinned in Task 2, step 5 (smoke test on both architectures).

---

### Task 1: The `healthcheck` command

**Files:**
- Create: `cmd/removarr/healthcheck.go`
- Create: `cmd/removarr/healthcheck_test.go`
- Modify: `cmd/removarr/main.go` (the `main` function only)

**Interfaces:**
- Consumes: `config.LoadEnv(func(string) string) (config.Env, error)`, `config.Env.Port int`, `httpjson.SameHostRedirects`.
- Produces: the command line `removarr healthcheck` (exit 0 healthy, 1 unhealthy, 2 usage error), used by the Dockerfile's `HEALTHCHECK` in Task 2. Go functions `command(args []string) (string, error)` and `healthcheck(ctx context.Context, port int, client *http.Client) error`.

- [ ] **Step 1: Write the failing tests**

`cmd/removarr/healthcheck_test.go`:

```go
package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// healthServer serves h on 127.0.0.1 and returns its port.
func healthServer(t *testing.T, h http.HandlerFunc) int {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func okHealth(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/health" || r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

func TestHealthcheckPassesWhenTheServerReportsOK(t *testing.T) {
	port := healthServer(t, okHealth)
	if err := healthcheck(context.Background(), port, &http.Client{}); err != nil {
		t.Fatalf("healthcheck = %v, want nil", err)
	}
}

func TestHealthcheckUsesTheConfiguredPort(t *testing.T) {
	port := healthServer(t, okHealth)
	env, err := loadHealthEnv(func(k string) string {
		if k == "REMOVARR_PORT" {
			return strconv.Itoa(port)
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if env != port {
		t.Fatalf("port = %d, want %d", env, port)
	}
}

func TestHealthcheckFailsWhenNothingListens(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	if err := healthcheck(context.Background(), port, &http.Client{}); err == nil {
		t.Fatal("healthcheck = nil with nothing listening")
	}
}

func TestHealthcheckFailsOnAnErrorStatus(t *testing.T) {
	port := healthServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"status":"ok"}`, http.StatusServiceUnavailable)
	})
	if err := healthcheck(context.Background(), port, &http.Client{}); err == nil {
		t.Fatal("healthcheck = nil for a 503")
	}
}

func TestHealthcheckFailsWhenTheAnswerIsNotOK(t *testing.T) {
	for _, body := range []string{`{"status":"starting"}`, `not json`, ``} {
		port := healthServer(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) })
		if err := healthcheck(context.Background(), port, &http.Client{}); err == nil {
			t.Errorf("healthcheck = nil for body %q", body)
		}
	}
}

func TestHealthcheckGivesUpOnAServerThatHangs(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	port := healthServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := healthcheck(ctx, port, &http.Client{}); err == nil {
		t.Fatal("healthcheck = nil for a server that never answers")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("healthcheck did not give up at its deadline")
	}
}

func TestCommandRunsTheServerWithoutArguments(t *testing.T) {
	got, err := command(nil)
	if err != nil || got != "serve" {
		t.Fatalf("command(nil) = %q, %v; want serve", got, err)
	}
}

func TestCommandRecognisesHealthcheck(t *testing.T) {
	got, err := command([]string{"healthcheck"})
	if err != nil || got != "healthcheck" {
		t.Fatalf("command = %q, %v; want healthcheck", got, err)
	}
}

func TestCommandRefusesAnythingElse(t *testing.T) {
	for _, args := range [][]string{{"serve"}, {"healthcheck", "extra"}, {"--help"}, {"helthcheck"}} {
		if got, err := command(args); err == nil {
			t.Errorf("command(%q) = %q, nil; want an error", args, got)
		}
	}
}
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `go test ./cmd/removarr/ -run 'Healthcheck|Command'`
Expected: build failure, `undefined: healthcheck`, `undefined: command`, `undefined: loadHealthEnv`.

- [ ] **Step 3: Write the implementation**

`cmd/removarr/healthcheck.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/removarr/removarr/internal/config"
	"github.com/removarr/removarr/internal/httpjson"
)

// healthTimeout bounds one health check. The image's HEALTHCHECK gives it longer.
const healthTimeout = 4 * time.Second

// command decides what the binary does from its arguments: "serve" without any, and
// "healthcheck" for exactly that word. Anything else is an error, so a mistyped command never
// starts a second server (spec section 11).
func command(args []string) (string, error) {
	switch {
	case len(args) == 0:
		return "serve", nil
	case len(args) == 1 && args[0] == "healthcheck":
		return "healthcheck", nil
	default:
		return "", errors.New("usage: removarr [healthcheck]")
	}
}

// loadHealthEnv returns the port removarr listens on, read from the environment as the server
// reads it.
func loadHealthEnv(getenv func(string) string) (int, error) {
	env, err := config.LoadEnv(getenv)
	if err != nil {
		return 0, err
	}
	return env.Port, nil
}

// healthcheck asks /api/health of the removarr listening on 127.0.0.1 at port, and returns
// nil only for a 200 whose status is "ok" (spec section 11). The base image has no shell or
// HTTP client, so the image's HEALTHCHECK runs this.
func healthcheck(ctx context.Context, port int, client *http.Client) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/api/health", port), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("removarr does not answer on port %d", port)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check answered status %d", resp.StatusCode)
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&body); err != nil || body.Status != "ok" {
		return errors.New("health check answered without status ok")
	}
	return nil
}

// runHealthcheck runs the healthcheck command and returns the process exit code.
func runHealthcheck() int {
	port, err := loadHealthEnv(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "removarr healthcheck:", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), healthTimeout)
	defer cancel()
	client := &http.Client{CheckRedirect: httpjson.SameHostRedirects}
	if err := healthcheck(ctx, port, client); err != nil {
		fmt.Fprintln(os.Stderr, "removarr healthcheck:", err)
		return 1
	}
	return 0
}
```

In `cmd/removarr/main.go`, replace the body of `main`:

```go
func main() {
	cmd, err := command(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if cmd == "healthcheck" {
		os.Exit(runHealthcheck())
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("removarr stopped", "error", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `go test ./cmd/removarr/` and `gofmt -l cmd internal` (prints nothing) and `go vet ./cmd/...`
Expected: PASS.

- [ ] **Step 5: Check the glue by hand**

Build and run: `go build -o "$TMPDIR/removarr-hc" ./cmd/removarr`. With nothing on 7272: `"$TMPDIR/removarr-hc" healthcheck; echo $?` prints an error and `1`. `"$TMPDIR/removarr-hc" bogus; echo $?` prints the usage and `2`. Start the fake server (`go run ./internal/e2e/fakeserver -port 7399 -dir "$TMPDIR/hc-fake"` in the background), then `REMOVARR_PORT=7399 "$TMPDIR/removarr-hc" healthcheck; echo $?` prints `0`. Stop the fake server and delete `$TMPDIR/hc-fake` and the binary. Record the three exit codes in the task report.

- [ ] **Step 6: Commit**

```bash
git add cmd/removarr
git commit -m "feat: add a healthcheck command for the container image"
```
Body: the distroless base has no shell or curl, so the image's health check needs the binary itself; unknown arguments are now refused so a typo never starts a second server.

---

### Task 2: The image and its smoke test

**Files:**
- Create: `Dockerfile`
- Create: `.dockerignore`
- Create: `scripts/smoke-image.sh` (executable)

**Interfaces:**
- Consumes: `removarr healthcheck` from Task 1; `PUT /api/settings` with `{"rules":{"graceDays":N}}` and `GET /api/settings` returning `"graceDays":N` inside `rules`; changing requests need `Content-Type: application/json` and `X-Removarr-Request: 1`.
- Produces: `scripts/smoke-image.sh IMAGE` (exit 0 when the image passes, non-zero with a message otherwise; leaves no container or volume behind). Used by `image.yml` in Task 3. `Dockerfile` at the repository root, building for `TARGETOS`/`TARGETARCH`.

- [ ] **Step 1: Write the smoke test first**

`scripts/smoke-image.sh`:

```bash
#!/usr/bin/env bash
# Smoke test for a built removarr image (spec section 12): the container reports healthy, runs
# as a non-root user, and keeps a saved setting in its volume across a restart.
#
#   scripts/smoke-image.sh IMAGE
#
# Needs docker (or podman behind the docker CLI) and curl. Leaves nothing behind.
set -euo pipefail

image=${1:?usage: scripts/smoke-image.sh IMAGE}
name=removarr-smoke-$$
volume=removarr-smoke-$$

cleanup() {
  docker rm -f "$name" >/dev/null 2>&1 || true
  docker volume rm -f "$volume" >/dev/null 2>&1 || true
}
trap cleanup EXIT

fail() {
  echo "smoke test failed: $*" >&2
  docker logs "$name" 2>&1 | tail -20 >&2 || true
  exit 1
}

wait_healthy() {
  local status
  for _ in $(seq 1 120); do
    status=$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$name" 2>/dev/null || echo gone)
    case $status in
      healthy) return 0 ;;
      none) fail "the image has no health check" ;;
      gone) fail "the container stopped" ;;
    esac
    sleep 1
  done
  fail "not healthy after 120 seconds (last status: $status)"
}

user=$(docker image inspect -f '{{.Config.User}}' "$image")
case $user in
  "" | root | 0 | 0:*) fail "the image runs as root (user '$user')" ;;
esac

docker volume create "$volume" >/dev/null
docker run -d --name "$name" -v "$volume:/config" -p 127.0.0.1::7272 "$image" >/dev/null
wait_healthy

addr=$(docker port "$name" 7272/tcp | head -1)
base="http://$addr"
curl -fsS -X PUT -H 'Content-Type: application/json' -H 'X-Removarr-Request: 1' \
  -d '{"rules":{"graceDays":17}}' "$base/api/settings" >/dev/null || fail "saving a setting was refused"

docker restart "$name" >/dev/null
wait_healthy
addr=$(docker port "$name" 7272/tcp | head -1)
curl -fsS "http://$addr/api/settings" | grep -q '"graceDays":17' || fail "the saved setting did not survive a restart"

echo "smoke test passed: $image (user $user)"
```

Make it executable: `chmod +x scripts/smoke-image.sh`.

- [ ] **Step 2: Run it to see it fail**

Run: `scripts/smoke-image.sh removarr:smoke`
Expected: FAIL, because the image does not exist (`docker image inspect` errors: no such image).

- [ ] **Step 3: Write `.dockerignore`**

```
# The build context lists what it may use (spec section 11). Everything else, in particular
# .env, stays out of every build stage.
*
!cmd/
!internal/
!web/
!go.mod
!go.sum
internal/webui/dist/*
!internal/webui/dist/.gitkeep
web/node_modules/
web/test-results/
web/playwright-report/
**/.env
**/.env.*
**/*.db
**/*.db-wal
**/*.db-shm
```

- [ ] **Step 4: Write the `Dockerfile`**

```dockerfile
# syntax=docker/dockerfile:1

# The UI and the binary are built on the build machine's own architecture; the binary is
# cross-compiled for the target, so no stage needs emulation (spec section 11).

FROM --platform=$BUILDPLATFORM node:24-trixie-slim AS web
ENV COREPACK_ENABLE_DOWNLOAD_PROMPT=0
RUN corepack enable
WORKDIR /src
COPY web/package.json web/pnpm-lock.yaml web/
RUN pnpm --dir web install --frozen-lockfile
COPY web web
# Vite writes the build into ../internal/webui/dist.
RUN mkdir -p internal/webui/dist && pnpm --dir web build

FROM --platform=$BUILDPLATFORM golang:1.27-trixie AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd cmd
COPY internal internal
COPY --from=web /src/internal/webui/dist internal/webui/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
      go build -trimpath -ldflags='-s -w' -o /out/removarr ./cmd/removarr \
 && install -d -m 0700 /out/config

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/removarr /removarr
COPY --from=build --chown=65532:65532 /out/config /config
ENV REMOVARR_CONFIG_DIR=/config
EXPOSE 7272
VOLUME ["/config"]
USER 65532:65532
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 CMD ["/removarr", "healthcheck"]
ENTRYPOINT ["/removarr"]
```

If `pnpm --dir web install` needs `pnpm-workspace.yaml` or another file from `web/` that the first COPY leaves out, add exactly that file to the first COPY and say so in the report.

The smoke test waits up to 120 seconds, and a 30-second interval would make it wait 30 seconds after start. Docker runs the first check after `--interval` unless `--start-interval` is given; check whether podman 5.7 (`docker build --format docker`) and Docker accept `--start-interval=2s`. If both do, add it; if podman rejects it, leave it out and say so in the report.

- [ ] **Step 5: Build and smoke-test both architectures**

```bash
docker build --format docker --platform linux/arm64 -t removarr:smoke-arm64 .
scripts/smoke-image.sh removarr:smoke-arm64
docker build --format docker --platform linux/amd64 -t removarr:smoke-amd64 .
scripts/smoke-image.sh removarr:smoke-amd64
docker image inspect -f '{{.Architecture}} {{.Size}}' removarr:smoke-arm64 removarr:smoke-amd64
```
Expected: both print `smoke test passed`. The amd64 one runs under emulation and is slower. Record the architectures, sizes and how long each took to become healthy in the report.

- [ ] **Step 6: Check what the build context holds**

Create a sentinel `.env` and `web/.env.local` in the worktree (content `SENTINEL=1`; both are git-ignored or must not be committed: delete them afterwards), then list the context with a throwaway build that copies it whole:

```bash
printf 'FROM docker.io/library/busybox:1.37\nCOPY . /ctx\nRUN find /ctx -maxdepth 3 | sort\n' > "$TMPDIR/ctx.Dockerfile"
docker build --no-cache -f "$TMPDIR/ctx.Dockerfile" . 2>&1 | grep -E '/ctx' | head -60
```
Expected: no `.env`, no `.env.local`, no `node_modules`, no `.git`, no `.claude`, no `docs`. Delete both sentinels and `$TMPDIR/ctx.Dockerfile`. Record the top-level entries in the report.

- [ ] **Step 7: A bind mount the user cannot write**

```bash
d=$(mktemp -d); chmod 0755 "$d"
docker run --rm -v "$d:/config" removarr:smoke-arm64; echo "exit $?"
rm -rf "$d"
```
With podman's rootless mapping the directory may come out writable; if the container starts, run it again with `--user 12345:12345` against a fresh `chmod 0555` directory so the user cannot write. Expected: removarr exits non-zero and its log names the config directory or the database. Record the exit code and the message (no paths of the host) in the report.

- [ ] **Step 8: Commit**

Remove the local images (`docker rmi removarr:smoke-arm64 removarr:smoke-amd64`).

```bash
git add Dockerfile .dockerignore scripts/smoke-image.sh
git commit -m "feat: build a multi-architecture container image"
```
Body: why distroless nonroot, cross-compilation, the listed build context, and the smoke test.

---

### Task 3: CI and the release workflow

**Files:**
- Modify: `.github/workflows/ci.yml` (the `go` job)
- Create: `.github/workflows/image.yml`

**Interfaces:**
- Consumes: `Dockerfile` and `scripts/smoke-image.sh IMAGE` from Task 2; `docker-compose.yml` from Task 4 is checked by Task 4 itself, not here.
- Produces: the job `image` on every pull request and push to `main`; on `release: published`, the same job pushes `ghcr.io/removarr/removarr` with the release's tags.

- [ ] **Step 1: Change the `go` job in `ci.yml`**

Replace its `run` steps with:

```yaml
      - name: gofmt
        run: |
          unformatted=$(gofmt -l cmd internal)
          if [ -n "$unformatted" ]; then
            echo "These files need gofmt:"
            echo "$unformatted"
            exit 1
          fi
      - run: go vet ./...
      - run: go test -race ./...
      - run: go build ./...
```
`go test -race ./...` replaces `go test ./...`: the race build runs the same tests. Check locally that `go test -race ./...` passes before relying on it; if a package outside the AGENTS.md race list fails under the race detector, stop and report it instead of narrowing the command.

- [ ] **Step 2: Write `image.yml`**

```yaml
name: image

# Builds the image for both architectures and smoke-tests it on every pull request and push to
# main. Only a published release pushes it to ghcr.io (spec section 11, releases).

on:
  pull_request:
  push:
    branches: [main]
  release:
    types: [published]

permissions:
  contents: read

env:
  IMAGE: ghcr.io/removarr/removarr

jobs:
  image:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write
    steps:
      - uses: actions/checkout@v7
      - uses: docker/setup-buildx-action@v4

      - name: Build for the smoke test
        uses: docker/build-push-action@v7
        with:
          context: .
          platforms: linux/amd64
          load: true
          tags: removarr:smoke
          cache-from: type=gha
          cache-to: type=gha,mode=max
      - name: Smoke test
        run: scripts/smoke-image.sh removarr:smoke

      - id: meta
        uses: docker/metadata-action@v6
        with:
          images: ${{ env.IMAGE }}
          tags: |
            type=semver,pattern={{version}}
            type=semver,pattern={{major}}.{{minor}}
            type=semver,pattern={{major}}
          flavor: |
            latest=auto
      - if: github.event_name == 'release'
        uses: docker/login-action@v4
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - name: Build both architectures, and push for a release
        uses: docker/build-push-action@v7
        with:
          context: .
          platforms: linux/amd64,linux/arm64
          push: ${{ github.event_name == 'release' }}
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
          cache-from: type=gha
```

The metadata action gives a pre-release only its `{{version}}` tag and no `latest`, which is what spec 11 asks for. Read the action's README for v6 (`gh api repos/docker/metadata-action/readme -H 'Accept: application/vnd.github.raw' | less`) and confirm that, and that `flavor: latest=auto` adds `latest` for a non-pre-release semver tag. Change the configuration if v6 differs, and say what you found in the report.

On a pull request `steps.meta.outputs.tags` holds no semver tag; `build-push-action` with `push: false` builds without tags. Confirm it does not fail on empty tags (README of v7); if it does, give it `tags: ${{ steps.meta.outputs.tags || 'removarr:ci' }}`.

- [ ] **Step 3: Check the YAML**

Run: `python3 -c 'import yaml,sys; [yaml.safe_load(open(f)) for f in sys.argv[1:]]' .github/workflows/ci.yml .github/workflows/image.yml` (if `yaml` is missing, use `ruby -ryaml -e 'ARGV.each{|f| YAML.load_file(f)}' ...`). If `actionlint` is installed, run it too. The workflows themselves run on the pull request; their result is checked there (step 6 of the run).

- [ ] **Step 4: Commit**

```bash
git add .github/workflows
git commit -m "chore: check formatting, races and the image in ci, and publish images for releases"
```
Body: why one workflow serves pull requests and releases (the release path is exercised on every pull request except the push), why the smoke test runs before the push.

---

### Task 4: Compose example and README for operators

**Files:**
- Create: `docker-compose.yml`
- Modify: `README.md` (restructure; keep the developer sections)
- Modify: `.github/workflows/image.yml` (one step: validate the compose file)

**Interfaces:**
- Consumes: the image and its behaviour from Tasks 1–3; spec sections 5.1, 6.1–6.7, 9, 10, 11.
- Produces: `docker-compose.yml` at the root; README sections Installing, Configuration, Reaching apps on the host, Reaching removarr by name, Behind a reverse proxy, What the lists mean, Development.

- [ ] **Step 1: Write `docker-compose.yml`**

```yaml
# removarr: suggests what to delete from a self-hosted media server.
# Run only on a network you trust. removarr has no login; never expose it to the internet.
services:
  removarr:
    image: ghcr.io/removarr/removarr:latest
    container_name: removarr
    restart: unless-stopped
    ports:
      - "7272:7272"
    volumes:
      # A named volume belongs to the image's user (uid 65532). For a directory of the host
      # instead, see "Keeping the config in a directory" in the README.
      - removarr-config:/config
    environment:
      # Host names you open removarr by, besides its IP address and localhost. Without the
      # name here a browser gets "403". List the reverse proxy's name too if you use one.
      # REMOVARR_ALLOWED_HOSTS: pi.local,removarr.example.lan
      # Start with a dry run: executing a plan then changes nothing in your apps.
      REMOVARR_DRY_RUN: "true"
      # Connections can also be set in the UI, under Settings, Connections; the UI wins.
      # Apps on this machine outside Docker are reached through host.docker.internal.
      # REMOVARR_JELLYFIN_URL: http://host.docker.internal:8096
      # REMOVARR_JELLYFIN_API_KEY: ...
      # REMOVARR_SONARR_URL: http://host.docker.internal:8989
      # REMOVARR_SONARR_API_KEY: ...
      # REMOVARR_RADARR_URL: http://host.docker.internal:7878
      # REMOVARR_RADARR_API_KEY: ...
      # REMOVARR_QBITTORRENT_URL: http://host.docker.internal:8080
      # REMOVARR_QBITTORRENT_USERNAME: ...
      # REMOVARR_QBITTORRENT_PASSWORD: ...
    extra_hosts:
      # Lets the container reach apps that run on the host itself (needed on Linux).
      - "host.docker.internal:host-gateway"
    # An execution that is running finishes its item before removarr stops; that can take
    # up to two minutes. Docker would kill removarr after 10 seconds.
    stop_grace_period: 130s

volumes:
  removarr-config:
```

- [ ] **Step 2: Validate it in CI**

Add to the `image` job in `image.yml`, right after checkout:

```yaml
      - name: Check the compose example
        run: docker compose -f docker-compose.yml config --quiet
```
Locally, run `docker compose -f docker-compose.yml config --quiet` if a compose provider is installed (with podman, `podman compose` may need `docker-compose` or `podman-compose`); if none is, parse it with the YAML check from Task 3, step 3, and say so in the report.

- [ ] **Step 3: Rewrite `README.md`**

Keep it in the style of the existing README and the spec: short sentences, concrete, no marketing. Order of sections:

1. **Title and one paragraph** (what removarr does, that it never deletes on its own). Replace the "Status: early development … There is no container image yet" line with what is true now: version 1, the image, the link to the spec.
2. **Do not expose it to the internet.** A short section near the top: no login, trusted network only; removarr listens on every interface of its container, so who can reach the port can delete. Mention `127.0.0.1:7272:7272` in `ports` to keep it on the host only.
3. **Installing.** Docker Compose with the example file (`docker compose up -d`), and the equivalent `docker run` (with `-v removarr-config:/config`, `-p 7272:7272`, `--add-host host.docker.internal:host-gateway`, `--stop-timeout 130`, `-e REMOVARR_DRY_RUN=true`). Images: `ghcr.io/removarr/removarr:<version>`, `:latest`; `linux/arm64` and `linux/amd64`. Updating: `docker compose pull && docker compose up -d`; the database migrates itself and stays in the volume.
4. **First steps.** Open `http://<address>:7272`, set the connections under Settings, Connections, choose the counting users, scan, review. Keep dry run on until a dry-run execution's history matches the plan; then remove `REMOVARR_DRY_RUN` (spec 6.8, rule 6).
5. **Configuration.** The variable table from the current README (keep its rows), plus: in the image `REMOVARR_CONFIG_DIR` is `/config` and should stay so; port, config directory, dry run and allowed hosts are environment-only; connections from the UI win over the environment (spec 9.2).
6. **Keeping the config in a directory.** A named volume needs nothing. A bind mount must be writable by uid 65532 (`sudo chown 65532:65532 ./config`), or run the container as the directory's owner with `user: "1000:1000"`. What happens otherwise: removarr stops at start with an error about the database. Back up by copying `removarr.db` while removarr is stopped.
7. **Reaching apps on the host.** Apps installed natively on the same machine: use `http://host.docker.internal:<port>`, which the `extra_hosts` line provides on Linux (Docker Desktop has it built in). The app must listen on an address the container can reach, not only `127.0.0.1` (for example Sonarr/Radarr "Bind Address" `*`, Jellyfin's default, qBittorrent's Web UI address `*`). qBittorrent's "Bypass authentication for clients on localhost" does not apply to the container: give removarr a user name and password, or add the Docker network to the whitelist. If qBittorrent's host header validation is on, add `host.docker.internal` to its server domains. Alternatives: the host's LAN address, or `network_mode: host` (then `ports` is ignored and removarr listens on the host's port 7272). Apps in other containers: put removarr on the same Docker network and use the service name.
8. **Reaching removarr by name** and **Behind a reverse proxy.** Keep the two existing sections, and add the compose form (`REMOVARR_ALLOWED_HOSTS: pi.local`) and, for a proxy, an nginx line `proxy_set_header Host $host;` (Caddy and Traefik pass Host on by default). Say that the health check at `/api/health` answers under any name, so a monitor can use the container name.
9. **Health check.** The image checks itself every 30 seconds with `removarr healthcheck`; `docker ps` shows `healthy`. A monitor can call `GET /api/health`.
10. **Stopping.** Keep the paragraph about the two-minute grace, now pointing at `stop_grace_period: 130s` and `--stop-timeout 130`.
11. **What the lists mean.** From spec 5.1 and 6.1–6.7, in a few lines each: Movies (watched by the counting users, past the grace period, not protected); Series (whole series or per season as chosen; a still-airing season waits; specials); Torrents (orphans: in qBittorrent but no longer behind any library file, with seeding state). Then **Limits of linking torrents to files** (spec 6.6): removarr links a torrent to library files through Sonarr's and Radarr's history and, where history is silent, by exact file size (files of at least 50 MB); it never reads the disk, so it cannot see hardlinks; a link by size alone is marked in the plan; a manually imported file has no history; a torrent goes as a whole or stays (spec 2); unsure means unwatched and unsure means keep. Read spec 6.6 and its "Limits" before writing this, and do not claim more than it says.
12. **Development** (the existing sections: Development, Building, Developing the UI, Trying the API), plus **Building the image**: `docker build -t removarr .`, `scripts/smoke-image.sh removarr`, and with podman `--format docker` so the health check is kept.
13. **Releasing** (for maintainers): move "Unreleased" in `CHANGELOG.md` under the version with its date on `main`; publish a GitHub release with tag `vX.Y.Z`; the `image` workflow tests and pushes the image; check that `ghcr.io/removarr/removarr:X.Y.Z` pulls on both architectures. Before the first release: the manual pass on the reference stack with dry run on, then off (spec 12); make the repository public, and after the first push make the package public on ghcr.io (spec 11).
14. **License:** MIT, see `LICENSE`.

Never put real host names, addresses or keys from anyone's setup in the README; use `pi.local`, `removarr.example.lan` and `...`.

- [ ] **Step 4: Check the README against the code**

For every variable, default, path, port and command in the README, find where the code or the compose file defines it (`internal/config/env.go`, `Dockerfile`, `docker-compose.yml`, `cmd/removarr`). Fix any difference in the README. List the ones you checked in the report.

- [ ] **Step 5: Commit**

```bash
git add docker-compose.yml README.md .github/workflows/image.yml
git commit -m "docs: add a compose example and the operator's readme"
```
