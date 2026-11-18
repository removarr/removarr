package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/config"
	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/providers"
	"github.com/removarr/removarr/internal/scanner"
	"github.com/removarr/removarr/internal/store"
)

var ctx = context.Background()

type memStore map[string]string

func (m memStore) GetSetting(_ context.Context, k string) (string, bool, error) {
	v, ok := m[k]
	return v, ok, nil
}
func (m memStore) SetSetting(_ context.Context, k, v string) error { m[k] = v; return nil }
func (m memStore) DeleteSetting(_ context.Context, k string) error { delete(m, k); return nil }

func apps(conn map[string]string) *Apps {
	env := config.Env{Port: 7272, ConfigDir: "/config", Conn: conn}
	return &Apps{Config: config.NewService(env, memStore{}), Timeout: time.Second}
}

func TestProvidersAreNilUntilConfigured(t *testing.T) {
	p, err := apps(map[string]string{}).Providers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p.MediaServer != nil || p.Movies != nil || p.Series != nil || p.Downloads != nil {
		t.Errorf("nothing is configured, every provider must be nil: %+v", p)
	}
}

func TestProvidersForConfiguredApps(t *testing.T) {
	p, err := apps(map[string]string{
		"radarr.url":      "http://radarr:7878",
		"radarr.api_key":  "rk",
		"qbittorrent.url": "http://qbit:8080",
		"sonarr.url":      "http://sonarr:8989", // no API key: incomplete
	}).Providers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p.Movies == nil || p.Downloads == nil {
		t.Errorf("radarr and qbittorrent are configured: %+v", p)
	}
	if p.Series != (incomplete{}) {
		t.Errorf("sonarr is half-configured, want the incomplete stand-in: %+v", p)
	}
	if p.MediaServer != nil {
		t.Errorf("jellyfin is not configured: %+v", p)
	}
}

func TestProvidersForIncompleteApps(t *testing.T) {
	// Each read the scanner makes of a provider, so every one can be checked.
	reads := map[string]func(p scanner.Providers) (bool, error){
		"Users": func(p scanner.Providers) (bool, error) {
			if p.MediaServer == nil {
				return false, nil
			}
			_, err := p.MediaServer.Users(ctx)
			return true, err
		},
		"Library": func(p scanner.Providers) (bool, error) {
			if p.MediaServer == nil {
				return false, nil
			}
			_, err := p.MediaServer.Library(ctx, []string{"a"})
			return true, err
		},
		"Movies": func(p scanner.Providers) (bool, error) {
			if p.Movies == nil {
				return false, nil
			}
			_, err := p.Movies.Movies(ctx)
			return true, err
		},
		"Series": func(p scanner.Providers) (bool, error) {
			if p.Series == nil {
				return false, nil
			}
			_, err := p.Series.Series(ctx)
			return true, err
		},
		"Downloads": func(p scanner.Providers) (bool, error) {
			if p.Downloads == nil {
				return false, nil
			}
			_, err := p.Downloads.Downloads(ctx)
			return true, err
		},
	}
	tests := []struct {
		name  string
		conn  map[string]string
		reads []string
	}{
		{"radarr URL without API key", map[string]string{"radarr.url": "http://radarr:7878"}, []string{"Movies"}},
		{"radarr API key without URL", map[string]string{"radarr.api_key": "rk"}, []string{"Movies"}},
		{"sonarr URL without API key", map[string]string{"sonarr.url": "http://sonarr:8989"}, []string{"Series"}},
		{"sonarr API key without URL", map[string]string{"sonarr.api_key": "sk"}, []string{"Series"}},
		{"jellyfin URL without API key", map[string]string{"jellyfin.url": "http://jellyfin:8096"}, []string{"Users", "Library"}},
		{"jellyfin API key without URL", map[string]string{"jellyfin.api_key": "jk"}, []string{"Users", "Library"}},
		{"qbittorrent username without URL", map[string]string{"qbittorrent.username": "admin"}, []string{"Downloads"}},
		{"qbittorrent password without URL", map[string]string{"qbittorrent.password": "secret"}, []string{"Downloads"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := apps(tt.conn).Providers(ctx)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]bool{}
			for _, name := range tt.reads {
				want[name] = true
			}
			for name, read := range reads {
				set, err := read(p)
				switch {
				case want[name] && !set:
					t.Errorf("%s: the provider is nil, want a stand-in whose reads fail", name)
				case want[name] && !errors.Is(err, ErrIncomplete):
					t.Errorf("%s: error = %v, want ErrIncomplete", name, err)
				case !want[name] && set:
					t.Errorf("%s: the provider is set, but nothing of that app was entered", name)
				}
			}
		})
	}
}

