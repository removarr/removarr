package api

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/config"
	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/providers"
	"github.com/removarr/removarr/internal/store"
)

// One request that changes an app's URL and its API key must never be visible half done: a
// scan or a connection test that read the settings in between would send one server's key
// to another server.
func TestPutSettingsChangesAnAppsURLAndKeyTogether(t *testing.T) {
	h := newHarness(t, nil)
	put := func(n int) {
		t.Helper()
		body := fmt.Sprintf(`{"connections":{"sonarr":{"url":"http://sonarr-%d:8989","apiKey":"key-%d"}}}`, n, n)
		if rec := h.do(http.MethodPut, "/api/settings", body); rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body)
		}
	}
	put(0)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	var mixed string // the first half-done pair a reader saw
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			conn, _, err := h.config.App(context.Background(), config.AppSonarr)
			if err != nil {
				mixed = "read failed: " + err.Error()
				return
			}
			url := strings.TrimSuffix(strings.TrimPrefix(conn.URL, "http://sonarr-"), ":8989")
			key := strings.TrimPrefix(conn.APIKey, "key-")
			if url != key {
				mixed = conn.URL + " with " + conn.APIKey
				return
			}
		}
	}()
	for n := 1; n <= 300; n++ {
		put(n)
	}
	close(stop)
	wg.Wait()
	if mixed != "" {
		t.Errorf("a reader saw a half-done change: %s", mixed)
	}
}

// hookedSettings runs a hook once, right after the saved rules were read.
type hookedSettings struct {
	*store.Store
	afterRulesRead func()
}

func (h *hookedSettings) GetSetting(ctx context.Context, key string) (string, bool, error) {
	value, ok, err := h.Store.GetSetting(ctx, key)
	if hook := h.afterRulesRead; key == "rules" && hook != nil {
		h.afterRulesRead = nil
		hook()
	}
	return value, ok, err
}

// Two requests that each change one rule must both take effect. Each reads the saved rules,
// changes its field and saves all rules, so the later save used to undo the earlier one.
func TestConcurrentRuleChangesDoNotUndoEachOther(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "removarr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hooked := &hookedSettings{Store: st}
	h := &harness{
		t:       t,
		store:   st,
		config:  config.NewService(config.Env{Port: 7272, ConfigDir: "/config", Conn: map[string]string{}}, hooked),
		scanner: &fakeScanner{},
		apps:    &fakeApps{testers: map[string]providers.Tester{}},
	}
	h.handler = (&Server{
		Config: h.config, Store: st, Scanner: h.scanner, Apps: h.apps,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}).Handler()

	// While the first request holds the rules it read, a second request arrives. Without
	// anything holding it back it finishes within the wait below.
	second := make(chan int, 1)
	hooked.afterRulesRead = func() {
		done := make(chan int, 1)
		go func() { done <- h.do(http.MethodPut, "/api/settings", `{"rules":{"watchedMode":"any"}}`).Code }()
		select {
		case code := <-done:
			second <- code
		case <-time.After(150 * time.Millisecond):
			go func() { second <- <-done }()
		}
	}
	if rec := h.do(http.MethodPut, "/api/settings", `{"rules":{"graceDays":7}}`); rec.Code != http.StatusOK {
		t.Fatalf("first request: status = %d: %s", rec.Code, rec.Body)
	}
	if code := <-second; code != http.StatusOK {
		t.Fatalf("second request: status = %d", code)
	}

	rules, err := h.config.Rules(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rules.GraceDays != 7 || rules.WatchedMode != domain.WatchedAny {
		t.Errorf("rules = graceDays %d, watchedMode %q; want 7 and %q: one change undid the other",
			rules.GraceDays, rules.WatchedMode, domain.WatchedAny)
	}
}
