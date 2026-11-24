package executor

import (
	"context"
	"fmt"
	"time"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/gate"
)

// Store is the persistence an execution needs. *store.Store satisfies it.
type Store interface {
	TakePlan(ctx context.Context, id string, now time.Time) (domain.Plan, error)
	RecordAction(ctx context.Context, a domain.Action, drop []domain.RowRef) (int64, error)
}

// Result is what an execution did.
type Result struct {
	PlanID  string          `json:"planId"`
	DryRun  bool            `json:"dryRun"`
	Actions []domain.Action `json:"actions"` // one per item, in plan order; never nil
}

// Service executes plans, one at a time and never during a scan or plan building.
type Service struct {
	Build   func(ctx context.Context) (Providers, error) // the apps from the current settings
	Store   Store
	Gate    *gate.Gate // shared with the scanner and the planner; nil gives a gate of its own
	DryRun  bool
	Now     func() time.Time
	Timeout time.Duration // limit for one execution; 0 means 30 minutes

	own gate.Gate
}

const defaultTimeout = 30 * time.Minute

// Execute runs the plan with this ID (spec 6.11). It is refused with a *gate.BusyError while
// another job runs, and with store.ErrPlanNotFound or store.ErrPlanExpired; those change
// nothing. Once the plan is taken it runs to the end, even when ctx is cancelled, and every
// item is recorded as it finishes. It stops early only when an item cannot be recorded:
// nothing more is deleted than the history holds.
func (s *Service) Execute(ctx context.Context, planID string) (Result, error) {
	g := s.Gate
	if g == nil {
		g = &s.own
	}
	release, err := g.Enter(gate.Execute)
	if err != nil {
		return Result{}, err
	}
	defer release()

	p, err := s.Build(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("load connections: %w", err)
	}
	plan, err := s.Store.TakePlan(ctx, planID, s.Now())
	if err != nil {
		return Result{}, err
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	r := newRun(p, s.DryRun)
	res := Result{PlanID: plan.ID, DryRun: s.DryRun, Actions: []domain.Action{}}
	for _, it := range plan.Items {
		a, drop := r.item(ctx, it)
		a.PlanID, a.At = plan.ID, s.Now()
		id, err := s.Store.RecordAction(ctx, a, drop)
		if err != nil {
			return res, fmt.Errorf("record an item: %w", err)
		}
		a.ID = id
		res.Actions = append(res.Actions, a)
	}
	return res, nil
}
