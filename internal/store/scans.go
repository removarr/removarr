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

const (
	ScanRunning = "running"
	ScanDone    = "done"

	keepScans = 10
)

// ScanRecord is one stored scan.
type ScanRecord struct {
	ID         int64              `json:"id"`
	StartedAt  time.Time          `json:"startedAt"`
	FinishedAt *time.Time         `json:"finishedAt"`
	Status     string             `json:"status"`
	Summary    domain.ScanSummary `json:"summary"`
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// BeginScan records the start of a scan and returns its ID. A scan left running by an
// earlier crash is dropped first; the scanner guarantees only one scan runs at a time.
func (s *Store) BeginScan(ctx context.Context, startedAt time.Time) (int64, error) {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM scans WHERE status = ?`, ScanRunning); err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO scans (started_at, status) VALUES (?, ?)`, formatTime(startedAt), ScanRunning)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FinishScan stores a scan's summary and candidates in one transaction. The candidates of
// earlier scans are dropped, and only the last ten scans are kept.
func (s *Store) FinishScan(ctx context.Context, id int64, finishedAt time.Time, summary domain.ScanSummary, candidates []domain.Candidate) error {
	rawSummary, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `UPDATE scans SET finished_at = ?, status = ?, summary = ? WHERE id = ?`,
		formatTime(finishedAt), ScanDone, string(rawSummary), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("scan %d does not exist", id)
	}
	for _, c := range candidates {
		data, err := json.Marshal(c)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO candidates (scan_id, list, key, data) VALUES (?, ?, ?, ?)`,
			id, string(c.List), c.Key, string(data)); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM candidates WHERE scan_id <> ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM scans WHERE id NOT IN (SELECT id FROM scans ORDER BY id DESC LIMIT ?)`, keepScans); err != nil {
		return err
	}
	return tx.Commit()
}

// queryer is what a read needs: the database itself, or a transaction on it.
type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// LatestScan returns the most recent finished scan, or nil when there is none.
func (s *Store) LatestScan(ctx context.Context) (*ScanRecord, error) {
	return latestScan(ctx, s.db)
}

// LatestList returns the most recent finished scan together with its rows of one list. Both
// are read in one transaction: FinishScan drops the rows of earlier scans, so a scan that
// finished between two separate reads would leave the older scan with no rows. The scan is
// nil when there is none.
func (s *Store) LatestList(ctx context.Context, list domain.ListKind) (*ScanRecord, []domain.Candidate, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback() // nothing was written

	rec, err := latestScan(ctx, tx)
	if err != nil {
		return nil, nil, err
	}
	if rec == nil {
		return nil, []domain.Candidate{}, nil
	}
	rows, err := candidates(ctx, tx, rec.ID, list)
	if err != nil {
		return nil, nil, err
	}
	return rec, rows, nil
}

// Latest is the latest finished scan, every row it stored and the ignores, read together.
type Latest struct {
	Scan    *ScanRecord        // nil when no scan has finished
	Rows    []domain.Candidate // the rows of every list, listed and held back, by list and key; never nil
	Ignores []domain.Ignore    // sorted by key; never nil
}

// ReadLatest reads the latest finished scan, its rows and the ignores in one transaction.
// FinishScan drops the rows of earlier scans, so a scan that finished between two separate
// reads would leave the older scan with no rows; and the lists are only right with the
// ignores of the same moment (spec 6.5).
func (s *Store) ReadLatest(ctx context.Context) (Latest, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Latest{}, err
	}
	defer tx.Rollback() // nothing was written

	out := Latest{Rows: []domain.Candidate{}}
	if out.Scan, err = latestScan(ctx, tx); err != nil {
		return Latest{}, err
	}
	if out.Scan != nil {
		if out.Rows, err = allCandidates(ctx, tx, out.Scan.ID); err != nil {
			return Latest{}, err
		}
	}
	if out.Ignores, err = ignores(ctx, tx); err != nil {
		return Latest{}, err
	}
	return out, nil
}

func latestScan(ctx context.Context, q queryer) (*ScanRecord, error) {
	var (
		rec      ScanRecord
		started  string
		finished sql.NullString
		summary  string
	)
	err := q.QueryRowContext(ctx,
		`SELECT id, started_at, finished_at, status, summary FROM scans WHERE status = ? ORDER BY id DESC LIMIT 1`, ScanDone).
		Scan(&rec.ID, &started, &finished, &rec.Status, &summary)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if rec.StartedAt, err = time.Parse(time.RFC3339Nano, started); err != nil {
		return nil, fmt.Errorf("scan %d: bad start time: %w", rec.ID, err)
	}
	if finished.Valid {
		t, err := time.Parse(time.RFC3339Nano, finished.String)
		if err != nil {
			return nil, fmt.Errorf("scan %d: bad finish time: %w", rec.ID, err)
		}
		rec.FinishedAt = &t
	}
	if err := json.Unmarshal([]byte(summary), &rec.Summary); err != nil {
		return nil, fmt.Errorf("scan %d: bad summary: %w", rec.ID, err)
	}
	return &rec, nil
}

// Candidates returns the rows of one list of one scan, sorted by key.
func (s *Store) Candidates(ctx context.Context, scanID int64, list domain.ListKind) ([]domain.Candidate, error) {
	return candidates(ctx, s.db, scanID, list)
}

func candidates(ctx context.Context, q queryer, scanID int64, list domain.ListKind) ([]domain.Candidate, error) {
	return readCandidates(q.QueryContext(ctx, `SELECT data FROM candidates WHERE scan_id = ? AND list = ? ORDER BY key`, scanID, string(list)))
}

func allCandidates(ctx context.Context, q queryer, scanID int64) ([]domain.Candidate, error) {
	return readCandidates(q.QueryContext(ctx, `SELECT data FROM candidates WHERE scan_id = ? ORDER BY list, key`, scanID))
}

// readCandidates decodes the rows of a query on the data column. The slice is never nil.
func readCandidates(rows *sql.Rows, err error) ([]domain.Candidate, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Candidate{}
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var c domain.Candidate
		if err := json.Unmarshal([]byte(data), &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
