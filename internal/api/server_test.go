package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/removarr/removarr/internal/config"
	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/executor"
	"github.com/removarr/removarr/internal/providers"
	"github.com/removarr/removarr/internal/scanner"
	"github.com/removarr/removarr/internal/store"
)

type fakeScanner struct {
	status  scanner.Status
	err     error
	started int
}

func (f *fakeScanner) Start() error {
	if f.err != nil {
		return f.err
	}
	f.started++
	return nil
}

func (f *fakeScanner) Status() scanner.Status { return f.status }

type testerFunc func(context.Context) (string, error)

func (f testerFunc) Test(ctx context.Context) (string, error) { return f(ctx) }

type fakeApps struct {
	testers map[string]providers.Tester
	server  providers.MediaServer
}

func (f *fakeApps) Tester(_ context.Context, app string) (providers.Tester, error) {
	if t, ok := f.testers[app]; ok {
		return t, nil
	}
	return nil, providers.ErrNotConfigured
}

func (f *fakeApps) MediaServer(context.Context) (providers.MediaServer, error) {
	if f.server == nil {
		return nil, providers.ErrNotConfigured
	}
	return f.server, nil
}

// harness is a Server over a real store in a temporary directory, with fake apps and a
// fake scanner.
type harness struct {
	t        *testing.T
	store    *store.Store
	config   *config.Service
	scanner  *fakeScanner
	apps     *fakeApps
	planner  *fakePlanner
	executor *fakeExecutor
	handler  http.Handler
}

func newHarness(t *testing.T, envConn map[string]string) *harness {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "removarr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if envConn == nil {
		envConn = map[string]string{}
	}
	h := &harness{
		t:     t,
		store: st,
		config: config.NewService(config.Env{
			Port: 7272, ConfigDir: "/config", DryRun: true, Conn: envConn,
			AllowedHosts: []string{"removarr.local"},
		}, st),
		scanner:  &fakeScanner{},
		apps:     &fakeApps{testers: map[string]providers.Tester{}},
		planner:  &fakePlanner{},
		executor: &fakeExecutor{},
	}
	srv := &Server{
		Config:   h.config,
		Store:    st,
		Scanner:  h.scanner,
		Apps:     h.apps,
		Planner:  h.planner,
		Executor: h.executor,
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	h.handler = srv.Handler()
	return h
}

// do sends a request. Requests other than GET get the headers the guard asks for; mods can
// change or remove them.
func (h *harness) do(method, path, body string, mods ...func(*http.Request)) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "removarr.local:7272"
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(RequestHeader, "1")
	}
	for _, m := range mods {
		m(req)
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("response is not the expected JSON: %v\n%s", err, rec.Body.String())
	}
	return v
}

func TestHealth(t *testing.T) {
	rec := newHarness(t, nil).do(http.MethodGet, "/api/health", "")
	if rec.Code != http.StatusOK || decode[map[string]string](t, rec)["status"] != "ok" {
		t.Errorf("health = %d %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestUnknownPathsAreNotFound(t *testing.T) {
	if rec := newHarness(t, nil).do(http.MethodGet, "/api/nope", ""); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestGuard(t *testing.T) {
	tests := []struct {
		name string
		mod  func(*http.Request)
		want int
	}{
		{"all headers present", func(*http.Request) {}, http.StatusOK},
		{"no request header", func(r *http.Request) { r.Header.Del(RequestHeader) }, http.StatusForbidden},
		{"form content type", func(r *http.Request) {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}, http.StatusUnsupportedMediaType},
		{"json with a charset", func(r *http.Request) {
			r.Header.Set("Content-Type", "application/json; charset=utf-8")
		}, http.StatusOK},
		{"origin of another site", func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") }, http.StatusForbidden},
		{"origin of this server", func(r *http.Request) { r.Header.Set("Origin", "http://removarr.local:7272") }, http.StatusOK},
		{"origin that is not a URL", func(r *http.Request) { r.Header.Set("Origin", "null") }, http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := newHarness(t, nil).do(http.MethodPut, "/api/settings", `{}`, tt.mod)
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d: %s", rec.Code, tt.want, rec.Body)
			}
		})
	}
}

func TestGuardLeavesReadsAlone(t *testing.T) {
	rec := newHarness(t, nil).do(http.MethodGet, "/api/settings", "", func(r *http.Request) {
		r.Header.Set("Origin", "http://evil.example")
	})
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200: an Origin header does not block a read", rec.Code)
	}
}

func TestGuardComparesTheOriginWithoutRegardToCase(t *testing.T) {
	rec := newHarness(t, nil).do(http.MethodPut, "/api/settings", `{}`, func(r *http.Request) {
		r.Header.Set("Origin", "http://Removarr.Local:7272")
	})
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200: host names are not case-sensitive", rec.Code)
	}
}

