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
	// ErrPlanNotFound is returned for a plan ID that is not stored.
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

// Plan returns the stored plan with this ID. It returns ErrPlanNotFound when there is none,
// and ErrPlanExpired when the plan expired by now.
func (s *Store) Plan(ctx context.Context, id string, now time.Time) (domain.Plan, error) {
	var data string
	err := s.db.QueryRowContext(ctx, `SELECT data FROM plans WHERE id = ?`, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Plan{}, ErrPlanNotFound
	}
	if err != nil {
		return domain.Plan{}, err
	}
	var p domain.Plan
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		return domain.Plan{}, fmt.Errorf("plan %s: %w", id, err)
	}
	if p.Expired(now) {
		return domain.Plan{}, ErrPlanExpired
	}
	return p, nil
}
