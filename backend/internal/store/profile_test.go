package store

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Steffeh1337/job-pilot/backend/internal/domain"
)

func TestProfileRoundTrip(t *testing.T) {
	ctx := context.Background()
	st := open(t)

	if p, err := st.LoadProfile(ctx); p != nil || err != nil {
		t.Fatalf("empty store: %v, %v", p, err)
	}
	at := time.Unix(1_700_000_000, 0)
	row := ProfileRow{Data: []byte(`{"headline":"Engineer"}`), CVText: "the cv text", CVFilename: "cv.pdf", ExtractedBy: "keywords", UploadedAt: at, UpdatedAt: at}
	if err := st.SaveProfile(ctx, row); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadProfile(ctx)
	if err != nil || got == nil {
		t.Fatalf("LoadProfile = %v, %v", got, err)
	}
	if string(got.Data) != `{"headline":"Engineer"}` || got.CVText != "the cv text" || got.CVFilename != "cv.pdf" ||
		got.ExtractedBy != "keywords" || !got.UploadedAt.Equal(at) || got.Embedding != nil {
		t.Errorf("profile = %+v", got)
	}

	applied, err := st.SetProfileEmbedding(ctx, row.Data, "nomic-embed-text", []byte{1, 2, 3, 4})
	if err != nil || !applied {
		t.Fatalf("SetProfileEmbedding = %v, %v", applied, err)
	}
	got, _ = st.LoadProfile(ctx)
	if got.EmbedModel != "nomic-embed-text" || !reflect.DeepEqual(got.Embedding, []byte{1, 2, 3, 4}) {
		t.Errorf("embedding not stored: %+v", got)
	}

	// Saving again replaces the single row, including dropping the stale embedding.
	row.Data = []byte(`{"headline":"Lead"}`)
	if err := st.SaveProfile(ctx, row); err != nil {
		t.Fatal(err)
	}
	got, _ = st.LoadProfile(ctx)
	if string(got.Data) != `{"headline":"Lead"}` || got.Embedding != nil || got.EmbedModel != "" {
		t.Errorf("save should replace the row and clear the embedding: %+v", got)
	}
	var n int
	st.db.QueryRow(`SELECT COUNT(*) FROM profile`).Scan(&n)
	if n != 1 {
		t.Errorf("%d profile rows, want 1", n)
	}
}

func TestStaleProfileEmbeddingIsNotStored(t *testing.T) {
	ctx := context.Background()
	st := open(t)
	old := ProfileRow{Data: []byte(`{"v":1}`), CVText: "cv", CVFilename: "cv.pdf", UploadedAt: time.Now(), UpdatedAt: time.Now()}
	st.SaveProfile(ctx, old)
	// The profile is edited while the old text is still being embedded.
	edited := old
	edited.Data = []byte(`{"v":2}`)
	st.SaveProfile(ctx, edited)

	applied, err := st.SetProfileEmbedding(ctx, old.Data, "m", []byte{1, 2, 3, 4})
	if err != nil || applied {
		t.Fatalf("applied = %v, err = %v; a stale embedding must be dropped", applied, err)
	}
	if got, _ := st.LoadProfile(ctx); got.Embedding != nil {
		t.Error("the stale embedding was stored")
	}
	if applied, _ := st.SetProfileEmbedding(ctx, edited.Data, "m", []byte{1, 2, 3, 4}); !applied {
		t.Error("an embedding of the current profile must be stored")
	}
	if applied, err := open(t).SetProfileEmbedding(ctx, []byte("x"), "m", nil); applied || err != nil {
		t.Errorf("with no profile: %v, %v", applied, err)
	}
}

func TestDeleteProfile(t *testing.T) {
	ctx := context.Background()
	st := open(t)
	if err := st.DeleteProfile(ctx); err != nil {
		t.Fatalf("deleting nothing should be fine: %v", err)
	}
	st.SaveProfile(ctx, ProfileRow{Data: []byte(`{}`), CVText: "secret cv", CVFilename: "cv.pdf", UploadedAt: time.Now(), UpdatedAt: time.Now()})
	if err := st.DeleteProfile(ctx); err != nil {
		t.Fatal(err)
	}
	if p, _ := st.LoadProfile(ctx); p != nil {
		t.Error("the profile and the CV text must be gone")
	}
	var n int
	st.db.QueryRow(`SELECT COUNT(*) FROM profile WHERE cv_text LIKE '%secret%'`).Scan(&n)
	if n != 0 {
		t.Error("CV text is still in the database")
	}
}