// TestAHalfConfiguredManagerBlocksTheOrphanList runs a whole scan through the real
// settings, store, provider clients and scanner: Sonarr is complete, Radarr has only a URL.
// Radarr's torrents must not be listed as orphans; the torrent list must report an error.
func TestAHalfConfiguredManagerBlocksTheOrphanList(t *testing.T) {
	sonarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "history") || strings.Contains(r.URL.Path, "queue") {
			fmt.Fprint(w, `{"totalRecords":0,"records":[]}`)
			return
		}
		fmt.Fprint(w, `[]`)
	}))
	defer sonarrSrv.Close()
	qbitSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/torrents/info":
			fmt.Fprint(w, `[{"hash":"ABC123","name":"A Movie (2024)","size":1000,"state":"uploading",
				"category":"radarr","content_path":"/downloads/A Movie (2024)","added_on":1700000000,
				"ratio":1.5,"seeding_time":86400,"tracker":"https://tracker.example/announce"}]`)
		default:
			fmt.Fprint(w, `[]`)
		}
	}))
	defer qbitSrv.Close()

	st, err := store.Open(filepath.Join(t.TempDir(), "removarr.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	env := config.Env{Port: 7272, ConfigDir: t.TempDir(), Conn: map[string]string{
		"sonarr.url":      sonarrSrv.URL,
		"sonarr.api_key":  "sk",
		"radarr.url":      "http://radarr.invalid:7878", // no API key
		"qbittorrent.url": qbitSrv.URL,
	}}
	cfg := config.NewService(env, st)
	a := &Apps{Config: cfg, Timeout: 5 * time.Second}
	sc := &scanner.Service{
		Build: a.Providers,
		Rules: cfg.Rules,
		Store: st,
		Now:   time.Now,
		Log:   slog.New(slog.DiscardHandler),
	}
	if err := sc.Run(ctx); err != nil {
		t.Fatal(err)
	}

	rec, err := st.LatestScan(ctx)
	if err != nil || rec == nil {
		t.Fatalf("LatestScan = %v, %v", rec, err)
	}
	torrents := rec.Summary.Lists[domain.ListTorrents]
	const want = "The movie manager could not be read: its connection settings are incomplete"
	if torrents.Error != want {
		t.Errorf("torrent list error = %q, want %q", torrents.Error, want)
	}
	listed, err := st.Candidates(ctx, rec.ID, domain.ListTorrents)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Errorf("stored %d torrent candidates, want none: %+v", len(listed), listed)
	}
}

func TestTesterAndMediaServer(t *testing.T) {
	a := apps(map[string]string{
		"jellyfin.url":     "http://jellyfin:8096",
		"jellyfin.api_key": "jk",
		"qbittorrent.url":  "http://qbit:8080",
	})
	for _, name := range []string{config.AppJellyfin, config.AppQBittorrent} {
		if tester, err := a.Tester(ctx, name); err != nil || tester == nil {
			t.Errorf("Tester(%s) = %v, %v; want a tester", name, tester, err)
		}
	}
	for _, name := range []string{config.AppSonarr, config.AppRadarr, "plex"} {
		if _, err := a.Tester(ctx, name); !errors.Is(err, providers.ErrNotConfigured) {
			t.Errorf("Tester(%s) error = %v, want ErrNotConfigured", name, err)
		}
	}
	if ms, err := a.MediaServer(ctx); err != nil || ms == nil {
		t.Errorf("MediaServer = %v, %v", ms, err)
	}
	if _, err := apps(map[string]string{}).MediaServer(ctx); !errors.Is(err, providers.ErrNotConfigured) {
		t.Errorf("MediaServer without settings: error = %v, want ErrNotConfigured", err)
	}
}

func TestAnAppThatRedirectsToAnotherHostIsNotFollowed(t *testing.T) {
	var reached atomic.Int32
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Add(1) }))
	defer b.Close()
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, b.URL, http.StatusFound)
	}))
	defer a.Close()

	tester, err := apps(map[string]string{"sonarr.url": a.URL, "sonarr.api_key": "k"}).Tester(ctx, config.AppSonarr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tester.Test(ctx); err == nil {
		t.Error("Test must fail when the app redirects to another host")
	}
	if n := reached.Load(); n != 0 {
		t.Errorf("the other host received %d requests, want none (the API key must not leave the app)", n)
	}
}
