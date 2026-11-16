package matcher

import (
	"context"
	"fmt"
	"strings"

	"github.com/Steffeh1337/job-pilot/backend/internal/domain"
	"github.com/Steffeh1337/job-pilot/backend/internal/fit"
	"github.com/Steffeh1337/job-pilot/backend/internal/ollama"
	"github.com/Steffeh1337/job-pilot/backend/internal/skills"
	"github.com/Steffeh1337/job-pilot/backend/internal/store"
)

// SyncReport says what one indexing pass did.
type SyncReport struct {
	SkillsIndexed   int    `json:"skills_indexed"`
	JobsEmbedded    int    `json:"jobs_embedded"`
	ProfileEmbedded bool   `json:"profile_embedded"`
	EmbedError      string `json:"embed_error,omitempty"`
}

// RequestSync indexes in the background and returns at once. Requests made
// while a pass is running are folded into one more pass afterwards.
func (m *Matcher) RequestSync() {
	m.mu.Lock()
	m.pending = true
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.mu.Unlock()

	go func() {
		for {
			m.mu.Lock()
			if !m.pending {
				m.running = false
				m.mu.Unlock()
				return
			}
			m.pending = false
			m.mu.Unlock()
			if _, err := m.Sync(m.ctx()); err != nil && m.Log != nil {
				m.Log.Warn("index jobs", "err", err)
			}
		}
	}()
}

const skillBatch = 500

// Sync finds the skills of every job that needs them and, when there is a
// profile and a local embedding model, embeds the profile and the jobs. A
// missing or stopped Ollama is not an error: skills matching still works, and
// the problem is reported.
func (m *Matcher) Sync(ctx context.Context) (SyncReport, error) {
	m.syncMu.Lock()
	defer m.syncMu.Unlock()

	var rep SyncReport
	n, err := m.indexSkills(ctx)
	rep.SkillsIndexed = n
	if err == nil {
		err = m.indexEmbeddings(ctx, &rep)
	}

	m.mu.Lock()
	m.lastSync, m.lastRun = m.now(), rep
	m.lastErr = ""
	switch {
	case err != nil:
		m.lastErr = err.Error()
	case rep.EmbedError != "":
		m.lastErr = rep.EmbedError
	}
	m.mu.Unlock()
	return rep, err
}

func (m *Matcher) indexSkills(ctx context.Context) (int, error) {
	version := m.featureVersion()
	total := 0
	for {
		jobs, err := m.Store.JobsNeedingSkills(ctx, m.Cfg.MinStage, version, skillBatch)
		if err != nil {
			return total, err
		}
		if len(jobs) == 0 {
			return total, nil
		}
		batch := make([]store.JobSkills, len(jobs))
		for i, j := range jobs {
			batch[i] = m.jobSkills(j, version)
		}
		if err := m.Store.SaveJobSkills(ctx, batch); err != nil {
			return total, err
		}
		total += len(batch)
		if err := ctx.Err(); err != nil {
			return total, err
		}
	}
}

// extractionVersion changes whenever the way skills are read from a job
// changes, so jobs indexed the old way are indexed again.
const extractionVersion = "2"

// featureVersion identifies the skills vocabulary and extraction logic that
// produced a job's stored skills.
func (m *Matcher) featureVersion() string { return m.Vocab.Version() + "." + extractionVersion }

// thinDescription is how short a description can be before the tags are
// consulted too. Tags are not trustworthy evidence by themselves: some boards
// let posters stuff them with unrelated keywords (a recruiter role tagged
// "golang"), so they only fill in when there is almost no text to read.
const thinDescription = 300

// jobSkills reads the skills a job asks for from its title and description.
// Title skills, which count double in the score, come from the title alone.
func (m *Matcher) jobSkills(j *domain.Job, version string) store.JobSkills {
	text := j.Title + "\n" + j.Description
	if len(strings.TrimSpace(j.Description)) < thinDescription {
		text += "\n" + strings.Join(j.Tags, "\n")
	}
	return store.JobSkills{
		JobID:        j.ID,
		Skills:       skillNames(m.Vocab.Extract(text)),
		TitleSkills:  skillNames(m.Vocab.Extract(j.Title)),
		VocabVersion: version,
	}
}

func (m *Matcher) indexEmbeddings(ctx context.Context, rep *SyncReport) error {
	if m.LLM == nil {
		return nil
	}
	row, err := m.Store.LoadProfile(ctx)
	if err != nil || row == nil {
		return err // no profile: nothing to compare yet, so don't spend time embedding jobs
	}
	model := m.Cfg.EmbedModel

	if row.EmbedModel != model || len(row.Embedding) == 0 {
		var p Profile
		if err := unmarshalProfile(row.Data, &p); err != nil {
			return err
		}
		vecs, err := m.LLM.Embed(ctx, model, []string{m.Cfg.EmbedPrefix + profileText(p, row.CVText)})
		if err != nil {
			rep.EmbedError = embedProblem(err)
			return nil
		}
		applied, err := m.Store.SetProfileEmbedding(ctx, row.Data, model, fit.EncodeVector(fit.Normalize(vecs[0])))
		if err != nil {
			return err
		}
		if applied {
			rep.ProfileEmbedded = true
			m.invalidate()
		} else {
			return nil // edited meanwhile; the next pass embeds the new text
		}
	}

	for {
		jobs, err := m.Store.JobsNeedingEmbeddings(ctx, m.Cfg.MinStage, model, m.Cfg.BatchSize)
		if err != nil {
			return err
		}
		if len(jobs) == 0 {
			return nil
		}
		inputs := make([]string, len(jobs))
		for i, j := range jobs {
			inputs[i] = m.Cfg.EmbedPrefix + jobText(j)
		}
		vecs, err := m.LLM.Embed(ctx, model, inputs)
		if err != nil {
			rep.EmbedError = embedProblem(err)
			return nil
		}
		batch := make([]store.JobEmbedding, len(jobs))
		for i, j := range jobs {
			batch[i] = store.JobEmbedding{JobID: j.ID, Model: model, Vector: fit.EncodeVector(fit.Normalize(vecs[i]))}
		}
		if err := m.Store.SaveJobEmbeddings(ctx, batch); err != nil {
			return err
		}
		rep.JobsEmbedded += len(batch)
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}

func embedProblem(err error) string {
	switch {
	case ollama.IsUnavailable(err):
		return "Ollama is not running, so jobs are matched on skills only. Start it to add the semantic score."
	case ollama.IsModelMissing(err):
		return err.Error()
	default:
		return fmt.Sprintf("embedding failed: %v", err)
	}
}

func skillNames(hits []skills.Hit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.Name
	}
	return out
}

// profileText is what the profile is embedded as: who the person is, their
// skills, and the start of the CV itself.
func profileText(p Profile, cvText string) string {
	var b strings.Builder
	if p.Headline != "" {
		b.WriteString(p.Headline + "\n")
	}
	if p.Summary != "" {
		b.WriteString(p.Summary + "\n")
	}
	if len(p.Skills) > 0 {
		names := make([]string, len(p.Skills))
		for i, s := range p.Skills {
			names[i] = s.Name
		}
		b.WriteString("Skills: " + strings.Join(names, ", ") + "\n")
	}
	b.WriteString(clipRunes(cvText, 4000))
	return b.String()
}

// jobText is what a job is embedded as.
func jobText(j *domain.Job) string {
	var b strings.Builder
	b.WriteString(j.Title + "\n")
	if len(j.Tags) > 0 {
		b.WriteString(strings.Join(j.Tags, ", ") + "\n")
	}
	b.WriteString(clipRunes(j.Description, 2500))
	return b.String()
}
