package config

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/removarr/removarr/internal/domain"
)

var ctx = context.Background()

// memStore is an in-memory SettingsStore.
type memStore map[string]string

func (m memStore) GetSetting(_ context.Context, k string) (string, bool, error) {
	v, ok := m[k]
	return v, ok, nil
}
func (m memStore) SetSetting(_ context.Context, k, v string) error { m[k] = v; return nil }
func (m memStore) DeleteSetting(_ context.Context, k string) error { delete(m, k); return nil }

func field(t *testing.T, app, name string) Field {
	t.Helper()
	f, ok := FieldFor(app, name)
	if !ok {
		t.Fatalf("no field %s.%s", app, name)
	}
	return f
}

func service(envConn map[string]string) (*Service, memStore) {
	if envConn == nil {
		envConn = map[string]string{}
	}
	st := memStore{}
	return NewService(Env{Port: 7272, ConfigDir: "/config", Conn: envConn}, st), st
}

func TestConnPrecedence(t *testing.T) {
	s, _ := service(map[string]string{"sonarr.url": "http://env:8989"})
	url := field(t, AppSonarr, "url")

	check := func(step, wantValue string, wantSource Source) {
		t.Helper()
		v, src, err := s.Conn(ctx, url.Key)
		if err != nil || v != wantValue || src != wantSource {
			t.Errorf("%s: Conn = %q, %q, %v; want %q, %q", step, v, src, err, wantValue, wantSource)
		}
	}

	check("environment only", "http://env:8989", SourceEnv)

	if err := s.SetConn(ctx, url, "http://ui:8989/"); err != nil {
		t.Fatal(err)
	}
	check("saved value wins", "http://ui:8989", SourceUI)

	if err := s.SetConn(ctx, url, ""); err != nil {
		t.Fatal(err)
	}
	check("cleared, back to the environment", "http://env:8989", SourceEnv)

	if v, src, _ := s.Conn(ctx, field(t, AppRadarr, "url").Key); v != "" || src != SourceNone {
		t.Errorf("unset field: Conn = %q, %q; want empty, none", v, src)
	}
}

func TestSetConnRejectsABadURL(t *testing.T) {
	s, st := service(nil)
	err := s.SetConn(ctx, field(t, AppSonarr, "url"), "pi:8989")
	var fe *domain.FieldError
	if !errors.As(err, &fe) || fe.Field != "connections.sonarr.url" {
		t.Fatalf("err = %v, want a FieldError for connections.sonarr.url", err)
	}
	if len(st) != 0 {
		t.Errorf("nothing must be saved on error, store = %v", st)
	}
}

func TestConnViewHidesSecrets(t *testing.T) {
	s, _ := service(map[string]string{"sonarr.api_key": "env-key"})
	if err := s.SetConn(ctx, field(t, AppRadarr, "url"), "http://radarr:7878"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetConn(ctx, field(t, AppQBittorrent, "password"), "hunter2"); err != nil {
		t.Fatal(err)
	}

	view, err := s.ConnView(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]map[string]FieldView{
		AppJellyfin: {"url": {Source: SourceNone}, "apiKey": {Source: SourceNone}},
		AppSonarr:   {"url": {Source: SourceNone}, "apiKey": {Set: true, Source: SourceEnv}},
		AppRadarr:   {"url": {Value: "http://radarr:7878", Set: true, Source: SourceUI}, "apiKey": {Source: SourceNone}},
		AppQBittorrent: {
			"url":      {Source: SourceNone},
			"username": {Source: SourceNone},
			"password": {Set: true, Source: SourceUI},
		},
	}
	if !reflect.DeepEqual(view, want) {
		t.Errorf("view =\n%+v\nwant\n%+v", view, want)
	}
}

func TestApp(t *testing.T) {
	s, _ := service(map[string]string{
		"sonarr.url":      "http://sonarr:8989",
		"radarr.url":      "http://radarr:7878",
		"radarr.api_key":  "rk",
		"qbittorrent.url": "http://qbit:8080",
	})

	if _, configured, _ := s.App(ctx, AppSonarr); configured {
		t.Error("sonarr has a URL but no API key: not configured")
	}
	conn, configured, err := s.App(ctx, AppRadarr)
	if err != nil || !configured || conn.URL != "http://radarr:7878" || conn.APIKey != "rk" {
		t.Errorf("radarr = %+v, %v, %v", conn, configured, err)
	}
	if _, configured, _ := s.App(ctx, AppQBittorrent); !configured {
		t.Error("qBittorrent needs only a URL: it may allow access without a login")
	}
	if _, configured, _ := s.App(ctx, AppJellyfin); configured {
		t.Error("jellyfin has nothing set: not configured")
	}
}

func TestRules(t *testing.T) {
	s, st := service(nil)

	got, err := s.Rules(ctx)
	if err != nil || !reflect.DeepEqual(got, domain.DefaultRules()) {
		t.Fatalf("Rules on a fresh store = %+v, %v; want the defaults", got, err)
	}

	want := domain.DefaultRules()
	want.CountingUsers = []string{"u1"}
	want.SeriesUnit = domain.UnitSeason
	want.GraceDays = 7
	if err := s.SetRules(ctx, want); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Rules(ctx); !reflect.DeepEqual(got, want) {
		t.Errorf("Rules = %+v, want %+v", got, want)
	}

	bad := want
	bad.GraceDays = -1
	var fe *domain.FieldError
	if err := s.SetRules(ctx, bad); !errors.As(err, &fe) || fe.Field != "graceDays" {
		t.Errorf("err = %v, want a FieldError for graceDays", err)
	}
	if got, _ := s.Rules(ctx); got.GraceDays != 7 {
		t.Errorf("invalid rules must not be saved, GraceDays = %d", got.GraceDays)
	}

	// Rules saved by an older version may lack fields, or hold null lists.
	st["rules"] = `{"graceDays":5,"countingUsers":null}`
	got, err = s.Rules(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.GraceDays != 5 || got.WatchedMode != domain.WatchedAll || got.ProtectionTag != "keep" {
		t.Errorf("missing fields must take their defaults: %+v", got)
	}
	if got.CountingUsers == nil || got.ExcludedTags == nil {
		t.Errorf("lists must never be nil: %+v", got)
	}
}
