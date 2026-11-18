// Package qbittorrent reads torrents from qBittorrent's Web API (v2).
package qbittorrent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/httpjson"
	"github.com/removarr/removarr/internal/providers"
)

var (
	_ providers.DownloadClient = (*Client)(nil)
	_ providers.Tester         = (*Client)(nil)
)

var (
	// ErrLogin is returned when qBittorrent rejects the username or password.
	ErrLogin = errors.New("qBittorrent rejected the username or password")
	// ErrLoginRequired is returned when qBittorrent asks for a login and none is configured.
	ErrLoginRequired = errors.New("qBittorrent requires a username and password")
)

// Client talks to one qBittorrent instance and keeps its session cookie.
type Client struct {
	baseURL  string
	username string
	password string
	http     *http.Client
}

// New returns a client. baseURL has no trailing slash. An empty username means qBittorrent
// is expected to allow access without a login.
func New(baseURL, username, password string, timeout time.Duration) (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &Client{
		baseURL:  baseURL,
		username: username,
		password: password,
		http:     &http.Client{Jar: jar, Timeout: timeout},
	}, nil
}

type torrentJSON struct {
	Hash        string  `json:"hash"`
	Name        string  `json:"name"`
	Size        int64   `json:"size"`
	State       string  `json:"state"`
	Category    string  `json:"category"`
	Tags        string  `json:"tags"`
	ContentPath string  `json:"content_path"`
	AddedOn     int64   `json:"added_on"`
	Ratio       float64 `json:"ratio"`
	SeedingTime int64   `json:"seeding_time"`
	Tracker     string  `json:"tracker"`
}

// Downloads returns every torrent.
func (c *Client) Downloads(ctx context.Context) ([]domain.Download, error) {
	body, err := c.get(ctx, "/api/v2/torrents/info", nil)
	if err != nil {
		return nil, fmt.Errorf("torrents: %w", err)
	}
	var raw []torrentJSON
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("torrents: decode response: %w", err)
	}

	out := make([]domain.Download, 0, len(raw))
	for _, t := range raw {
		hash := strings.ToLower(strings.TrimSpace(t.Hash))
		if hash == "" {
			continue
		}
		ratio := t.Ratio
		seeded := time.Duration(t.SeedingTime) * time.Second
		tracker := trackerHost(t.Tracker)
		if tracker == "" {
			tracker = c.firstTracker(ctx, hash)
		}
		out = append(out, domain.Download{
			ID:          hash,
			Name:        t.Name,
			Size:        t.Size,
			State:       t.State,
			Category:    t.Category,
			Tags:        splitTags(t.Tags),
			ContentPath: t.ContentPath,
			AddedAt:     time.Unix(t.AddedOn, 0).UTC(),
			Ratio:       &ratio,
			SeedingTime: &seeded,
			Tracker:     tracker,
		})
	}
	return out, nil
}

// Test checks the connection and returns the app name and version.
func (c *Client) Test(ctx context.Context) (string, error) {
	body, err := c.get(ctx, "/api/v2/app/version", nil)
	if err != nil {
		return "", err
	}
	return "qBittorrent " + strings.TrimSpace(string(body)), nil
}

// firstTracker asks for a torrent's tracker list and returns the host of its first real
// tracker. It is used when qBittorrent reports no working tracker. Any failure gives "".
func (c *Client) firstTracker(ctx context.Context, hash string) string {
	body, err := c.get(ctx, "/api/v2/torrents/trackers", url.Values{"hash": {hash}})
	if err != nil {
		return ""
	}
	var raw []struct {
		URL string `json:"url"`
	}
	if json.Unmarshal(body, &raw) != nil {
		return ""
	}
	for _, t := range raw {
		if host := trackerHost(t.URL); host != "" {
			return host
		}
	}
	return ""
}

// get requests path and returns the response body. When qBittorrent answers 403 the client
// logs in and tries once more.
func (c *Client) get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	body, status, err := c.once(ctx, path, q)
	if err != nil {
		return nil, err
	}
	if status == http.StatusForbidden {
		if c.username == "" {
			return nil, ErrLoginRequired
		}
		if err := c.login(ctx); err != nil {
			return nil, err
		}
		if body, status, err = c.once(ctx, path, q); err != nil {
			return nil, err
		}
	}
	if status < 200 || status > 299 {
		return nil, &httpjson.StatusError{Status: status, Body: snippet(body)}
	}
	return body, nil
}

func (c *Client) once(ctx context.Context, path string, q url.Values) ([]byte, int, error) {
	u := c.baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Referer", c.baseURL)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}
	return body, resp.StatusCode, nil
}

// login starts a session. The cookie jar keeps the session cookie for later requests.
func (c *Client) login(ctx context.Context) error {
	form := url.Values{"username": {c.username}, "password": {c.password}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v2/auth/login", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", c.baseURL)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("qBittorrent login: %w", &httpjson.StatusError{Status: resp.StatusCode, Body: snippet(body)})
	}
	if strings.HasPrefix(strings.TrimSpace(string(body)), "Fails") {
		return ErrLogin
	}
	return nil
}

// trackerHost returns the host name of a tracker URL, or "" when raw is not a URL.
func trackerHost(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Hostname()
}

// splitTags splits qBittorrent's comma-separated tag string.
func splitTags(s string) []string {
	var out []string
	for _, tag := range strings.Split(s, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			out = append(out, tag)
		}
	}
	return out
}

func snippet(body []byte) string {
	if len(body) > 200 {
		body = body[:200]
	}
	return strings.TrimSpace(string(body))
}
