package store

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

func testPlan(id string, built time.Time) domain.Plan {
	return domain.Plan{
		ID: id, CreatedAt: built, ExpiresAt: built.Add(domain.PlanLifetime),
		Items: []domain.PlanItem{{
			Rows:     []domain.PlanRow{{Row: domain.Candidate{List: domain.ListMovies, Key: "tmdb:1", Title: "One", Torrents: []domain.TorrentInfo{}}}},
			Steps:    []domain.ManagerStep{{Manager: "movies", Action: domain.StepRemoveMovie, ItemID: 1}},
			Torrents: []domain.PlanTorrent{{Torrent: domain.TorrentInfo{ID: "m1", Name: "One.Torrent"}, KeepData: true}},
			Kept:     []domain.KeptTorrent{},
		}},
		Skipped: []domain.SkippedRow{{Row: domain.RowRef{List: domain.ListSeries, Key: "tvdb:1"}, Reason: "It is ignored."}},
	}
}

func TestASavedPlanCanBeReadUntilItExpires(t *testing.T) {
	s := open(t)
	built := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	want := testPlan("abc", built)
	if err := s.SavePlan(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.Plan(ctx, "abc", built.Add(domain.PlanLifetime-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("read %+v, want %+v", got, want)
	}
	if _, err := s.Plan(ctx, "abc", built.Add(domain.PlanLifetime)); !errors.Is(err, ErrPlanExpired) {
		t.Errorf("at ten minutes: err = %v, want ErrPlanExpired", err)
	}
}

func TestAnUnknownPlanIsNotFound(t *testing.T) {
	if _, err := open(t).Plan(ctx, "nope", time.Now()); !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("err = %v, want ErrPlanNotFound", err)
	}
}

func TestSavingAPlanDropsThePlansThatExpired(t *testing.T) {
	s := open(t)
	built := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	for _, p := range []domain.Plan{
		testPlan("old", built),
		testPlan("recent", built.Add(5*time.Minute)),
		testPlan("new", built.Add(domain.PlanLifetime)),
	} {
		if err := s.SavePlan(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	if n := count(t, s, `SELECT COUNT(*) FROM plans`); n != 2 {
		t.Errorf("%d plans stored, want 2: the expired one is dropped", n)
	}
	if _, err := s.Plan(ctx, "old", built); !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("old plan: err = %v, want ErrPlanNotFound", err)
	}
}
