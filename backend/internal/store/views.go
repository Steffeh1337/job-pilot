package store

import (
	"context"
	"strings"
	"time"
)

// viewSchema remembers which jobs the person has opened, so the list can show
// them as read and sort them below the ones still to look at. It runs on every
// open and is safe to repeat.
const viewSchema = `
CREATE TABLE IF NOT EXISTS job_views (
	job_id    INTEGER PRIMARY KEY,
	viewed_at INTEGER NOT NULL
);
`

// MarkViewed records that a job was opened. Marking one that is already viewed
// keeps it viewed.
func (s *Store) MarkViewed(ctx context.Context, jobID int64, at time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO job_views (job_id, viewed_at) VALUES (?, ?) ON CONFLICT(job_id) DO UPDATE SET viewed_at = excluded.viewed_at`,
		jobID, at.Unix())
	return err
}

// MarkUnread forgets that a job was opened. Doing it to one that isn't viewed is fine.
func (s *Store) MarkUnread(ctx context.Context, jobID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM job_views WHERE job_id = ?`, jobID)
	return err
}

// ViewedAmong returns the ids among the given jobs that have been viewed.
func (s *Store) ViewedAmong(ctx context.Context, ids []int64) (map[int64]bool, error) {
	out := make(map[int64]bool)
	const chunk = 500
	for start := 0; start < len(ids); start += chunk {
		part := ids[start:min(start+chunk, len(ids))]
		marks := strings.TrimSuffix(strings.Repeat("?,", len(part)), ",")
		args := make([]any, len(part))
		for i, id := range part {
			args[i] = id
		}
		rows, err := s.db.QueryContext(ctx, `SELECT job_id FROM job_views WHERE job_id IN (`+marks+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			out[id] = true
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}
