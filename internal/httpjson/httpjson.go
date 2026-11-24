// Package httpjson holds the small HTTP helpers the provider clients share.
package httpjson

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	return Do(ctx, c, http.MethodGet, rawURL, header, nil, out)
}

// Do sends a request and decodes the JSON response into out. A nil body sends none; any other
// body is sent as JSON. A nil out skips decoding. Credentials belong in header, never in
// rawURL, so they cannot end up in error messages.
func Do(ctx context.Context, c *http.Client, method, rawURL string, header http.Header, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
	if err != nil {
		return err
	}
	for name, values := range header {
		req.Header[name] = values
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return &StatusError{Status: resp.StatusCode, Body: strings.TrimSpace(string(snippet))}
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

// SameHostRedirects is an http.Client CheckRedirect policy that refuses to follow a redirect
// to another host or port, or from https to http. removarr contacts only the apps configured
// in its settings (spec section 10), and Go would otherwise forward a custom header such as
// X-Api-Key to wherever a server redirects, encrypted or not. It stops after 10 redirects,
// as Go's default policy does.
func SameHostRedirects(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	// Go turns any method but GET or HEAD into a GET when it follows a 301, 302 or 303. For a
	// write that would run a read and report success while nothing changed.
	if req.Method != via[0].Method {
		return fmt.Errorf("refusing to follow a redirect that changes the method from %s to %s", via[0].Method, req.Method)
	}
	if !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
		return fmt.Errorf("refusing to follow a redirect from %s to %s", via[0].URL.Host, req.URL.Host)
	}
	if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
		return fmt.Errorf("refusing to follow a redirect from https to %s on %s", req.URL.Scheme, req.URL.Host)
	}
	return nil
}
