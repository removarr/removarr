# removarr stage 3: web UI — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** All pages of removarr's web UI (Movies, Series, Torrents, History, Settings) on top of the finished API, built to static files and embedded in the Go binary.

**Architecture:** Two small server changes come first: a scan records a fingerprint of the settings so `GET /api/status` can say the lists are out of date, and a new `internal/webui` package serves the embedded build at every path outside `/api/`. The UI lives in `web/` (Vite, React, TypeScript, Tailwind CSS, shadcn/ui, TanStack Query, TanStack Table, React Router). It talks to the HTTP API only, through one client module that adds the request header and turns error bodies into messages. An end-to-end test drives the real server, wired to in-memory fake providers, with Playwright.

**Tech Stack:** Go 1.27 standard library; Node 24, pnpm 12.20.0; Vite 8.3.8, React 19.3.1, TypeScript 7.0.2, Tailwind CSS 4.3.5, shadcn 4.23.0 (CLI) with radix-ui 1.8.1, TanStack Query 5.107.1, TanStack Table 9.3.1, React Router 8.5.1, Vitest 5.1.0, Testing Library, jsdom 31.0.0, Playwright 1.65.0.

**Spec:** `docs/superpowers/specs/2026-11-18-removarr-design.md`, sections 5, 6.5, 6.7, 6.10, 6.11, 7.3, 7.4, 7.6, 7.7, 8, 9.2, 10, 12 and 15. Issue #6.

## Global Constraints

- Go: 1.27 and the standard library; `modernc.org/sqlite` is the only third-party Go module. No new Go module.
- Frontend packages are exactly those listed under "Pinned versions" below. Adding any other npm package is the owner's decision: stop and report instead.
- Every version is pinned exactly in `web/package.json` (no `^` or `~`), and `web/pnpm-lock.yaml` is committed.
- The UI talks to the HTTP API only (spec 7.1). Every request that changes something sends `X-Removarr-Request: 1` and `Content-Type: application/json` (spec 10).
- A request body that names rows carries only `list` and `key` per row: the server decodes with `DisallowUnknownFields` and refuses anything else.
- Secrets are write-only (spec 10): the UI never shows a secret and never sends a connection field the user did not change. Sending `null` or `""` for a connection field clears it (spec 9.2).
- Sizes are labelled "size", never "space freed" (spec 6.9). Use IEC units (KiB, MiB, GiB, TiB) with one decimal.
- A watched date that is absent shows as "unknown" (spec 6.1).
- Nothing in this stage changes what removarr sends to the apps. The fake server of task 11 never opens a network connection to an app.
- Commit subjects: `type: what changed`, lower case, one of `feat`, `fix`, `docs`, `test`, `chore`. Bodies say why. End every commit message with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Never print or commit real URLs, keys, user names or titles from anyone's library.

### Pinned versions

Each is the newest release at least two weeks old on 2026-11-26.

| Package | Version | Kind |
|---|---|---|
| pnpm | 12.20.0 | `packageManager` |
| vite | 8.3.8 | dev |
| @vitejs/plugin-react | 6.3.0 | dev |
| typescript | 7.0.2 | dev |
| tailwindcss | 4.3.5 | dev |
| @tailwindcss/vite | 4.3.5 | dev |
| react, react-dom | 19.3.1 | runtime |
| @types/react, @types/react-dom | 19.3.0 | dev |
| @types/node | 26.6.6 | dev |
| @tanstack/react-query | 5.107.1 | runtime |
| @tanstack/react-table | 9.3.1 | not used (task 5) |
| react-router | 8.5.1 | runtime |
| radix-ui | 1.8.1 | runtime (shadcn/ui) |
| lucide-react | 1.72.0 | runtime (shadcn/ui icons) |
| class-variance-authority | 0.7.1 | runtime (shadcn/ui) |
| clsx | 2.1.1 | runtime (shadcn/ui) |
| tailwind-merge | 3.7.0 | runtime (shadcn/ui) |
| tw-animate-css | 1.4.0 | dev (shadcn/ui) |
| shadcn | 4.23.0 | run with `pnpm dlx shadcn@4.23.0`, not a dependency |
| vitest | 5.1.0 | dev |
| jsdom | 31.0.0 | dev |
| @testing-library/react | 16.3.3 | dev |
| @testing-library/user-event | 14.6.10 | dev |
| @testing-library/jest-dom | 7.0.1 | dev |
| @playwright/test | 1.65.0 | dev |

TanStack Table 9 and React Router 8 are newer than most examples on the web. Read their type definitions in `web/node_modules` before using an API; do not write version 8 or version 7 code from memory.

## Review Focus

- **A bulk action on rows the user cannot see.** Select all, then narrow the filter, or a scan reloads the list: Delete and Ignore must name only rows that are visible and still in the list (spec 5.3). Task 5 tests both.
- **Saving settings clears a secret.** A PUT that sends a connection field the user did not touch, as `null` or `""`, deletes the saved value. Saving a changed URL must send that URL only. Task 10 tests it.
- **A plan that cannot run any more.** Execute answers 410 for an expired plan and 404 for one executed or discarded. The dialog must offer to build it again from the same rows and must never execute anything without a new confirmation. Task 8 tests both.
- **The server is busy.** Scan, plan building and execution refuse each other with 409 and the name of the running job. The UI shows that message and does not retry on its own. Tasks 4 and 8 test it.
- **A list that could not be produced.** When the latest scan has an error for a list, the page shows the error, no rows and no bulk actions (spec 6.8, rule 5). Task 6 tests it.

---

## File structure

| Path | Responsibility |
|---|---|
| `internal/config/fingerprint.go` | `Service.Fingerprint`: hash of the rules and of each app's URL and user name |
| `internal/domain/summary.go` | `ScanSummary.SettingsFingerprint` |
| `internal/scanner/scanner.go` | records the fingerprint when a scan starts |
| `internal/api/scans.go` | `stale` in the status response |
| `internal/webui/webui.go` | embeds `dist`, serves files, falls back to `index.html`, placeholder page |
| `internal/webui/dist/.gitkeep` | keeps the embed pattern valid before the UI is built |
| `internal/api/server.go` | routes `/` to the UI; refused host outside `/api/` gets an HTML page |
| `cmd/removarr/main.go` | wires the fingerprint and the UI |
| `web/` | the Vite project |
| `web/go.mod` | keeps the go tool out of `web/node_modules` |
| `web/src/api/types.ts`, `client.ts`, `queries.ts` | API types, fetch wrapper, TanStack Query hooks |
| `web/src/lib/format.ts` | sizes, dates, ratios |
| `web/src/lib/selection.ts` | pure selection rules |
| `web/src/components/header/*` | header: scan button, summary, free space |
| `web/src/components/list/*` | the list: table, cards, filter, sort, bulk bar |
| `web/src/components/rows/*` | per-list columns and cards |
| `web/src/components/delete/*` | plan dialog and outcome |
| `web/src/pages/*` | Movies, Series, Torrents, History, Settings |
| `internal/e2e/fakeserver/` | the real API server on fake providers, for the end-to-end test |
| `web/e2e/*.spec.ts` | the Playwright test |

---

### Task 1: Settings fingerprint and out-of-date lists

**Files:**
- Create: `internal/config/fingerprint.go`, `internal/config/fingerprint_test.go`
- Modify: `internal/domain/summary.go`, `internal/scanner/scanner.go`, `internal/scanner/scanner_test.go`, `internal/api/scans.go`, `internal/api/scans_test.go` (or the status tests wherever they live), `cmd/removarr/main.go`

**Interfaces:**
- Produces: `func (s *Service) Fingerprint(ctx context.Context) (string, error)` in `internal/config`; `ScanSummary.SettingsFingerprint string` with JSON name `settingsFingerprint,omitempty`; `scanner.Service.Fingerprint func(ctx context.Context) (string, error)` (nil means none is recorded); `GET /api/status` gains `"stale": bool`.

Spec 5.4, "Out of date", and 7.6. The fingerprint covers the rules and, per app, the URL and the user name as `ConnView` reports them (UI over environment). It never covers a secret. Do not hold `s.mu` while calling `Rules` or `ConnView`: both lock it, and a recursive read lock deadlocks when a writer waits.

- [ ] **Step 1: Write the failing fingerprint tests** in `internal/config/fingerprint_test.go`. Use the existing test helpers of the package for a `Service` over a temporary store (read `settings_test.go` for them).