func TestJobSkillsAndEmbeddings(t *testing.T) {
	ctx := context.Background()
	st := open(t)
	a := insert(t, st, domain.Job{Source: "s", ExternalID: "1", Title: "A", Stage: domain.StageSenior})

	if err := st.SaveJobSkills(ctx, []JobSkills{{JobID: a, Skills: []string{"Go", "Docker"}, TitleSkills: []string{"Go"}, VocabVersion: "v1"}}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveJobEmbeddings(ctx, []JobEmbedding{{JobID: a, Model: "m1", Vector: []byte{9, 9, 9, 9}}}); err != nil {
		t.Fatal(err)
	}
	f, err := st.JobFeaturesFor(ctx, []int64{a, 999})
	if err != nil || len(f) != 1 {
		t.Fatalf("features = %v, %v", f, err)
	}
	got := f[a]
	if !reflect.DeepEqual(got.Skills, []string{"Go", "Docker"}) || !reflect.DeepEqual(got.TitleSkills, []string{"Go"}) ||
		got.VocabVersion != "v1" || got.EmbedModel != "m1" || !reflect.DeepEqual(got.Embedding, []byte{9, 9, 9, 9}) {
		t.Errorf("features = %+v", got)
	}

	// Recomputing skills must not wipe the embedding.
	st.SaveJobSkills(ctx, []JobSkills{{JobID: a, Skills: []string{"Go"}, VocabVersion: "v2"}})
	got = mustFeatures(t, st, a)
	if got.EmbedModel != "m1" || got.Embedding == nil || got.VocabVersion != "v2" || len(got.Skills) != 1 {
		t.Errorf("skills update disturbed the embedding: %+v", got)
	}
}

func mustFeatures(t *testing.T, st *Store, id int64) JobFeatures {
	t.Helper()
	m, err := st.JobFeaturesFor(context.Background(), []int64{id})
	if err != nil {
		t.Fatal(err)
	}
	return m[id]
}

func TestEmptySkillListsAreStoredAsLists(t *testing.T) {
	st := open(t)
	id := insert(t, st, domain.Job{Source: "s", ExternalID: "1", Title: "A", Stage: domain.StageSenior})
	st.SaveJobSkills(context.Background(), []JobSkills{{JobID: id, VocabVersion: "v"}})
	f := mustFeatures(t, st, id)
	if f.Skills == nil || f.TitleSkills == nil || len(f.Skills) != 0 {
		t.Errorf("want empty non-nil slices, got %#v", f)
	}
}

func TestJobsNeedingSkills(t *testing.T) {
	ctx := context.Background()
	st := open(t)
	low := insert(t, st, domain.Job{Source: "s", ExternalID: "low", Title: "low", Stage: domain.StageUnique})
	fresh := insert(t, st, domain.Job{Source: "s", ExternalID: "fresh", Title: "fresh", Stage: domain.StageRegion})
	stale := insert(t, st, domain.Job{Source: "s", ExternalID: "stale", Title: "stale", Stage: domain.StageSenior})
	todo := insert(t, st, domain.Job{Source: "s", ExternalID: "todo", Title: "todo", Stage: domain.StageSenior})
	st.SaveJobSkills(ctx, []JobSkills{
		{JobID: fresh, VocabVersion: "v2"},
		{JobID: stale, VocabVersion: "v1"},
	})

	jobs, err := st.JobsNeedingSkills(ctx, domain.StageRegion, "v2", 10)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64]bool{}
	for _, j := range jobs {
		got[j.ID] = true
	}
	if !got[stale] || !got[todo] || got[fresh] || got[low] || len(got) != 2 {
		t.Errorf("needing skills = %v (stale %d, todo %d, fresh %d, low %d)", got, stale, todo, fresh, low)
	}
	if limited, _ := st.JobsNeedingSkills(ctx, domain.StageRegion, "v2", 1); len(limited) != 1 {
		t.Errorf("limit not applied: %d", len(limited))
	}
}

func TestJobsNeedingEmbeddingsFollowTheModel(t *testing.T) {
	ctx := context.Background()
	st := open(t)
	a := insert(t, st, domain.Job{Source: "s", ExternalID: "a", Title: "a", Stage: domain.StageSenior})
	b := insert(t, st, domain.Job{Source: "s", ExternalID: "b", Title: "b", Stage: domain.StageSenior})
	insert(t, st, domain.Job{Source: "s", ExternalID: "noskills", Title: "c", Stage: domain.StageSenior})
	st.SaveJobSkills(ctx, []JobSkills{{JobID: a, VocabVersion: "v"}, {JobID: b, VocabVersion: "v"}})
	st.SaveJobEmbeddings(ctx, []JobEmbedding{{JobID: a, Model: "old", Vector: []byte{1, 0, 0, 0}}})

	ids := func(model string) map[int64]bool {
		jobs, err := st.JobsNeedingEmbeddings(ctx, domain.StageRegion, model, 10)
		if err != nil {
			t.Fatal(err)
		}
		m := map[int64]bool{}
		for _, j := range jobs {
			m[j.ID] = true
		}
		return m
	}
	if got := ids("old"); len(got) != 1 || !got[b] {
		t.Errorf("with the model already used: %v, want only b (a is done, the third has no skills row yet)", got)
	}
	if got := ids("new"); len(got) != 2 || !got[a] || !got[b] {
		t.Errorf("after switching model both need embedding: %v", got)
	}
}

