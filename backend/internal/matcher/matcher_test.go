package matcher

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Steffeh1337/job-pilot/backend/internal/domain"
	"github.com/Steffeh1337/job-pilot/backend/internal/ollama"
	"github.com/Steffeh1337/job-pilot/backend/internal/skills"
	"github.com/Steffeh1337/job-pilot/backend/internal/store"
)

const sampleCV = `Jane Developer
jane@example.com
Senior Backend Engineer

Experience
8 years building distributed systems. Led the migration to Go microservices on Kubernetes.
Stack: Go, TypeScript, PostgreSQL, Docker, Kubernetes, React. Go, Docker and Kubernetes daily.
Languages: English, Romanian.
Also used Temporal for workflow orchestration.`

// fakeLLM stands in for Ollama. Embeddings are keyword counts, so texts about
// the same things are close and unrelated ones are not.
type fakeLLM struct {
	mu      sync.Mutex
	models  []ollama.Model
	down    error
	chat    any // an llmProfile, or a raw string that is returned as-is
	chatErr error

	embedCalls [][]string
	chatCalls  int
	onEmbed    func() // runs in the middle of an Embed call
}

func (f *fakeLLM) Version(context.Context) (string, error) {
	if f.down != nil {
		return "", f.down
	}
	return "0.9.0", nil
}

func (f *fakeLLM) Models(context.Context) ([]ollama.Model, error) {
	if f.down != nil {
		return nil, f.down
	}
	return f.models, nil
}

var embedKeywords = []string{"go", "kubernetes", "react", "figma", "python", "design", "backend"}

func kwVector(text string) []float32 {
	t := strings.ToLower(text)
	v := make([]float32, len(embedKeywords))
	for i, k := range embedKeywords {
		v[i] = 0.01 + float32(strings.Count(t, k))
	}
	return v
}

func (f *fakeLLM) Embed(_ context.Context, _ string, inputs []string) ([][]float32, error) {
	f.mu.Lock()
	f.embedCalls = append(f.embedCalls, inputs)
	down, hook := f.down, f.onEmbed
	f.mu.Unlock()
	if down != nil {
		return nil, down
	}
	if hook != nil {
		hook()
	}
	out := make([][]float32, len(inputs))
	for i, in := range inputs {
		out[i] = kwVector(in)
	}
	return out, nil
}

func (f *fakeLLM) ChatJSON(_ context.Context, _, _, _ string, _ any, out any) error {
	f.mu.Lock()
	f.chatCalls++
	f.mu.Unlock()
	if f.down != nil {
		return f.down
	}
	if f.chatErr != nil {
		return f.chatErr
	}
	if s, ok := f.chat.(string); ok {
		return json.Unmarshal([]byte(s), out)
	}
	b, _ := json.Marshal(f.chat)
	return json.Unmarshal(b, out)
}

var chatAndEmbed = []ollama.Model{{Name: "qwen2.5:7b"}, {Name: "nomic-embed-text:latest"}}

func newMatcher(t *testing.T, llm LLM) (*Matcher, *store.Store) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	m := &Matcher{
		Store: st, LLM: llm, Vocab: skills.Default(), Cfg: DefaultConfig(),
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now: func() time.Time { return time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC) },
	}
	m.Cfg.EmbedPrefix = "" // keep keyword vectors simple unless a test sets one
	m.disableAutoSync = true
	return m, st
}

var jobSeq int

