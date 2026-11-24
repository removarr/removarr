package api

import (
	"context"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/scan"
	"github.com/removarr/removarr/internal/store"
)

// latestView is the latest scan as the user sees it with the current ignores (spec 6.5).
type latestView struct {
	rec   *store.ScanRecord // nil before the first scan; its summary counts what the lists show
	rows  []domain.Candidate
	lists map[domain.ListKind]scan.ListView
}

// latest reads the latest scan, its rows and the ignores together, and applies the ignores.
// Each produced list's summary counts the rows shown and the rows that wait; a list that
// could not be produced keeps its error.
func (s *Server) latest(ctx context.Context) (latestView, error) {
	l, err := s.Store.ReadLatest(ctx)
	if err != nil {
		return latestView{}, err
	}
	ignores := scan.NewIgnoreSet(l.Ignores)
	v := latestView{rows: l.Rows, lists: map[domain.ListKind]scan.ListView{}}
	for _, list := range []domain.ListKind{domain.ListMovies, domain.ListSeries, domain.ListTorrents} {
		v.lists[list] = scan.View(list, l.Rows, ignores)
	}
	if l.Scan != nil {
		rec := *l.Scan
		lists := make(map[domain.ListKind]domain.ListSummary, len(rec.Summary.Lists))
		for list, sum := range rec.Summary.Lists {
			if sum.Error == "" {
				sum.Candidates, sum.Waiting = len(v.lists[list].Shown), v.lists[list].Waiting
			}
			lists[list] = sum
		}
		rec.Summary.Lists = lists
		v.rec = &rec
	}
	return v, nil
}
