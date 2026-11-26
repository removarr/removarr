package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/removarr/removarr/internal/domain"
)

// RecordAction stores the record of one executed item and drops the rows it deleted from the
// latest finished scan, in one transaction, so that the lists and the history agree (spec
// 6.11). It returns the ID of the record.
func (s *Store) RecordAction(ctx context.Context, a domain.Action, drop []domain.RowRef) (int64, error) {
	data, err := json.Marshal(a)
	if err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO actions (plan_id, executed_at, dry_run, outcome, size, data) VALUES (?, ?, ?, ?, ?, ?)`,
		a.PlanID, formatTime(a.At), a.DryRun, a.Outcome, a.Size, string(data))
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if len(drop) > 0 {
		scan, err := latestScan(ctx, tx)
		if err != nil {
			return 0, err
		}
		if scan != nil {
			for _, r := range drop {
				if _, err := tx.ExecContext(ctx,
					`DELETE FROM candidates WHERE scan_id = ? AND list = ? AND key = ?`,
					scan.ID, string(r.List), r.Key); err != nil {
					return 0, err
				}
			}
		}
	}
	return id, tx.Commit()
}

// Actions returns history records, newest first: at most limit of them with IDs below before,
// or from the newest when before is 0. It never returns nil.
func (s *Store) Actions(ctx context.Context, before int64, limit int) ([]domain.Action, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, data FROM actions WHERE ? = 0 OR id < ? ORDER BY id DESC LIMIT ?`, before, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Action{}
	for rows.Next() {
		var (
			id   int64
			data string
		)
		if err := rows.Scan(&id, &data); err != nil {
			return nil, err
		}
		var a domain.Action
		if err := json.Unmarshal([]byte(data), &a); err != nil {
			return nil, fmt.Errorf("action %d: %w", id, err)
		}
		a.ID = id
		out = append(out, a)
	}
	return out, rows.Err()
}
