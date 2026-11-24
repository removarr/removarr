package executor

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/gate"
	"github.com/removarr/removarr/internal/store"
)

var at = time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)

type fakeStore struct {
	plan     domain.Plan
	takeErr  error
	taken    int
	recorded []domain.Action
	drops    [][]domain.RowRef
	failAt   int // RecordAction fails on this call, counted from 1; 0 never
}

func (f *fakeStore) TakePlan(_ context.Context, id string, now time.Time) (domain.Plan, error) {
	f.taken++
	if f.takeErr != nil {
		return domain.Plan{}, f.takeErr
	}
	return f.plan, nil
}

func (f *fakeStore) RecordAction(_ context.Context, a domain.Action, drop []domain.RowRef) (int64, error) {
	if len(f.recorded)+1 == f.failAt {
		return 0, errors.New("disk full")
	}
	f.recorded = append(f.recorded, a)
	f.drops = append(f.drops, drop)
	return int64(len(f.recorded)), nil
}

func newService(a *apps, st *fakeStore) *Service {
	return &Service{
		Build: func(context.Context) (Providers, error) { return a.providers(), nil },
		Store: st,
		Gate:  &gate.Gate{},
		Now:   func() time.Time { return at },
	}
}

func TestExecuteRecordsEachItemAndGoesOnAfterAFailure(t *testing.T) {
	a := newApps(t)
	a.fail["remove movie 1 exclusion=false"] = errors.New("unexpected status 500")
	st := &fakeStore{plan: domain.Plan{ID: "p1", Items: []domain.PlanItem{movieItem(1, "tmdb:1", 0), movieItem(2, "tmdb:2", 0)}}}
	res, err := newService(a, st).Execute(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Actions) != 2 || res.Actions[0].Outcome != domain.OutcomeFailed || res.Actions[1].Outcome != domain.OutcomeDone {
		t.Fatalf("actions = %+v", res.Actions)
	}
	if res.Actions[1].ID != 2 || res.Actions[1].PlanID != "p1" || !res.Actions[1].At.Equal(at) {
		t.Errorf("second record = %+v, want ID 2, plan p1 and the time", res.Actions[1])
	}
	if !reflect.DeepEqual(st.drops[1], []domain.RowRef{{List: domain.ListMovies, Key: "tmdb:2"}}) || len(st.drops[0]) != 0 {
		t.Errorf("drops = %+v", st.drops)
	}
}

func TestExecuteIsRefusedWhileTheGateIsHeld(t *testing.T) {
	a := newApps(t)
	st := &fakeStore{}
	svc := newService(a, st)
	release, _ := svc.Gate.Enter(gate.Scan)
	defer release()
	if _, err := svc.Execute(ctx, "p1"); !errors.Is(err, gate.ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
	if st.taken != 0 {
		t.Error("the plan was taken although the execution was refused")
	}
}

func TestAnUnknownOrExpiredPlanChangesNothing(t *testing.T) {
	for _, want := range []error{store.ErrPlanNotFound, store.ErrPlanExpired} {
		a := newApps(t)
		a.noWrites = true
		st := &fakeStore{takeErr: want}
		if _, err := newService(a, st).Execute(ctx, "p1"); !errors.Is(err, want) {
			t.Errorf("err = %v, want %v", err, want)
		}
		if len(st.recorded) != 0 || len(a.log) != 0 {
			t.Errorf("%v: recorded %d, calls %q", want, len(st.recorded), a.log)
		}
	}
}

func TestExecuteDoesNotTakeThePlanWhenTheAppsCannotBeBuilt(t *testing.T) {
	st := &fakeStore{}
	svc := newService(newApps(t), st)
	svc.Build = func(context.Context) (Providers, error) { return Providers{}, errors.New("database is locked") }
	if _, err := svc.Execute(ctx, "p1"); err == nil || st.taken != 0 {
		t.Errorf("err = %v, taken %d; want an error and the plan left", err, st.taken)
	}
}

// cancelling is an app that cancels the request when the first item runs and checks that
// later calls still get a live context.
type cancelling struct {
	*apps
	cancel func()
	dead   int
}

func (c *cancelling) RemoveMovie(ctx context.Context, id int, excl bool) error {
	c.cancel()
	if ctx.Err() != nil {
		c.dead++
	}
	return c.apps.RemoveMovie(ctx, id, excl)
}

func TestExecuteRunsToTheEndWhenTheRequestGoesAway(t *testing.T) {
	a := newApps(t)
	reqCtx, cancel := context.WithCancel(context.Background())
	c := &cancelling{apps: a, cancel: cancel}
	st := &fakeStore{plan: domain.Plan{ID: "p1", Items: []domain.PlanItem{movieItem(1, "tmdb:1", 0), movieItem(2, "tmdb:2", 0)}}}
	svc := newService(a, st)
	svc.Build = func(context.Context) (Providers, error) {
		p := a.providers()
		p.Movies = c
		return p, nil
	}
	res, err := svc.Execute(reqCtx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if c.dead != 0 || len(res.Actions) != 2 || res.Actions[1].Outcome != domain.OutcomeDone {
		t.Errorf("dead contexts %d, actions %+v; want both items run with a live context", c.dead, res.Actions)
	}
}

func TestExecuteStopsWhenAnItemCannotBeRecorded(t *testing.T) {
	a := newApps(t)
	st := &fakeStore{failAt: 1, plan: domain.Plan{ID: "p1", Items: []domain.PlanItem{movieItem(1, "tmdb:1", 0), movieItem(2, "tmdb:2", 0)}}}
	if _, err := newService(a, st).Execute(ctx, "p1"); err == nil {
		t.Fatal("want an error")
	}
	if contains(a.log, "remove movie 2 exclusion=false") {
		t.Errorf("calls = %q: nothing may be deleted after a record failed", a.log)
	}
}

func TestADryRunExecutionIsMarked(t *testing.T) {
	a := newApps(t)
	a.noWrites = true
	st := &fakeStore{plan: domain.Plan{ID: "p1", Items: []domain.PlanItem{movieItem(1, "tmdb:1", 0)}}}
	svc := newService(a, st)
	svc.DryRun = true
	res, err := svc.Execute(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun || res.Actions[0].Outcome != domain.OutcomeDryRun || len(st.drops[0]) != 0 {
		t.Errorf("result = %+v, drops %+v", res, st.drops)
	}
}
