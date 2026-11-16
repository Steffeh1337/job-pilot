package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/Steffeh1337/job-pilot/backend/internal/domain"
)

// Status narrows a search to matches, dropped jobs, or everything.
type Status string

const (
	StatusMatch    Status = "match"
	StatusRejected Status = "rejected"
	StatusAll      Status = "all"
)

// Search selects stored jobs, newest first. Zero values mean "no filter".
type Search struct {
	Status     Status
	Source     string
	Reason     string // exact reject reason, e.g. "stack"
	B2BOnly    bool
	Unverified bool      // only jobs whose region was not positively confirmed
	Query      string    // case-insensitive substring of title or company
	Since      time.Time // first seen at or after
	Limit      int
	Offset     int
}

// Search returns one page of jobs and the total number matching the filters.
func (s *Store) Search(ctx context.Context, q Search) ([]*domain.Job, int, error) {
	var (
		where []string
		args  []any
	)
	switch q.Status {
	case StatusRejected:
		where = append(where, "reject_reason != ''")
	case StatusAll:
	default: // StatusMatch, and the zero value
		where = append(where, "stage >= ?")
		args = append(args, int(domain.StageSenior))
	}
	if q.Source != "" {
		where = append(where, "source = ?")
		args = append(args, q.Source)
	}
	if q.Reason != "" {
		where = append(where, "reject_reason = ?")
		args = append(args, q.Reason)
	}
	if q.B2BOnly {
		where = append(where, "b2b = 1")
	}
	if q.Unverified {
		where = append(where, "region_verified = 0")
	}
	if q.Query != "" {
		where = append(where, `(title LIKE ? ESCAPE '\' OR company LIKE ? ESCAPE '\')`)
		like := "%" + escapeLike(q.Query) + "%"
		args = append(args, like, like)
	}
	if !q.Since.IsZero() {
		where = append(where, "first_seen_at >= ?")
		args = append(args, q.Since.Unix())
	}
	cond := ""
	if len(where) > 0 {
		cond = " WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM jobs"+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	jobs, err := s.queryJobs(ctx,
		"SELECT "+jobCols+" FROM jobs"+cond+" ORDER BY first_seen_at DESC, id DESC LIMIT ? OFFSET ?",
		append(args, limit, q.Offset)...)
	return jobs, total, err
}

// Job returns one job by id, or nil if there is none.
func (s *Store) Job(ctx context.Context, id int64) (*domain.Job, error) {
	j, err := scanJob(s.db.QueryRowContext(ctx, "SELECT "+jobCols+" FROM jobs WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return j, err
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
