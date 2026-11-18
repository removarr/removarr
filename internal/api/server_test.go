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
	t       *testing.T
	store   *store.Store
	config  *config.Service
	scanner *fakeScanner
	apps    *fakeApps
	handler http.Handler
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
		t:       t,
		store:   st,
		config:  config.NewService(config.Env{Port: 7272, ConfigDir: "/config", DryRun: true, Conn: envConn}, st),
		scanner: &fakeScanner{},
		apps:    &fakeApps{testers: map[string]providers.Tester{}},
	}
	srv := &Server{
		Config:  h.config,
		Store:   st,
		Scanner: h.scanner,
		Apps:    h.apps,
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
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
		t.Errorf("status = %d, want 200: reads are not guarded", rec.Code)
	}
}
