package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Fingerprint identifies the settings a scan depends on, so the UI can tell when the stored
// lists were computed with other settings (spec 5.4). It covers the rules and each app's URL
// and user name, as resolved from the UI and the environment. Secrets are left out: they are
// write-only, and a new key for the same app does not change what the app answers.
//
// The rules and the connections are read one after the other. A change in between gives a
// fingerprint that matches neither state, so the lists are reported out of date: the safe
// direction.
func (s *Service) Fingerprint(ctx context.Context) (string, error) {
	rules, err := s.Rules(ctx)
	if err != nil {
		return "", err
	}
	conns, err := s.ConnView(ctx)
	if err != nil {
		return "", err
	}
	apps := map[string]map[string]string{}
	for app, fields := range conns {
		apps[app] = map[string]string{}
		for name, f := range fields {
			if f.Value != "" { // ConnView never sets Value for a secret
				apps[app][name] = f.Value
			}
		}
	}
	// encoding/json sorts map keys, so equal settings give equal bytes.
	raw, err := json.Marshal(struct {
		Rules any                          `json:"rules"`
		Apps  map[string]map[string]string `json:"apps"`
	}{rules, apps})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
