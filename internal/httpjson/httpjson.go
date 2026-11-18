// Package httpjson holds the small HTTP helpers the provider clients share.
package httpjson

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// StatusError is returned when a server answers with a status outside 200 to 299.
type StatusError struct {
	Status int
	Body   string // the first 200 bytes of the response, for diagnostics
}

func (e *StatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("unexpected status %d", e.Status)
	}
	return fmt.Sprintf("unexpected status %d: %s", e.Status, e.Body)
}

// Get sends a GET request and decodes the JSON response into out. A nil out skips decoding.
// Credentials belong in header, never in rawURL, so they cannot end up in error messages.
func Get(ctx context.Context, c *http.Client, rawURL string, header http.Header, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	for name, values := range header {
		req.Header[name] = values
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return &StatusError{Status: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// ParseTime parses the timestamp formats the supported apps use. It returns nil when s is
// empty, is not a timestamp, or is a placeholder date such as year 1.
func ParseTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05"} {
		t, err := time.Parse(layout, s)
		if err != nil {
			continue
		}
		if t.Year() < 1900 {
			return nil
		}
		u := t.UTC()
		return &u
	}
	return nil
}