```go
func TestFingerprintChangesWithRulesAndAddresses(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t, Env{}) // the package's helper; adapt the name
	base, err := svc.Fingerprint(ctx)
	if err != nil || base == "" {
		t.Fatalf("Fingerprint() = %q, %v", base, err)
	}

	rules := domain.DefaultRules()
	rules.GraceDays = 7
	if err := svc.SetRules(ctx, rules); err != nil {
		t.Fatal(err)
	}
	afterRules, _ := svc.Fingerprint(ctx)
	if afterRules == base {
		t.Error("changing a rule did not change the fingerprint")
	}

	url, _ := FieldFor(AppSonarr, "url")
	if err := svc.SetConn(ctx, url, "http://192.0.2.1:8989"); err != nil {
		t.Fatal(err)
	}
	afterURL, _ := svc.Fingerprint(ctx)
	if afterURL == afterRules {
		t.Error("changing a URL did not change the fingerprint")
	}

	user, _ := FieldFor(AppQBittorrent, "username")
	if err := svc.SetConn(ctx, user, "someone"); err != nil {
		t.Fatal(err)
	}
	afterUser, _ := svc.Fingerprint(ctx)
	if afterUser == afterURL {
		t.Error("changing a user name did not change the fingerprint")
	}
}

func TestFingerprintLeavesSecretsOut(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t, Env{})
	before, _ := svc.Fingerprint(ctx)
	key, _ := FieldFor(AppSonarr, "apiKey")
	if err := svc.SetConn(ctx, key, "0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	after, _ := svc.Fingerprint(ctx)
	if after != before {
		t.Error("a secret changed the fingerprint")
	}
}

func TestFingerprintIsStable(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t, Env{})
	a, _ := svc.Fingerprint(ctx)
	b, _ := svc.Fingerprint(ctx)
	if a != b {
		t.Error("two reads of unchanged settings differ")
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/config/ -run Fingerprint`. Expected: FAIL, `svc.Fingerprint undefined`.

- [ ] **Step 3: Implement** `internal/config/fingerprint.go`:

```go
package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Fingerprint identifies the settings a scan depends on, so the UI can tell when the stored
// lists were computed with other settings (spec 5.4). It covers the rules and each app's URL
// and user name, as resolved from the UI and the environment. Secrets are left out: they are
// write-only, and a new key for the same app does not change what the app answers.
//
// The rules and the connections are read one after the other. A change in between gives a
// fingerprint that matches neither state, so the lists are reported out of date: the safe
// direction.
func (s *Service) Fingerprint(ctx context.Context) (string, error) {
	rules, err := s.Rules(ctx)
	if err != nil {
		return "", err
	}
	conns, err := s.ConnView(ctx)
	if err != nil {
		return "", err
	}
	apps := map[string]map[string]string{}
	for app, fields := range conns {
		apps[app] = map[string]string{}
		for name, f := range fields {
			if f.Value != "" { // ConnView never sets Value for a secret
				apps[app][name] = f.Value
			}
		}
	}
	// encoding/json sorts map keys, so equal settings give equal bytes.
	raw, err := json.Marshal(struct {
		Rules any                          `json:"rules"`
		Apps  map[string]map[string]string `json:"apps"`
	}{rules, apps})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
```

- [ ] **Step 4: Run** `go test ./internal/config/`. Expected: PASS.

- [ ] **Step 5: Write the failing scanner test** in `internal/scanner/scanner_test.go`, using the package's existing fake store and providers (read the file first). It must check that the fingerprint is taken before anything else of the scan runs and that it reaches the stored summary:

```go
func TestScanRecordsTheSettingsFingerprintFromItsStart(t *testing.T) {
	// Arrange a Service the way the other tests in this file do, then:
	var calls []string
	svc.Fingerprint = func(context.Context) (string, error) { calls = append(calls, "fingerprint"); return "fp-1", nil }
	rules := svc.Rules
	svc.Rules = func(ctx context.Context) (domain.Rules, error) { calls = append(calls, "rules"); return rules(ctx) }
	// run a scan to its end, as the other tests do
	// assert: calls[0] == "fingerprint", and the summary passed to FinishScan has SettingsFingerprint == "fp-1"
}

func TestScanFailsWhenTheFingerprintCannotBeRead(t *testing.T) {
	// Fingerprint returns errors.New("boom"): the scan ends with an error and FinishScan is not called,
	// as for a failure to load the rules.
}
```

Write both in full in the style of the file.

- [ ] **Step 6: Run** `go test ./internal/scanner/ -run Fingerprint`. Expected: FAIL, no field `Fingerprint`.

- [ ] **Step 7: Implement.** In `internal/domain/summary.go` add to `ScanSummary`:

```go
	// SettingsFingerprint identifies the settings the scan started with (spec 5.4).
	SettingsFingerprint string `json:"settingsFingerprint,omitempty"`
```

In `scanner.Service` add the field `Fingerprint func(ctx context.Context) (string, error) // identifies the settings at the start; nil records none`. In `run`, first thing, before `s.Rules`:

```go
	var fingerprint string
	if s.Fingerprint != nil {
		fp, err := s.Fingerprint(ctx)
		if err != nil {
			return fmt.Errorf("load settings fingerprint: %w", err)
		}
		fingerprint = fp
	}
```

and after `summary, candidates := Compute(...)`: `summary.SettingsFingerprint = fingerprint`. In `cmd/removarr/main.go` `newJobs`, set `Fingerprint: cfg.Fingerprint` on the scanner.

- [ ] **Step 8: Run** `go test ./internal/scanner/ ./cmd/...`. Expected: PASS.

- [ ] **Step 9: Write the failing status tests** through the harness in `internal/api/server_test.go` (read how it builds a `Server`, a store with a scan, and a config service):

```go
func TestStatusReportsListsOutOfDateWhenSettingsChangedSinceTheScan(t *testing.T) {
	// a finished scan whose summary has SettingsFingerprint equal to the current Config.Fingerprint:
	//   GET /api/status → "stale": false
	// change a rule through PUT /api/settings (graceDays 3):
	//   GET /api/status → "stale": true
}

func TestStatusDoesNotCallListsOutOfDateWithoutAFingerprint(t *testing.T) {
	// a scan from before this change (empty SettingsFingerprint) → "stale": false
	// no scan at all → "stale": false
}
```

Write both in full.

- [ ] **Step 10: Run** `go test ./internal/api/ -run Status`. Expected: FAIL, no `stale` in the body.

- [ ] **Step 11: Implement.** In `internal/api/scans.go` add `Stale bool \`json:"stale"\`` to `statusResponse` with a comment pointing to spec 5.4. In `status`, after `resp.LastScan = v.rec`:

```go
	if v.rec != nil && v.rec.Summary.SettingsFingerprint != "" {
		current, err := s.Config.Fingerprint(r.Context())
		if err != nil {
			s.fail(w, err)
			return
		}
		resp.Stale = current != v.rec.Summary.SettingsFingerprint
	}
```

- [ ] **Step 12: Run the checks** of `AGENTS.md` (gofmt, vet, test, race, build). Expected: all pass. Then break it on purpose once: make `Fingerprint` also record `f.Set` for every field, which changes when a secret is saved, and check that `TestFingerprintLeavesSecretsOut` fails; revert. Then remove the line `summary.SettingsFingerprint = fingerprint` and check that the scanner test fails; revert.

- [ ] **Step 13: Commit** `feat: report when the lists were computed with other settings`.

---

### Task 2: Serve the embedded UI

**Files:**
- Create: `internal/webui/webui.go`, `internal/webui/webui_test.go`, `internal/webui/dist/.gitkeep` (empty)
- Modify: `internal/api/server.go`, `internal/api/server_test.go`, `cmd/removarr/main.go`, `.gitignore`

**Interfaces:**
- Produces: `webui.Files() fs.FS` (the embedded build, rooted at `dist`); `webui.Handler(files fs.FS) http.Handler`; `api.Server.UI http.Handler` (nil: no UI routes). Task 3 builds into `internal/webui/dist`.

Spec 7.7 "Serving" and section 10. Behaviour of `webui.Handler`:
- Only GET and HEAD; anything else gets 405 with `Allow: GET, HEAD`.
- A path that names a file in `files` serves it (`http.FileServerFS` semantics, no directory listings). Files under `assets/` get `Cache-Control: public, max-age=31536000, immutable`; everything else `Cache-Control: no-cache`.
- Any other path serves `index.html` with status 200 and `no-cache`, so `/series` can be reloaded.
- When `files` holds no `index.html`, every path gets status 200 and a small HTML page: "The web UI is not part of this build. Build it with `pnpm --dir web build` and build removarr again." with `Content-Type: text/html; charset=utf-8`.

In `internal/api`: the mux keeps its `/api/...` routes; add `mux.Handle("/api/", ...)` answering JSON 404 `{"error":"not found"}` so an unknown API path never gets the UI's page; register `mux.Handle("/", s.UI)` when `s.UI != nil`. In `guard`, a refused host for a path that does not start with `/api/` gets 403 with `Content-Type: text/html; charset=utf-8` and a page holding the same message as the JSON answer (HTML-escaped with `html.EscapeString`). The health check exemption is unchanged.

- [ ] **Step 1: Write the failing webui tests** with `testing/fstest.MapFS`:

