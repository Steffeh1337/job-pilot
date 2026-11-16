package fit

import (
	"math"
	"reflect"
	"testing"
)

func approx(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Errorf("%s = nil, want %.3f", name, want)
		return
	}
	if math.Abs(*got-want) > 1e-6 {
		t.Errorf("%s = %.4f, want %.4f", name, *got, want)
	}
}

func profile(core []string, familiar ...string) Profile {
	var p Profile
	for _, n := range core {
		p.Skills = append(p.Skills, Skill{Name: n, Core: true})
	}
	for _, n := range familiar {
		p.Skills = append(p.Skills, Skill{Name: n})
	}
	return p
}

func TestSkillScoreIsWeightedCoverage(t *testing.T) {
	p := profile([]string{"Go", "PostgreSQL"}, "Docker")
	j := Job{Skills: []string{"Go", "PostgreSQL", "Docker", "Kafka"}}
	f, ok := Score(p, j, DefaultConfig())
	if !ok {
		t.Fatal("expected a score")
	}
	// Go 1.0 + PostgreSQL 1.0 + Docker 0.6 of 4 job skills.
	approx(t, "skill score", f.SkillScore, 2.6/4)
	if f.Semantic != nil {
		t.Error("no vectors, so no semantic part")
	}
	if f.Score != 65 || f.Band != "good" {
		t.Errorf("score = %d (%s), want 65 (good)", f.Score, f.Band)
	}
	if !reflect.DeepEqual(f.Matched, []string{"Go", "PostgreSQL", "Docker"}) {
		t.Errorf("matched = %v", f.Matched)
	}
	if !reflect.DeepEqual(f.Missing, []string{"Kafka"}) {
		t.Errorf("missing = %v", f.Missing)
	}
}

func TestTitleSkillsCountDouble(t *testing.T) {
	p := profile([]string{"Go"})
	j := Job{Skills: []string{"Go", "Rust"}, TitleSkills: []string{"Go"}}
	f, _ := Score(p, j, DefaultConfig())
	approx(t, "skill score", f.SkillScore, 2.0/3) // Go counts 2 of 3

	flipped := Job{Skills: []string{"Go", "Rust"}, TitleSkills: []string{"Rust"}}
	g, _ := Score(p, flipped, DefaultConfig())
	approx(t, "skill score when the title wants Rust", g.SkillScore, 1.0/3)
	if g.Score >= f.Score {
		t.Errorf("a job whose title wants a skill you lack must score lower: %d vs %d", g.Score, f.Score)
	}
}

func TestDuplicateJobSkillsCountOnce(t *testing.T) {
	f, _ := Score(profile([]string{"Go"}), Job{Skills: []string{"Go", "Go", "Rust", "Rust"}}, DefaultConfig())
	approx(t, "skill score", f.SkillScore, 0.5)
	if len(f.Matched) != 1 || len(f.Missing) != 1 {
		t.Errorf("matched %v, missing %v", f.Matched, f.Missing)
	}
}

func TestSemanticCalibration(t *testing.T) {
	c := Config{SkillWeight: 0.6, SemanticWeight: 0.4, SemanticLow: 0.4, SemanticHigh: 0.8}
	a := Normalize([]float32{1, 0})
	for _, tt := range []struct {
		name string
		b    []float32
		want float64
		cos  float64
	}{
		{"identical", []float32{1, 0}, 1, 1},
		{"orthogonal", []float32{0, 1}, 0, 0},
		{"opposite", []float32{-1, 0}, 0, -1},
		{"in the middle", []float32{0.6, 0.8}, 0.5, 0.6}, // cos 0.6: halfway from 0.4 to 0.8
	} {
		f, ok := Score(Profile{Vector: a}, Job{Vector: Normalize(tt.b)}, c)
		if !ok {
			t.Fatalf("%s: not scorable", tt.name)
		}
		approx(t, tt.name+" semantic", f.Semantic, tt.want)
		approx(t, tt.name+" similarity", f.Similarity, tt.cos)
	}
}

func TestSemanticOnlyAndSkillsOnly(t *testing.T) {
	c := DefaultConfig()
	v := Normalize([]float32{1, 1})

	semantic, ok := Score(Profile{Vector: v}, Job{Vector: v}, c)
	if !ok || semantic.SkillScore != nil || semantic.Score != 100 {
		t.Errorf("semantic only = %+v, %v", semantic, ok)
	}
	skills, ok := Score(profile([]string{"Go"}), Job{Skills: []string{"Go"}}, c)
	if !ok || skills.Semantic != nil || skills.Score != 100 {
		t.Errorf("skills only = %+v, %v", skills, ok)
	}
}

