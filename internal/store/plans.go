package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

var (
	// ErrPlanNotFound is returned for a plan ID that is not stored: it never was, or it was taken or discarded.
	ErrPlanNotFound = errors.New("no such plan")
	// ErrPlanExpired is returned for a plan that can no longer be executed (spec 6.8, rule 2).
	ErrPlanExpired = errors.New("the plan has expired")
)

// SavePlan stores a new plan. The plans that expired by the time it was created are dropped
// in the same transaction.
func (s *Store) SavePlan(ctx context.Context, p domain.Plan) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM plans WHERE expires_at <= ?`, p.CreatedAt.UnixNano()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO plans (id, created_at, expires_at, data) VALUES (?, ?, ?, ?)`,
		p.ID, formatTime(p.CreatedAt), p.ExpiresAt.UnixNano(), string(data)); err != nil {
		return err
	}
	return tx.Commit()
}

// TakePlan returns the stored plan with this ID and drops every stored plan, in one
// transaction: a plan runs once, and the other pending plans were built from data that the
// execution changes (spec 6.11). The expires_at column decides whether the plan can still
// run, so there is one expiry source. An unknown plan gives ErrPlanNotFound and an expired
// one ErrPlanExpired; both change nothing.
func (s *Store) TakePlan(ctx context.Context, id string, now time.Time) (domain.Plan, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Plan{}, err
	}
	defer tx.Rollback()

	var (
		data    string
		expires int64
	)
	err = tx.QueryRowContext(ctx, `SELECT data, expires_at FROM plans WHERE id = ?`, id).Scan(&data, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Plan{}, ErrPlanNotFound
	}
	if err != nil {
		return domain.Plan{}, err
	}
	if expires <= now.UnixNano() {
		return domain.Plan{}, ErrPlanExpired
	}
	var p domain.Plan
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		return domain.Plan{}, fmt.Errorf("plan %s: %w", id, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM plans`); err != nil {
		return domain.Plan{}, err
	}
	return p, tx.Commit()
}
