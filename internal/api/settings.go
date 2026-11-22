package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/removarr/removarr/internal/config"
	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/providers"
)

type settingsResponse struct {
	Rules       domain.Rules                           `json:"rules"`
	Connections map[string]map[string]config.FieldView `json:"connections"`
	DryRun      bool                                   `json:"dryRun"`
}

func (s *Server) settings(ctx context.Context) (settingsResponse, error) {
	rules, err := s.Config.Rules(ctx)
	if err != nil {
		return settingsResponse{}, err
	}
	connections, err := s.Config.ConnView(ctx)
	if err != nil {
		return settingsResponse{}, err
	}
	return settingsResponse{Rules: rules, Connections: connections, DryRun: s.Config.Env().DryRun}, nil
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	resp, err := s.settings(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

type settingsRequest struct {
	Rules       json.RawMessage               `json:"rules"`
	Connections map[string]map[string]*string `json:"connections"`
}

// putSettings validates the whole request before saving any of it, and saves it as one step.
func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req settingsRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "the request body is not valid: "+err.Error())
		return
	}

	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()

	var rules *domain.Rules
	if len(req.Rules) > 0 && string(req.Rules) != "null" {
		merged, err := s.Config.Rules(ctx)
		if err != nil {
			s.fail(w, err)
			return
		}
		// Decoding over the saved rules changes only the fields the request names.
		rulesDecoder := json.NewDecoder(bytes.NewReader(req.Rules))
		rulesDecoder.DisallowUnknownFields()
		if err := rulesDecoder.Decode(&merged); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: err.Error(), Field: "rules"})
			return
		}
		if err := merged.Validate(); err != nil {
			var fe *domain.FieldError
			if errors.As(err, &fe) {
				writeJSON(w, http.StatusBadRequest, errorBody{Error: fe.Message, Field: "rules." + fe.Field})
				return
			}
			s.fail(w, err)
			return
		}
		rules = &merged
	}

	var changes []config.ConnChange
	for app, fields := range req.Connections {
		for name, value := range fields {
			field, ok := config.FieldFor(app, name)
			if !ok {
				writeJSON(w, http.StatusBadRequest, errorBody{
					Error: "unknown connection setting",
					Field: "connections." + app + "." + name,
				})
				return
			}
			v := ""
			if value != nil {
				v = *value
			}
			if _, err := config.CheckConn(field, v); err != nil {
				s.fail(w, err)
				return
			}
			changes = append(changes, config.ConnChange{Field: field, Value: v})
		}
	}

	if err := s.Config.Apply(ctx, rules, changes); err != nil {
		s.fail(w, err)
		return
	}
	s.getSettings(w, r)
}

type testResponse struct {
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}

// testConnection tries the saved connection of one app. A failed test is a normal answer,
// not an HTTP error.
func (s *Server) testConnection(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	if !slices.Contains(config.Apps, app) {
		writeError(w, http.StatusNotFound, "unknown app")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	tester, err := s.Apps.Tester(ctx, app)
	if errors.Is(err, providers.ErrNotConfigured) {
		writeJSON(w, http.StatusOK, testResponse{Error: "not configured"})
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	detail, err := tester.Test(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, testResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, testResponse{OK: true, Detail: detail})
}

// users lists the media server's users for the counting-users picker.
func (s *Server) users(w http.ResponseWriter, r *http.Request) {
	server, err := s.Apps.MediaServer(r.Context())
	if errors.Is(err, providers.ErrNotConfigured) {
		writeError(w, http.StatusConflict, "The media server is not configured.")
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	users, err := server.Users(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "The media server could not be read: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, users)
}
