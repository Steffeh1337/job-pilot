// Package fit scores how well a job matches a profile. It is pure: no I/O,
// no models. Embeddings come in as vectors, so the same code scores whatever
// model produced them.
package fit

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
)

// Skill is something the profile claims. Core skills are the ones the person
// works with day to day and count for more than skills they merely know.
type Skill struct {
	Name string `json:"name"`
	Core bool   `json:"core"`
}

// Profile is what jobs are scored against.
type Profile struct {
	Skills []Skill
	Vector []float32 // unit length; nil when there is no embedding
}

// Job is the scoring view of one job.
type Job struct {
	Skills      []string // every skill the posting mentions
	TitleSkills []string // the ones in its title or tags: what the role is about
	Vector      []float32
}

// Config tunes the score. Weights are relative; they are normalised over the
// parts that are available.
type Config struct {
	SkillWeight    float64
	SemanticWeight float64
	// SemanticLow and SemanticHigh map a raw cosine similarity onto 0..1.
	// Similarity between any two texts is rarely near 0, and where "related"
	// starts depends on the embedding model, so these are configuration.
	SemanticLow  float64
	SemanticHigh float64
}

func DefaultConfig() Config {
	// Measured, not guessed: with nomic-embed-text, a CV and ~300 real jobs,
	// raw similarity ran 0.62-0.86. Unrelated jobs had a median of 0.71 and
	// engineering jobs 0.79 (p90 0.82). `make calibrate` re-measures this for
	// your own CV and model.
	return Config{SkillWeight: 0.6, SemanticWeight: 0.4, SemanticLow: 0.72, SemanticHigh: 0.82}
}

// Fit is the result for one job.
type Fit struct {
	Score      int      `json:"score"` // 0..100
	Band       string   `json:"band"`  // strong, good, fair, weak
	SkillScore *float64 `json:"skill_score,omitempty"`
	Semantic   *float64 `json:"semantic,omitempty"`   // calibrated 0..1
	Similarity *float64 `json:"similarity,omitempty"` // raw cosine
	Matched    []string `json:"matched"`              // job skills the profile has
	Missing    []string `json:"missing"`              // job skills the profile lacks
}

const (
	coreWeight     = 1.0
	familiarWeight = 0.6
	titleBoost     = 2.0 // a skill in the title counts double
)

// Band names a score.
func Band(score int) string {
	switch {
	case score >= 75:
		return "strong"
	case score >= 55:
		return "good"
	case score >= 35:
		return "fair"
	default:
		return "weak"
	}
}

// Score rates a job. ok is false when there is nothing to go on: the posting
// names no known skill and there is no pair of vectors to compare.
func Score(p Profile, j Job, c Config) (f Fit, ok bool) {
	f.Matched, f.Missing = []string{}, []string{}

	have := make(map[string]float64, len(p.Skills))
	for _, s := range p.Skills {
		w := familiarWeight
		if s.Core {
			w = coreWeight
		}
		if w > have[s.Name] {
			have[s.Name] = w
		}
	}
	inTitle := make(map[string]bool, len(j.TitleSkills))
	for _, s := range j.TitleSkills {
		inTitle[s] = true
	}

	var matchedW, totalW float64
	seen := map[string]bool{}
	for _, name := range j.Skills {
		if seen[name] {
			continue
		}
		seen[name] = true
		w := 1.0
		if inTitle[name] {
			w = titleBoost
		}
		totalW += w
		if pw, has := have[name]; has {
			matchedW += w * pw
			f.Matched = append(f.Matched, name)
		} else {
			f.Missing = append(f.Missing, name)
		}
	}
	sortSkills(f.Matched, have, inTitle)
	sortSkills(f.Missing, nil, inTitle)

	var parts, weights float64
	if totalW > 0 {
		s := matchedW / totalW
		f.SkillScore = &s
		parts += c.SkillWeight * s
		weights += c.SkillWeight
	}
	if len(p.Vector) > 0 && len(p.Vector) == len(j.Vector) {
		cos := Cosine(p.Vector, j.Vector)
		sem := clamp((cos-c.SemanticLow)/(c.SemanticHigh-c.SemanticLow), 0, 1)
		f.Similarity, f.Semantic = &cos, &sem
		parts += c.SemanticWeight * sem
		weights += c.SemanticWeight
	}
	if weights == 0 {
		return f, false
	}
	f.Score = int(math.Round(100 * parts / weights))
	f.Band = Band(f.Score)
	return f, true
}

// sortSkills orders skills most relevant first: title skills, then core ones
// the profile has, then alphabetically, so the output is stable.
func sortSkills(names []string, have map[string]float64, inTitle map[string]bool) {
	sort.SliceStable(names, func(i, j int) bool {
		a, b := names[i], names[j]
		if inTitle[a] != inTitle[b] {
			return inTitle[a]
		}
		if have != nil && have[a] != have[b] {
			return have[a] > have[b]
		}
		return a < b
	})
}

func clamp(x, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, x)) }

// Normalize scales v to unit length so that a dot product is a cosine. The
// zero vector is returned unchanged.
func Normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return v
	}
	n := float32(math.Sqrt(sum))
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x / n
	}
	return out
}

// Cosine is the dot product of two unit vectors; 0 if their sizes differ.
func Cosine(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	return dot
}

// EncodeVector stores a vector as little-endian float32s.
func EncodeVector(v []float32) []byte {
	if len(v) == 0 {
		return nil
	}
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return b
}

// DecodeVector is the inverse of EncodeVector.
func DecodeVector(b []byte) ([]float32, error) {
	if len(b)%4 != 0 {
		return nil, fmt.Errorf("vector has %d bytes, not a multiple of 4", len(b))
	}
	if len(b) == 0 {
		return nil, nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v, nil
}
