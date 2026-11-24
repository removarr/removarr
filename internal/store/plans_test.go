package store

import (
	"errors"
	"path/filepath"
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

func TestTakingAPlanReturnsItAndDropsEveryPlan(t *testing.T) {
	s := open(t)
	built := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	want := testPlan("abc", built)
	for _, p := range []domain.Plan{want, testPlan("other", built)} {
		if err := s.SavePlan(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.TakePlan(ctx, "abc", built.Add(domain.PlanLifetime-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("took %+v, want %+v", got, want)
	}
	if n := count(t, s, `SELECT COUNT(*) FROM plans`); n != 0 {
		t.Errorf("%d plans left, want none: the other plan is discarded too", n)
	}
	if _, err := s.TakePlan(ctx, "abc", built); !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("taking it again: err = %v, want ErrPlanNotFound", err)
	}
}

// A plan built before migration 4 holds its steps in the old order: it must not be executed.
func TestMigrationFourDropsThePlansBuiltBeforeIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "removarr.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SavePlan(ctx, testPlan("old", time.Now())); err != nil {
		t.Fatal(err)
	}
	// Take the database back to before migration 4, keeping the plan.
	for _, q := range []string{`DROP TABLE actions`, `DELETE FROM schema_migrations WHERE version = 4`} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if n := count(t, s, `SELECT COUNT(*) FROM plans`); n != 0 {
		t.Errorf("%d plans after migration 4, want none", n)
	}
}

func TestAnExpiredPlanCannotBeTakenAndStays(t *testing.T) {
	s := open(t)
	built := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	if err := s.SavePlan(ctx, testPlan("abc", built)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TakePlan(ctx, "abc", built.Add(domain.PlanLifetime)); !errors.Is(err, ErrPlanExpired) {
		t.Fatalf("at ten minutes: err = %v, want ErrPlanExpired", err)
	}
	if n := count(t, s, `SELECT COUNT(*) FROM plans`); n != 1 {
		t.Errorf("%d plans, want the refused plan left as it was", n)
	}
}

func TestTheStoredExpiryColumnDecides(t *testing.T) {
	s := open(t)
	built := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	if err := s.SavePlan(ctx, testPlan("abc", built)); err != nil {
		t.Fatal(err)
	}
	// The JSON still says ten minutes; the column says one.
	if _, err := s.db.ExecContext(ctx, `UPDATE plans SET expires_at = ?`, built.Add(time.Minute).UnixNano()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TakePlan(ctx, "abc", built.Add(2*time.Minute)); !errors.Is(err, ErrPlanExpired) {
		t.Errorf("err = %v, want ErrPlanExpired from the column", err)
	}
}

func TestAnUnknownPlanIsNotFound(t *testing.T) {
	if _, err := open(t).TakePlan(ctx, "nope", time.Now()); !errors.Is(err, ErrPlanNotFound) {
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
	if _, err := s.TakePlan(ctx, "old", built); !errors.Is(err, ErrPlanNotFound) {
		t.Errorf("old plan: err = %v, want ErrPlanNotFound", err)
	}
}