```go
package webui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

var built = fstest.MapFS{
	"index.html":       {Data: []byte("<!doctype html><title>removarr</title>")},
	"assets/app-1a.js": {Data: []byte("console.log(1)")},
}

func get(t *testing.T, h http.Handler, method, path string) *http.Response {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec.Result()
}

func body(t *testing.T, r *http.Response) string {
	b, _ := io.ReadAll(r.Body)
	return string(b)
}

func TestServesAFileWithLongCachingForAssets(t *testing.T) {
	r := get(t, Handler(built), "GET", "/assets/app-1a.js")
	if r.StatusCode != 200 || body(t, r) != "console.log(1)" {
		t.Fatalf("got %d %q", r.StatusCode, body(t, r))
	}
	if cc := r.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %q", cc)
	}
}

func TestServesTheIndexForAPathTheUIHandlesItself(t *testing.T) {
	for _, p := range []string{"/", "/series", "/settings/connections"} {
		r := get(t, Handler(built), "GET", p)
		if r.StatusCode != 200 || !strings.Contains(body(t, r), "<title>removarr</title>") {
			t.Errorf("%s: got %d", p, r.StatusCode)
		}
		if cc := r.Header.Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("%s: Cache-Control = %q", p, cc)
		}
	}
}

func TestRefusesMethodsOtherThanGetAndHead(t *testing.T) {
	r := get(t, Handler(built), "POST", "/")
	if r.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("got %d", r.StatusCode)
	}
}

func TestSaysSoWhenTheUIWasNotBuilt(t *testing.T) {
	r := get(t, Handler(fstest.MapFS{".gitkeep": {}}), "GET", "/movies")
	if r.StatusCode != 200 || !strings.Contains(body(t, r), "pnpm --dir web build") {
		t.Fatalf("got %d %q", r.StatusCode, body(t, r))
	}
}

func TestEmbeddedFilesAreRootedAtDist(t *testing.T) {
	if _, err := fs.Stat(Files(), ".gitkeep"); err != nil {
		t.Fatalf("Files() is not rooted at dist: %v", err)
	}
}
```

(Add `io/fs` to the imports.)

- [ ] **Step 2: Run** `go test ./internal/webui/`. Expected: FAIL, package has no `Handler`.

- [ ] **Step 3: Implement** `internal/webui/webui.go`:

```go
// Package webui serves the browser UI, built into dist by `pnpm --dir web build` and
// embedded in the binary (spec 7.7).
package webui

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var dist embed.FS

// Files returns the embedded build, rooted at dist.
func Files() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // dist is embedded at compile time
	}
	return sub
}

const notBuilt = `<!doctype html><meta charset="utf-8"><title>removarr</title>
<p>The web UI is not part of this build. Build it with <code>pnpm --dir web build</code> and build removarr again.</p>`

// Handler serves files, and index.html for any path that names no file, so a page of the UI
// can be reloaded.
func Handler(files fs.FS) http.Handler {
	index, err := fs.ReadFile(files, "index.html")
	built := err == nil
	fileServer := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !built {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(notBuilt))
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name != "" && name != "index.html" {
			if st, err := fs.Stat(files, name); err == nil && !st.IsDir() {
				if strings.HasPrefix(name, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "no-cache")
				}
				fileServer.ServeHTTP(w, r)
				return
			} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(index)
	})
}
```

Create the empty file `internal/webui/dist/.gitkeep`. In `.gitignore` add:

```
/internal/webui/dist/*
!/internal/webui/dist/.gitkeep
/web/node_modules/
/web/test-results/
/web/playwright-report/
```

- [ ] **Step 4: Run** `go test ./internal/webui/`. Expected: PASS.

- [ ] **Step 5: Write the failing API tests** in `internal/api/server_test.go`, through the harness:

```go
func TestServesTheUIOutsideTheAPI(t *testing.T) {
	// Server with UI = webui.Handler(fstest.MapFS{"index.html": {Data: []byte("UI")}})
	// GET /movies with Host 127.0.0.1:7272 → 200, body "UI"
}

func TestAnUnknownAPIPathIsAJSONNotFoundAndNeverTheUI(t *testing.T) {
	// GET /api/nope → 404, Content-Type application/json, body has "error"
}

func TestARefusedHostGetsAReadablePageOutsideTheAPI(t *testing.T) {
	// GET /movies with Host evil.example → 403, Content-Type text/html; charset=utf-8,
	// body contains "REMOVARR_ALLOWED_HOSTS"
	// GET /api/status with Host evil.example → 403, JSON as before
}

func TestTheUIIsNotServedForChangingMethods(t *testing.T) {
	// POST / with the request header and JSON content type → 405
}
```

Write them in full.

- [ ] **Step 6: Run** `go test ./internal/api/`. Expected: the new tests FAIL.

- [ ] **Step 7: Implement** the routes and the guard change in `internal/api/server.go` as described above. The HTML page:

```go
const refusedHostMessage = "this server does not answer to the host name in the request; reach it by its IP address, or list the name in REMOVARR_ALLOWED_HOSTS"
```

Use the constant for both answers. For a path outside `/api/`:

```go
w.Header().Set("Content-Type", "text/html; charset=utf-8")
w.WriteHeader(http.StatusForbidden)
fmt.Fprintf(w, "<!doctype html><meta charset=\"utf-8\"><title>removarr</title><p>%s</p>", html.EscapeString(refusedHostMessage))
```

In `cmd/removarr/main.go` set `UI: webui.Handler(webui.Files())` on the `api.Server`.

- [ ] **Step 8: Run the checks.** Expected: all pass. Break it on purpose: drop the `/api/` 404 route and check that `TestAnUnknownAPIPathIsAJSONNotFoundAndNeverTheUI` fails; revert.

- [ ] **Step 9: Commit** `feat: serve the embedded web ui outside the api`.

---

### Task 3: The web project, the API client and the app shell

**Files:**
- Create: `web/package.json`, `web/pnpm-lock.yaml`, `web/go.mod`, `web/tsconfig.json`, `web/tsconfig.app.json`, `web/tsconfig.node.json`, `web/vite.config.ts`, `web/vitest.setup.ts`, `web/index.html`, `web/components.json`, `web/src/main.tsx`, `web/src/App.tsx`, `web/src/index.css`, `web/src/lib/utils.ts`, `web/src/components/ui/*` (shadcn), `web/src/api/types.ts`, `web/src/api/client.ts`, `web/src/api/client.test.ts`, `web/src/lib/format.ts`, `web/src/lib/format.test.ts`, `web/src/lib/theme.ts`, `web/src/pages/*.tsx` (placeholders), `web/src/components/layout/Shell.tsx`
- Modify: `.github/workflows/ci.yml`, `AGENTS.md`

**Interfaces:**
- Produces:
  - `web/src/api/types.ts` exactly as below.
  - `web/src/api/client.ts`: `class ApiError extends Error { status: number; field?: string }`; `api<T>(path: string, init?: { method?: string; body?: unknown }): Promise<T>` (undefined for 204); `rowsBody(rows: { list: ListKind; key: string }[])`.
  - `web/src/lib/format.ts`: `formatSize(bytes: number): string`, `formatDate(iso?: string | null): string` ("unknown" for absent), `formatDateTime(iso?: string | null): string`, `formatRatio(r: number | null): string` ("–" for null), `formatDays(d: number | null): string`.
  - Routes: `/` redirects to `/movies`; `/movies`, `/series`, `/torrents`, `/history`, `/settings`; `Shell` renders the header slot, the navigation and an `<Outlet />`.
  - A `QueryClient` with `retry: false` for mutations and `retry: 1` for queries, created in `main.tsx` and exported from `web/src/api/queryClient.ts` as `makeQueryClient()` so tests can build their own.

- [ ] **Step 1: Create the project.** In `web/`:
  - `package.json` with `"name": "removarr-web"`, `"private": true`, `"type": "module"`, `"packageManager": "pnpm@12.20.0"`, the exact versions of "Pinned versions", and scripts:

    ```json
    {
      "dev": "vite",
      "build": "tsc -b && vite build",
      "typecheck": "tsc -b --noEmit",
      "test": "vitest run",
      "e2e": "playwright test"
    }
    ```

    (If TypeScript 7 rejects `-b --noEmit`, use `tsc -p tsconfig.app.json --noEmit` and record that in the task report.)
  - `go.mod` containing only:

    ```
    // This module exists so that `go build ./...` and `go vet ./...` in the repository root
    // skip web/, whose node_modules can hold Go files of npm packages.
    module removarr.invalid/web

    go 1.27
    ```
  - `vite.config.ts`:

    ```ts
    import { defineConfig } from "vite";
    import react from "@vitejs/plugin-react";
    import tailwindcss from "@tailwindcss/vite";
    import path from "node:path";
    import fs from "node:fs";

    const outDir = path.resolve(__dirname, "../internal/webui/dist");

    export default defineConfig({
      plugins: [
        react(),
        tailwindcss(),
        {
          // Vite empties outDir; the Go embed pattern needs the placeholder to stay.
          name: "keep-gitkeep",
          closeBundle() {
            fs.writeFileSync(path.join(outDir, ".gitkeep"), "");
          },
        },
      ],
      resolve: { alias: { "@": path.resolve(__dirname, "src") } },
      build: { outDir, emptyOutDir: true },
      server: {
        // The Host header stays localhost:5173, which removarr accepts, and the Origin
        // matches it (spec 10). Start removarr on 7272 first.
        proxy: { "/api": { target: "http://127.0.0.1:7272", changeOrigin: false } },
      },
      test: {
        environment: "jsdom",
        setupFiles: ["./vitest.setup.ts"],
        include: ["src/**/*.test.{ts,tsx}"],
      },
    });
    ```
  - `vitest.setup.ts` imports `@testing-library/jest-dom/vitest`, and defines `window.matchMedia` (jsdom has none) as a stub that reports `matches: true` for `(min-width: 768px)` unless a test overrides it, and calls `cleanup()` after each test.
  - Run `pnpm install`, then `pnpm dlx shadcn@4.23.0 init` (Tailwind 4, `src/index.css`, alias `@/components`, style "new-york", base colour "neutral"). Add the components the later tasks use: `pnpm dlx shadcn@4.23.0 add button checkbox input switch dialog badge table card select label separator tooltip`. Check that `package.json` gained no package outside "Pinned versions"; pin any version shadcn wrote with `^` to the exact version from the table. If shadcn insists on a package that is not in the table, stop and report it.

