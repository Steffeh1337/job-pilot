package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Steffeh1337/job-pilot/backend/internal/domain"
)

// matchSchema holds the tables behind CV matching. It runs on every open and
// is safe to repeat.
const matchSchema = `
CREATE TABLE IF NOT EXISTS profile (
	id           INTEGER PRIMARY KEY CHECK (id = 1),
	data         TEXT NOT NULL,
	cv_text      TEXT NOT NULL,
	cv_filename  TEXT NOT NULL,
	extracted_by TEXT NOT NULL,
	embed_model  TEXT NOT NULL DEFAULT '',
	embedding    BLOB,
	uploaded_at  INTEGER NOT NULL,
	updated_at   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS job_features (
	job_id        INTEGER PRIMARY KEY,
	skills        TEXT NOT NULL,
	title_skills  TEXT NOT NULL,
	vocab_version TEXT NOT NULL,
	embed_model   TEXT NOT NULL DEFAULT '',
	embedding     BLOB,
	updated_at    INTEGER NOT NULL
);
`

// ProfileRow is the single stored CV profile. Data is the editable profile as
// JSON; the CV text stays here so it can be re-read without uploading again.
type ProfileRow struct {
	Data        []byte
	CVText      string
	CVFilename  string
	ExtractedBy string
	EmbedModel  string
	Embedding   []byte
	UploadedAt  time.Time
	UpdatedAt   time.Time
}

// SaveProfile replaces the stored profile. Its embedding is cleared unless the
// row carries one, because the text it was computed from has changed.
func (s *Store) SaveProfile(ctx context.Context, p ProfileRow) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO profile (id, data, cv_text, cv_filename, extracted_by, embed_model, embedding, uploaded_at, updated_at)
VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET data = excluded.data, cv_text = excluded.cv_text, cv_filename = excluded.cv_filename,
	extracted_by = excluded.extracted_by, embed_model = excluded.embed_model, embedding = excluded.embedding,
	uploaded_at = excluded.uploaded_at, updated_at = excluded.updated_at`,
		string(p.Data), p.CVText, p.CVFilename, p.ExtractedBy, p.EmbedModel, nilIfEmpty(p.Embedding),
		p.UploadedAt.Unix(), p.UpdatedAt.Unix())
	if err != nil {
		return err
	}
	s.flush(ctx)
	return nil
}

// flush moves everything committed so far out of the write-ahead log into the
// main database file and syncs it. In WAL mode a commit alone survives the
// program crashing, but a power cut right afterwards can lose it; a person's CV
// is worth the extra moment. It is best effort: if another program is reading
// the database right now the checkpoint can't finish, and the data is still
// safely committed in the log.
func (s *Store) flush(ctx context.Context) {
	for attempt := 0; attempt < 3; attempt++ {
		var busy, logFrames, done int
		if err := s.db.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logFrames, &done); err != nil || busy == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// LoadProfile returns the stored profile, or nil if there is none.
func (s *Store) LoadProfile(ctx context.Context) (*ProfileRow, error) {
	var (
		p                   ProfileRow
		data                string
		uploaded, updatedAt int64
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT data, cv_text, cv_filename, extracted_by, embed_model, embedding, uploaded_at, updated_at FROM profile WHERE id = 1`).
		Scan(&data, &p.CVText, &p.CVFilename, &p.ExtractedBy, &p.EmbedModel, &p.Embedding, &uploaded, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.Data = []byte(data)
	p.UploadedAt, p.UpdatedAt = time.Unix(uploaded, 0), time.Unix(updatedAt, 0)
	return &p, nil
}

// SetProfileEmbedding stores the embedding of the profile whose Data is
// forData. It does nothing, and reports false, if the profile has been edited
// or replaced since: embedding takes seconds and the edit must not be
// overwritten by an embedding of its older text.
func (s *Store) SetProfileEmbedding(ctx context.Context, forData []byte, model string, vec []byte) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE profile SET embed_model = ?, embedding = ? WHERE id = 1 AND data = ?`,
		model, nilIfEmpty(vec), string(forData))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// DeleteProfile removes the CV text and the profile. Job features stay: they
// describe the jobs, not the person.
func (s *Store) DeleteProfile(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM profile WHERE id = 1`); err != nil {
		return err
	}
	s.flush(ctx) // also drops the CV text from the log
	return nil
}

