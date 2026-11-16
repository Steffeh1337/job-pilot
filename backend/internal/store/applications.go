package store

import (
	"context"
	"strings"
	"time"
)

// applicationSchema is the manual application tracker. Applying happens on
// other sites, so this only records what the person says they did. It runs on
// every open and is safe to repeat.
const applicationSchema = `
CREATE TABLE IF NOT EXISTS applications (
	job_id     INTEGER PRIMARY KEY,
	status     TEXT NOT NULL,
	applied_on TEXT NOT NULL,
	notes      TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
`

// ApplicationStatuses are the stages an application can be in, in order.
var ApplicationStatuses = []string{"applied", "interviewing", "offer", "rejected", "withdrawn"}

// Application is one job the person has applied to.
type Application struct {
	JobID     int64
	Status    string
	AppliedOn string // YYYY-MM-DD, the day it was sent
	Notes     string
	UpdatedAt time.Time
}

// SaveApplication records or updates an application. Saving is synced to the
// main database file straight away: this is the one record that can't be
// rebuilt by polling the boards again.
func (s *Store) SaveApplication(ctx context.Context, a Application) error {
	now := time.Now().Unix()
	if !a.UpdatedAt.IsZero() {
		now = a.UpdatedAt.Unix()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO applications (job_id, status, applied_on, notes, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(job_id) DO UPDATE SET status = excluded.status, applied_on = excluded.applied_on,
	notes = excluded.notes, updated_at = excluded.updated_at`,
		a.JobID, a.Status, a.AppliedOn, a.Notes, now, now)
	if err == nil {
		s.flush(ctx)
	}
	return err
}

// DeleteApplication forgets an application. Deleting one that isn't there is fine.
func (s *Store) DeleteApplication(ctx context.Context, jobID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM applications WHERE job_id = ?`, jobID)
	if err == nil {
		s.flush(ctx)
	}
	return err
}

// ApplicationsFor returns the applications among the given jobs, by job id.
func (s *Store) ApplicationsFor(ctx context.Context, ids []int64) (map[int64]Application, error) {
	out := make(map[int64]Application)
	const chunk = 500
	for start := 0; start < len(ids); start += chunk {
		part := ids[start:min(start+chunk, len(ids))]
		marks := strings.TrimSuffix(strings.Repeat("?,", len(part)), ",")
		args := make([]any, len(part))
		for i, id := range part {
			args[i] = id
		}
		rows, err := s.db.QueryContext(ctx,
			`SELECT job_id, status, applied_on, notes, updated_at FROM applications WHERE job_id IN (`+marks+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var (
				a       Application
				updated int64
			)
			if err := rows.Scan(&a.JobID, &a.Status, &a.AppliedOn, &a.Notes, &updated); err != nil {
				rows.Close()
				return nil, err
			}
			a.UpdatedAt = time.Unix(updated, 0)
			out[a.JobID] = a
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

// ApplicationCount is how many jobs have been applied to.
func (s *Store) ApplicationCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM applications`).Scan(&n)
	return n, err
}
