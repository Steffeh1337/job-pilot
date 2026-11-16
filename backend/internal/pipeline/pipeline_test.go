package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/Steffeh1337/job-pilot/backend/internal/config"
	"github.com/Steffeh1337/job-pilot/backend/internal/domain"
	"github.com/Steffeh1337/job-pilot/backend/internal/store"
)

func newPipeline(t *testing.T) (*Pipeline, *store.Store) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(testFilter(t), st), st
}

func goJob(source, id, company string) *domain.Job {
	return &domain.Job{
		Source: source, ExternalID: id, URL: "https://example.com/" + id,
		Title: "Senior Backend Engineer", Company: company, Remote: true,
		Regions: []string{"Worldwide"}, Description: "Go and TypeScript",
		PostedAt: time.Now(),
	}
}

func TestIngestDedupesAcrossSources(t *testing.T) {
	ctx := context.Background()
	p, st := newPipeline(t)

	added, err := p.Ingest(ctx, []*domain.Job{
		goJob("remoteok", "1", "Acme, Inc."),
		goJob("himalayas", "x", "ACME Inc"), // same role on another board
		goJob("remotive", "2", "Other Co"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if added != 3 {
		t.Fatalf("added = %d, want 3", added)
	}

	rows, err := st.Funnel(ctx, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	total := rows[len(rows)-1]
	if total.Source != "TOTAL" || total.New != 3 || total.Unique != 2 || total.Senior != 2 {
		t.Fatalf("total = %+v, want new=3 unique=2 senior=2", total)
	}
}

func TestIngestIsIdempotent(t *testing.T) {
	ctx := context.Background()
	p, st := newPipeline(t)

	for i := 0; i < 3; i++ {
		added, err := p.Ingest(ctx, []*domain.Job{goJob("remoteok", "1", "Acme")})
		if err != nil {
			t.Fatal(err)
		}
		if want := map[bool]int{true: 1, false: 0}[i == 0]; added != want {
			t.Fatalf("poll %d: added = %d, want %d", i, added, want)
		}
	}
	jobs, err := st.AllJobs(ctx)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("stored %d jobs (err %v), want 1", len(jobs), err)
	}
}

func TestIngestSkipsIncompleteJobs(t *testing.T) {
	p, _ := newPipeline(t)
	added, err := p.Ingest(context.Background(), []*domain.Job{
		{Source: "x", ExternalID: "", Title: "No id"},
		{Source: "x", ExternalID: "1", Title: ""},
	})
	if err != nil || added != 0 {
		t.Fatalf("added = %d, err = %v; want 0, nil", added, err)
	}
}

func TestReclassifyAppliesNewFilters(t *testing.T) {
	ctx := context.Background()
	p, st := newPipeline(t)

	j := goJob("remoteok", "1", "Acme")
	j.Title = "Senior Rust Engineer"
	j.Description = "Rust only"
	if _, err := p.Ingest(ctx, []*domain.Job{j}); err != nil {
		t.Fatal(err)
	}
	if matches, _ := st.Matches(ctx, 10); len(matches) != 1 {
		t.Fatalf("a Rust role matches under the default filters, got %v", matches)
	}
	if n, _ := p.Reclassify(ctx); n != 0 {
		t.Fatalf("unchanged filters changed %d jobs", n)
	}

	// Narrow the role filter to exclude Rust and re-run over stored data.
	c := config.Default().Filter
	c.TitleExclude += `|(?i:\brust\b)`
	f, err := NewFilter(c)
	if err != nil {
		t.Fatal(err)
	}
	n, err := New(f, st).Reclassify(ctx)
	if err != nil || n != 1 {
		t.Fatalf("reclassify changed %d (err %v), want 1", n, err)
	}
	if matches, err := st.Matches(ctx, 10); err != nil || len(matches) != 0 {
		t.Fatalf("matches = %v (err %v), want none", matches, err)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	p, st := newPipeline(t)

	j := goJob("himalayas", "g", "Acme")
	j.Tags, j.Location, j.SalaryMin, j.SalaryMax, j.Currency = []string{"go", "react"}, "Remote", 100, 200, "EUR"
	if _, err := p.Ingest(ctx, []*domain.Job{j}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Matches(ctx, 1)
	if err != nil || len(got) != 1 {
		t.Fatalf("matches = %v, err = %v", got, err)
	}
	m := got[0]
	if len(m.Tags) != 2 || m.Tags[1] != "react" || len(m.Regions) != 1 || m.SalaryMax != 200 || m.Currency != "EUR" || !m.Remote {
		t.Fatalf("round trip lost data: %+v", m)
	}
}

func TestPollRecording(t *testing.T) {
	ctx := context.Background()
	_, st := newPipeline(t)

	if p, err := st.LastPoll(ctx, "remoteok"); err != nil || p != nil {
		t.Fatalf("LastPoll before any poll = %v, %v; want nil, nil", p, err)
	}
	at := time.Unix(1_700_000_000, 0)
	for i, errMsg := range []string{"", "boom"} {
		err := st.RecordPoll(ctx, store.Poll{Source: "remoteok", StartedAt: at.Add(time.Duration(i) * time.Hour), Fetched: 10 * (i + 1), Error: errMsg})
		if err != nil {
			t.Fatal(err)
		}
	}
	last, err := st.LastPoll(ctx, "remoteok")
	if err != nil || last == nil || last.Error != "boom" || last.Fetched != 20 {
		t.Fatalf("LastPoll = %+v, %v", last, err)
	}
	recent, err := st.RecentPolls(ctx)
	if err != nil || len(recent) != 1 || recent[0].Error != "boom" {
		t.Fatalf("RecentPolls = %+v, %v", recent, err)
	}
}

func TestIngestDecodesHTMLEntities(t *testing.T) {
	ctx := context.Background()
	p, st := newPipeline(t)
	a := goJob("remoteok", "1", "Hims &#038; Hers")
	a.Title = "Senior R&amp;D Engineer"
	a.Location = "  Remote &amp; Hybrid "
	b := goJob("jobicy", "2", "Hims & Hers") // the same role, spelled normally
	b.Title = "Senior R&D Engineer"
	if _, err := p.Ingest(ctx, []*domain.Job{a, b}); err != nil {
		t.Fatal(err)
	}
	jobs, _ := st.AllJobs(ctx)
	if len(jobs) != 2 {
		t.Fatalf("stored %d jobs", len(jobs))
	}
	if jobs[0].Title != "Senior R&D Engineer" || jobs[0].Company != "Hims & Hers" || jobs[0].Location != "Remote & Hybrid" {
		t.Errorf("text was not decoded: %q / %q / %q", jobs[0].Title, jobs[0].Company, jobs[0].Location)
	}
	if jobs[1].Stage != domain.StageDuplicate {
		t.Error("the same role from two boards must dedupe once the entities are decoded")
	}
}

func TestReclassifyFixesTextStoredBeforeNormalisation(t *testing.T) {
	ctx := context.Background()
	p, st := newPipeline(t)
	now := time.Now()
	old := goJob("remoteok", "1", "Aisles &amp; Abroad")
	old.FirstSeenAt, old.LastSeenAt, old.Fingerprint = now, now, "aisles amp abroad|senior backend engineer"
	if err := st.Insert(ctx, old); err != nil { // as the earlier version stored it
		t.Fatal(err)
	}

	if _, err := p.Reclassify(ctx); err != nil {
		t.Fatal(err)
	}
	jobs, _ := st.AllJobs(ctx)
	if jobs[0].Company != "Aisles & Abroad" {
		t.Errorf("company = %q", jobs[0].Company)
	}
	if want := domain.Fingerprint("Aisles & Abroad", "Senior Backend Engineer"); jobs[0].Fingerprint != want {
		t.Errorf("fingerprint = %q, want %q", jobs[0].Fingerprint, want)
	}
}
