package api

import (
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

func TestBuildPlanPassesTheRowsAndReturnsThePlan(t *testing.T) {
	h := newHarness(t, nil)
	built := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	h.planner.plan = domain.Plan{
		ID: "abc", CreatedAt: built, ExpiresAt: built.Add(domain.PlanLifetime),
		Items:   []domain.PlanItem{},
		Skipped: []domain.SkippedRow{{Row: domain.RowRef{List: domain.ListMovies, Key: "tmdb:1"}, Reason: "It is ignored."}},
	}
	rec := h.do(http.MethodPost, "/api/plans", ignoreBody("movies/tmdb:1", "torrents/hash:abc"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST = %d %s, want 201", rec.Code, rec.Body)
	}
	want := []domain.RowRef{{List: domain.ListMovies, Key: "tmdb:1"}, {List: domain.ListTorrents, Key: "hash:abc"}}
	if !reflect.DeepEqual(h.planner.rows, want) {
		t.Errorf("planner got %+v, want %+v", h.planner.rows, want)
	}
	if got := decode[domain.Plan](t, rec); !reflect.DeepEqual(got, h.planner.plan) {
		t.Errorf("response = %+v, want %+v", got, h.planner.plan)
	}
}

func TestBuildPlanRejectsABadBody(t *testing.T) {
	h := newHarness(t, nil)
	for _, body := range []string{`{"rows":[]}`, `{"rows":[{"list":"films","key":"x"}]}`, `not json`} {
		if rec := h.do(http.MethodPost, "/api/plans", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", body, rec.Code)
		}
	}
	if h.planner.rows != nil {
		t.Errorf("the planner was asked for %+v", h.planner.rows)
	}
}

func TestBuildPlanGoesThroughTheGuard(t *testing.T) {
	h := newHarness(t, nil)
	rec := h.do(http.MethodPost, "/api/plans", ignoreBody("movies/tmdb:1"), func(r *http.Request) { r.Header.Del(RequestHeader) })
	if rec.Code != http.StatusForbidden || h.planner.rows != nil {
		t.Errorf("status = %d, planner asked = %v; want 403 and no plan", rec.Code, h.planner.rows != nil)
	}
}

func TestBuildPlanFailureIsAServerError(t *testing.T) {
	h := newHarness(t, nil)
	h.planner.err = errors.New("disk full")
	if rec := h.do(http.MethodPost, "/api/plans", ignoreBody("movies/tmdb:1")); rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}