- [ ] **Step 2: Write `web/src/api/types.ts`**, mirroring the Go JSON exactly:

```ts
export type ListKind = "movies" | "series" | "torrents";
export type AppName = "jellyfin" | "sonarr" | "radarr" | "qbittorrent";

export interface RowRef { list: ListKind; key: string; title: string; season?: number }

export interface TorrentInfo {
  id: string; name: string; size: number; state: string; category: string; tracker: string;
  ratio: number | null; seedingDays: number | null; addedAt: string; belowSeedMin: boolean;
  outsideLibrary?: number; // bytes not in the library; only set when 50 MB or more (spec 6.7)
}

export interface Candidate {
  list: ListKind; key: string; title: string; year?: number; size: number;
  scope?: "series" | "season"; season?: number; stillAiring?: boolean;
  watchedBy?: string[]; watchedAt?: string; reason?: "orphan" | "unlinked";
  torrents: TorrentInfo[] | null; belowSeedMin: boolean; tiedWith?: RowRef[];
  waiting?: boolean; waitReason?: string; ignoredAt?: string; notInScan?: boolean; merged?: boolean;
}

export interface CandidatesResponse { list: ListKind; scannedAt: string | null; error?: string; candidates: Candidate[] | null }

export interface ListSummary { candidates: number; protected: number; unmatched: number; waiting: number; error?: string }
export interface RootFolder { path: string; freeBytes: number }
export interface ScanSummary { lists: Partial<Record<ListKind, ListSummary>>; rootFolders: RootFolder[] | null; settingsFingerprint?: string }
export interface ScanRecord { id: number; startedAt: string; finishedAt: string | null; status: string; summary: ScanSummary }
export interface ScanStatus { running: boolean; phase?: string; startedAt?: string; lastError?: string }
export interface Status {
  dryRun: boolean; connections: Record<AppName, { configured: boolean }>;
  scan: ScanStatus; lastScan: ScanRecord | null; stale: boolean;
}
export interface ScanCurrent { status: ScanStatus; last: ScanRecord | null }

export type StepAction = "remove-movie" | "remove-series" | "delete-episode-files" | "unmonitor-episodes" | "unmonitor-seasons";
export interface ManagerStep {
  manager: "movies" | "series"; action: StepAction; rowKey: string; itemId: number;
  fileIds?: number[]; episodeIds?: number[]; seasons?: number[]; addExclusion?: boolean;
}
export interface PlanRow { row: Candidate; added?: boolean; addedWith?: RowRef; shares?: string[]; notInScan?: boolean }
export interface PlanTorrent { torrent: TorrentInfo; keepData?: boolean; linkedBySize?: boolean }
export interface KeptTorrent { torrent: TorrentInfo; reason: "queued" | "backs-library" }
export interface PlanItem { rows: PlanRow[]; steps: ManagerStep[] | null; torrents: PlanTorrent[] | null; kept: KeptTorrent[] | null }
export interface SkippedRow { row: RowRef; reason: string }
export interface Plan { id: string; createdAt: string; expiresAt: string; items: PlanItem[] | null; skipped: SkippedRow[] | null }

export type Outcome = "done" | "failed" | "dry-run";
export type StepResultKind = "done" | "failed" | "skipped" | "gone" | "dry-run";
export interface ActionRow { list: ListKind; key: string; title: string; season?: number; size: number; added?: boolean }
export interface StepResult { manager?: ManagerStep; torrent?: TorrentInfo; keepData?: boolean; result: StepResultKind; error?: string }
export interface Action { id: number; planId: string; at: string; dryRun: boolean; outcome: Outcome; size: number; rows: ActionRow[]; steps: StepResult[] }
export interface ExecuteResult { planId: string; dryRun: boolean; actions: Action[] }
export interface ActionsPage { actions: Action[]; next?: number }

export type Source = "ui" | "env" | "none";
export interface FieldView { value?: string; set: boolean; source: Source }
export interface Rules {
  countingUsers: string[]; watchedMode: "all" | "any"; graceDays: number; seriesUnit: "series" | "season";
  protectFavorites: boolean; protectionTag: string; minRatio: number; minSeedDays: number;
  excludedCategories: string[]; excludedTags: string[]; addExclusionOnDelete: boolean;
}
export interface Settings { rules: Rules; connections: Record<AppName, Record<string, FieldView>>; dryRun: boolean }
export interface MediaUser { id: string; name: string }
export interface TestResult { ok: boolean; detail?: string; error?: string }
```

Check every name against the Go types it mirrors (`internal/domain`, `internal/api`, `internal/scanner`, `internal/store`, `internal/config`) and correct this file where they differ; record any correction in the task report. Check `domain.WatchedAll`, `WatchedAny`, `UnitSeries`, `UnitSeason` for the literal values.

- [ ] **Step 3: Write the failing client tests** `web/src/api/client.test.ts`:

```ts
import { afterEach, describe, expect, it, vi } from "vitest";
import { api, ApiError, rowsBody } from "./client";

function mockFetch(status: number, body?: unknown) {
  const fn = vi.fn().mockResolvedValue(
    new Response(body === undefined ? null : JSON.stringify(body), {
      status,
      headers: body === undefined ? {} : { "Content-Type": "application/json" },
    }),
  );
  vi.stubGlobal("fetch", fn);
  return fn;
}

afterEach(() => vi.unstubAllGlobals());

describe("api", () => {
  it("sends the request header and JSON on a request that changes something", async () => {
    const fetch = mockFetch(204);
    await api("/api/ignores", { method: "POST", body: { rows: [] } });
    const [, init] = fetch.mock.calls[0];
    expect(init.method).toBe("POST");
    expect(new Headers(init.headers).get("X-Removarr-Request")).toBe("1");
    expect(new Headers(init.headers).get("Content-Type")).toBe("application/json");
    expect(init.body).toBe('{"rows":[]}');
  });

  it("returns undefined for 204 and the parsed body otherwise", async () => {
    mockFetch(204);
    expect(await api("/api/ignores", { method: "DELETE", body: {} })).toBeUndefined();
    mockFetch(200, { status: "ok" });
    expect(await api("/api/health")).toEqual({ status: "ok" });
  });

  it("turns an error body into an ApiError with the server's message and field", async () => {
    mockFetch(400, { error: "must be at least 0", field: "rules.graceDays" });
    const err = await api("/api/settings", { method: "PUT", body: {} }).catch((e) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(400);
    expect(err.message).toBe("must be at least 0");
    expect(err.field).toBe("rules.graceDays");
  });

  it("shows the host refusal message of a 403", async () => {
    mockFetch(403, { error: "this server does not answer to the host name in the request; reach it by its IP address, or list the name in REMOVARR_ALLOWED_HOSTS" });
    const err = await api("/api/status").catch((e) => e);
    expect(err.message).toContain("REMOVARR_ALLOWED_HOSTS");
  });

  it("gives a plain message when the body is not JSON", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("Bad Gateway", { status: 502 })));
    const err = await api("/api/status").catch((e) => e);
    expect(err.message).toBe("The server answered 502.");
  });

  it("names a failed connection", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("Failed to fetch")));
    const err = await api("/api/status").catch((e) => e);
    expect(err.message).toBe("removarr could not be reached.");
    expect(err.status).toBe(0);
  });
});

describe("rowsBody", () => {
  it("sends only list and key, because the server refuses other fields", () => {
    const body = rowsBody([{ list: "series", key: "tvdb:1:s2", title: "x", season: 2 } as never]);
    expect(body).toEqual({ rows: [{ list: "series", key: "tvdb:1:s2" }] });
  });
});
```

- [ ] **Step 4: Run** `pnpm --dir web test`. Expected: FAIL, `./client` not found.

- [ ] **Step 5: Implement** `web/src/api/client.ts`:

```ts
import type { ListKind } from "./types";

export class ApiError extends Error {
  constructor(message: string, readonly status: number, readonly field?: string) {
    super(message);
    this.name = "ApiError";
  }
}

const CHANGES = new Set(["POST", "PUT", "DELETE", "PATCH"]);

export async function api<T = unknown>(path: string, init: { method?: string; body?: unknown } = {}): Promise<T> {
  const method = init.method ?? "GET";
  const headers: Record<string, string> = { Accept: "application/json" };
  if (CHANGES.has(method)) {
    headers["X-Removarr-Request"] = "1";
    headers["Content-Type"] = "application/json";
  }
  let res: Response;
  try {
    res = await fetch(path, {
      method,
      headers,
      body: init.body === undefined ? undefined : JSON.stringify(init.body),
    });
  } catch {
    throw new ApiError("removarr could not be reached.", 0);
  }
  if (res.status === 204) return undefined as T;
  let data: unknown;
  try {
    data = await res.json();
  } catch {
    if (!res.ok) throw new ApiError(`The server answered ${res.status}.`, res.status);
    throw new ApiError("The server's answer could not be read.", res.status);
  }
  if (!res.ok) {
    const e = data as { error?: string; field?: string };
    throw new ApiError(e.error || `The server answered ${res.status}.`, res.status, e.field);
  }
  return data as T;
}

export function rowsBody(rows: { list: ListKind; key: string }[]) {
  return { rows: rows.map(({ list, key }) => ({ list, key })) };
}
```

