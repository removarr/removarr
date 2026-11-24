// Package api is removarr's HTTP API (spec 7.6).
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/removarr/removarr/internal/config"
	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/providers"
	"github.com/removarr/removarr/internal/scanner"
	"github.com/removarr/removarr/internal/store"
)

// RequestHeader must be present on every request that changes something (spec section 10).
const RequestHeader = "X-Removarr-Request"

// Scanner is the part of the scanner the API uses. *scanner.Service satisfies it.
type Scanner interface {
	Start() error
	Status() scanner.Status
}

// Store is the part of the store the API uses. *store.Store satisfies it.
type Store interface {
	ReadLatest(ctx context.Context) (store.Latest, error)
	AddIgnores(ctx context.Context, ignores []domain.Ignore) error
	RemoveIgnores(ctx context.Context, rows []domain.RowRef) error
}

// Apps gives access to the configured apps. *app.Apps satisfies it.
type Apps interface {
	Tester(ctx context.Context, app string) (providers.Tester, error)
	MediaServer(ctx context.Context) (providers.MediaServer, error)
}

// Server holds what the handlers need.
type Server struct {
	Config  *config.Service
	Store   Store
	Scanner Scanner
	Apps    Apps
	Log     *slog.Logger
	Now     func() time.Time // nil means time.Now

	// settingsMu lets one settings change run at a time. A change reads the saved rules,
	// merges the request into them and saves the result, so two requests that each change
	// one rule must not work from the same saved copy.
	settingsMu sync.Mutex
}

// Handler returns the API's routes behind the request guard.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("GET /api/settings", s.getSettings)
	mux.HandleFunc("PUT /api/settings", s.putSettings)
	mux.HandleFunc("POST /api/connections/{app}/test", s.testConnection)
	mux.HandleFunc("GET /api/media-server/users", s.users)
	mux.HandleFunc("POST /api/scans", s.startScan)
	mux.HandleFunc("GET /api/scans/current", s.currentScan)
	mux.HandleFunc("GET /api/candidates", s.candidates)
	mux.HandleFunc("POST /api/ignores", s.addIgnores)
	mux.HandleFunc("DELETE /api/ignores", s.removeIgnores)
	return guard(mux, s.Config.Env().AllowedHosts)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// guard decides which requests reach the handlers (spec section 10).
//
// Every request must name this server by an IP address, by localhost or by a host name the
// owner listed. Without that check a page on another site could point its own name at
// removarr's address (DNS rebinding): the browser would treat removarr as that site, let
// the page read every answer and send any header, and Host and Origin would agree. The
// health check is exempt: it tells nothing about the library, and a monitor may reach it by
// a container or service name.
//
// A request that changes something must also be one that only removarr's own UI or a
// deliberate API client sends: it carries RequestHeader, is JSON, and, when it names an
// origin, comes from this server's own address.
func guard(next http.Handler, allowedHosts []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) && r.URL.Path == "/api/health" {
			next.ServeHTTP(w, r)
			return
		}
		if !hostAllowed(r.Host, allowedHosts) {
			writeError(w, http.StatusForbidden,
				"this server does not answer to the host name in the request; reach it by its IP address, or list the name in REMOVARR_ALLOWED_HOSTS")
			return
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get(RequestHeader) == "" {
			writeError(w, http.StatusForbidden, "missing "+RequestHeader+" header")
			return
		}
		if mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mediaType != "application/json" {
			writeError(w, http.StatusUnsupportedMediaType, "the content type must be application/json")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host == "" || !strings.EqualFold(u.Host, r.Host) {
				writeError(w, http.StatusForbidden, "the request does not come from this server's address")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// hostAllowed reports whether a Host header names this server in a way another site cannot
// imitate: an IP address, localhost, or one of the listed names. The port is ignored, but
// the header must be well formed: brackets hold an IPv6 address and nothing else, and a
// port is a number.
func hostAllowed(host string, allowed []string) bool {
	name := host
	if h, port, err := net.SplitHostPort(host); err == nil {
		if _, err := strconv.ParseUint(port, 10, 16); err != nil {
			return false
		}
		name = h
	} else if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		name = host[1 : len(host)-1]
	}
	addr, err := netip.ParseAddr(name) // accepts an IPv6 zone, unlike net.ParseIP
	if strings.ContainsAny(host, "[]") {
		return err == nil && addr.Is6() && strings.HasPrefix(host, "[")
	}
	if err == nil {
		return true
	}
	name = strings.TrimSuffix(strings.ToLower(name), ".")
	return name == "localhost" || (name != "" && slices.Contains(allowed, name))
}

// errorBody is the JSON body of every error response.
type errorBody struct {
	Error string `json:"error"`
	Field string `json:"field,omitempty"` // the setting at fault, for example "rules.graceDays"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}

// fail answers an error from a handler. A *domain.FieldError is the caller's mistake and
// becomes a 400 naming the field. Anything else is logged and becomes a 500.
func (s *Server) fail(w http.ResponseWriter, err error) {
	var fe *domain.FieldError
	if errors.As(err, &fe) {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: fe.Message, Field: fe.Field})
		return
	}
	s.Log.Error("request failed", "error", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