func TestCombinedScoreUsesBothParts(t *testing.T) {
	c := DefaultConfig() // 0.6 skills, 0.4 semantic
	v := Normalize([]float32{1, 0})
	p := Profile{Skills: []Skill{{Name: "Go", Core: true}}, Vector: v}
	j := Job{Skills: []string{"Go", "Rust"}, Vector: Normalize([]float32{0, 1})}
	f, _ := Score(p, j, c)
	// skill 0.5, semantic 0 -> (0.6*0.5 + 0.4*0) / 1.0
	if f.Score != 30 {
		t.Errorf("score = %d, want 30", f.Score)
	}
}

func TestVectorsOfDifferentSizesAreIgnored(t *testing.T) {
	p := Profile{Skills: []Skill{{Name: "Go", Core: true}}, Vector: []float32{1, 0, 0}}
	f, ok := Score(p, Job{Skills: []string{"Go"}, Vector: []float32{1, 0}}, DefaultConfig())
	if !ok || f.Semantic != nil {
		t.Errorf("mismatched vectors should be skipped: %+v, %v", f, ok)
	}
}

func TestNothingToScore(t *testing.T) {
	if _, ok := Score(profile([]string{"Go"}), Job{}, DefaultConfig()); ok {
		t.Error("a job with no known skills and no vector is not scorable")
	}
	if _, ok := Score(Profile{}, Job{Vector: []float32{1}}, DefaultConfig()); ok {
		t.Error("a profile with no vector can't be compared on meaning")
	}
}

func TestProfileWithoutSkillsScoresZeroOnSkills(t *testing.T) {
	f, ok := Score(Profile{}, Job{Skills: []string{"Go", "Rust"}}, DefaultConfig())
	if !ok || f.Score != 0 || f.Band != "weak" || len(f.Missing) != 2 {
		t.Errorf("f = %+v, ok = %v", f, ok)
	}
}

func TestOutputOrderIsStableAndRelevantFirst(t *testing.T) {
	p := profile([]string{"Go"}, "Docker", "Redis")
	j := Job{Skills: []string{"Redis", "Docker", "Go", "Zig", "Ada", "Rust"}, TitleSkills: []string{"Rust"}}
	f, _ := Score(p, j, DefaultConfig())
	if !reflect.DeepEqual(f.Matched, []string{"Go", "Docker", "Redis"}) {
		t.Errorf("matched = %v: core first, then alphabetical", f.Matched)
	}
	if !reflect.DeepEqual(f.Missing, []string{"Rust", "Ada", "Zig"}) {
		t.Errorf("missing = %v: title skills first, then alphabetical", f.Missing)
	}
}

func TestBands(t *testing.T) {
	for score, want := range map[int]string{100: "strong", 75: "strong", 74: "good", 55: "good", 54: "fair", 35: "fair", 34: "weak", 0: "weak"} {
		if got := Band(score); got != want {
			t.Errorf("Band(%d) = %s, want %s", score, got, want)
		}
	}
}

func TestNormalizeAndCosine(t *testing.T) {
	v := Normalize([]float32{3, 4})
	if math.Abs(Cosine(v, v)-1) > 1e-6 {
		t.Errorf("a unit vector has cosine 1 with itself, got %v", Cosine(v, v))
	}
	if z := Normalize([]float32{0, 0}); z[0] != 0 || z[1] != 0 {
		t.Errorf("zero vector = %v", z)
	}
	if Cosine([]float32{1}, []float32{1, 2}) != 0 {
		t.Error("different sizes should give 0")
	}
}

func TestVectorRoundTrip(t *testing.T) {
	in := []float32{0, 1.5, -2.25, 1e-7, float32(math.MaxFloat32)}
	out, err := DecodeVector(EncodeVector(in))
	if err != nil || !reflect.DeepEqual(in, out) {
		t.Errorf("round trip = %v, %v", out, err)
	}
	if EncodeVector(nil) != nil {
		t.Error("empty vector should encode to nil")
	}
	if v, err := DecodeVector(nil); v != nil || err != nil {
		t.Errorf("DecodeVector(nil) = %v, %v", v, err)
	}
	if _, err := DecodeVector([]byte{1, 2, 3}); err == nil {
		t.Error("a truncated vector should be an error")
	}
}