func nilIfEmpty(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

// JobSkills is the skills found in one job.
type JobSkills struct {
	JobID        int64
	Skills       []string
	TitleSkills  []string
	VocabVersion string
}

// JobEmbedding is the embedding of one job.
type JobEmbedding struct {
	JobID  int64
	Model  string
	Vector []byte
}

// JobFeatures is what is stored about a job for matching.
type JobFeatures struct {
	Skills       []string
	TitleSkills  []string
	VocabVersion string
	EmbedModel   string
	Embedding    []byte
}

// SaveJobSkills stores skills for a batch of jobs, keeping any embedding they
// already have.
func (s *Store) SaveJobSkills(ctx context.Context, batch []JobSkills) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	for _, b := range batch {
		skills, _ := json.Marshal(nonNil(b.Skills))
		title, _ := json.Marshal(nonNil(b.TitleSkills))
		if _, err := tx.ExecContext(ctx, `
INSERT INTO job_features (job_id, skills, title_skills, vocab_version, updated_at) VALUES (?, ?, ?, ?, ?)
ON CONFLICT(job_id) DO UPDATE SET skills = excluded.skills, title_skills = excluded.title_skills,
	vocab_version = excluded.vocab_version, updated_at = excluded.updated_at`,
			b.JobID, string(skills), string(title), b.VocabVersion, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SaveJobEmbeddings stores embeddings for jobs that already have a features row.
func (s *Store) SaveJobEmbeddings(ctx context.Context, batch []JobEmbedding) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, b := range batch {
		if _, err := tx.ExecContext(ctx, `UPDATE job_features SET embed_model = ?, embedding = ? WHERE job_id = ?`,
			b.Model, nilIfEmpty(b.Vector), b.JobID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// JobFeaturesFor returns the stored features of the given jobs. Jobs without
// any are absent from the map.
func (s *Store) JobFeaturesFor(ctx context.Context, ids []int64) (map[int64]JobFeatures, error) {
	out := make(map[int64]JobFeatures, len(ids))
	const chunk = 500
	for start := 0; start < len(ids); start += chunk {
		part := ids[start:min(start+chunk, len(ids))]
		marks := strings.TrimSuffix(strings.Repeat("?,", len(part)), ",")
		args := make([]any, len(part))
		for i, id := range part {
			args[i] = id
		}
		rows, err := s.db.QueryContext(ctx,
			`SELECT job_id, skills, title_skills, vocab_version, embed_model, embedding FROM job_features WHERE job_id IN (`+marks+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var (
				id            int64
				skills, title string
				f             JobFeatures
			)
			if err := rows.Scan(&id, &skills, &title, &f.VocabVersion, &f.EmbedModel, &f.Embedding); err != nil {
				rows.Close()
				return nil, err
			}
			_ = json.Unmarshal([]byte(skills), &f.Skills)
			_ = json.Unmarshal([]byte(title), &f.TitleSkills)
			out[id] = f
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

// JobsNeedingSkills returns jobs at or past minStage that have no skills yet,
// or skills computed with another vocabulary version, newest first.
func (s *Store) JobsNeedingSkills(ctx context.Context, minStage domain.Stage, vocabVersion string, limit int) ([]*domain.Job, error) {
	return s.queryJobs(ctx, `
SELECT `+jobCols+` FROM jobs LEFT JOIN job_features f ON f.job_id = jobs.id
WHERE stage >= ? AND (f.job_id IS NULL OR f.vocab_version != ?)
ORDER BY first_seen_at DESC, jobs.id DESC LIMIT ?`, int(minStage), vocabVersion, limit)
}

// JobsNeedingEmbeddings returns jobs at or past minStage that have skills but
// no embedding from the given model, newest first.
func (s *Store) JobsNeedingEmbeddings(ctx context.Context, minStage domain.Stage, model string, limit int) ([]*domain.Job, error) {
	return s.queryJobs(ctx, `
SELECT `+jobCols+` FROM jobs JOIN job_features f ON f.job_id = jobs.id
WHERE stage >= ? AND (f.embed_model != ? OR f.embedding IS NULL)
ORDER BY first_seen_at DESC, jobs.id DESC LIMIT ?`, int(minStage), model, limit)
}

// FeatureCounts says how far indexing has got.
type FeatureCounts struct {
	Eligible       int `json:"eligible"`
	WithSkills     int `json:"with_skills"`
	WithEmbeddings int `json:"with_embeddings"`
}

func (s *Store) FeatureCounts(ctx context.Context, minStage domain.Stage, vocabVersion, model string) (FeatureCounts, error) {
	var c FeatureCounts
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*),
	COALESCE(SUM(f.job_id IS NOT NULL AND f.vocab_version = ?), 0),
	COALESCE(SUM(f.job_id IS NOT NULL AND f.embed_model = ? AND f.embedding IS NOT NULL), 0)
FROM jobs LEFT JOIN job_features f ON f.job_id = jobs.id WHERE stage >= ?`,
		vocabVersion, model, int(minStage)).Scan(&c.Eligible, &c.WithSkills, &c.WithEmbeddings)
	return c, err
}

// JobEmbeddings returns the stored embeddings of jobs at or past minStage that
// were made with model.
func (s *Store) JobEmbeddings(ctx context.Context, minStage domain.Stage, model string) ([][]byte, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT f.embedding FROM job_features f JOIN jobs ON jobs.id = f.job_id
WHERE jobs.stage >= ? AND f.embed_model = ? AND f.embedding IS NOT NULL`, int(minStage), model)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][]byte
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