func addJob(t *testing.T, st *store.Store, title, desc string, stage domain.Stage, tags ...string) int64 {
	t.Helper()
	jobSeq++
	j := &domain.Job{
		Source: "test", ExternalID: strings.Repeat("x", jobSeq), Title: title, Company: "Co", Description: desc, Tags: tags,
		Stage: stage, FirstSeenAt: time.Now(), LastSeenAt: time.Now(), PostedAt: time.Now(),
	}
	if err := st.Insert(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	return j.ID
}

func skillNamesOf(p Profile) []string {
	var out []string
	for _, s := range p.Skills {
		out = append(out, s.Name)
	}
	return out
}

func hasSkill(p Profile, name string) (Skill, bool) {
	for _, s := range p.Skills {
		if s.Name == name {
			return s, true
		}
	}
	return Skill{}, false
}

func TestUploadWithKeywordsOnly(t *testing.T) {
	m, _ := newMatcher(t, nil)
	view, warnings, err := m.Upload(context.Background(), "../../Jane CV.txt", []byte(sampleCV))
	if err != nil {
		t.Fatal(err)
	}
	if !view.Exists || view.ExtractedBy != "keywords" || view.CVFilename != "Jane CV.txt" {
		t.Errorf("view = %+v", view)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "keywords only") {
		t.Errorf("warnings = %v", warnings)
	}
	for _, name := range []string{"Go", "TypeScript", "PostgreSQL", "Docker", "Kubernetes", "React"} {
		if _, ok := hasSkill(view.Profile, name); !ok {
			t.Errorf("skill %s not found in %v", name, skillNamesOf(view.Profile))
		}
	}
	for _, name := range []string{"Go", "Docker", "Kubernetes"} { // mentioned more than once
		if s, _ := hasSkill(view.Profile, name); !s.Core {
			t.Errorf("%s should be core", name)
		}
	}
	if s, _ := hasSkill(view.Profile, "React"); s.Core {
		t.Error("React is mentioned once, so not core")
	}
	if view.YearsExperience != 8 || view.Seniority != "senior" {
		t.Errorf("years %d, seniority %s", view.YearsExperience, view.Seniority)
	}
	if view.CVChars == 0 || !strings.Contains(view.CVPreview, "Senior Backend Engineer") {
		t.Errorf("cv meta: %d chars, preview %q", view.CVChars, view.CVPreview)
	}
	if view.Headline != "" || view.Summary != "" {
		t.Error("keywords can't write a headline or summary")
	}
	// Core skills come first.
	if !view.Skills[0].Core {
		t.Errorf("core skills should sort first: %v", skillNamesOf(view.Profile))
	}
}

func TestUploadWithModelKeepsOnlyGroundedClaims(t *testing.T) {
	llm := &fakeLLM{models: chatAndEmbed, chat: llmProfile{
		Headline: "Senior Backend Engineer", Seniority: "senior", YearsExperience: 9,
		// Haskell and Terraform are not in the CV; Leadership and Go are not custom skills.
		Skills:    []string{"Go", "Kubernetes", "Haskell", "Terraform", "Temporal", "Leadership"},
		Languages: []string{"English", "romanian", "French"},
		Summary:   "Backend engineer. Contact jane@example.com",
	}}
	m, _ := newMatcher(t, llm)
	view, warnings, err := m.Upload(context.Background(), "cv.txt", []byte(sampleCV))
	if err != nil || len(warnings) != 0 {
		t.Fatalf("err %v, warnings %v", err, warnings)
	}
	if view.ExtractedBy != "llm:qwen2.5:7b" || view.Headline != "Senior Backend Engineer" || view.YearsExperience != 9 {
		t.Errorf("view = %+v", view)
	}
	names := skillNamesOf(view.Profile)
	for _, bad := range []string{"Haskell", "Terraform", "Leadership"} {
		if _, ok := hasSkill(view.Profile, bad); ok {
			t.Errorf("%s was not in the CV but made it into the profile: %v", bad, names)
		}
	}
	if _, ok := hasSkill(view.Profile, "Temporal"); !ok {
		t.Errorf("a custom skill that is in the CV should be kept: %v", names)
	}
	if got := strings.Join(view.Languages, ","); got != "English,Romanian" {
		t.Errorf("languages = %q, want only the ones the CV mentions, capitalised", got)
	}
	if view.Summary != "" {
		t.Errorf("a summary with an email address must be dropped, got %q", view.Summary)
	}
}

func TestVocabularySkillsComeFromTheCVNotTheModel(t *testing.T) {
	llm := &fakeLLM{models: chatAndEmbed, chat: llmProfile{Seniority: "senior", Skills: []string{"Rust", "Python"}}}
	m, _ := newMatcher(t, llm)
	view, _, _ := m.Upload(context.Background(), "cv.txt", []byte(sampleCV))
	for _, bad := range []string{"Rust", "Python"} {
		if _, ok := hasSkill(view.Profile, bad); ok {
			t.Errorf("%s is a vocabulary skill the CV never mentions; the model must not add it", bad)
		}
	}
}

func TestModelOutputIsValidated(t *testing.T) {
	llm := &fakeLLM{models: chatAndEmbed, chat: llmProfile{
		Headline: strings.Repeat("x", 200), Seniority: "wizard", YearsExperience: 300,
	}}
	m, _ := newMatcher(t, llm)
	view, _, err := m.Upload(context.Background(), "cv.txt", []byte(sampleCV))
	if err != nil {
		t.Fatal(err)
	}
	if view.Headline != "" || view.Seniority != "senior" || view.YearsExperience != 8 {
		t.Errorf("nonsense must fall back to the keyword values: %+v", view.Profile)
	}
}

func TestUploadFallsBackWhenOllamaIsDown(t *testing.T) {
	m, _ := newMatcher(t, &fakeLLM{down: &ollama.UnavailableError{Err: errors.New("connection refused")}})
	view, warnings, err := m.Upload(context.Background(), "cv.txt", []byte(sampleCV))
	if err != nil {
		t.Fatalf("a missing model must not block the upload: %v", err)
	}
	if view.ExtractedBy != "keywords" || len(view.Skills) == 0 {
		t.Errorf("view = %+v", view)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "not running") {
		t.Errorf("warnings = %v", warnings)
	}
}

func TestUploadWithoutAChatModel(t *testing.T) {
	m, _ := newMatcher(t, &fakeLLM{models: []ollama.Model{{Name: "nomic-embed-text:latest"}}})
	_, warnings, err := m.Upload(context.Background(), "cv.txt", []byte(sampleCV))
	if err != nil || len(warnings) != 1 || !strings.Contains(warnings[0], "ollama pull") {
		t.Errorf("err %v, warnings %v; want a hint to pull a model", err, warnings)
	}
}

func TestUploadWhenTheModelReturnsGarbage(t *testing.T) {
	m, _ := newMatcher(t, &fakeLLM{models: chatAndEmbed, chat: "not json at all"})
	view, warnings, err := m.Upload(context.Background(), "cv.txt", []byte(sampleCV))
	if err != nil || view.ExtractedBy != "keywords" || len(warnings) != 1 {
		t.Errorf("view %+v, warnings %v, err %v", view, warnings, err)
	}
}

func TestUploadRejectsFilesThatAreNotCVs(t *testing.T) {
	m, st := newMatcher(t, nil)
	for name, data := range map[string][]byte{
		"binary": append([]byte{0x89, 'P', 'N', 'G', 0}, make([]byte, 64)...),
		"empty":  nil,
		"tiny":   []byte("hi"),
	} {
		_, _, err := m.Upload(context.Background(), name, data)
		var bad *InvalidCVError
		if !errors.As(err, &bad) {
			t.Errorf("%s: err = %v, want InvalidCVError", name, err)
		}
	}
	if p, _ := st.LoadProfile(context.Background()); p != nil {
		t.Error("a rejected upload must not leave a profile behind")
	}
}

func TestUpdateProfile(t *testing.T) {
	m, _ := newMatcher(t, nil)
	ctx := context.Background()
	if _, err := m.UpdateProfile(ctx, Profile{}); !errors.Is(err, ErrNoProfile) {
		t.Fatalf("err = %v, want ErrNoProfile", err)
	}
	m.Upload(ctx, "cv.txt", []byte(sampleCV))

	view, err := m.UpdateProfile(ctx, Profile{
		Headline: "  Lead engineer ", Seniority: "STAFF", YearsExperience: 12,
		Skills: []Skill{
			{Name: "golang", Core: false}, {Name: "Go", Core: true}, {Name: "  "}, {Name: "Temporal", Core: false},
			{Name: "k8s", Core: true}, {Name: "FooBar"},
		},
		Languages: []string{"English", " english ", ""},
		Summary:   "  Writes Go.  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.Headline != "Lead engineer" || view.Seniority != "staff" || view.YearsExperience != 12 || view.Summary != "Writes Go." {
		t.Errorf("view = %+v", view.Profile)
	}
	if got := strings.Join(skillNamesOf(view.Profile), ","); got != "Go,Kubernetes,Temporal,FooBar" {
		t.Errorf("skills = %s; want aliases renamed, duplicates merged (core wins), blanks dropped, core first", got)
	}
	if s, _ := hasSkill(view.Profile, "Go"); !s.Core || s.Category != "Languages" {
		t.Errorf("Go = %+v", s)
	}
	if len(view.Languages) != 1 {
		t.Errorf("languages = %v", view.Languages)
	}
	// The CV itself survives editing.
	if view.CVChars == 0 {
		t.Error("the CV text must be kept")
	}
}

func TestUpdateProfileValidation(t *testing.T) {
	m, _ := newMatcher(t, nil)
	ctx := context.Background()
	m.Upload(ctx, "cv.txt", []byte(sampleCV))
	tooMany := make([]Skill, 201)
	for i := range tooMany {
		tooMany[i] = Skill{Name: strings.Repeat("a", i%5+1) + string(rune('a'+i%26))}
	}
	for name, p := range map[string]Profile{
		"seniority":     {Seniority: "wizard"},
		"negative":      {YearsExperience: -1},
		"too many":      {YearsExperience: 61},
		"long headline": {Headline: strings.Repeat("h", 121)},
		"long summary":  {Summary: strings.Repeat("s", 1501)},
		"long skill":    {Skills: []Skill{{Name: strings.Repeat("s", 51)}}},
		"many skills":   {Skills: tooMany},
		"many langs":    {Languages: strings.Split(strings.Repeat("l,", 13), ",")},
	} {
		_, err := m.UpdateProfile(ctx, p)
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("%s: err = %v, want ValidationError", name, err)
		}
	}
}

func TestEditingClearsTheEmbedding(t *testing.T) {
	llm := &fakeLLM{models: chatAndEmbed, chat: llmProfile{}}
	m, _ := newMatcher(t, llm)
	ctx := context.Background()
	m.Upload(ctx, "cv.txt", []byte(sampleCV))
	m.Sync(ctx)
	if v, _ := m.Profile(ctx); !v.HasEmbedding {
		t.Fatal("expected the profile to be embedded after a sync")
	}
	view, _ := m.UpdateProfile(ctx, Profile{Seniority: "senior", Skills: []Skill{{Name: "Go", Core: true}}})
	if view.HasEmbedding {
		t.Error("the embedding describes the old profile and must be dropped")
	}
}

func TestDeleteProfile(t *testing.T) {
	m, st := newMatcher(t, nil)
	ctx := context.Background()
	m.Upload(ctx, "cv.txt", []byte(sampleCV))
	id := addJob(t, st, "Go dev", "Go", domain.StageSenior)
	m.Sync(ctx)
	if fits, _ := m.Fits(ctx, []int64{id}); len(fits) != 1 {
		t.Fatal("expected a fit while the profile exists")
	}

	if err := m.DeleteProfile(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := m.Profile(ctx); v.Exists || len(v.Skills) != 0 {
		t.Errorf("profile still exists: %+v", v)
	}
	if fits, err := m.Fits(ctx, []int64{id}); fits != nil || err != nil {
		t.Errorf("no profile means no fits (and the cache must not serve a stale one): %v, %v", fits, err)
	}
	if row, _ := st.LoadProfile(ctx); row != nil {
		t.Error("the CV text must be deleted from the database")
	}
	if err := m.DeleteProfile(ctx); err != nil {
		t.Errorf("deleting twice should be fine: %v", err)
	}
}

func TestReextract(t *testing.T) {
	ctx := context.Background()
	llm := &fakeLLM{down: &ollama.UnavailableError{Err: errors.New("refused")}}
	m, _ := newMatcher(t, llm)
	if _, _, err := m.Reextract(ctx); !errors.Is(err, ErrNoProfile) {
		t.Fatalf("err = %v", err)
	}
	m.Upload(ctx, "cv.txt", []byte(sampleCV))
	m.UpdateProfile(ctx, Profile{Seniority: "principal", Skills: []Skill{{Name: "Zig", Core: true}}})

	// Ollama is now running: re-reading the CV replaces the hand edits.
	llm.mu.Lock()
	llm.down, llm.models, llm.chat = nil, chatAndEmbed, llmProfile{Headline: "Backend engineer", Seniority: "senior"}
	llm.mu.Unlock()
	view, warnings, err := m.Reextract(ctx)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("err %v, warnings %v", err, warnings)
	}
	if view.ExtractedBy != "llm:qwen2.5:7b" || view.Headline != "Backend engineer" {
		t.Errorf("view = %+v", view)
	}
	if _, ok := hasSkill(view.Profile, "Zig"); ok {
		t.Error("re-reading should discard hand-added skills")
	}
}

func TestSyncIndexesSkillsWithoutOllama(t *testing.T) {
	m, st := newMatcher(t, nil)
	ctx := context.Background()
	a := addJob(t, st, "Senior Go Engineer", "We use Go, PostgreSQL and Docker.", domain.StageSenior, "backend")
	addJob(t, st, "Not eligible", "Go", domain.StageUnique)

	rep, err := m.Sync(ctx)
	if err != nil || rep.SkillsIndexed != 1 || rep.JobsEmbedded != 0 {
		t.Fatalf("rep = %+v, err = %v", rep, err)
	}
	f, _ := st.JobFeaturesFor(ctx, []int64{a})
	got := f[a]
	if strings.Join(got.Skills, ",") != "Go,Docker,PostgreSQL" && len(got.Skills) != 3 {
		t.Errorf("skills = %v", got.Skills)
	}
	if len(got.TitleSkills) != 1 || got.TitleSkills[0] != "Go" {
		t.Errorf("title skills = %v, want only Go (from the title)", got.TitleSkills)
	}
	if rep, _ := m.Sync(ctx); rep.SkillsIndexed != 0 {
		t.Errorf("a second pass should find nothing to do, got %+v", rep)
	}
}

func TestTagsAreNotEvidenceOnTheirOwn(t *testing.T) {
	m, st := newMatcher(t, nil)
	ctx := context.Background()
	long := strings.Repeat("We are hiring a coordinator to manage recruiting pipelines and hiring plans. ", 8)
	stuffed := addJob(t, st, "Recruiting Coordinator", long, domain.StageSenior, "coordinator", "golang", "kubernetes")
	real := addJob(t, st, "Backend Engineer", long+" You will write Go services.", domain.StageSenior, "backend")
	thin := addJob(t, st, "Backend Engineer", "Join us.", domain.StageSenior, "golang", "docker")
	m.Sync(ctx)

	feats, _ := st.JobFeaturesFor(ctx, []int64{stuffed, real, thin})
	if got := feats[stuffed].Skills; len(got) != 0 {
		t.Errorf("tags on a job with a real description must not add skills, got %v", got)
	}
	if got := feats[real].Skills; len(got) != 1 || got[0] != "Go" {
		t.Errorf("skills named in the text are found: %v", got)
	}
	got := strings.Join(feats[thin].Skills, ",")
	if !strings.Contains(got, "Go") || !strings.Contains(got, "Docker") {
		t.Errorf("with almost no text, tags fill in: %v", feats[thin].Skills)
	}
	if len(feats[thin].TitleSkills) != 0 {
		t.Errorf("tags never count as title skills: %v", feats[thin].TitleSkills)
	}
}

func TestChangingTheExtractionLogicReindexes(t *testing.T) {
	m, st := newMatcher(t, nil)
	ctx := context.Background()
	id := addJob(t, st, "Go dev", "Backend role using Go", domain.StageSenior)
	m.Sync(ctx)
	// Jobs indexed by an older version of the logic are stale.
	st.SaveJobSkills(ctx, []store.JobSkills{{JobID: id, Skills: []string{"Stale"}, VocabVersion: m.Vocab.Version() + ".1"}})
	if rep, _ := m.Sync(ctx); rep.SkillsIndexed != 1 {
		t.Fatalf("rep = %+v", rep)
	}
	if f, _ := st.JobFeaturesFor(ctx, []int64{id}); len(f[id].Skills) != 1 || f[id].Skills[0] != "Go" {
		t.Errorf("skills = %v", f[id].Skills)
	}
}

func TestSyncReindexesWhenTheVocabularyChanges(t *testing.T) {
	m, st := newMatcher(t, nil)
	ctx := context.Background()
	id := addJob(t, st, "Dev", "We use COBOL and Go.", domain.StageSenior)
	m.Sync(ctx)
	if f, _ := st.JobFeaturesFor(ctx, []int64{id}); strings.Contains(strings.Join(f[id].Skills, ","), "COBOL") {
		t.Fatal("the default vocabulary doesn't know COBOL")
	}

	v, err := skills.Parse("[X]\nGo | ^Go\nCOBOL | cobol\n")
	if err != nil {
		t.Fatal(err)
	}
	m.Vocab = v
	rep, _ := m.Sync(ctx)
	if rep.SkillsIndexed != 1 {
		t.Errorf("a new vocabulary should reindex, got %+v", rep)
	}
	if f, _ := st.JobFeaturesFor(ctx, []int64{id}); !strings.Contains(strings.Join(f[id].Skills, ","), "COBOL") {
		t.Errorf("skills = %v", f[id].Skills)
	}
}

func TestSyncEmbedsTheProfileAndJobs(t *testing.T) {
	llm := &fakeLLM{models: chatAndEmbed, chat: llmProfile{}}
	m, st := newMatcher(t, llm)
	m.Cfg.EmbedPrefix = "clustering: "
	m.Cfg.BatchSize = 2
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		addJob(t, st, "Go engineer", "Go and Kubernetes", domain.StageSenior)
	}
	addJob(t, st, "Too early", "Go", domain.StageUnique)

	// With no profile nothing is embedded: there is nothing to compare against.
	if rep, _ := m.Sync(ctx); rep.JobsEmbedded != 0 || len(llm.embedCalls) != 0 {
		t.Fatalf("embedded without a profile: %+v, %d calls", rep, len(llm.embedCalls))
	}

	m.Upload(ctx, "cv.txt", []byte(sampleCV))
	rep, err := m.Sync(ctx)
	if err != nil || !rep.ProfileEmbedded || rep.JobsEmbedded != 5 || rep.EmbedError != "" {
		t.Fatalf("rep = %+v, err = %v", rep, err)
	}
	if st := m.Status(ctx); st.Index.Eligible != 5 || st.Index.WithSkills != 5 || st.Index.WithEmbeddings != 5 {
		t.Errorf("index = %+v", st.Index)
	}
	for i, call := range llm.embedCalls {
		if len(call) > 2 {
			t.Errorf("call %d embedded %d texts, batch size is 2", i, len(call))
		}
		for _, in := range call {
			if !strings.HasPrefix(in, "clustering: ") {
				t.Errorf("call %d: missing prefix in %q", i, in)
			}
		}
	}
	// 1 call for the profile + 3 batches (2+2+1) for the jobs.
	if len(llm.embedCalls) != 4 {
		t.Errorf("%d embed calls, want 4", len(llm.embedCalls))
	}
	before := len(llm.embedCalls)
	if rep, _ := m.Sync(ctx); rep.JobsEmbedded != 0 || rep.ProfileEmbedded || len(llm.embedCalls) != before {
		t.Errorf("a second pass should do nothing: %+v", rep)
	}
}

