package api

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/removarr/removarr/internal/config"
	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/providers"
)

type settingsBody struct {
	Rules       domain.Rules                           `json:"rules"`
	Connections map[string]map[string]config.FieldView `json:"connections"`
	DryRun      bool                                   `json:"dryRun"`
}

func TestGetSettings(t *testing.T) {
	h := newHarness(t, map[string]string{"sonarr.url": "http://sonarr:8989", "sonarr.api_key": "env-secret"})
	rec := h.do(http.MethodGet, "/api/settings", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	got := decode[settingsBody](t, rec)
	if !reflect.DeepEqual(got.Rules, domain.DefaultRules()) {
		t.Errorf("rules = %+v, want the defaults", got.Rules)
	}
	if !got.DryRun {
		t.Error("dryRun must reflect the environment")
	}
	if len(got.Connections) != 4 {
		t.Errorf("connections for %d apps, want 4", len(got.Connections))
	}
	if v := got.Connections["sonarr"]["url"]; v.Value != "http://sonarr:8989" || v.Source != config.SourceEnv {
		t.Errorf("sonarr url = %+v", v)
	}
	if v := got.Connections["sonarr"]["apiKey"]; !v.Set || v.Value != "" || v.Source != config.SourceEnv {
		t.Errorf("sonarr apiKey = %+v, want set, from env, without a value", v)
	}
	if strings.Contains(rec.Body.String(), "env-secret") {
		t.Error("the response must never contain an API key")
	}
}

func TestPutSettings(t *testing.T) {
	h := newHarness(t, nil)
	rec := h.do(http.MethodPut, "/api/settings", `{
		"rules": {"graceDays": 7, "countingUsers": ["u1"]},
		"connections": {"sonarr": {"url": "http://sonarr:8989/", "apiKey": "abc123"}}
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	got := decode[settingsBody](t, rec)
	if got.Rules.GraceDays != 7 || !reflect.DeepEqual(got.Rules.CountingUsers, []string{"u1"}) {
		t.Errorf("rules = %+v", got.Rules)
	}
	if got.Rules.WatchedMode != domain.WatchedAll || got.Rules.ProtectionTag != "keep" {
		t.Errorf("fields that were not sent must keep their values: %+v", got.Rules)
	}
	if v := got.Connections["sonarr"]["url"]; v.Value != "http://sonarr:8989" || v.Source != config.SourceUI {
		t.Errorf("sonarr url = %+v, want the normalised URL saved from the UI", v)
	}
	if v := got.Connections["sonarr"]["apiKey"]; !v.Set || v.Source != config.SourceUI {
		t.Errorf("sonarr apiKey = %+v", v)
	}
	if strings.Contains(rec.Body.String(), "abc123") {
		t.Error("the response must never contain an API key")
	}

	// The settings survive in the store.
	saved, err := h.config.Rules(context.Background())
	if err != nil || saved.GraceDays != 7 {
		t.Errorf("saved rules = %+v, %v", saved, err)
	}

	// null clears a saved value.
	rec = h.do(http.MethodPut, "/api/settings", `{"connections": {"sonarr": {"apiKey": null}}}`)
	got = decode[settingsBody](t, rec)
	if v := got.Connections["sonarr"]["apiKey"]; v.Set || v.Source != config.SourceNone {
		t.Errorf("sonarr apiKey after clearing = %+v, want unset", v)
	}
	if v := got.Connections["sonarr"]["url"]; v.Value != "http://sonarr:8989" {
		t.Errorf("sonarr url must be untouched, got %+v", v)
	}
}

func TestPutSettingsRejectsBadInputAndSavesNothing(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		field string
	}{
		{"invalid rule", `{"rules": {"graceDays": -1}}`, "rules.graceDays"},
		{"unknown rule", `{"rules": {"colour": "blue"}}`, "rules"},
		{"bad url", `{"connections": {"sonarr": {"url": "sonarr:8989"}}}`, "connections.sonarr.url"},
		{"unknown field", `{"connections": {"sonarr": {"token": "x"}}}`, "connections.sonarr.token"},
		{"unknown app", `{"connections": {"plex": {"url": "http://plex"}}}`, "connections.plex.url"},
		{"good rules with a bad url", `{"rules": {"graceDays": 9}, "connections": {"radarr": {"url": "nope"}}}`, "connections.radarr.url"},
		{"malformed json", `{"rules": `, ""},
		{"unknown top-level key", `{"theme": "dark"}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, nil)
			rec := h.do(http.MethodPut, "/api/settings", tt.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body)
			}
			got := decode[errorBody](t, rec)
			if got.Error == "" || got.Field != tt.field {
				t.Errorf("error body = %+v, want a message and field %q", got, tt.field)
			}
			rules, _ := h.config.Rules(context.Background())
			if !reflect.DeepEqual(rules, domain.DefaultRules()) {
				t.Errorf("rules were saved despite the error: %+v", rules)
			}
		})
	}
}

func TestConnectionTest(t *testing.T) {
	h := newHarness(t, nil)
	h.apps.testers[config.AppSonarr] = testerFunc(func(context.Context) (string, error) { return "Sonarr 4.0.9", nil })
	h.apps.testers[config.AppRadarr] = testerFunc(func(context.Context) (string, error) {
		return "", errors.New("unexpected status 401: Unauthorized")
	})

	type result struct {
		OK     bool   `json:"ok"`
		Detail string `json:"detail"`
		Error  string `json:"error"`
	}
	tests := []struct {
		app  string
		code int
		want result
	}{
		{"sonarr", http.StatusOK, result{OK: true, Detail: "Sonarr 4.0.9"}},
		{"radarr", http.StatusOK, result{Error: "unexpected status 401: Unauthorized"}},
		{"jellyfin", http.StatusOK, result{Error: "not configured"}},
	}
	for _, tt := range tests {
		rec := h.do(http.MethodPost, "/api/connections/"+tt.app+"/test", "")
		if rec.Code != tt.code {
			t.Errorf("%s: status = %d, want %d", tt.app, rec.Code, tt.code)
			continue
		}
		if got := decode[result](t, rec); got != tt.want {
			t.Errorf("%s: result = %+v, want %+v", tt.app, got, tt.want)
		}
	}
	if rec := h.do(http.MethodPost, "/api/connections/plex/test", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unknown app: status = %d, want 404", rec.Code)
	}
}

type fakeMediaServer struct {
	users []domain.User
	err   error
}

func (f fakeMediaServer) Users(context.Context) ([]domain.User, error) { return f.users, f.err }
func (f fakeMediaServer) Library(context.Context, []string) (domain.Library, error) {
	return domain.Library{}, nil
}

var _ providers.MediaServer = fakeMediaServer{}

func TestUsers(t *testing.T) {
	h := newHarness(t, nil)
	if rec := h.do(http.MethodGet, "/api/media-server/users", ""); rec.Code != http.StatusConflict {
		t.Errorf("not configured: status = %d, want 409", rec.Code)
	}

	h.apps.server = fakeMediaServer{users: []domain.User{{ID: "a", Name: "Alice"}}}
	rec := h.do(http.MethodGet, "/api/media-server/users", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if got := decode[[]domain.User](t, rec); !reflect.DeepEqual(got, []domain.User{{ID: "a", Name: "Alice"}}) {
		t.Errorf("users = %+v", got)
	}

	h.apps.server = fakeMediaServer{err: errors.New("connection refused")}
	if rec := h.do(http.MethodGet, "/api/media-server/users", ""); rec.Code != http.StatusBadGateway {
		t.Errorf("unreachable: status = %d, want 502", rec.Code)
	}
}
