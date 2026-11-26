package executor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
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

// ErrStopping is returned when removarr was stopped during an execution: the items after the
// last one recorded were not started.
var ErrStopping = errors.New("removarr is stopping: the remaining items were not started")

// Service executes plans, one at a time and never during a scan or plan building.
type Service struct {
	Build   func(ctx context.Context) (Providers, error) // the apps from the current settings
	Store   Store
	Gate    *gate.Gate // shared with the scanner and the planner; nil gives a gate of its own
	DryRun  bool
	Now     func() time.Time
	Timeout time.Duration   // limit for one execution; 0 means 30 minutes
	Stop    <-chan struct{} // closed when removarr is stopping; nil means never
	Log     *slog.Logger    // nil means slog.Default()

	own gate.Gate

	mu      sync.Mutex
	running int           // executions in progress
	idle    chan struct{} // closed when running drops to zero
}

const (
	defaultTimeout = 30 * time.Minute
	// recordTimeout limits one history write. It runs on a context the execution's
	// deadline cannot end, so an item is recorded even after the execution ran out of time.
	recordTimeout = time.Minute
)

// Execute runs the plan with this ID (spec 6.11). It is refused with a *gate.BusyError while
// another job runs, and with store.ErrPlanNotFound or store.ErrPlanExpired; those change
// nothing. Once the plan is taken it runs to the end, even when ctx is cancelled, and every
// item is recorded as it finishes. It stops early when an item cannot be recorded, so nothing
// more is deleted than the history holds, and with ErrStopping when Stop is closed: then it
// starts no further item.
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
	s.begin()
	defer s.end()

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
	detached := context.WithoutCancel(ctx)
	runCtx, cancel := context.WithTimeout(detached, timeout)
	defer cancel()

	r := newRun(p, s.DryRun)
	res := Result{PlanID: plan.ID, DryRun: s.DryRun, Actions: []domain.Action{}}
	for _, it := range plan.Items {
		if s.stopping() {
			return res, ErrStopping
		}
		a, drop := r.item(runCtx, it)
		a.PlanID, a.At = plan.ID, s.Now()
		id, err := s.record(detached, a, drop)
		if err != nil {
			s.logUnrecorded(a, err)
			return res, fmt.Errorf("record an item: %w", err)
		}
		a.ID = id
		res.Actions = append(res.Actions, a)
	}
	return res, nil
}

// Wait waits until no execution is running, for at most timeout, and reports whether none is.
// removarr calls it when stopping, before it closes the database.
func (s *Service) Wait(timeout time.Duration) bool {
	s.mu.Lock()
	idle, running := s.idle, s.running
	s.mu.Unlock()
	if running == 0 {
		return true
	}
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-idle:
		return true
	case <-t.C:
		return false
	}
}

func (s *Service) begin() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running == 0 {
		s.idle = make(chan struct{})
	}
	s.running++
}

func (s *Service) end() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running--
	if s.running == 0 {
		close(s.idle)
	}
}

// stopping reports whether Stop is closed.
func (s *Service) stopping() bool {
	select {
	case <-s.Stop:
		return true
	default:
		return false
	}
}

// record writes one item to the history on a context of its own, derived from the request
// but cancelled by neither the request nor the execution's deadline (spec 6.11: each item is
// recorded as soon as it finishes).
func (s *Service) record(ctx context.Context, a domain.Action, drop []domain.RowRef) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, recordTimeout)
	defer cancel()
	return s.Store.RecordAction(ctx, a, drop)
}

// logUnrecorded logs an item that ran but could not be recorded, so what it did is not lost.
// It names rows by key and steps by IDs, never by title.
func (s *Service) logUnrecorded(a domain.Action, err error) {
	log := s.Log
	if log == nil {
		log = slog.Default()
	}
	rows := make([]string, 0, len(a.Rows))
	for _, r := range a.Rows {
		rows = append(rows, string(r.List)+"/"+r.Key)
	}
	steps := make([]string, 0, len(a.Steps))
	for _, st := range a.Steps {
		steps = append(steps, describeStep(st))
	}
	log.Error("an executed item could not be recorded in the history",
		"plan", a.PlanID, "dryRun", a.DryRun, "outcome", a.Outcome,
		"rows", strings.Join(rows, ", "), "steps", strings.Join(steps, "; "), "error", err)
}

// describeStep names a step by its action and IDs, and gives its result.
func describeStep(st domain.StepResult) string {
	var b strings.Builder
	switch {
	case st.Manager != nil:
		m := st.Manager
		fmt.Fprintf(&b, "%s %s item=%d", m.Manager, m.Action, m.ItemID)
		if len(m.FileIDs) > 0 {
			fmt.Fprintf(&b, " files=%v", m.FileIDs)
		}
		if len(m.EpisodeIDs) > 0 {
			fmt.Fprintf(&b, " episodes=%v", m.EpisodeIDs)
		}
		if len(m.Seasons) > 0 {
			fmt.Fprintf(&b, " seasons=%v", m.Seasons)
		}
	case st.Torrent != nil:
		fmt.Fprintf(&b, "torrent %s keepData=%v", st.Torrent.ID, st.KeepData)
	}
	b.WriteString(": " + st.Result)
	return b.String()
}
