package matcher

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/Steffeh1337/job-pilot/backend/internal/fit"
)

// Calibration describes how similar your profile is to the indexed jobs, and
// the fit.semantic_low and fit.semantic_high that follow from it.
type Calibration struct {
	Model  string
	Jobs   int
	Min    float64
	P25    float64
	Median float64
	P75    float64
	P95    float64
	Max    float64
	// SuggestedLow is where "related" starts: the bottom quarter of jobs fall
	// at or below it and score 0% on wording. SuggestedHigh is where "very
	// close" starts: only the top 5% reach it and score 100%.
	SuggestedLow  float64
	SuggestedHigh float64
}

// minCalibrationJobs is how few jobs make a percentile meaningless.
const minCalibrationJobs = 20

// Calibrate measures the raw similarity between the profile and every indexed
// job. Raw similarity is never near zero, and where it starts to mean
// something depends on the embedding model and on the CV, so the scale is
// worth measuring rather than guessing.
func (m *Matcher) Calibrate(ctx context.Context) (*Calibration, error) {
	row, err := m.Store.LoadProfile(ctx)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, ErrNoProfile
	}
	prof, err := m.loadFitProfile(ctx)
	if err != nil {
		return nil, err
	}
	if prof == nil || len(prof.Vector) == 0 {
		return nil, errors.New("your profile has not been embedded yet: start Ollama, then re-index jobs from the Profile page")
	}
	blobs, err := m.Store.JobEmbeddings(ctx, m.Cfg.MinStage, m.Cfg.EmbedModel)
	if err != nil {
		return nil, err
	}
	sims := make([]float64, 0, len(blobs))
	for _, b := range blobs {
		if v, err := fit.DecodeVector(b); err == nil && len(v) == len(prof.Vector) {
			sims = append(sims, fit.Cosine(prof.Vector, v))
		}
	}
	if len(sims) < minCalibrationJobs {
		return nil, fmt.Errorf("only %d jobs are indexed with %s; at least %d are needed to calibrate", len(sims), m.Cfg.EmbedModel, minCalibrationJobs)
	}
	sort.Float64s(sims)

	c := &Calibration{
		Model: m.Cfg.EmbedModel, Jobs: len(sims), Min: sims[0], Max: sims[len(sims)-1],
		P25: percentile(sims, 0.25), Median: percentile(sims, 0.5), P75: percentile(sims, 0.75), P95: percentile(sims, 0.95),
	}
	c.SuggestedLow, c.SuggestedHigh = round2(c.P25), round2(c.P95)
	if c.SuggestedHigh-c.SuggestedLow < 0.05 { // a degenerate spread would divide by almost nothing
		c.SuggestedHigh = round2(c.SuggestedLow + 0.05)
	}
	return c, nil
}

// percentile returns the p-quantile (0..1) of an ascending slice.
func percentile(sorted []float64, p float64) float64 {
	return sorted[int(p*float64(len(sorted)-1))]
}

func round2(x float64) float64 { return math.Round(x*100) / 100 }
