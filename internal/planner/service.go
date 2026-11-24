package planner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/removarr/removarr/internal/domain"
	"github.com/removarr/removarr/internal/scan"
	"github.com/removarr/removarr/internal/scanner"
	"github.com/removarr/removarr/internal/store"
)

// Store is the persistence planning needs. *store.Store satisfies it.
type Store interface {
	ReadLatest(ctx context.Context) (store.Latest, error)
	SavePlan(ctx context.Context, p domain.Plan) error
}

// Service builds plans, one at a time: each reads every app, as a scan does.
type Service struct {
	Build func(ctx context.Context) (scanner.Providers, error) // providers from the current settings
	Rules func(ctx context.Context) (domain.Rules, error)      // the current rules
	Store Store
	Now   func() time.Time
	NewID func() (string, error) // nil means 16 random bytes in hex

	mu sync.Mutex
}

// Plan builds the plan for the selected rows from live data, stores it with its expiry and
// returns it (spec 6.8, 6.10). An app that does not answer skips the rows that need it; only
// a failure to load the settings, read the latest scan or save the plan is an error.
func (s *Service) Plan(ctx context.Context, selected []domain.RowRef) (domain.Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rules, err := s.Rules(ctx)
	if err != nil {
		return domain.Plan{}, fmt.Errorf("load rules: %w", err)
	}
	p, err := s.Build(ctx)
	if err != nil {
		return domain.Plan{}, fmt.Errorf("load connections: %w", err)
	}
	f := scanner.Read(ctx, p, rules, nil)
	// Read the ignores after the apps: reading them takes a while, and an ignore saved
	// meanwhile must hold.
	latest, err := s.Store.ReadLatest(ctx)
	if err != nil {
		return domain.Plan{}, fmt.Errorf("read the latest scan: %w", err)
	}
	ignores := scan.NewIgnoreSet(latest.Ignores)
	f.Ignored = ignores.Keys()
	now := s.Now()
	items, skipped := Make(Input{
		Rules: rules, Now: now, Eval: scanner.Evaluate(rules, now, f), Ignores: ignores,
		Latest: latest.Rows, Selected: selected,
	})

	newID := s.NewID
	if newID == nil {
		newID = randomID
	}
	id, err := newID()
	if err != nil {
		return domain.Plan{}, fmt.Errorf("make a plan ID: %w", err)
	}
	plan := domain.Plan{ID: id, CreatedAt: now, ExpiresAt: now.Add(domain.PlanLifetime), Items: items, Skipped: skipped}
	if err := s.Store.SavePlan(ctx, plan); err != nil {
		return domain.Plan{}, fmt.Errorf("save plan: %w", err)
	}
	return plan, nil
}

// randomID returns 16 random bytes in hex.
func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
