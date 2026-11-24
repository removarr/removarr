package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/removarr/removarr/internal/domain"
)

// rowRef names one row in an ignore request.
type rowRef struct {
	List domain.ListKind `json:"list"`
	Key  string          `json:"key"`
}

// readRows decodes and checks the body of a request that names rows (ignore, un-ignore, plan): {"rows":[{"list":…,"key":…}]}.
func readRows(w http.ResponseWriter, r *http.Request) ([]rowRef, error) {
	var req struct {
		Rows []rowRef `json:"rows"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	// The object must be the whole body: anything after it, valid JSON or not, is refused,
	// so the size limit and the checks below cover everything that was sent.
	if err := decoder.Decode(&req); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return nil, &domain.FieldError{Field: "", Message: `the body must be {"rows":[{"list":…,"key":…}]}`}
	}
	if len(req.Rows) == 0 {
		return nil, &domain.FieldError{Field: "rows", Message: "must name at least one row"}
	}
	for i, row := range req.Rows {
		switch row.List {
		case domain.ListMovies, domain.ListSeries, domain.ListTorrents:
		default:
			return nil, &domain.FieldError{Field: fmt.Sprintf("rows[%d].list", i), Message: `must be "movies", "series" or "torrents"`}
		}
		if row.Key == "" {
			return nil, &domain.FieldError{Field: fmt.Sprintf("rows[%d].key", i), Message: "must not be empty"}
		}
	}
	return req.Rows, nil
}

// addIgnores ignores rows of the latest scan (spec 6.5). Every row must be in the latest
// scan, in the list named; otherwise nothing is saved. The kind and the title come from the
// stored row.
func (s *Server) addIgnores(w http.ResponseWriter, r *http.Request) {
	rows, err := readRows(w, r)
	if err != nil {
		s.fail(w, err)
		return
	}
	v, err := s.latest(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	stored := map[rowRef]domain.Candidate{}
	for _, c := range v.rows {
		stored[rowRef{c.List, c.Key}] = c
	}
	now := s.now()
	ignores := make([]domain.Ignore, 0, len(rows))
	for i, row := range rows {
		c, ok := stored[row]
		if !ok {
			s.fail(w, &domain.FieldError{Field: fmt.Sprintf("rows[%d].key", i), Message: "is not a row of the latest scan; reload the list"})
			return
		}
		ignores = append(ignores, domain.Ignore{Kind: domain.IgnoreKind(c), Key: c.Key, Title: c.Title, CreatedAt: now})
	}
	if err := s.Store.AddIgnores(r.Context(), ignores); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// removeIgnores un-ignores rows. A key that is not ignored in the list named is skipped. The
// latest scan is not consulted: an ignore whose row is gone can still be removed.
func (s *Server) removeIgnores(w http.ResponseWriter, r *http.Request) {
	rows, err := readRows(w, r)
	if err != nil {
		s.fail(w, err)
		return
	}
	refs := make([]domain.RowRef, 0, len(rows))
	for _, row := range rows {
		refs = append(refs, domain.RowRef{List: row.List, Key: row.Key})
	}
	// The store matches the list inside its delete, so the request is atomic.
	if err := s.Store.RemoveIgnores(r.Context(), refs); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
