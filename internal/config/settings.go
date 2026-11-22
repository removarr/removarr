package config

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/removarr/removarr/internal/domain"
)

const (
	connPrefix = "conn."
	rulesKey   = "rules"
)

// SettingsStore is the persistence the service needs. *store.Store satisfies it.
type SettingsStore interface {
	GetSetting(ctx context.Context, key string) (value string, ok bool, err error)
	// ApplySettings stores and removes several settings as one step: all of it or none.
	ApplySettings(ctx context.Context, set map[string]string, remove []string) error
}

// Source says where a connection value comes from.
type Source string

const (
	SourceUI   Source = "ui"
	SourceEnv  Source = "env"
	SourceNone Source = "none"
)

// Service resolves connection settings and rules.
type Service struct {
	env   Env
	store SettingsStore
	// mu makes a change of several settings appear at once. Apply holds it for writing, and
	// a read of more than one setting holds it for reading. Without it a scan could read an
	// app's new URL together with its old API key, and send that key to the new address.
	mu sync.RWMutex
}

// NewService returns a Service over the environment and the saved settings.
func NewService(env Env, store SettingsStore) *Service {
	return &Service{env: env, store: store}
}

// Env returns the configuration read from the environment.
func (s *Service) Env() Env { return s.env }

// Conn resolves one connection field: a saved value wins, then the environment (spec 9.2).
func (s *Service) Conn(ctx context.Context, key string) (string, Source, error) {
	saved, ok, err := s.store.GetSetting(ctx, connPrefix+key)
	if err != nil {
		return "", SourceNone, err
	}
	if ok && saved != "" {
		return saved, SourceUI, nil
	}
	if v := s.env.Conn[key]; v != "" {
		return v, SourceEnv, nil
	}
	return "", SourceNone, nil
}

// CheckConn validates a value for a connection field without saving it, and returns the
// value to store. An invalid URL gives a *domain.FieldError.
func CheckConn(f Field, value string) (string, error) {
	if !f.Secret {
		value = strings.TrimSpace(value)
	}
	if value == "" || !f.URL {
		return value, nil
	}
	normalized, err := NormalizeURL(value)
	if err != nil {
		return "", &domain.FieldError{Field: "connections." + f.App + "." + f.Name, Message: err.Error()}
	}
	return normalized, nil
}

// ConnChange is a new value for one connection field. An empty value clears the saved
// value, so the environment variable applies again.
type ConnChange struct {
	Field Field
	Value string
}

// Apply saves new rules and new connection values as one step: either all of them are
// stored or none, and no reader sees only some of them. rules may be nil. Everything is
// validated before anything is saved.
func (s *Service) Apply(ctx context.Context, rules *domain.Rules, conns []ConnChange) error {
	set := map[string]string{}
	var remove []string
	if rules != nil {
		r := withLists(*rules)
		if err := r.Validate(); err != nil {
			return err
		}
		raw, err := json.Marshal(r)
		if err != nil {
			return err
		}
		set[rulesKey] = string(raw)
	}
	for _, c := range conns {
		v, err := CheckConn(c.Field, c.Value)
		if err != nil {
			return err
		}
		if v == "" {
			remove = append(remove, connPrefix+c.Field.Key)
		} else {
			set[connPrefix+c.Field.Key] = v
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.ApplySettings(ctx, set, remove)
}

// SetConn saves a connection field. An empty value clears the saved value, so the
// environment variable applies again.
func (s *Service) SetConn(ctx context.Context, f Field, value string) error {
	return s.Apply(ctx, nil, []ConnChange{{Field: f, Value: value}})
}

// FieldView describes one connection field for the settings page.
type FieldView struct {
	Value  string `json:"value,omitempty"` // never set for secrets
	Set    bool   `json:"set"`
	Source Source `json:"source"`
}

// ConnView describes every connection field, keyed by app and field name, without
// revealing secrets.
func (s *Service) ConnView(ctx context.Context) (map[string]map[string]FieldView, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]map[string]FieldView{}
	for _, f := range Fields {
		v, source, err := s.Conn(ctx, f.Key)
		if err != nil {
			return nil, err
		}
		view := FieldView{Set: v != "", Source: source}
		if !f.Secret {
			view.Value = v
		}
		if out[f.App] == nil {
			out[f.App] = map[string]FieldView{}
		}
		out[f.App][f.Name] = view
	}
	return out, nil
}

// AppConn is the resolved connection of one app.
type AppConn struct {
	URL      string
	APIKey   string
	Username string
	Password string
}

// App returns the resolved connection of one app. configured is false while a required
// field is missing: the URL, and the API key for every app except qBittorrent, which can
// allow access without a login.
func (s *Service) App(ctx context.Context, app string) (AppConn, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var c AppConn
	for _, f := range Fields {
		if f.App != app {
			continue
		}
		v, _, err := s.Conn(ctx, f.Key)
		if err != nil {
			return AppConn{}, false, err
		}
		switch f.Name {
		case "url":
			c.URL = v
		case "apiKey":
			c.APIKey = v
		case "username":
			c.Username = v
		case "password":
			c.Password = v
		}
	}
	configured := c.URL != "" && (app == AppQBittorrent || c.APIKey != "")
	return c, configured, nil
}

// Rules returns the saved rules. Fields that were never saved take their defaults.
func (s *Service) Rules(ctx context.Context) (domain.Rules, error) {
	r := domain.DefaultRules()
	raw, ok, err := s.store.GetSetting(ctx, rulesKey)
	if err != nil || !ok {
		return r, err
	}
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return domain.DefaultRules(), fmt.Errorf("the saved rules are not valid JSON: %w", err)
	}
	return withLists(r), nil
}

// SetRules validates and saves the rules.
func (s *Service) SetRules(ctx context.Context, r domain.Rules) error {
	return s.Apply(ctx, &r, nil)
}

// withLists replaces nil lists with empty ones so they serialise as [] and not null.
func withLists(r domain.Rules) domain.Rules {
	if r.CountingUsers == nil {
		r.CountingUsers = []string{}
	}
	if r.ExcludedCategories == nil {
		r.ExcludedCategories = []string{}
	}
	if r.ExcludedTags == nil {
		r.ExcludedTags = []string{}
	}
	return r
}
