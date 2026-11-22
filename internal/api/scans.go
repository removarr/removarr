package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/removarr/removarr/internal/config"
	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/scanner"
	"github.com/removarr/removarr/internal/store"
)

// startScan starts a scan in the background.
func (s *Server) startScan(w http.ResponseWriter, r *http.Request) {
	err := s.Scanner.Start()
	if errors.Is(err, scanner.ErrRunning) {
		writeError(w, http.StatusConflict, "A scan is already running.")
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"started": true})
}

type scanResponse struct {
	Status scanner.Status    `json:"status"`
	Last   *store.ScanRecord `json:"last"`
}

// currentScan reports the scan in progress and the latest finished scan.
func (s *Server) currentScan(w http.ResponseWriter, r *http.Request) {
	last, err := s.Store.LatestScan(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, scanResponse{Status: s.Scanner.Status(), Last: last})
}

type candidatesResponse struct {
	List       domain.ListKind    `json:"list"`
	ScannedAt  *time.Time         `json:"scannedAt"`
	Error      string             `json:"error,omitempty"`
	Candidates []domain.Candidate `json:"candidates"`
}

// candidates returns the rows of one list from the latest finished scan.
func (s *Server) candidates(w http.ResponseWriter, r *http.Request) {
	list := domain.ListKind(r.URL.Query().Get("list"))
	switch list {
	case domain.ListMovies, domain.ListSeries, domain.ListTorrents:
	default:
		writeError(w, http.StatusBadRequest, `list must be "movies", "series" or "torrents"`)
		return
	}

	// The scan and its rows are read together: a scan that finishes meanwhile drops the
	// rows of the scan before it.
	last, rows, err := s.Store.LatestList(r.Context(), list)
	if err != nil {
		s.fail(w, err)
		return
	}
	resp := candidatesResponse{List: list, Candidates: []domain.Candidate{}}
	if last != nil {
		resp.ScannedAt = last.FinishedAt
		resp.Error = last.Summary.Lists[list].Error
		resp.Candidates = rows
	}
	writeJSON(w, http.StatusOK, resp)
}

type connectionStatus struct {
	Configured bool `json:"configured"`
}

type statusResponse struct {
	DryRun      bool                        `json:"dryRun"`
	Connections map[string]connectionStatus `json:"connections"`
	Scan        scanner.Status              `json:"scan"`
	LastScan    *store.ScanRecord           `json:"lastScan"`
}

// status gives the header its overview: which apps are configured, the scan in progress,
// and the latest scan with its summary and free space.
func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	resp := statusResponse{
		DryRun:      s.Config.Env().DryRun,
		Connections: map[string]connectionStatus{},
		Scan:        s.Scanner.Status(),
	}
	for _, app := range config.Apps {
		_, configured, err := s.Config.App(r.Context(), app)
		if err != nil {
			s.fail(w, err)
			return
		}
		resp.Connections[app] = connectionStatus{Configured: configured}
	}
	last, err := s.Store.LatestScan(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	resp.LastScan = last
	writeJSON(w, http.StatusOK, resp)
}
