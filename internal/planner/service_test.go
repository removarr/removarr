package planner

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/gate"
	"github.com/removarr/removarr/internal/scan"
	"github.com/removarr/removarr/internal/scanner"
	"github.com/removarr/removarr/internal/store"
)

// Providers that answer with a fixed read.

type fakeServer struct{ lib domain.Library }

func (f fakeServer) Users(context.Context) ([]domain.User, error) {
	return []domain.User{{ID: "a", Name: "Alice"}}, nil
}
func (f fakeServer) Library(context.Context, []string) (domain.Library, error) { return f.lib, nil }

type fakeMovies struct {
	snap   domain.MovieSnapshot
	err    error
	onRead func() // nil, or called when the movies are read
}

func (f fakeMovies) Movies(context.Context) (domain.MovieSnapshot, error) {
	if f.onRead != nil {
		f.onRead()
	}
	return f.snap, f.err
}

type fakeSeries struct{ snap domain.SeriesSnapshot }

func (f fakeSeries) Series(context.Context) (domain.SeriesSnapshot, error) { return f.snap, nil }

type fakeDownloads struct{ list []domain.Download }

func (f fakeDownloads) Downloads(context.Context) ([]domain.Download, error) { return f.list, nil }
func (f fakeDownloads) Files(context.Context, string) ([]domain.DownloadFile, error) {
	return nil, nil
}

func providersOf(f scanner.Fetched) scanner.Providers {
	return scanner.Providers{
		MediaServer: fakeServer{f.Library.Value},
		Movies:      fakeMovies{snap: f.Movies.Value},
		Series:      fakeSeries{f.Series.Value},
		Downloads:   fakeDownloads{f.Downloads.Value},
	}
}

type fakeStore struct {
	latest  store.Latest
	readErr error
	saved   []domain.Plan
}

func (s *fakeStore) ReadLatest(context.Context) (store.Latest, error) { return s.latest, s.readErr }
func (s *fakeStore) SavePlan(_ context.Context, p domain.Plan) error {
	s.saved = append(s.saved, p)
	return nil
}

func service(p scanner.Providers, st *fakeStore) *Service {
	return &Service{
		Build: func(context.Context) (scanner.Providers, error) { return p, nil },
		Rules: func(context.Context) (domain.Rules, error) { return rules(), nil },
		Store: st,
		Now:   func() time.Time { return now },
		NewID: func() (string, error) { return "plan1", nil },
	}
}

func TestPlanReadsTheAppsAndStoresThePlan(t *testing.T) {
	f := world()
	st := &fakeStore{latest: store.Latest{Rows: scanned(rules(), f), Ignores: []domain.Ignore{}}}
	got, err := service(providersOf(f), st).Plan(context.Background(), []domain.RowRef{movieOne})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "plan1" || !got.CreatedAt.Equal(now) || !got.ExpiresAt.Equal(now.Add(domain.PlanLifetime)) {
		t.Errorf("plan = %s, %v, %v; want plan1 built now, expiring in ten minutes", got.ID, got.CreatedAt, got.ExpiresAt)
	}
	if len(got.Items) != 1 || !reflect.DeepEqual(torrentIDs(got.Items[0]), []string{"m1"}) {
		t.Errorf("items = %+v, want movie One with m1", got.Items)
	}
	if len(st.saved) != 1 || !reflect.DeepEqual(st.saved[0], got) {
		t.Errorf("saved = %+v, want the plan returned", st.saved)
	}
}

func TestPlanSkipsTheRowsOfAnAppThatDoesNotAnswer(t *testing.T) {
	f := world()
	st := &fakeStore{latest: store.Latest{Rows: scanned(rules(), f), Ignores: []domain.Ignore{}}}
	p := providersOf(f)
	p.Movies = fakeMovies{err: errors.New("connection refused")}
	got, err := service(p, st).Plan(context.Background(), []domain.RowRef{movieOne, theShow})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 0 || len(got.Skipped) != 2 || !strings.Contains(got.Skipped[0].Reason, "connection refused") {
		t.Errorf("plan = %+v, want both rows skipped with the movie manager's error", got)
	}
}

