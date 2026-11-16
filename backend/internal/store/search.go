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

// AppliedFilter narrows a search by whether the job has been applied to.
type AppliedFilter string

const (
	AppliedAny  AppliedFilter = ""
	AppliedOnly AppliedFilter = "only" // only jobs with an application, latest applied first
	AppliedHide AppliedFilter = "hide" // everything not applied to
)

// Search selects stored jobs, newest first. Zero values mean "no filter".
type Search struct {
	Status     Status
	Source     string
	Reason     string // exact reject reason, e.g. "seniority"
	B2BOnly    bool
	Unverified bool // only jobs whose region was not positively confirmed
	Applied    AppliedFilter
	Query      string    // case-insensitive substring of title or company
	Since      time.Time // first seen at or after
	Limit      int
	Offset     int
}

// where builds the SQL condition for the filters.
func (q Search) where() (string, []any) {
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
	switch q.Applied {
	case AppliedOnly:
		where = append(where, "id IN (SELECT job_id FROM applications)")
	case AppliedHide:
		where = append(where, "id NOT IN (SELECT job_id FROM applications)")
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
	if len(where) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(where, " AND "), args
}

// order is how results are sorted: newest first, except that the tracker lists
// the most recently applied first.
func (q Search) order() string {
	if q.Applied == AppliedOnly {
		return " ORDER BY (SELECT applied_on FROM applications a WHERE a.job_id = jobs.id) DESC, first_seen_at DESC, id DESC"
	}
	return " ORDER BY first_seen_at DESC, id DESC"
}

// SearchIDs returns the ids of every job matching the filters, newest first,
// ignoring Limit and Offset. It lets a caller rank or sort by something the
// database doesn't know, such as fit, and then fetch only one page.
func (s *Store) SearchIDs(ctx context.Context, q Search) ([]int64, error) {
	cond, args := q.where()
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM jobs"+cond+q.order(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// JobsByIDs returns the jobs with the given ids, in that order. Unknown ids
// are skipped.
func (s *Store) JobsByIDs(ctx context.Context, ids []int64) ([]*domain.Job, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	jobs, err := s.queryJobs(ctx, "SELECT "+jobCols+" FROM jobs WHERE id IN ("+marks+")", args...)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]*domain.Job, len(jobs))
	for _, j := range jobs {
		byID[j.ID] = j
	}
	out := make([]*domain.Job, 0, len(ids))
	for _, id := range ids {
		if j, ok := byID[id]; ok {
			out = append(out, j)
		}
	}
	return out, nil
}

// Search returns one page of jobs and the total number matching the filters.
func (s *Store) Search(ctx context.Context, q Search) ([]*domain.Job, int, error) {
	cond, args := q.where()

	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM jobs"+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	jobs, err := s.queryJobs(ctx,
		"SELECT "+jobCols+" FROM jobs"+cond+q.order()+" LIMIT ? OFFSET ?",
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
