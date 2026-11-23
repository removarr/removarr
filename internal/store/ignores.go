package store

import (
	"context"
	"fmt"
	"time"

	"github.com/removarr/removarr/internal/domain"
)

// AddIgnores saves ignores in one transaction (spec 6.5). An ignore whose key is saved
// already keeps its first title and time.
func (s *Store) AddIgnores(ctx context.Context, ignores []domain.Ignore) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, i := range ignores {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO ignores (key, kind, title, created_at) VALUES (?, ?, ?, ?)`,
			i.Key, i.Kind, i.Title, formatTime(i.CreatedAt)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RemoveIgnores deletes the ignores with the given keys in one transaction. A key that is
// not ignored is skipped.
func (s *Store) RemoveIgnores(ctx context.Context, keys []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, key := range keys {
		if _, err := tx.ExecContext(ctx, `DELETE FROM ignores WHERE key = ?`, key); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Ignores returns every ignore, sorted by key. The slice is never nil.
func (s *Store) Ignores(ctx context.Context) ([]domain.Ignore, error) {
	return ignores(ctx, s.db)
}

func ignores(ctx context.Context, q queryer) ([]domain.Ignore, error) {
	rows, err := q.QueryContext(ctx, `SELECT key, kind, title, created_at FROM ignores ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Ignore{}
	for rows.Next() {
		var i domain.Ignore
		var created string
		if err := rows.Scan(&i.Key, &i.Kind, &i.Title, &created); err != nil {
			return nil, err
		}
		if i.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			return nil, fmt.Errorf("ignore %s: bad time: %w", i.Key, err)
		}
		out = append(out, i)
	}
	return out, rows.Err()
}
