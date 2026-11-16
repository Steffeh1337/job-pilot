package matcher

import (
	"context"
	"sort"

	"github.com/Steffeh1337/job-pilot/backend/internal/fit"
)

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
	out := make(map[int64]fit.Fit, len(feats))
	for id, f := range feats {
		job := fit.Job{Skills: f.Skills, TitleSkills: f.TitleSkills}
		if f.EmbedModel == m.Cfg.EmbedModel {
			if v, err := fit.DecodeVector(f.Embedding); err == nil {
				job.Vector = v
			}
		}
		if res, ok := fit.Score(*prof, job, m.Cfg.Fit); ok {
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
