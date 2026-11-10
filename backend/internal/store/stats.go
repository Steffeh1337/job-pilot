package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Steffeh1337/job-pilot/backend/internal/domain"
)

// FunnelRow counts distinct jobs first seen in the window that reached each
// stage. Every column is cumulative: Region counts jobs that also passed Unique.
type FunnelRow struct {
	Source string `json:"source"`
	New    int    `json:"new"`
	Unique int    `json:"unique"`
	Region int    `json:"region"`
	Role   int    `json:"role"`
	Stack  int    `json:"stack"`
	Senior int    `json:"senior"`
	B2B    int    `json:"b2b"` // subset of Senior that mentions B2B/contract
}

// Funnel returns one row per source plus a final "TOTAL" row.
func (s *Store) Funnel(ctx context.Context, since time.Time) ([]FunnelRow, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT source,
	COUNT(*),
	COALESCE(SUM(stage >= ?), 0),
	COALESCE(SUM(stage >= ?), 0),
	COALESCE(SUM(stage >= ?), 0),
	COALESCE(SUM(stage >= ?), 0),
	COALESCE(SUM(stage >= ?), 0),
	COALESCE(SUM(stage >= ? AND b2b = 1), 0)
FROM jobs WHERE first_seen_at >= ?
GROUP BY source ORDER BY source`,
		int(domain.StageUnique), int(domain.StageRegion), int(domain.StageRole),
		int(domain.StageStack), int(domain.StageSenior), int(domain.StageSenior), since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	total := FunnelRow{Source: "TOTAL"}
	var out []FunnelRow
	for rows.Next() {
		var r FunnelRow
		if err := rows.Scan(&r.Source, &r.New, &r.Unique, &r.Region, &r.Role, &r.Stack, &r.Senior, &r.B2B); err != nil {
			return nil, err
		}
		out = append(out, r)
		total.New += r.New
		total.Unique += r.Unique
		total.Region += r.Region
		total.Role += r.Role
		total.Stack += r.Stack
		total.Senior += r.Senior
		total.B2B += r.B2B
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return append(out, total), nil
}

// RejectReasons counts how many new jobs were dropped for each reason, so a
// filter that is too strict is easy to spot.
func (s *Store) RejectReasons(ctx context.Context, since time.Time) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT reject_reason, COUNT(*) FROM jobs WHERE first_seen_at >= ? AND reject_reason != '' GROUP BY reject_reason`,
		since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}

// Poll is one fetch of one source.
type Poll struct {
	Source    string        `json:"source"`
	StartedAt time.Time     `json:"started_at"`
	Duration  time.Duration `json:"duration"`
	Fetched   int           `json:"fetched"`
	NewJobs   int           `json:"new_jobs"`
	Error     string        `json:"error,omitempty"`
}

func (s *Store) RecordPoll(ctx context.Context, p Poll) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO polls (source, started_at, duration_ms, fetched, new_jobs, error) VALUES (?,?,?,?,?,?)`,
		p.Source, p.StartedAt.Unix(), p.Duration.Milliseconds(), p.Fetched, p.NewJobs, p.Error)
	return err
}

// LastPoll returns the most recent poll of source, or nil if it never ran.
// Failed polls count: a broken source must not be hammered every tick.
func (s *Store) LastPoll(ctx context.Context, source string) (*Poll, error) {
	var (
		p       Poll
		started int64
		ms      int64
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT source, started_at, duration_ms, fetched, new_jobs, error FROM polls WHERE source = ? ORDER BY id DESC LIMIT 1`,
		source).Scan(&p.Source, &started, &ms, &p.Fetched, &p.NewJobs, &p.Error)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.StartedAt, p.Duration = time.Unix(started, 0), time.Duration(ms)*time.Millisecond
	return &p, nil
}

// RecentPolls returns the latest poll per source.
func (s *Store) RecentPolls(ctx context.Context) ([]Poll, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT source, started_at, duration_ms, fetched, new_jobs, error FROM polls
WHERE id IN (SELECT MAX(id) FROM polls GROUP BY source) ORDER BY source`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Poll
	for rows.Next() {
		var (
			p       Poll
			started int64
			ms      int64
		)
		if err := rows.Scan(&p.Source, &started, &ms, &p.Fetched, &p.NewJobs, &p.Error); err != nil {
			return nil, err
		}
		p.StartedAt, p.Duration = time.Unix(started, 0), time.Duration(ms)*time.Millisecond
		out = append(out, p)
	}
	return out, rows.Err()
}