- [ ] **Step 6: Write the failing format tests** `web/src/lib/format.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { formatDate, formatDays, formatRatio, formatSize } from "./format";

describe("format", () => {
  it("formats sizes in IEC units with one decimal", () => {
    expect(formatSize(0)).toBe("0 B");
    expect(formatSize(1023)).toBe("1023 B");
    expect(formatSize(1536)).toBe("1.5 KiB");
    expect(formatSize(52428800)).toBe("50.0 MiB");
    expect(formatSize(5 * 1024 ** 3)).toBe("5.0 GiB");
    expect(formatSize(2.5 * 1024 ** 4)).toBe("2.5 TiB");
  });
  it("says unknown for a missing date", () => {
    expect(formatDate(undefined)).toBe("unknown");
    expect(formatDate(null)).toBe("unknown");
  });
  it("shows a dash for a missing ratio or seeding time", () => {
    expect(formatRatio(null)).toBe("–");
    expect(formatRatio(1.234)).toBe("1.23");
    expect(formatDays(null)).toBe("–");
    expect(formatDays(12.6)).toBe("12.6 d");
  });
});
```

Implement `format.ts` to pass (`formatDate` and `formatDateTime` use `Intl.DateTimeFormat(undefined, …)` with `dateStyle: "medium"` and `dateStyle: "medium", timeStyle: "short"`; the tests do not pin the locale output of a real date).