func TestSyncReembedsWhenTheModelChanges(t *testing.T) {
	llm := &fakeLLM{models: chatAndEmbed, chat: llmProfile{}}
	m, st := newMatcher(t, llm)
	ctx := context.Background()
	addJob(t, st, "Go engineer", "Go", domain.StageSenior)
	m.Upload(ctx, "cv.txt", []byte(sampleCV))
	m.Sync(ctx)

	m.invalidate()
	m.Cfg.EmbedModel = "mxbai-embed-large"
	rep, _ := m.Sync(ctx)
	if !rep.ProfileEmbedded || rep.JobsEmbedded != 1 {
		t.Errorf("a different model's vectors aren't comparable, so everything must be redone: %+v", rep)
	}
}

func TestSyncSurvivesAMissingOllama(t *testing.T) {
	llm := &fakeLLM{models: chatAndEmbed, chat: llmProfile{}}
	m, st := newMatcher(t, llm)
	ctx := context.Background()
	id := addJob(t, st, "Go engineer", "Go and Docker", domain.StageSenior)
	m.Upload(ctx, "cv.txt", []byte(sampleCV))

	llm.mu.Lock()
	llm.down = &ollama.UnavailableError{Err: errors.New("connection refused")}
	llm.mu.Unlock()
	rep, err := m.Sync(ctx)
	if err != nil {
		t.Fatalf("Ollama being down is not an error: %v", err)
	}
	if rep.SkillsIndexed != 1 || !strings.Contains(rep.EmbedError, "not running") {
		t.Errorf("rep = %+v", rep)
	}
	// Skills matching still works.
	fits, _ := m.Fits(ctx, []int64{id})
	if f, ok := fits[id]; !ok || f.SkillScore == nil || f.Semantic != nil {
		t.Errorf("fit = %+v, %v; want a skills-only score", f, ok)
	}
	if st := m.Status(ctx); st.Index.LastError == "" || st.Ollama.Reachable {
		t.Errorf("status should surface the problem: %+v", st)
	}
}

