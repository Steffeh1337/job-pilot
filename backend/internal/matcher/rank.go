package matcher

import (
	"context"
	"sort"

	"github.com/Steffeh1337/job-pilot/backend/internal/fit"
	"github.com/Steffeh1337/job-pilot/backend/internal/skills"
	"github.com/Steffeh1337/job-pilot/backend/internal/store"
)

type customFound struct{ all, title []string }

// customHits looks for the user's own skills (the ones outside the
// vocabulary) in the text of the given jobs. Nothing is stored: it is cheap
// and it keeps the result correct the moment the skills are edited.
func (m *Matcher) customHits(ctx context.Context, custom []string, feats map[int64]store.JobFeatures) (map[int64]customFound, error) {
	if len(custom) == 0 || len(feats) == 0 {
		return nil, nil
	}
	ids := make([]int64, 0, len(feats))
	for id := range feats {
		ids = append(ids, id)
	}
	jobs, err := m.Store.JobsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	finder := skills.NewMentioner(custom)
	out := make(map[int64]customFound, len(jobs))
	for _, j := range jobs {
		all := finder.In(skillText(j))
		if len(all) == 0 {
			continue
		}
		out[j.ID] = customFound{all: all, title: finder.In(j.Title)}
	}
	return out, nil
}

// Fits scores the given jobs against the profile. Jobs that can't be scored
// (not indexed yet, or no known skills and no embedding) are absent. With no
// profile the result is nil.
func (m *Matcher) Fits(ctx context.Context, ids []int64) (map[int64]fit.Fit, error) {
	prof, err := m.loadFitProfile(ctx)
	if err != nil || prof == nil {
		return nil, err
	}
	feats, err := m.Store.JobFeaturesFor(ctx, ids)
	if err != nil {
		return nil, err
	}
	extra, err := m.customHits(ctx, prof.Custom, feats)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]fit.Fit, len(feats))
	for id, f := range feats {
		job := fit.Job{Skills: f.Skills, TitleSkills: f.TitleSkills}
		if c, ok := extra[id]; ok {
			job.Skills = append(append([]string(nil), f.Skills...), c.all...)
			job.TitleSkills = append(append([]string(nil), f.TitleSkills...), c.title...)
		}
		if f.EmbedModel == m.Cfg.EmbedModel {
			if v, err := fit.DecodeVector(f.Embedding); err == nil {
				job.Vector = v
			}
		}
		if res, ok := fit.Score(prof.Profile, job, m.Cfg.Fit); ok {
			out[id] = res
		}
	}
	return out, nil
}

// Rank orders jobs from best to worst fit. Jobs that can't be scored come last
// in their original order, and are dropped if minFit is set. With no profile
// the order is unchanged.
func (m *Matcher) Rank(ctx context.Context, ids []int64, minFit int) ([]int64, map[int64]fit.Fit, error) {
	fits, err := m.Fits(ctx, ids)
	if err != nil || fits == nil {
		return ids, fits, err
	}
	ranked := make([]int64, 0, len(ids))
	for _, id := range ids {
		if f, ok := fits[id]; ok {
			if f.Score >= minFit {
				ranked = append(ranked, id)
			}
		} else if minFit <= 0 {
			ranked = append(ranked, id)
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		a, aok := fits[ranked[i]]
		b, bok := fits[ranked[j]]
		switch {
		case aok != bok:
			return aok
		case !aok:
			return false
		default:
			return a.Score > b.Score
		}
	})
	return ranked, fits, nil
}
