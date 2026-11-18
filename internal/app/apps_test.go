package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/config"
	"github.com/removarr/removarr/internal/providers"
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
		"sonarr.url":      "http://sonarr:8989", // no API key: not configured
	}).Providers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p.Movies == nil || p.Downloads == nil {
		t.Errorf("radarr and qbittorrent are configured: %+v", p)
	}
	if p.Series != nil || p.MediaServer != nil {
		t.Errorf("sonarr and jellyfin are not configured: %+v", p)
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
