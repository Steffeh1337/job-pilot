package matcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/Steffeh1337/job-pilot/backend/internal/cv"
	"github.com/Steffeh1337/job-pilot/backend/internal/fit"
	"github.com/Steffeh1337/job-pilot/backend/internal/store"
)

// InvalidCVError is a file that could not be read as a CV.
type InvalidCVError struct{ Err error }

func (e *InvalidCVError) Error() string { return e.Err.Error() }
func (e *InvalidCVError) Unwrap() error { return e.Err }

// ProfileView is a profile with what is known about where it came from.
type ProfileView struct {
	Exists bool `json:"exists"`
	Profile
	CVFilename  string    `json:"cv_filename,omitempty"`
	CVChars     int       `json:"cv_chars,omitempty"`
	CVPreview   string    `json:"cv_preview,omitempty"`
	ExtractedBy string    `json:"extracted_by,omitempty"`
	UploadedAt  time.Time `json:"uploaded_at,omitzero"`
	UpdatedAt   time.Time `json:"updated_at,omitzero"`
	// HasEmbedding is true once the CV has been embedded for semantic matching.
	HasEmbedding bool `json:"has_embedding"`
}

// Upload reads a CV, builds a profile from it and stores both.
func (m *Matcher) Upload(ctx context.Context, filename string, data []byte) (*ProfileView, []string, error) {
	res, err := cv.Extract(data)
	if err != nil {
		return nil, nil, &InvalidCVError{Err: err}
	}
	p, by, warnings := m.extractProfile(ctx, res.Text)
	p, err = m.clean(p)
	if err != nil {
		return nil, nil, err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, nil, err
	}
	now := m.now()
	if err := m.Store.SaveProfile(ctx, store.ProfileRow{
		Data: raw, CVText: res.Text, CVFilename: displayName(filename), ExtractedBy: by, UploadedAt: now, UpdatedAt: now,
	}); err != nil {
		return nil, nil, err
	}
	m.invalidate()
	m.autoSync()
	view, err := m.Profile(ctx)
	return view, warnings, err
}

// Reextract rebuilds the profile from the stored CV text, discarding edits.
// It is for after Ollama or a model has been installed.
func (m *Matcher) Reextract(ctx context.Context) (*ProfileView, []string, error) {
	row, err := m.Store.LoadProfile(ctx)
	if err != nil {
		return nil, nil, err
	}
	if row == nil {
		return nil, nil, ErrNoProfile
	}
	p, by, warnings := m.extractProfile(ctx, row.CVText)
	if p, err = m.clean(p); err != nil {
		return nil, nil, err
	}
	raw, _ := json.Marshal(p)
	row.Data, row.ExtractedBy, row.UpdatedAt = raw, by, m.now()
	row.EmbedModel, row.Embedding = "", nil
	if err := m.Store.SaveProfile(ctx, *row); err != nil {
		return nil, nil, err
	}
	m.invalidate()
	m.autoSync()
	view, err := m.Profile(ctx)
	return view, warnings, err
}

// ErrNoProfile means no CV has been uploaded.
var ErrNoProfile = errors.New("no CV has been uploaded yet")

// Profile returns the stored profile; Exists is false when there is none.
func (m *Matcher) Profile(ctx context.Context) (*ProfileView, error) {
	row, err := m.Store.LoadProfile(ctx)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return &ProfileView{Profile: Profile{Skills: []Skill{}, Languages: []string{}, Highlights: []string{}}}, nil
	}
	var p Profile
	if err := json.Unmarshal(row.Data, &p); err != nil {
		return nil, fmt.Errorf("stored profile is unreadable: %w", err)
	}
	if p.Skills == nil {
		p.Skills = []Skill{}
	}
	if p.Languages == nil {
		p.Languages = []string{}
	}
	if p.Highlights == nil {
		p.Highlights = []string{}
	}
	return &ProfileView{
		Exists: true, Profile: p, CVFilename: row.CVFilename, CVChars: len([]rune(row.CVText)),
		CVPreview: clipRunes(row.CVText, 1500), ExtractedBy: row.ExtractedBy,
		UploadedAt: row.UploadedAt, UpdatedAt: row.UpdatedAt,
		HasEmbedding: len(row.Embedding) > 0 && row.EmbedModel == m.Cfg.EmbedModel,
	}, nil
}

// UpdateProfile stores the user's edits. The CV text is kept.
func (m *Matcher) UpdateProfile(ctx context.Context, p Profile) (*ProfileView, error) {
	row, err := m.Store.LoadProfile(ctx)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, ErrNoProfile
	}
	if p, err = m.clean(p); err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(p)
	row.Data, row.UpdatedAt = raw, m.now()
	row.EmbedModel, row.Embedding = "", nil // the text it described has changed
	if err := m.Store.SaveProfile(ctx, *row); err != nil {
		return nil, err
	}
	m.invalidate()
	m.autoSync()
	return m.Profile(ctx)
}

// DeleteProfile removes the CV text and the profile.
func (m *Matcher) DeleteProfile(ctx context.Context) error {
	if err := m.Store.DeleteProfile(ctx); err != nil {
		return err
	}
	m.invalidate()
	return nil
}

// displayName keeps a safe, short version of an uploaded file name for display.
func displayName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	if name == "." || name == "/" {
		return ""
	}
	return clipRunes(name, 100)
}

// fitProfile is the profile in the form scoring needs.
type fitProfile struct {
	fit.Profile
	// Custom are the skills the user added that the vocabulary doesn't know.
	// Jobs are indexed against the vocabulary only, so these are looked for in
	// the job text when a job is scored.
	Custom []string
}

// loadFitProfile returns the scoring view of the stored profile, or nil if
// there is no profile.
func (m *Matcher) loadFitProfile(ctx context.Context) (*fitProfile, error) {
	m.mu.Lock()
	if m.cache != nil {
		c := m.cache
		m.mu.Unlock()
		return c, nil
	}
	gen := m.gen
	m.mu.Unlock()

	row, err := m.Store.LoadProfile(ctx)
	if err != nil || row == nil {
		return nil, err
	}
	var p Profile
	if err := json.Unmarshal(row.Data, &p); err != nil {
		return nil, fmt.Errorf("stored profile is unreadable: %w", err)
	}
	fp := &fitProfile{}
	for _, s := range p.Skills {
		fp.Skills = append(fp.Skills, fit.Skill{Name: s.Name, Core: s.Core})
		if _, known := m.Vocab.Lookup(s.Name); !known {
			fp.Custom = append(fp.Custom, s.Name)
		}
	}
	if row.EmbedModel == m.Cfg.EmbedModel {
		if v, err := fit.DecodeVector(row.Embedding); err == nil {
			fp.Vector = v
		}
	}
	m.mu.Lock()
	if m.gen == gen { // not changed while we were reading it
		m.cache = fp
	}
	m.mu.Unlock()
	return fp, nil
}

func unmarshalProfile(data []byte, p *Profile) error {
	if err := json.Unmarshal(data, p); err != nil {
		return fmt.Errorf("stored profile is unreadable: %w", err)
	}
	return nil
}
