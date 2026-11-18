// Package config resolves removarr's configuration: environment variables, and the
// settings saved through the API, which take priority (spec section 9).
package config

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const (
	AppJellyfin    = "jellyfin"
	AppSonarr      = "sonarr"
	AppRadarr      = "radarr"
	AppQBittorrent = "qbittorrent"
)

// Apps lists the apps removarr connects to.
var Apps = []string{AppJellyfin, AppSonarr, AppRadarr, AppQBittorrent}

// Field describes one connection setting.
type Field struct {
	Key    string // settings key, for example "jellyfin.api_key"
	App    string // one of Apps
	Name   string // name within the app in API payloads: "url", "apiKey", "username" or "password"
	Env    string // environment variable
	Secret bool   // write-only: never returned by the API
	URL    bool   // validated and normalised as a base URL
}

// Fields lists every connection setting (spec 9.1).
var Fields = []Field{
	{Key: "jellyfin.url", App: AppJellyfin, Name: "url", Env: "REMOVARR_JELLYFIN_URL", URL: true},
	{Key: "jellyfin.api_key", App: AppJellyfin, Name: "apiKey", Env: "REMOVARR_JELLYFIN_API_KEY", Secret: true},
	{Key: "sonarr.url", App: AppSonarr, Name: "url", Env: "REMOVARR_SONARR_URL", URL: true},
	{Key: "sonarr.api_key", App: AppSonarr, Name: "apiKey", Env: "REMOVARR_SONARR_API_KEY", Secret: true},
	{Key: "radarr.url", App: AppRadarr, Name: "url", Env: "REMOVARR_RADARR_URL", URL: true},
	{Key: "radarr.api_key", App: AppRadarr, Name: "apiKey", Env: "REMOVARR_RADARR_API_KEY", Secret: true},
	{Key: "qbittorrent.url", App: AppQBittorrent, Name: "url", Env: "REMOVARR_QBITTORRENT_URL", URL: true},
	{Key: "qbittorrent.username", App: AppQBittorrent, Name: "username", Env: "REMOVARR_QBITTORRENT_USERNAME"},
	{Key: "qbittorrent.password", App: AppQBittorrent, Name: "password", Env: "REMOVARR_QBITTORRENT_PASSWORD", Secret: true},
}

// FieldFor finds a connection field by app and name.
func FieldFor(app, name string) (Field, bool) {
	for _, f := range Fields {
		if f.App == app && f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

// Env is removarr's configuration from the environment.
type Env struct {
	Port      int
	ConfigDir string
	DryRun    bool
	Conn      map[string]string // connection field key -> value, for the fields that are set
}

// LoadEnv reads the environment through getenv. Errors name the offending variable.
func LoadEnv(getenv func(string) string) (Env, error) {
	env := Env{Port: 7272, ConfigDir: "/config", Conn: map[string]string{}}

	if v := strings.TrimSpace(getenv("REMOVARR_PORT")); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil || port < 1 || port > 65535 {
			return Env{}, fmt.Errorf("REMOVARR_PORT: %q is not a port number", v)
		}
		env.Port = port
	}
	if v := strings.TrimSpace(getenv("REMOVARR_CONFIG_DIR")); v != "" {
		env.ConfigDir = v
	}
	if v := strings.TrimSpace(getenv("REMOVARR_DRY_RUN")); v != "" {
		dry, err := strconv.ParseBool(v)
		if err != nil {
			return Env{}, fmt.Errorf("REMOVARR_DRY_RUN: %q is not true or false", v)
		}
		env.DryRun = dry
	}
	for _, f := range Fields {
		v := getenv(f.Env)
		if !f.Secret {
			v = strings.TrimSpace(v)
		}
		if v == "" {
			continue
		}
		if f.URL {
			normalized, err := NormalizeURL(v)
			if err != nil {
				return Env{}, fmt.Errorf("%s: %w", f.Env, err)
			}
			v = normalized
		}
		env.Conn[f.Key] = v
	}
	return env, nil
}

// NormalizeURL validates an app's base URL and removes trailing slashes, the query and the
// fragment. A path prefix such as /sonarr is kept. A URL with a username or password is
// rejected, because the API would return it. So is a URL with no host name or with a path
// starting "//", which is a scheme written twice or a stray double slash. The errors are fixed texts: they never repeat
// the value, which may hold a credential.
func NormalizeURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err == nil && u.User != nil {
		return "", errors.New("must not contain a username or password")
	}
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || strings.HasPrefix(u.Path, "//") {
		return "", errors.New("not an http or https URL")
	}
	u.RawQuery, u.Fragment = "", ""
	return strings.TrimRight(u.String(), "/"), nil
}