func TestPlanAppliesTheCurrentIgnores(t *testing.T) {
	f := world()
	st := &fakeStore{latest: store.Latest{
		Rows:    scanned(rules(), f),
		Ignores: []domain.Ignore{{Kind: domain.IgnoreMovie, Key: "tmdb:1", Title: "One"}},
	}}
	got, err := service(providersOf(f), st).Plan(context.Background(), []domain.RowRef{movieOne})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 0 || len(got.Skipped) != 1 {
		t.Errorf("plan = %+v, want tmdb:1 skipped as ignored", got)
	}
}

// Reading the apps takes a while; an ignore saved meanwhile must hold.
func TestPlanAppliesAnIgnoreSavedWhileTheAppsAreRead(t *testing.T) {
	f := world()
	st := &fakeStore{latest: store.Latest{Rows: scanned(rules(), f), Ignores: []domain.Ignore{}}}
	p := providersOf(f)
	p.Movies = fakeMovies{snap: f.Movies.Value, onRead: func() {
		st.latest.Ignores = []domain.Ignore{{Kind: domain.IgnoreMovie, Key: "tmdb:1", Title: "One"}}
	}}
	got, err := service(p, st).Plan(context.Background(), []domain.RowRef{movieOne})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 0 || len(got.Skipped) != 1 || got.Skipped[0].Reason != scan.HoldIgnored {
		t.Errorf("plan = %+v, want tmdb:1 skipped as ignored", got)
	}
}

// In per-season mode a season ignore keeps the seasons of a series apart, so the read has to
// know the ignores (spec 6.3): otherwise it merges them into one series row.
func TestPlanReadsWithTheIgnoresForThePerSeasonMerge(t *testing.T) {
	r := rules()
	r.SeriesUnit = domain.UnitSeason
	f := world()
	s := &f.Series.Value.Series[0]
	s.Ended = false
	s.Episodes = append(s.Episodes, domain.Episode{ID: 21, Season: 2, Number: 1, File: mfile(121, 500)})
	f.Library.Value.Episodes = append(f.Library.Value.Episodes, domain.ServerEpisode{SeriesID: "ss1", Season: 2, Episode: 1, States: played()})
	ignores := []domain.Ignore{{Kind: domain.IgnoreSeason, Key: "tvdb:100:s1", Title: "Show, season 1"}}
	scannedWith := f
	scannedWith.Ignored = map[string]bool{"tvdb:100:s1": true}
	st := &fakeStore{latest: store.Latest{Rows: scanned(r, scannedWith), Ignores: ignores}}
	svc := service(providersOf(f), st)
	svc.Rules = func(context.Context) (domain.Rules, error) { return r, nil }
	got, err := svc.Plan(context.Background(), []domain.RowRef{sel(domain.ListSeries, "tvdb:100:s2")})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || len(got.Skipped) != 0 || !reflect.DeepEqual(rowKeys(got.Items[0]), []string{"series/tvdb:100:s2"}) {
		t.Errorf("plan = %+v, want an item for season 2", got)
	}
}

func TestPlanFailsWhenTheLatestScanCannotBeRead(t *testing.T) {
	st := &fakeStore{readErr: errors.New("disk full")}
	if _, err := service(providersOf(world()), st).Plan(context.Background(), []domain.RowRef{movieOne}); err == nil {
		t.Error("err = nil, want the store's error")
	}
	if len(st.saved) != 0 {
		t.Error("a plan was saved")
	}
}

func TestPlanIDsAreRandom(t *testing.T) {
	a, errA := randomID()
	b, errB := randomID()
	if errA != nil || errB != nil || len(a) != 32 || a == b {
		t.Errorf("randomID = %q, %q (%v, %v); want two different 32-character IDs", a, b, errA, errB)
	}
}

func TestPlanIsRefusedWhileAScanRuns(t *testing.T) {
	f := world()
	st := &fakeStore{latest: store.Latest{Rows: scanned(rules(), f)}}
	svc := service(providersOf(f), st)
	svc.Gate = &gate.Gate{}
	release, _ := svc.Gate.Enter(gate.Scan)
	defer release()
	if _, err := svc.Plan(context.Background(), []domain.RowRef{sel(domain.ListMovies, "tmdb:603")}); !errors.Is(err, gate.ErrBusy) {
		t.Fatalf("Plan = %v, want refused while a scan runs", err)
	}
	if len(st.saved) != 0 {
		t.Error("no plan may be saved")
	}
}
