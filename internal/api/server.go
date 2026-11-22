// Package api is removarr's HTTP API (spec 7.6).
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"net/url"

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
	LatestScan(ctx context.Context) (*store.ScanRecord, error)
	LatestList(ctx context.Context, list domain.ListKind) (*store.ScanRecord, []domain.Candidate, error)
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
	return guard(mux)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// guard rejects requests that change something unless they could only have been sent by
// removarr's own UI or a deliberate API client: they must carry RequestHeader, be JSON,
// and, when they name an origin, come from this server's own address.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			if err != nil || u.Host == "" || u.Host != r.Host {
				writeError(w, http.StatusForbidden, "the request does not come from this server's address")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
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
