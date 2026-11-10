package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Steffeh1337/job-pilot/backend/internal/domain"
	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS jobs (
	id              INTEGER PRIMARY KEY,
	source          TEXT NOT NULL,
	external_id     TEXT NOT NULL,
	url             TEXT NOT NULL,
	title           TEXT NOT NULL,
	company         TEXT NOT NULL,
	location        TEXT NOT NULL,
	regions         TEXT NOT NULL,
	remote          INTEGER NOT NULL,
	employment_type TEXT NOT NULL,
	tags            TEXT NOT NULL,
	description     TEXT NOT NULL,
	salary_min      INTEGER NOT NULL,
	salary_max      INTEGER NOT NULL,
	currency        TEXT NOT NULL,
	posted_at       INTEGER NOT NULL,
	first_seen_at   INTEGER NOT NULL,
	last_seen_at    INTEGER NOT NULL,
	fingerprint     TEXT NOT NULL,
	dup_of          INTEGER NOT NULL DEFAULT 0,
	stage           INTEGER NOT NULL,
	b2b             INTEGER NOT NULL,
	reject_reason   TEXT NOT NULL,
	UNIQUE (source, external_id)
);
CREATE INDEX IF NOT EXISTS jobs_fingerprint ON jobs (fingerprint);
CREATE INDEX IF NOT EXISTS jobs_first_seen ON jobs (first_seen_at);

CREATE TABLE IF NOT EXISTS polls (
	id          INTEGER PRIMARY KEY,
	source      TEXT NOT NULL,
	started_at  INTEGER NOT NULL,
	duration_ms INTEGER NOT NULL,
	fetched     INTEGER NOT NULL,
	new_jobs    INTEGER NOT NULL,
	error       TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS polls_source ON polls (source, started_at);
`

// Open opens (creating if needed) the SQLite database at path.
// Use ":memory:" for tests.
func Open(path string) (*Store, error) {
	dsn := path
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		dsn = "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection keeps ":memory:" coherent and avoids writer contention.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Seen reports whether (source, externalID) is stored and, if so, bumps its
// last_seen_at.
func (s *Store) Seen(ctx context.Context, source, externalID string, now time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET last_seen_at = ? WHERE source = ? AND external_id = ?`,
		now.Unix(), source, externalID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// FindDuplicate returns the id of the earliest non-duplicate job with the
// fingerprint, or 0 if there is none.
func (s *Store) FindDuplicate(ctx context.Context, fingerprint string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM jobs WHERE fingerprint = ? AND dup_of = 0 ORDER BY id LIMIT 1`,
		fingerprint).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

func (s *Store) Insert(ctx context.Context, j *domain.Job) error {
	regions, _ := json.Marshal(j.Regions)
	tags, _ := json.Marshal(j.Tags)
	res, err := s.db.ExecContext(ctx, `
INSERT INTO jobs (source, external_id, url, title, company, location, regions, remote,
	employment_type, tags, description, salary_min, salary_max, currency, posted_at,
	first_seen_at, last_seen_at, fingerprint, dup_of, stage, b2b, reject_reason)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		j.Source, j.ExternalID, j.URL, j.Title, j.Company, j.Location, string(regions), b2i(j.Remote),
		j.EmploymentType, string(tags), j.Description, j.SalaryMin, j.SalaryMax, j.Currency, j.PostedAt.Unix(),
		j.FirstSeenAt.Unix(), j.LastSeenAt.Unix(), j.Fingerprint, j.DupOf, int(j.Stage), b2i(j.B2B), j.RejectReason)
	if err != nil {
		return err
	}
	j.ID, err = res.LastInsertId()
	return err
}

// UpdateClassification stores a re-run of the filters.
func (s *Store) UpdateClassification(ctx context.Context, j *domain.Job) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET stage = ?, b2b = ?, reject_reason = ? WHERE id = ?`,
		int(j.Stage), b2i(j.B2B), j.RejectReason, j.ID)
	return err
}

const jobCols = `id, source, external_id, url, title, company, location, regions, remote,
	employment_type, tags, description, salary_min, salary_max, currency, posted_at,
	first_seen_at, last_seen_at, fingerprint, dup_of, stage, b2b, reject_reason`

func scanJob(sc interface{ Scan(...any) error }) (*domain.Job, error) {
	var (
		j                           domain.Job
		regions, tags               string
		remote, b2b                 int
		posted, firstSeen, lastSeen int64
		stage                       int
	)
	err := sc.Scan(&j.ID, &j.Source, &j.ExternalID, &j.URL, &j.Title, &j.Company, &j.Location, &regions, &remote,
		&j.EmploymentType, &tags, &j.Description, &j.SalaryMin, &j.SalaryMax, &j.Currency, &posted,
		&firstSeen, &lastSeen, &j.Fingerprint, &j.DupOf, &stage, &b2b, &j.RejectReason)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(regions), &j.Regions)
	_ = json.Unmarshal([]byte(tags), &j.Tags)
	j.Remote, j.B2B, j.Stage = remote != 0, b2b != 0, domain.Stage(stage)
	j.PostedAt, j.FirstSeenAt, j.LastSeenAt = time.Unix(posted, 0), time.Unix(firstSeen, 0), time.Unix(lastSeen, 0)
	return &j, nil
}

// AllJobs returns every stored job, for re-running filters.
func (s *Store) AllJobs(ctx context.Context) ([]*domain.Job, error) {
	return s.queryJobs(ctx, `SELECT `+jobCols+` FROM jobs ORDER BY id`)
}

// Matches returns jobs that cleared every stage, newest first.
func (s *Store) Matches(ctx context.Context, limit int) ([]*domain.Job, error) {
	return s.queryJobs(ctx, `SELECT `+jobCols+` FROM jobs WHERE stage >= ? ORDER BY first_seen_at DESC, id DESC LIMIT ?`,
		int(domain.StageSenior), limit)
}

func (s *Store) queryJobs(ctx context.Context, q string, args ...any) ([]*domain.Job, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
