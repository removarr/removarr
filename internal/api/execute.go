package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/store"
)

// executePlan carries out a confirmed plan (spec 6.11). The plan is taken once: executing
// it discards the others, and a second request for it finds nothing.
func (s *Server) executePlan(w http.ResponseWriter, r *http.Request) {
	result, err := s.Executor.Execute(r.Context(), r.PathValue("id"))
	if busy(w, err) {
		return
	}
	switch {
	case errors.Is(err, store.ErrPlanNotFound):
		writeError(w, http.StatusNotFound, "There is no such plan. It was executed or discarded already: build it again.")
	case errors.Is(err, store.ErrPlanExpired):
		writeError(w, http.StatusGone, "The plan has expired. Build it again.")
	case err != nil:
		s.fail(w, err)
	default:
		writeJSON(w, http.StatusOK, result)
	}
}

// listActions pages through the history, newest first. next is the ID to pass as before
// for the following page; it is absent on the last page.
func (s *Server) listActions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var before int64
	if v := q.Get("before"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "before must be a record ID")
			return
		}
		before = n
	}
	limit := 50
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			writeError(w, http.StatusBadRequest, "limit must be from 1 to 200")
			return
		}
		limit = n
	}
	actions, err := s.Store.Actions(r.Context(), before, limit)
	if err != nil {
		s.fail(w, err)
		return
	}
	if actions == nil {
		actions = []domain.Action{}
	}
	body := struct {
		Actions any    `json:"actions"`
		Next    *int64 `json:"next,omitempty"`
	}{Actions: actions}
	if len(actions) == limit {
		id := actions[len(actions)-1].ID
		body.Next = &id
	}
	writeJSON(w, http.StatusOK, body)
}