func TestStaleProfileEmbeddingIsDropped(t *testing.T) {
	llm := &fakeLLM{models: chatAndEmbed, chat: llmProfile{}}
	m, _ := newMatcher(t, llm)
	ctx := context.Background()
	m.Upload(ctx, "cv.txt", []byte(sampleCV))

	// The user edits the profile while the old one is being embedded.
	llm.mu.Lock()
	llm.onEmbed = func() {
		llm.mu.Lock()
		llm.onEmbed = nil
		llm.mu.Unlock()
		m.UpdateProfile(ctx, Profile{Seniority: "senior", Skills: []Skill{{Name: "Python", Core: true}}})
	}
	llm.mu.Unlock()
	rep, _ := m.Sync(ctx)
	if rep.ProfileEmbedded {
		t.Error("the embedding of the replaced profile must not be stored")
	}
	if v, _ := m.Profile(ctx); v.HasEmbedding {
		t.Error("the stored profile has an embedding of different text")
	}
}

func TestFitsAndRank(t *testing.T) {
	m, st := newMatcher(t, nil)
	ctx := context.Background()
	m.Upload(ctx, "cv.txt", []byte(sampleCV))
	m.UpdateProfile(ctx, Profile{Seniority: "senior", Skills: []Skill{
		{Name: "Go", Core: true}, {Name: "PostgreSQL", Core: true}, {Name: "Docker"},
	}})

	good := addJob(t, st, "Senior Go Engineer", "Backend role using Go, PostgreSQL and Docker", domain.StageSenior)
	middling := addJob(t, st, "Fullstack Engineer", "Fullstack role using Go and React and Figma", domain.StageSenior)
	poor := addJob(t, st, "Frontend Engineer", "React, Vue and Angular", domain.StageSenior)
	none := addJob(t, st, "Mystery role", "Great culture and snacks", domain.StageSenior)
	m.Sync(ctx)

	ids := []int64{none, poor, middling, good}
	ranked, fits, err := m.Rank(ctx, ids, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []int64{good, middling, poor, none}
	for i := range want {
		if ranked[i] != want[i] {
			t.Fatalf("ranked = %v, want %v", ranked, want)
		}
	}
	if _, ok := fits[none]; ok {
		t.Error("a job that names no skill can't be scored")
	}
	// Every skill matches, but Docker is only "familiar" (0.6) and Go is in the
	// title (counts double): (2*1 + 1*1 + 1*0.6) / (2 + 1 + 1) = 0.9.
	if f := fits[good]; f.Score != 90 || f.Band != "strong" || len(f.Missing) != 0 || len(f.Matched) != 3 {
		t.Errorf("good = %+v", f)
	}
	if f := fits[poor]; f.Score != 0 || len(f.Missing) != 3 {
		t.Errorf("poor = %+v", f)
	}

	// A minimum fit drops weak and unscorable jobs.
	ranked, _, _ = m.Rank(ctx, ids, 30)
	if len(ranked) != 2 || ranked[0] != good || ranked[1] != middling {
		t.Errorf("ranked with min fit = %v", ranked)
	}
}

func TestRankWithoutAProfileKeepsTheOrder(t *testing.T) {
	m, st := newMatcher(t, nil)
	a := addJob(t, st, "A", "Go", domain.StageSenior)
	b := addJob(t, st, "B", "Rust", domain.StageSenior)
	ranked, fits, err := m.Rank(context.Background(), []int64{b, a}, 50)
	if err != nil || fits != nil || len(ranked) != 2 || ranked[0] != b {
		t.Errorf("ranked = %v, fits = %v, err = %v", ranked, fits, err)
	}
}

func TestSemanticScoreSeparatesJobsWithEqualSkills(t *testing.T) {
	llm := &fakeLLM{models: chatAndEmbed, chat: llmProfile{}}
	m, st := newMatcher(t, llm)
	m.Cfg.Fit.SemanticLow, m.Cfg.Fit.SemanticHigh = 0.2, 0.9
	ctx := context.Background()
	m.Upload(ctx, "cv.txt", []byte(sampleCV))
	m.UpdateProfile(ctx, Profile{Seniority: "senior", Skills: []Skill{{Name: "Go", Core: true}}})

	// Both jobs name exactly one known skill, Go, so skills can't tell them
	// apart. The text can.
	backend := addJob(t, st, "Backend engineer", "Backend services written in Go. Backend backend backend.", domain.StageSenior)
	design := addJob(t, st, "Design engineer", "Design systems in Figma, design design design. Some work in Go.", domain.StageSenior)
	m.Sync(ctx)

	ranked, fits, _ := m.Rank(ctx, []int64{design, backend}, 0)
	if ranked[0] != backend {
		t.Fatalf("ranked = %v: the backend job should read as the closer match", ranked)
	}
	if fits[backend].Semantic == nil || *fits[backend].Semantic <= *fits[design].Semantic {
		t.Errorf("semantic: backend %v vs design %v", fits[backend].Semantic, fits[design].Semantic)
	}
}

func TestUploadStartsIndexingInTheBackground(t *testing.T) {
	llm := &fakeLLM{models: chatAndEmbed, chat: llmProfile{}}
	m, st := newMatcher(t, llm)
	m.disableAutoSync = false
	for i := 0; i < 3; i++ {
		addJob(t, st, "Go dev", "Go", domain.StageSenior)
	}
	if _, _, err := m.Upload(context.Background(), "cv.txt", []byte(sampleCV)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s := m.Status(context.Background()); !s.Index.Syncing && s.Index.WithEmbeddings == 3 {
			if v, _ := m.Profile(context.Background()); !v.HasEmbedding {
				t.Error("the profile should have been embedded too")
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("uploading did not start indexing: %+v", m.Status(context.Background()).Index)
}

func TestRequestSyncRunsInTheBackground(t *testing.T) {
	m, st := newMatcher(t, nil)
	for i := 0; i < 3; i++ {
		addJob(t, st, "Go dev", "Go", domain.StageSenior)
	}
	for i := 0; i < 20; i++ {
		m.RequestSync() // bursts fold into a few passes
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s := m.Status(context.Background()); !s.Index.Syncing && s.Index.WithSkills == 3 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("background sync did not finish: %+v", m.Status(context.Background()).Index)
}

func TestStatus(t *testing.T) {
	ctx := context.Background()

	m, _ := newMatcher(t, nil)
	if s := m.Status(ctx); s.Ollama.Enabled || s.HasProfile || s.Ollama.Installed == nil {
		t.Errorf("no LLM: %+v", s)
	}

	m, _ = newMatcher(t, &fakeLLM{models: chatAndEmbed})
	s := m.Status(ctx)
	if !s.Ollama.Enabled || !s.Ollama.Reachable || s.Ollama.Version != "0.9.0" || !s.Ollama.EmbedInstalled ||
		!s.Ollama.ChatInstalled || s.Ollama.ChatModel != "qwen2.5:7b" || len(s.Ollama.Installed) != 2 {
		t.Errorf("healthy: %+v", s.Ollama)
	}

	m, _ = newMatcher(t, &fakeLLM{models: []ollama.Model{{Name: "llama3.2:3b"}}})
	if s := m.Status(ctx); s.Ollama.EmbedInstalled || !s.Ollama.ChatInstalled {
		t.Errorf("no embed model: %+v", s.Ollama)
	}

	m, _ = newMatcher(t, &fakeLLM{models: chatAndEmbed})
	m.Cfg.ChatModel = "gemma3:12b"
	if s := m.Status(ctx); s.Ollama.ChatInstalled || s.Ollama.ChatModel != "gemma3:12b" {
		t.Errorf("configured model missing: %+v", s.Ollama)
	}

	m, _ = newMatcher(t, &fakeLLM{down: &ollama.UnavailableError{Err: errors.New("refused")}})
	if s := m.Status(ctx); s.Ollama.Reachable || !strings.Contains(s.Ollama.Error, "not reachable") {
		t.Errorf("down: %+v", s.Ollama)
	}
}

func TestChooseChatModel(t *testing.T) {
	names := func(n ...string) []ollama.Model {
		var out []ollama.Model
		for _, x := range n {
			out = append(out, ollama.Model{Name: x})
		}
		return out
	}
	tests := []struct {
		name    string
		want    string
		models  []ollama.Model
		wantOut string
		wantErr string
	}{
		{"explicit and installed", "gemma3:4b", names("gemma3:4b", "qwen2.5:7b"), "gemma3:4b", ""},
		{"explicit without a tag matches latest", "mistral", names("mistral:latest"), "mistral", ""},
		{"explicit but missing", "gemma3:4b", names("qwen2.5:7b"), "", "ollama pull gemma3:4b"},
		{"preferred order", "", names("llama3.2:3b", "qwen2.5:7b"), "qwen2.5:7b", ""},
		{"falls back to any chat model", "", names("nomic-embed-text:latest", "phi4:14b"), "phi4:14b", ""},
		{"only embedding models", "", names("nomic-embed-text:latest", "bge-m3:latest", "mxbai-embed-large"), "", "no chat model"},
		{"nothing installed", "", nil, "", "no chat model"},
	}
	for _, tt := range tests {
		got, err := chooseChatModel(tt.want, tt.models)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("%s: err = %v, want containing %q", tt.name, err, tt.wantErr)
			}
			continue
		}
		if err != nil || got != tt.wantOut {
			t.Errorf("%s: got %q, %v; want %q", tt.name, got, err, tt.wantOut)
		}
	}
}

func TestHelpers(t *testing.T) {
	for in, want := range map[string]int{"8 years of experience": 8, "5+ years, previously 12 yrs": 12, "no numbers": 0, "100 years of history": 0, "3 YEARS": 3} {
		if got := guessYears(in); got != want {
			t.Errorf("guessYears(%q) = %d, want %d", in, got, want)
		}
	}
	for years, want := range map[int]string{0: "junior", 2: "junior", 3: "mid", 5: "mid", 6: "senior", 20: "senior"} {
		if got := seniorityForYears(years); got != want {
			t.Errorf("seniorityForYears(%d) = %s, want %s", years, got, want)
		}
	}
	for s, want := range map[string]bool{"jane@example.com": true, "call +40 721 234 567": true, "Senior engineer": false, "8 years": false, "Go 1.22": false} {
		if got := hasContactDetails(s); got != want {
			t.Errorf("hasContactDetails(%q) = %v, want %v", s, got, want)
		}
	}
	for s, want := range map[string]bool{"Temporal": true, "Go": false, "x": false, "Leadership": false, "a b c d e f": false, "": false, "person@x.com": false} {
		if got := plausibleCustomSkill(s); got != want {
			t.Errorf("plausibleCustomSkill(%q) = %v, want %v", s, got, want)
		}
	}
	for in, want := range map[string]string{
		"../../etc/passwd": "passwd", `C:\Users\jane\cv.pdf`: "cv.pdf", "cv\x00.pdf": "cv.pdf", "": "", "/": "", strings.Repeat("a", 300): strings.Repeat("a", 100),
	} {
		if got := displayName(in); got != want {
			t.Errorf("displayName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCalibrate(t *testing.T) {
	llm := &fakeLLM{models: chatAndEmbed, chat: llmProfile{}}
	m, st := newMatcher(t, llm)
	ctx := context.Background()
	m.Upload(ctx, "cv.txt", []byte(sampleCV))
	for i := 0; i < 30; i++ {
		body := "Backend services in Go on Kubernetes. Backend backend."
		if i%3 == 0 {
			body = "Design systems in Figma, design design design. React."
		}
		addJob(t, st, "Engineer", body+strings.Repeat(" x", i), domain.StageSenior)
	}
	m.Sync(ctx)

	c, err := m.Calibrate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if c.Jobs != 30 || c.Model != "nomic-embed-text" {
		t.Errorf("calibration = %+v", c)
	}
	if !(c.Min <= c.P25 && c.P25 <= c.Median && c.Median <= c.P75 && c.P75 <= c.P95 && c.P95 <= c.Max) {
		t.Errorf("percentiles are not in order: %+v", c)
	}
	if c.Min == c.Max {
		t.Error("the fake jobs differ, so similarity should vary")
	}
	if c.SuggestedLow != round2(c.P25) || c.SuggestedHigh-c.SuggestedLow < 0.05 {
		t.Errorf("suggested = %v..%v from p25 %v, p95 %v", c.SuggestedLow, c.SuggestedHigh, c.P25, c.P95)
	}
}

func TestCalibrateExplainsWhatIsMissing(t *testing.T) {
	ctx := context.Background()

	m, _ := newMatcher(t, nil)
	if _, err := m.Calibrate(ctx); !errors.Is(err, ErrNoProfile) {
		t.Errorf("no profile: %v", err)
	}

	m, _ = newMatcher(t, nil) // keywords only: the profile can't be embedded
	m.Upload(ctx, "cv.txt", []byte(sampleCV))
	if _, err := m.Calibrate(ctx); err == nil || !strings.Contains(err.Error(), "embedded") {
		t.Errorf("no embedding: %v", err)
	}

	llm := &fakeLLM{models: chatAndEmbed, chat: llmProfile{}}
	m, st := newMatcher(t, llm)
	m.Upload(ctx, "cv.txt", []byte(sampleCV))
	addJob(t, st, "Go dev", "Backend in Go", domain.StageSenior)
	m.Sync(ctx)
	if _, err := m.Calibrate(ctx); err == nil || !strings.Contains(err.Error(), "at least 20") {
		t.Errorf("too few jobs: %v", err)
	}
}

func TestPercentile(t *testing.T) {
	s := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	for p, want := range map[float64]float64{0: 1, 0.25: 3, 0.5: 5, 0.95: 9, 1: 10} {
		if got := percentile(s, p); got != want {
			t.Errorf("percentile(%v) = %v, want %v", p, got, want)
		}
	}
	if round2(0.8349) != 0.83 || round2(0.835) != 0.84 {
		t.Errorf("round2: %v %v", round2(0.8349), round2(0.835))
	}
}

// A skill the user adds that the vocabulary doesn't know can never be found by
// the vocabulary-based indexer, so it has to be looked for in the job text.
func TestCustomSkillsMatchJobText(t *testing.T) {
	m, st := newMatcher(t, nil)
	ctx := context.Background()
	m.Upload(ctx, "cv.txt", []byte(sampleCV))
	id := addJob(t, st, "Senior Go Engineer", "We use Go, Docker and Temporal for workflows. Some gRPC.", domain.StageSenior)
	other := addJob(t, st, "Senior Go Engineer", "We use Go and Docker.", domain.StageSenior)
	m.Sync(ctx)

	view, _ := m.Profile(ctx)
	skillsNow := append(view.Skills, Skill{Name: "Temporal", Core: true})
	if _, err := m.UpdateProfile(ctx, Profile{Seniority: "senior", Skills: skillsNow}); err != nil {
		t.Fatal(err)
	}

	fits, err := m.Fits(ctx, []int64{id, other})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(fits[id].Matched, "Temporal") {
		t.Errorf("matched = %v, want the custom skill Temporal", fits[id].Matched)
	}
	if slices.Contains(fits[other].Matched, "Temporal") || slices.Contains(fits[other].Missing, "Temporal") {
		t.Errorf("a job that doesn't mention it must not list it: %+v", fits[other])
	}
	if len(fits[id].Missing) > 0 && slices.Contains(fits[id].Missing, "Temporal") {
		t.Errorf("Temporal is both matched and missing: %+v", fits[id])
	}
}