- [ ] **Step 7: The shell.** `App.tsx` builds the router with `createBrowserRouter` (read React Router 8's types for the exact names) with the routes of "Interfaces". `Shell.tsx`: a top bar with the app name and a slot for the header (task 4 fills it; render nothing for now), navigation links Movies, Series, Torrents, History, Settings (`NavLink`, the current one marked with `aria-current="page"`), and `<Outlet />`. Each page in `web/src/pages/` renders an `<h1>` with its name for now. `theme.ts` exports `followSystemTheme()`, called once in `main.tsx`: it sets the `dark` class on `document.documentElement` from `matchMedia("(prefers-color-scheme: dark)")` and follows its `change` event (spec 7.7). The page `<title>` is "removarr".

- [ ] **Step 8: Run** in `web/`: `pnpm typecheck`, `pnpm test`, `pnpm build`. Expected: all pass; `internal/webui/dist/index.html` and `.gitkeep` exist, `git status` shows no change to `.gitkeep`. Then from the repository root run the Go checks: `go vet ./...` must not descend into `web/`.

- [ ] **Step 9: CI and AGENTS.md.** Add a job `web` to `.github/workflows/ci.yml`:

```yaml
  web:
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: web
    steps:
      - uses: actions/checkout@v7
      - uses: pnpm/action-setup@v5
      - uses: actions/setup-node@v6
        with:
          node-version: 24
          cache: pnpm
          cache-dependency-path: web/pnpm-lock.yaml
      - run: pnpm install --frozen-lockfile
      - run: pnpm typecheck
      - run: pnpm test
      - run: pnpm build
```

Check the newest major of `pnpm/action-setup` and `actions/setup-node` with `gh api repos/pnpm/action-setup/releases/latest -q .tag_name` and the same for `actions/setup-node`, and use those majors. `pnpm/action-setup` reads the version from `packageManager`; when it needs `package_json_file: web/package.json`, add that.

In `AGENTS.md`: add to the layout table `web` ("The browser UI: Vite, React, TypeScript. Builds into `internal/webui/dist`") and `internal/webui` ("Embeds the built UI and serves it outside `/api/`"); under "Commands", after the Go checks, add the web checks:

```
pnpm --dir web install --frozen-lockfile
pnpm --dir web typecheck
pnpm --dir web test
pnpm --dir web build
pnpm --dir web e2e      # needs `pnpm --dir web exec playwright install chromium` once
```

and in "Rules for the code" a line: "**Frontend dependencies** are pinned exactly in `web/package.json`. Adding one is the owner's decision, as for Go modules." The `e2e` line describes task 11; add it now so the list is complete.

- [ ] **Step 10: Commit** `feat: add the web project with the api client and the app shell`.

---

### Task 4: The header

**Files:**
- Create: `web/src/api/queries.ts`, `web/src/components/header/Header.tsx`, `web/src/components/header/Header.test.tsx`, `web/src/test/render.tsx` (test helper)
- Modify: `web/src/components/layout/Shell.tsx`

**Interfaces:**
- Consumes: `api`, `ApiError`, types, `formatSize`, `formatDateTime`.
- Produces in `queries.ts`: query keys `["status"]`, `["candidates", list, ignored]`, `["actions"]`, `["settings"]`, `["users"]`; `useStatus()` (refetch every 2000 ms while `scan.running`, every 15000 ms otherwise); `useStartScan()` mutation (POST `/api/scans`); `useScanFinishedRefresh(status)`: when `scan.running` goes from true to false, invalidates `["candidates"]` and `["status"]`. In `test/render.tsx`: `renderWithProviders(ui, { route?: string })` that wraps in a fresh `QueryClient` (retry off) and a memory router, and `mockApi(routes: Record<string, (init: RequestInit) => { status: number; body?: unknown }>)` that stubs `fetch` by `METHOD path` (e.g. `"GET /api/status"`, query string included when present) and fails the test on an unexpected request.

Spec 5.4, 7.3, 7.6. The header shows:
- the Scan button; disabled with "Scanning…" and the `phase` while a scan runs; a start refused with 409 shows the server's message next to the button; no automatic retry;
- "Last scan" with `formatDateTime(lastScan.finishedAt)`, or "No scan yet";
- per list a count: "Movies 12 · Series 4 · Torrents 9", a list with `error` shown as "Movies: error" with the error in a tooltip and in an accessible description; then "protected", "unmatched", "waiting" as sums over the lists; `scan.lastError` when set, as an error line;
- free space: each root folder's path and `formatSize(freeBytes)`;
- a "Dry run" badge when `dryRun`;
- when `stale`: "Settings changed since the last scan. Scan again to update the lists." (spec 5.4).

- [ ] **Step 1: Write the failing tests** `Header.test.tsx`, covering: the summary and free space from a status fixture; "No scan yet" with `lastScan: null`; the stale notice only when `stale`; the dry-run badge; clicking Scan sends `POST /api/scans` and a 409 `{"error":"a scan is running"}` shows that text and sends nothing more; while `running` the button is disabled and shows the phase; when a later status reports `running: false` after `running: true`, the candidates query is invalidated (render a tiny probe component that uses `useQuery(["candidates","movies",false])` with a counting fetch, and assert it fetched again). Use `vi.useFakeTimers({ shouldAdvanceTime: true })` or call `queryClient.refetchQueries` to move the polling on; do not sleep. Write each test in full.

- [ ] **Step 2: Run** `pnpm --dir web test`. Expected: FAIL.

- [ ] **Step 3: Implement** `queries.ts`, `Header.tsx`, the test helper, and render `<Header />` in `Shell`.

```ts
// queries.ts (core)
export function useStatus() {
  return useQuery({
    queryKey: ["status"],
    queryFn: () => api<Status>("/api/status"),
    refetchInterval: (q) => (q.state.data?.scan.running ? 2000 : 15000),
  });
}

export function useScanFinishedRefresh(running: boolean | undefined) {
  const qc = useQueryClient();
  const was = useRef(running);
  useEffect(() => {
    if (was.current === true && running === false) {
      qc.invalidateQueries({ queryKey: ["candidates"] });
      qc.invalidateQueries({ queryKey: ["status"] });
    }
    was.current = running;
  }, [running, qc]);
}

export function useStartScan() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api("/api/scans", { method: "POST", body: {} }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["status"] }),
  });
}
```

- [ ] **Step 4: Run** `pnpm --dir web test` and `pnpm --dir web typecheck`. Expected: PASS.

- [ ] **Step 5: Commit** `feat: show the scan, its summary and free space in the header`.

---

### Task 5: The list: sorting, filtering, selection and the bulk bar

**Files:**
- Create: `web/src/lib/selection.ts`, `web/src/lib/selection.test.ts`, `web/src/lib/useMediaQuery.ts`, `web/src/components/list/CandidateList.tsx`, `web/src/components/list/BulkBar.tsx`, `web/src/components/list/CandidateList.test.tsx`

**Interfaces:**
- Consumes: `Candidate`, `formatSize`.
- Produces:
  - `selection.ts`: `type Selection = ReadonlySet<string>` (row keys); `selectAll(visible: Candidate[]): Selection`; `toggle(sel, key): Selection`; `prune(sel, visible: Candidate[]): Selection` (keeps only keys of visible rows); `selectedRows(sel, visible): Candidate[]`; `totalSize(rows: Candidate[]): number`.
  - `CandidateList` props:

    ```ts
    interface CandidateListProps {
      rows: Candidate[];
      columns: ColumnSpec[];             // task 6 supplies them per list
      sortKeys: { id: "size" | "watchedAt" | "addedAt" | "title"; label: string }[];
      renderCard: (row: Candidate) => ReactNode;
      renderExpanded?: (row: Candidate) => ReactNode;
      actions: (selected: Candidate[], clear: () => void) => ReactNode; // buttons of the bulk bar
      emptyText: string;
    }
    interface ColumnSpec { id: string; header: string; cell: (row: Candidate) => ReactNode; className?: string }
    ```
  - `BulkBar` props: `{ count: number; size: number; children: ReactNode; onClear: () => void }`.

Spec 5.3, the selection rules amended on 2026-11-26, and 7.7. Behaviour:
- Text filter, case-insensitive, over the title, the year, the torrent names and, for torrent rows, category and tracker.
- Sort by the given keys, ascending or descending; default size descending. A missing `watchedAt` sorts as the oldest. `addedAt` reads `torrents[0].addedAt`. Title sorts with `localeCompare`.
- A header checkbox "Select all" selects every row the filter shows; it is checked when all of them are selected and indeterminate when some are.
- The selection holds only keys of visible rows: after a filter change and after `rows` changes, it is pruned (`prune`).
- The bulk bar is `position: sticky; bottom: 0` and shown while at least one row is selected, with "N selected · <size>" (size labelled as size, spec 6.9), the buttons from `actions`, and "Clear selection".
- Wide screens (`useMediaQuery("(min-width: 768px)")`) render a table (TanStack Table 9 for sorting, filtering and row selection state, or plain state with the `selection.ts` functions, whichever keeps the code smaller; record the choice); narrow screens render one card per row with its checkbox. Only one of the two is in the DOM.
- A row with `renderExpanded` has an expand button ("Show torrents") that toggles a detail row or a section of the card.
- Every checkbox has an accessible name: "Select <title>"; the header one "Select all shown rows".

- [ ] **Step 1: Write the failing unit tests** `selection.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { prune, selectAll, selectedRows, toggle, totalSize } from "./selection";
import type { Candidate } from "@/api/types";

const row = (key: string, size: number): Candidate =>
  ({ list: "movies", key, title: key, size, torrents: [], belowSeedMin: false });

describe("selection", () => {
  const a = row("a", 1), b = row("b", 2), c = row("c", 4);
  it("selects every visible row", () => {
    expect([...selectAll([a, b])]).toEqual(["a", "b"]);
  });
  it("toggles one row", () => {
    expect(toggle(new Set(["a"]), "a").has("a")).toBe(false);
    expect(toggle(new Set(), "a").has("a")).toBe(true);
  });
  it("drops rows the filter or a reload no longer shows", () => {
    expect([...prune(new Set(["a", "b", "c"]), [a, c])]).toEqual(["a", "c"]);
  });
  it("returns the selected rows in list order and their total size", () => {
    const rows = selectedRows(new Set(["c", "a"]), [a, b, c]);
    expect(rows.map((r) => r.key)).toEqual(["a", "c"]);
    expect(totalSize(rows)).toBe(5);
  });
});
```

- [ ] **Step 2: Write the failing component tests** `CandidateList.test.tsx` (spec 12 asks for component tests of selection and the bulk bar). With three rows `Alpha` (size 3 GiB), `Beta` (1 GiB), `Gamma` (2 GiB) and an `actions` that renders a button "Delete" calling a spy with the selected keys:
  - "Select all shown rows" selects all three; the bulk bar reads "3 selected · 6.0 GiB".
  - Type "al" in the filter: only Alpha shows (filter is over title); clicking "Select all shown rows" then "Delete" calls the spy with `["Alpha"]`'s key only.
  - Select all three, then type "be" in the filter: the bulk bar reads "1 selected · 1.0 GiB" and "Delete" calls the spy with Beta's key only (the review focus: never act on hidden rows).
  - Select Alpha and Beta, then rerender with rows `[Beta, Gamma]`: the bar reads "1 selected".
  - "Clear selection" hides the bar.
  - The header checkbox is indeterminate with one of three selected.
  - Sorting by title ascending puts Alpha, Beta, Gamma in order; by size descending Alpha, Gamma, Beta.
  - With `matchMedia` reporting narrow, cards render (no `table` role) and a card's checkbox selects its row.
  Write each test in full with Testing Library and `userEvent`.

- [ ] **Step 3: Run** `pnpm --dir web test`. Expected: FAIL.

- [ ] **Step 4: Implement** `selection.ts`, `useMediaQuery.ts`, `BulkBar.tsx`, `CandidateList.tsx` with shadcn `Table`, `Card`, `Checkbox`, `Input`, `Select`, `Button`. Keep `CandidateList` free of anything list-specific: columns, cards and actions come from props.

- [ ] **Step 5: Run** `pnpm --dir web test` and `pnpm --dir web typecheck`. Expected: PASS. Break it once: remove the `prune` call on filter change and check that the third component test fails; revert.

- [ ] **Step 6: Commit** `feat: add the list with sorting, filtering, selection and the bulk bar`.

---

### Task 6: The movies, series and torrents pages

**Files:**
- Create: `web/src/components/rows/MediaRow.tsx` (columns and card for movies and series), `web/src/components/rows/TorrentRow.tsx`, `web/src/components/rows/TorrentDetails.tsx`, `web/src/components/rows/rows.test.tsx`, `web/src/pages/ListPage.tsx`, `web/src/pages/ListPage.test.tsx`
- Modify: `web/src/pages/Movies.tsx`, `Series.tsx`, `Torrents.tsx`, `web/src/api/queries.ts`

**Interfaces:**
- Consumes: `CandidateList`, `useStatus`, formatters.
- Produces: `useCandidates(list: ListKind, ignored: boolean)` in `queries.ts` (key `["candidates", list, ignored]`, GET `/api/candidates?list=…&ignored=…`); `ListPage` props `{ list: ListKind }`, with a slot `bulkActions(selected, clear, ignoredView)` that tasks 7 and 8 fill, and the "Show ignored" switch state owned by `ListPage` (task 7 wires its behaviour; here it can be absent).

Spec 5.2, 6.7, 6.8 rule 5, 7.7. Content:
- **Movies and series columns:** title (with year), size, watched by (comma-separated names, or "—"), watched (`formatDate(watchedAt)`, "unknown" when absent), torrents (the number of `torrents`), a "Below seeding minimum" badge when `belowSeedMin`. Series rows add the scope ("Whole series" or "Season N") and a "Still airing" badge when `stillAiring`. A row with `tiedWith` shows "Deleted together with: <titles>" (spec 6.3, 6.7). Expanded: `TorrentDetails` lists each torrent with name, tracker, ratio, seeding time, the seeding badge, and "Outside the library: <size>" when `outsideLibrary` is set.
- **Torrent columns:** name, size, state, category, tracker, ratio, seeding time, added (`formatDate`), reason ("Orphan" or "Unlinked", with a tooltip: orphan "No media manager knows this torrent." / unlinked "A media manager grabbed it, but it backs no library file now."), seeding badge.
- Sort keys: movies and series `size`, `watchedAt`, `title`; torrents `size`, `addedAt`, `title`.
- **States of the page**, in this order of precedence:
  1. Status not loaded yet: a loading line.
  2. No app configured (`Object.values(status.connections).every(c => !c.configured)`): "removarr is not connected to any app yet." with a link to `/settings` (spec 7.7, first run).
  3. The response has `error`: the error in an alert, no rows, no bulk bar (spec 6.8, rule 5).
  4. `scannedAt` null: "No scan yet. Start one with Scan above."
  5. No rows: "Nothing to suggest." (or task 7's text for the ignored view).
  6. Otherwise the list.
- The request error of `useCandidates` (an `ApiError`) is shown in an alert with its message.

- [ ] **Step 1: Write the failing tests.** `rows.test.tsx`: a movie row renders title with year, size, watched by, "unknown" for a missing date, the torrent count and the seeding badge; a season row renders "Season 2" and "Still airing"; a tied row names the other row; expanding shows torrent name, tracker, "Outside the library: 6.0 GiB"; a torrent row shows "Unlinked". `ListPage.test.tsx` (with `mockApi`): the first-run text and link when nothing is configured; the list's error and no "Select all shown rows" checkbox when `error` is set (review focus); "No scan yet" when `scannedAt` is null; the rows otherwise; `candidates: null` renders "Nothing to suggest.". Write each in full.

- [ ] **Step 2: Run** `pnpm --dir web test`. Expected: FAIL.

- [ ] **Step 3: Implement.** `Movies.tsx` is `<ListPage list="movies" />`, and likewise for the others.

- [ ] **Step 4: Run** `pnpm --dir web test` and `pnpm --dir web typecheck`. Expected: PASS.

- [ ] **Step 5: Commit** `feat: show the movie, series and torrent lists`.

---

### Task 7: Ignore and the ignored view

**Files:**
- Create: `web/src/components/list/IgnoreActions.tsx`, `web/src/components/list/IgnoreActions.test.tsx`
- Modify: `web/src/pages/ListPage.tsx`, `web/src/api/queries.ts`, `web/src/components/rows/MediaRow.tsx`, `web/src/components/rows/TorrentRow.tsx`

**Interfaces:**
- Consumes: `rowsBody`, `useCandidates`, `ListPage`'s bulk action slot.
- Produces: `useIgnore()` and `useUnignore()` mutations (POST and DELETE `/api/ignores` with `rowsBody`); on success they invalidate `["candidates"]` and `["status"]` and the list clears its selection.

Spec 5.3, 6.5. Behaviour:
- A "Show ignored" switch on each list page. Off: the bulk bar offers Ignore (task 8 adds Delete). On: the list is `ignored=true` and the bulk bar offers only Un-ignore; Delete is not offered (an ignored row must be un-ignored first, spec 5.3). Changing the switch clears the selection.
- In the ignored view a row shows "Ignored <formatDate(ignoredAt)>"; a row with `notInScan` shows its title and "Not in the latest scan" and no other columns; a row with `waiting` shows "Waiting: <waitReason>" (or "Waiting" when there is no reason). Empty text: "Nothing is ignored in this list."
- A failed request shows the server's message (for example a 400 "is not a row of the latest scan; reload the list") and keeps the selection.

- [ ] **Step 1: Write the failing tests**: Ignore sends `POST /api/ignores` with exactly `{"rows":[{"list":"movies","key":"tmdb:1"}]}` for the selected row, then the list refetches and the selection is empty; the switch requests `ignored=true` and the bar shows Un-ignore and no Delete; Un-ignore sends `DELETE /api/ignores`; a `notInScan` row and a `waiting` row render as described; a 400 shows its message and keeps the bar. Write each in full with `mockApi`.

- [ ] **Step 2: Run** `pnpm --dir web test`. Expected: FAIL.

- [ ] **Step 3: Implement.**

- [ ] **Step 4: Run** `pnpm --dir web test` and `pnpm --dir web typecheck`. Expected: PASS.

- [ ] **Step 5: Commit** `feat: ignore rows and review the ignored ones`.

---

### Task 8: Delete: the plan, its confirmation and the outcome

**Files:**
- Create: `web/src/components/delete/DeleteFlow.tsx`, `web/src/components/delete/PlanView.tsx`, `web/src/components/delete/OutcomeView.tsx`, `web/src/components/delete/describe.ts`, `web/src/components/delete/describe.test.ts`, `web/src/components/delete/DeleteFlow.test.tsx`, `web/src/test/fixtures.ts`
- Modify: `web/src/pages/ListPage.tsx`, `web/src/api/queries.ts`

**Interfaces:**
- Consumes: `rowsBody`, `api`, `ApiError`, `useStatus` (for `dryRun`), formatters.
- Produces: `describeStep(step: ManagerStep, rowTitle: string): string`; `groupByApp(plan: Plan): { radarr: AppGroup; sonarr: AppGroup; qbittorrent: AppGroup }` where `AppGroup = { rows: PlanRow[]; steps: { step: ManagerStep; title: string }[]; torrents: PlanTorrent[]; kept: KeptTorrent[] }` (rows go to Radarr for `movies`, Sonarr for `series`, qBittorrent for `torrents`; steps by `manager`; torrents and kept torrents to qBittorrent); `fixtures.ts` exports `planFixture: Plan` covering every field below, `executeFixture: ExecuteResult` with one done item and one failed item, and `statusFixture`.

Spec 6.7, 6.8, 6.10, 6.11, 7.4, 8. The flow is a state machine in `DeleteFlow`:

| State | Shows | Next |
|---|---|---|
| building | "Building the plan… This reads every app and takes about as long as a scan." | built, or error |
| built | `PlanView`, buttons "Delete" (or "Run as dry run" when `dryRun`) and "Cancel" | executing |
| executing | "Deleting… This continues on the server if you close the page; the History page shows the result." | done, expired, or error |
| done | `OutcomeView` and "Close" | closed; invalidates `["candidates"]`, `["status"]`, `["actions"]`, clears the selection |
| expired | the server's message (410 or 404) and "Build the plan again", which posts the same rows again | building |
| error | the `ApiError` message (a 409 names the running job) and "Close" | closed |

The dialog cannot be closed while building or executing (no outside click, no Escape, no close button); a dialog that waits on the server must not suggest the request was cancelled. Nothing is ever executed without the user pressing the button in state `built` for that plan; after "Build the plan again" the new plan needs a new press.

`PlanView`, grouped by app (spec 7.4), each group only when it has content:
- **Radarr / Sonarr:** each row with title, size; an added row is marked "Added: deleted together with <addedWith.title>" and names the shared torrents by name (look up `shares` IDs among the item's torrents and kept torrents; none means "a shared library file"); a row with `notInScan` is marked "Not in the latest scan"; the steps in words via `describeStep`:
  - `remove-movie`: "Remove <title> from Radarr with its files" + ", and add it to the import exclusion list" when `addExclusion`;
  - `remove-series`: "Remove <title> from Sonarr with its files, specials included" + exclusion text;
  - `unmonitor-episodes`: "Unmonitor N episodes of <title>";
  - `unmonitor-seasons`: "Unmonitor season(s) <list> of <title>";
  - `delete-episode-files`: "Delete N episode files of <title>".
  The step's title is the title of the row whose key is `rowKey` in the item.
- **qBittorrent:** torrents removed, each with name, size, "Below seeding minimum", "Outside the library: <size>", "Keeps its data: another download uses it" when `keepData`, "Linked by file size only: check that it belongs" when `linkedBySize`; torrents that stay, with "In a download queue" or "Still backs a library file that stays".
- **Skipped:** each skipped row's title and reason.
- A summary line: "N items · <size of all rows>"; and when `dryRun`, a banner "Dry run: nothing will be changed. The history records what would have run." When the plan has no items: "Nothing can be deleted." and only "Close".
- The expiry: "Valid until <time>".

`OutcomeView`: per action its rows, outcome ("Done", "Failed", "Dry run"), and every step with its result (done, failed with its error, skipped, already gone, dry run) and "kept its data" for `keepData` torrents; a line at the top "N done · N failed".

- [ ] **Step 1: Write the failing tests.** `describe.test.ts`: each step action renders the text above, including the exclusion suffix and singular "1 episode"; `groupByApp` puts a movie row and its steps under Radarr, series under Sonarr, torrents and kept under qBittorrent. `DeleteFlow.test.tsx` (with `mockApi` and the fixtures):
  - Delete with two rows selected posts `/api/plans` with exactly those rows (`list` and `key` only) and shows the building text; the dialog has no close button meanwhile.
  - The built plan shows every group, the added row note naming the selected row and the shared torrent, the seeding badge, outside-library size, keep-data and linked-by-size notes, kept reasons, skipped reasons.
  - With `dryRun` the banner shows and the button reads "Run as dry run".
  - Confirm posts `/api/plans/<id>/execute` once; the outcome shows "1 done · 1 failed" and the failed step's error; Close invalidates the lists (a probe fetch counts) and clears the selection.
  - A 410 on execute shows the server's message and "Build the plan again"; pressing it posts `/api/plans` with the same rows and shows the new plan; it does not execute (assert no second `execute` request) until Delete is pressed again (review focus).
  - A 404 on execute behaves like 410.
  - A 409 on build shows "a scan is running" and only Close (review focus).
  - A plan with no items shows "Nothing can be deleted." and no Delete button.
  Write each test in full.

- [ ] **Step 2: Run** `pnpm --dir web test`. Expected: FAIL.

- [ ] **Step 3: Implement** with the shadcn `Dialog`. Wire a "Delete" button into the bulk bar of `ListPage` outside the ignored view.

- [ ] **Step 4: Run** `pnpm --dir web test` and `pnpm --dir web typecheck`. Expected: PASS. Break it once: make "Build the plan again" execute the new plan directly and check that the 410 test fails; revert.

- [ ] **Step 5: Commit** `feat: confirm and run deletions from a plan`.

---

### Task 9: The history page

**Files:**
- Create: `web/src/pages/History.test.tsx`, `web/src/components/history/ActionCard.tsx`
- Modify: `web/src/pages/History.tsx`, `web/src/api/queries.ts`

**Interfaces:**
- Consumes: `OutcomeView`'s step rendering if it can be shared (extract `StepLine` from task 8 into `web/src/components/delete/StepLine.tsx` and use it in both), formatters.
- Produces: `useActions()` with `useInfiniteQuery` over `GET /api/actions?limit=50&before=<next>` (key `["actions"]`).

Spec 5.5, 7.6. Each record: `formatDateTime(at)`, the rows (title, "Season N" when `season`, size, "added" when `added`), total size, the outcome badge, "Dry run" when `dryRun`, and every step via `StepLine`. A "Load more" button while the last page had `next`. Empty: "Nothing was deleted yet."

- [ ] **Step 1: Write the failing tests**: one page of two records renders time, rows, sizes, outcome and a failed step's error; "Load more" requests `before=<next>` and appends the next page; no button without `next`; the empty text. Write in full.

- [ ] **Step 2: Run** `pnpm --dir web test`. Expected: FAIL.

- [ ] **Step 3: Implement.**

- [ ] **Step 4: Run** `pnpm --dir web test` and `pnpm --dir web typecheck`. Expected: PASS.

- [ ] **Step 5: Commit** `feat: show the history of executed deletions`.

---

### Task 10: The settings page

**Files:**
- Create: `web/src/pages/settings/Connections.tsx`, `web/src/pages/settings/Rules.tsx`, `web/src/pages/settings/CountingUsers.tsx`, `web/src/pages/settings/changes.ts`, `web/src/pages/settings/changes.test.ts`, `web/src/pages/Settings.test.tsx`
- Modify: `web/src/pages/Settings.tsx`, `web/src/api/queries.ts`

**Interfaces:**
- Consumes: `Settings`, `Rules`, `FieldView`, `MediaUser`, `TestResult`, `api`, `ApiError`.
- Produces: `useSettings()`, `useSaveSettings()` (PUT `/api/settings`, on success sets `["settings"]` from the answer and invalidates `["status"]`), `useUsers()` (GET `/api/media-server/users`, enabled only when Jellyfin is configured), `useTestConnection(app)`; in `changes.ts`: `connectionChanges(edits: Record<AppName, Record<string, string | null | undefined>>)` returning only the fields with a value that is not `undefined` (`null` means "clear"), shaped as the request's `connections` object, or `undefined` when there are none.

Spec 5.6, 9.2, 10. The page has three sections and one Save button per section.

**Connections**, one card per app (Jellyfin: URL, API key; Sonarr: URL, API key; Radarr: URL, API key; qBittorrent: URL, user name, password):
- Each field shows its source: "Saved here", "From the environment" or "Not set" (`ui`, `env`, `none`).
- A non-secret field is an input with the current value. An edit is a change. A field whose source is `ui` has a button "Clear saved value" that sends `null`; the environment value, if there is one, then applies again (spec 9.2). The UI cannot know whether there is one, so the button does not promise it.
- A secret field never shows a value. It shows "Set" or "Not set" with the source, an empty password input with the placeholder "Enter a new value to replace it", and "Clear saved value" when the source is `ui`. An empty input means no change.
- "Test" per app calls `POST /api/connections/<app>/test` and shows "Connected: <detail>" or the error. It is disabled while that app has unsaved edits, with the hint "Save to test" (spec 5.6).
- Save sends `{"connections": connectionChanges(edits)}` and nothing else; a 400 shows its message next to the field named by `field` (`connections.<app>.<name>`).

**Counting users:** checkboxes from `useUsers()`; a 409 shows "Connect Jellyfin first."; the selection is saved as `rules.countingUsers` (IDs). With none selected, the note "Watched lists are not produced until at least one user is selected." (spec 6.1).

**Rules:** watched mode (all/any), grace period in days, series unit (whole series/per season), protect favourites, protection tag, minimum ratio (0 = off), minimum seeding time in days (0 = off), excluded categories and excluded tags (comma-separated inputs, trimmed, empty entries dropped), add to exclusion list on delete. Save sends `{"rules": {...only the changed fields}}`. A 400 shows the message next to the field named `rules.<name>`.

Dry run is shown read-only: "Dry run is on: set by REMOVARR_DRY_RUN." when `dryRun`.

After a successful save of rules or connections the header's stale notice comes from the status (task 1); the page itself adds a line "Saved. Scan again to apply the changes to the lists."

- [ ] **Step 1: Write the failing tests.** `changes.test.ts`: untouched fields are left out; an emptied secret input is left out; `null` is kept as a clear; no edits give `undefined`. `Settings.test.tsx`:
  - Changing Sonarr's URL and saving sends exactly `{"connections":{"sonarr":{"url":"http://192.0.2.1:8989"}}}` (review focus: no secret, no other field).
  - A secret field renders no value even when `set`, shows its source, and an input left empty is not sent.
  - "Clear saved value" on a UI-saved API key sends `{"connections":{"radarr":{"apiKey":null}}}`.
  - Test is disabled with "Save to test" after an edit and enabled after the save; pressing it shows "Connected: Sonarr 4.0.12" from a `{ok:true, detail:"Sonarr 4.0.12"}` answer, and an error answer shows its error.
  - A 400 `{"error":"must be at least 0","field":"rules.graceDays"}` shows the message next to the grace period.
  - Changing the grace period sends only `{"rules":{"graceDays":7}}`.
  - Counting users: the users list renders; a 409 shows "Connect Jellyfin first."; selecting a user and saving sends `rules.countingUsers` with that ID.
  Write each in full.

- [ ] **Step 2: Run** `pnpm --dir web test`. Expected: FAIL.

- [ ] **Step 3: Implement.**

- [ ] **Step 4: Run** `pnpm --dir web test` and `pnpm --dir web typecheck`. Expected: PASS. Break it once: make `connectionChanges` include untouched fields as `null` and check that the first settings test fails; revert.

- [ ] **Step 5: Commit** `feat: edit connections, counting users and rules in the settings`.

---

### Task 11: End-to-end test against a server with fake providers

**Files:**
- Create: `internal/e2e/fakeserver/main.go`, `internal/e2e/fakeserver/fakes.go`, `internal/e2e/fakeserver/fakes_test.go`, `web/playwright.config.ts`, `web/e2e/removarr.spec.ts`
- Modify: `.github/workflows/ci.yml`, `README.md` (a short "Developing the UI" section), `web/package.json` scripts only if needed

**Interfaces:**
- Consumes: `api.Server`, `scanner.Service`, `planner.Service`, `executor.Service`, `gate.Gate`, `config.NewService`, `store.Open`, `webui.Handler`, `webui.Files`, the `providers` interfaces and `scanner.Providers` / `executor.Providers` (read their fields).
- Produces: `go run ./internal/e2e/fakeserver -port 7373 -dir <tmp>` serves the real API and the embedded UI on 127.0.0.1 only, with fake providers.

The fake server is the real `api.Server` wiring of `cmd/removarr/main.go`, with three differences: `Build` functions return in-memory fakes, `Apps` is a fake whose `Tester` answers `"fake"` and whose `MediaServer` returns the fake media server, and the config's environment has a URL for every app (`http://fake.invalid`, never contacted) so the status reports them configured. The executor runs with dry run off: the fake writers record calls in memory and change the fake library, so a second scan reflects the deletion. It listens on `127.0.0.1:<port>` only. Its fixture library, small and fixed:
- Jellyfin: one user `u1` "viewer"; a movie "Fake Movie One" (TMDB 101) played by u1 30 days ago; a movie "Fake Movie Two" (TMDB 102) played; a series "Fake Series" (TVDB 201), ended, two seasons of two episodes each, all played.
- Radarr: both movies with a file of 2 GiB each; Sonarr: the series with four episode files of 1 GiB.
- qBittorrent: a torrent backing Fake Movie One by history (3 GiB, ratio 2.0), and an orphan torrent "fake.orphan.torrent" (500 MiB) no manager knows. No queue.
- Rules saved at start: counting users `["u1"]`.

`fakes_test.go` runs a scan through the fake wiring (`scanner.Service.Start` and wait for it) and asserts the three lists hold 2 movies, 1 series and 1 torrent, so a change of the rules or of the fakes shows up in `go test` before the browser test.

Playwright (`web/playwright.config.ts`): one project, Chromium; `webServer` runs `go run ../internal/e2e/fakeserver -port 7373 -dir <a fresh temp dir>` from `web/`, waits for `http://127.0.0.1:7373/api/health`, `reuseExistingServer: false`; `baseURL: "http://127.0.0.1:7373"`. The test needs the UI built first (`pnpm build`), because the server serves the embedded build.

`web/e2e/removarr.spec.ts`, one test in steps (`test.step`):
1. Open `/`: redirected to Movies; "No scan yet".
2. Press Scan; wait until "Last scan" shows; Movies lists 2 rows; Torrents lists "fake.orphan.torrent".
3. Movies: select "Fake Movie Two", Ignore; the list shows 1 row; switch "Show ignored": it is there; Un-ignore; back to 2 rows.
4. Movies: select "Fake Movie One", Delete; the plan shows Radarr removing it and qBittorrent removing its torrent; confirm; the outcome reads "1 done"; close; the list has 1 row.
5. History shows the record with its steps done.
6. Settings shows Jellyfin's source "From the environment" and Test answers "Connected: fake".

- [ ] **Step 1: Write `fakes_test.go`** as described (the failing test), run `go test ./internal/e2e/...`. Expected: FAIL, the package has no wiring.

- [ ] **Step 2: Implement** `fakes.go` (the fakes and a `wire(dir string, port int) (*http.Server, error)` that `main.go` and the test share) and `main.go` (flags `-port`, `-dir`; logs to stderr; stops on SIGINT/SIGTERM). Run `go test ./internal/e2e/...`. Expected: PASS. Run the Go checks.

- [ ] **Step 3: Write the Playwright config and test.** Run `pnpm --dir web exec playwright install chromium`, `pnpm --dir web build`, `pnpm --dir web e2e`. Expected: PASS. If a step fails because of the UI, fix the UI with a unit test that fails first (`fix:` commit), not the test.

- [ ] **Step 4: CI.** Add to the `web` job, after `pnpm build`: `actions/setup-go` with `go-version-file: go.mod`, then `pnpm exec playwright install --with-deps chromium` and `pnpm e2e`.

- [ ] **Step 5: README.** A section "Developing the UI": `pnpm --dir web install`; run removarr (or `go run ./internal/e2e/fakeserver -port 7272 -dir ./config-fake` for fake data) and `pnpm --dir web dev`; build with `pnpm --dir web build` before `go build`, because the binary embeds `internal/webui/dist`.

- [ ] **Step 6: Commit** `test: drive the ui end to end against fake providers`.

---

## After the tasks (done by the run, not by an implementer)

Step 6 of the implement-issue skill: run every check, update `CHANGELOG.md`, `LEARNINGS.md`, `README.md` (reverse proxy and Host header, allowed hosts, building the UI), check the UI against the real apps through the replay with dry run on, and the manual pass of the issue: scan, ignore, delete with dry run on, settings.