// The server answers only when the Host header is an IP address, localhost or a name the
// owner listed, for reads and for changes alike.
func TestGuardChecksTheHostHeader(t *testing.T) {
	h := newHarness(t, nil) // the requests change nothing, so one server serves every case
	tests := []struct {
		host string
		want int
	}{
		{"192.168.1.10:7272", http.StatusOK},
		{"192.168.1.10", http.StatusOK},
		{"[::1]:7272", http.StatusOK},
		{"[fe80::1]", http.StatusOK},
		{"[fe80::1%25en0]:7272", http.StatusOK}, // an IPv6 address with a zone is still an address
		{"localhost:7272", http.StatusOK},
		{"LOCALHOST", http.StatusOK},
		{"localhost.:7272", http.StatusOK},
		{"removarr.local:7272", http.StatusOK},
		{"Removarr.Local", http.StatusOK},
		{"removarr.local.:7272", http.StatusOK},
		{"evil.example", http.StatusForbidden},
		{"evil.example:7272", http.StatusForbidden},
		{"192.168.1.10.evil.example:7272", http.StatusForbidden},
		{"localhost.evil.example", http.StatusForbidden},
		{"removarr.local.evil.example", http.StatusForbidden},
		{"other.local:7272", http.StatusForbidden},
		{"", http.StatusForbidden},
		// Numbers that are not written as an IP address are names.
		{"127.1", http.StatusForbidden},
		{"2130706433", http.StatusForbidden},
		{"0x7f000001", http.StatusForbidden},
		// Brackets hold an IPv6 address and nothing else, and a port is a number.
		{"[localhost]", http.StatusForbidden},
		{"]localhost[", http.StatusForbidden},
		{"[removarr.local]:7272", http.StatusForbidden},
		{"[1.2.3.4]:80", http.StatusForbidden},
		{"[::1", http.StatusForbidden},
		{"::1]", http.StatusForbidden},
		{"removarr.local:abc", http.StatusForbidden},
		{"localhost:", http.StatusForbidden},
	}
	for _, tt := range tests {
		for _, method := range []string{http.MethodGet, http.MethodPut} {
			t.Run(method+" as "+tt.host, func(t *testing.T) {
				rec := h.do(method, "/api/settings", `{}`, func(r *http.Request) { r.Host = tt.host })
				if rec.Code != tt.want {
					t.Errorf("status = %d, want %d: %s", rec.Code, tt.want, rec.Body)
				}
			})
		}
	}
}

// A monitor may reach the health check by a container or service name. It tells nothing
// about the library, so it answers under any name.
func TestHealthAnswersUnderAnyName(t *testing.T) {
	h := newHarness(t, nil)
	rec := h.do(http.MethodGet, "/api/health", "", func(r *http.Request) { r.Host = "removarr:7272" })
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	rec = h.do(http.MethodGet, "/api/health/", "", func(r *http.Request) { r.Host = "removarr:7272" })
	if rec.Code != http.StatusForbidden {
		t.Errorf("another path under an unlisted name: status = %d, want 403", rec.Code)
	}
}

// DNS rebinding: a page on another site points its own name at removarr's address. The
// browser then sends that name as Host and as Origin, so the two agree, and the page can
// set any header. Only the Host check tells this apart from removarr's own UI.
func TestGuardStopsARequestMadeThroughAnotherSitesName(t *testing.T) {
	h := newHarness(t, nil)
	rebound := func(r *http.Request) {
		r.Host = "evil.example:7272"
		r.Header.Set("Origin", "http://evil.example:7272")
	}

	rec := h.do(http.MethodPut, "/api/settings", `{"connections":{"sonarr":{"url":"http://evil.example"}}}`, rebound)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("change: status = %d, want 403: %s", rec.Code, rec.Body)
	}
	if got := decode[errorBody](t, rec); !strings.Contains(got.Error, "REMOVARR_ALLOWED_HOSTS") {
		t.Errorf("error = %q, want it to name the setting that allows a host name", got.Error)
	}
	if v, _, _ := h.config.Conn(context.Background(), "sonarr.url"); v != "" {
		t.Errorf("the change was saved: sonarr.url = %q", v)
	}
	if rec := h.do(http.MethodGet, "/api/settings", "", rebound); rec.Code != http.StatusForbidden {
		t.Errorf("read: status = %d, want 403", rec.Code)
	}
	if rec := h.do(http.MethodPost, "/api/connections/sonarr/test", "", rebound); rec.Code != http.StatusForbidden {
		t.Errorf("connection test: status = %d, want 403", rec.Code)
	}
}

type fakeExecutor struct {
	plan   string
	result executor.Result
	err    error
}

func (f *fakeExecutor) Execute(_ context.Context, planID string) (executor.Result, error) {
	f.plan = planID
	return f.result, f.err
}

type fakePlanner struct {
	rows []domain.RowRef
	plan domain.Plan
	err  error
}

func (f *fakePlanner) Plan(_ context.Context, rows []domain.RowRef) (domain.Plan, error) {
	f.rows = rows
	return f.plan, f.err
}
