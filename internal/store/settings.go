package store

import (
	"context"
	"database/sql"
	"errors"
)

// GetSetting returns the value stored under key. ok is false when there is none.
func (s *Store) GetSetting(ctx context.Context, key string) (value string, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

// ApplySettings stores every value in set, replacing earlier values, and removes every key
// in remove, in one transaction: either all of it happens or none of it. Removing a key
// that does not exist is not an error.
func (s *Store) ApplySettings(ctx context.Context, set map[string]string, remove []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for key, value := range set {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			key, value); err != nil {
			return err
		}
	}
	for _, key := range remove {
		if _, err := tx.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, key); err != nil {
			return err
		}
	}
	return tx.Commit()
}
