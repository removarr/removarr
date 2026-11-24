package api

import (
	"net/http"

	"github.com/removarr/removarr/internal/domain"
)

// buildPlan builds the plan for deleting the selected rows from live data and returns it for
// confirmation (spec 6.8, rule 1; 6.10). The body names the rows as an ignore request does.
// Building reads every app, so it takes about as long as a scan.
func (s *Server) buildPlan(w http.ResponseWriter, r *http.Request) {
	rows, err := readRows(w, r)
	if err != nil {
		s.fail(w, err)
		return
	}
	refs := make([]domain.RowRef, 0, len(rows))
	for _, row := range rows {
		refs = append(refs, domain.RowRef{List: row.List, Key: row.Key})
	}
	plan, err := s.Planner.Plan(r.Context(), refs)
	if busy(w, err) {
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, plan)
}
