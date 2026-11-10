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
	if n, _ := p.Reclassify(ctx); n != 0 {
		t.Fatalf("unchanged filters changed %d jobs", n)
	}

	// Widen the stack filter to include Rust and re-run over stored data.
	c := config.Default().Filter
	c.Stack += `|(?i:\brust\b)`
	f, err := NewFilter(c)
	if err != nil {
		t.Fatal(err)
	}
	n, err := New(f, st).Reclassify(ctx)
	if err != nil || n != 1 {
		t.Fatalf("reclassify changed %d (err %v), want 1", n, err)
	}
	matches, err := st.Matches(ctx, 10)
	if err != nil || len(matches) != 1 || matches[0].Title != "Senior Rust Engineer" {
		t.Fatalf("matches = %v (err %v)", matches, err)
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