func TestFeatureCounts(t *testing.T) {
	ctx := context.Background()
	st := open(t)
	a := insert(t, st, domain.Job{Source: "s", ExternalID: "a", Title: "a", Stage: domain.StageSenior})
	b := insert(t, st, domain.Job{Source: "s", ExternalID: "b", Title: "b", Stage: domain.StageRegion})
	insert(t, st, domain.Job{Source: "s", ExternalID: "c", Title: "c", Stage: domain.StageSenior})
	insert(t, st, domain.Job{Source: "s", ExternalID: "d", Title: "d", Stage: domain.StageUnique}) // not eligible
	st.SaveJobSkills(ctx, []JobSkills{{JobID: a, VocabVersion: "v"}, {JobID: b, VocabVersion: "v"}})
	st.SaveJobEmbeddings(ctx, []JobEmbedding{{JobID: a, Model: "m", Vector: []byte{1, 0, 0, 0}}})

	c, err := st.FeatureCounts(ctx, domain.StageRegion, "v", "m")
	if err != nil {
		t.Fatal(err)
	}
	if c.Eligible != 3 || c.WithSkills != 2 || c.WithEmbeddings != 1 {
		t.Errorf("counts = %+v, want 3 eligible, 2 with skills, 1 with embeddings", c)
	}
	if c, _ := st.FeatureCounts(ctx, domain.StageRegion, "other", "other"); c.WithSkills != 0 || c.WithEmbeddings != 0 {
		t.Errorf("a different vocabulary and model make everything stale: %+v", c)
	}
}

func TestSearchIDsAndJobsByIDs(t *testing.T) {
	ctx := context.Background()
	st := open(t)
	var all []int64
	for _, id := range []string{"1", "2", "3"} {
		all = append(all, insert(t, st, domain.Job{Source: "s", ExternalID: id, Title: "T" + id, Stage: domain.StageSenior}))
	}
	insert(t, st, domain.Job{Source: "s", ExternalID: "x", Title: "dropped", Stage: domain.StageRole, RejectReason: "seniority"})

	ids, err := st.SearchIDs(ctx, Search{})
	if err != nil || len(ids) != 3 {
		t.Fatalf("SearchIDs = %v, %v; want the 3 matches", ids, err)
	}
	if ids[0] != all[2] {
		t.Errorf("newest first: %v", ids)
	}
	if limited, _ := st.SearchIDs(ctx, Search{Limit: 1, Offset: 2}); len(limited) != 3 {
		t.Error("SearchIDs must ignore Limit and Offset")
	}
	if rej, _ := st.SearchIDs(ctx, Search{Status: StatusRejected}); len(rej) != 1 {
		t.Errorf("filters apply: %v", rej)
	}

	// Order is the caller's, unknown ids are skipped.
	jobs, err := st.JobsByIDs(ctx, []int64{all[0], 9999, all[2], all[1]})
	if err != nil || len(jobs) != 3 || jobs[0].ID != all[0] || jobs[1].ID != all[2] || jobs[2].ID != all[1] {
		t.Errorf("JobsByIDs = %v, %v", jobs, err)
	}
	if jobs, err := st.JobsByIDs(ctx, nil); jobs != nil || err != nil {
		t.Errorf("empty ids: %v, %v", jobs, err)
	}
}

func TestJobFeaturesForHandlesManyIDs(t *testing.T) {
	ctx := context.Background()
	st := open(t)
	var ids []int64
	var batch []JobSkills
	for i := 0; i < 1200; i++ { // past the internal chunk size
		id := insert(t, st, domain.Job{Source: "s", ExternalID: string(rune('a'+i%26)) + time.Duration(i).String(), Title: "t", Stage: domain.StageSenior})
		ids = append(ids, id)
		batch = append(batch, JobSkills{JobID: id, Skills: []string{"Go"}, VocabVersion: "v"})
	}
	if err := st.SaveJobSkills(ctx, batch); err != nil {
		t.Fatal(err)
	}
	m, err := st.JobFeaturesFor(ctx, ids)
	if err != nil || len(m) != 1200 {
		t.Errorf("got %d features, err %v; want 1200", len(m), err)
	}
}
