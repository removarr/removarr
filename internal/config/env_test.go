package config

import (
	"strings"
	"testing"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestLoadEnvDefaults(t *testing.T) {
	e, err := LoadEnv(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if e.Port != 7272 || e.ConfigDir != "/config" || e.DryRun || len(e.Conn) != 0 {
		t.Errorf("defaults wrong: %+v", e)
	}
}

func TestLoadEnvReadsEverything(t *testing.T) {
	e, err := LoadEnv(env(map[string]string{
		"REMOVARR_PORT":                 "9000",
		"REMOVARR_CONFIG_DIR":           "/data",
		"REMOVARR_DRY_RUN":              "true",
		"REMOVARR_SONARR_URL":           " http://pi:8989/sonarr/ ",
		"REMOVARR_SONARR_API_KEY":       "abc",
		"REMOVARR_QBITTORRENT_USERNAME": " admin ",
		"REMOVARR_QBITTORRENT_PASSWORD": " secret ",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if e.Port != 9000 || e.ConfigDir != "/data" || !e.DryRun {
		t.Errorf("basic values wrong: %+v", e)
	}
	want := map[string]string{
		"sonarr.url":           "http://pi:8989/sonarr",
		"sonarr.api_key":       "abc",
		"qbittorrent.username": "admin",
		"qbittorrent.password": " secret ", // secrets are kept exactly as given
	}
	for k, v := range want {
		if e.Conn[k] != v {
			t.Errorf("Conn[%q] = %q, want %q", k, e.Conn[k], v)
		}
	}
	if len(e.Conn) != len(want) {
		t.Errorf("Conn = %v, want only the fields that were set", e.Conn)
	}
}

func TestLoadEnvRejectsBadValues(t *testing.T) {
	tests := []struct{ name, value string }{
		{"REMOVARR_PORT", "abc"},
		{"REMOVARR_PORT", "0"},
		{"REMOVARR_PORT", "70000"},
		{"REMOVARR_DRY_RUN", "maybe"},
		{"REMOVARR_RADARR_URL", "pi:7878"},
		{"REMOVARR_JELLYFIN_URL", "ftp://pi"},
	}
	for _, tt := range tests {
		t.Run(tt.name+"="+tt.value, func(t *testing.T) {
			_, err := LoadEnv(env(map[string]string{tt.name: tt.value}))
			if err == nil {
				t.Fatal("want an error")
			}
			if !strings.Contains(err.Error(), tt.name) {
				t.Errorf("error %q must name the variable %s", err, tt.name)
			}
		})
	}
}

func TestNormalizeURL(t *testing.T) {
	good := map[string]string{
		"http://pi:8989":                 "http://pi:8989",
		"http://pi:8989/":                "http://pi:8989",
		"http://pi:8989/sonarr/":         "http://pi:8989/sonarr",
		"  https://media.example/jf  ":   "https://media.example/jf",
		"http://pi:8989/sonarr?x=1#frag": "http://pi:8989/sonarr",
	}
	for in, want := range good {
		got, err := NormalizeURL(in)
		if err != nil || got != want {
			t.Errorf("NormalizeURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "pi:8989", "ftp://pi", "http://", "sonarr"} {
		if got, err := NormalizeURL(in); err == nil {
			t.Errorf("NormalizeURL(%q) = %q, want an error", in, got)
		}
	}
}

func TestFieldsAreConsistent(t *testing.T) {
	keys, envs := map[string]bool{}, map[string]bool{}
	apps := map[string]bool{}
	for _, f := range Fields {
		if keys[f.Key] || envs[f.Env] {
			t.Errorf("field %+v repeats a key or environment variable", f)
		}
		keys[f.Key], envs[f.Env], apps[f.App] = true, true, true
		if !strings.HasPrefix(f.Env, "REMOVARR_") {
			t.Errorf("environment variable %q must start with REMOVARR_", f.Env)
		}
		if got, ok := FieldFor(f.App, f.Name); !ok || got.Key != f.Key {
			t.Errorf("FieldFor(%q, %q) = %+v, %v", f.App, f.Name, got, ok)
		}
	}
	if len(Fields) != 9 || len(apps) != 4 {
		t.Errorf("%d fields over %d apps, want 9 over 4", len(Fields), len(apps))
	}
	if _, ok := FieldFor("sonarr", "password"); ok {
		t.Error("sonarr has no password field")
	}
}
