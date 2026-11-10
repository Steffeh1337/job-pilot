package scheduler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Steffeh1337/job-pilot/backend/internal/config"
	"github.com/Steffeh1337/job-pilot/backend/internal/domain"
	"github.com/Steffeh1337/job-pilot/backend/internal/pipeline"
	"github.com/Steffeh1337/job-pilot/backend/internal/store"
)

type fakeSource struct {
	name     string
	interval time.Duration
	jobs     []*domain.Job
	err      error
	calls    int
}

func (f *fakeSource) Name() string            { return f.name }
func (f *fakeSource) Interval() time.Duration { return f.interval }
func (f *fakeSource) Fetch(context.Context) ([]*domain.Job, error) {
	f.calls++
	return f.jobs, f.err
}

func setup(t *testing.T, srcs ...*fakeSource) (*Scheduler, *store.Store, *time.Time) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	f, err := pipeline.NewFilter(config.Default().Filter)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	s := &Scheduler{
		Pipeline: pipeline.New(f, st), Store: st, Tick: 15 * time.Minute,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now: func() time.Time { return now },
	}
	for _, src := range srcs {
		s.Sources = append(s.Sources, src)
	}
	return s, st, &now
}

func job(id string) *domain.Job {
	return &domain.Job{Source: "a", ExternalID: id, Title: "Senior Go Engineer", Company: "Acme" + id,
		Remote: true, Regions: []string{"Worldwide"}, Description: "Go"}
}

func TestRespectsSourceInterval(t *testing.T) {
	ctx := context.Background()
	fast := &fakeSource{name: "fast", interval: 15 * time.Minute, jobs: []*domain.Job{job("1")}}
	slow := &fakeSource{name: "slow", interval: 6 * time.Hour, jobs: []*domain.Job{job("2")}}
	s, _, now := setup(t, fast, slow)

	s.RunOnce(ctx) // first run: everything is due
	*now = now.Add(15 * time.Minute)
	s.RunOnce(ctx) // only fast is due again
	*now = now.Add(15 * time.Minute)
	s.RunOnce(ctx)
	*now = now.Add(6 * time.Hour)
	s.RunOnce(ctx) // both due

	if fast.calls != 4 || slow.calls != 2 {
		t.Fatalf("fast polled %d times (want 4), slow %d (want 2)", fast.calls, slow.calls)
	}
}

func TestFailedPollIsNotRetriedEveryTick(t *testing.T) {
	ctx := context.Background()
	broken := &fakeSource{name: "broken", interval: time.Hour, err: errors.New("503")}
	s, st, now := setup(t, broken)

	s.RunOnce(ctx)
	*now = now.Add(15 * time.Minute)
	s.RunOnce(ctx)
	if broken.calls != 1 {
		t.Fatalf("broken source polled %d times within its interval, want 1", broken.calls)
	}
	last, err := st.LastPoll(ctx, "broken")
	if err != nil || last == nil || last.Error != "503" {
		t.Fatalf("poll not recorded with its error: %+v, %v", last, err)
	}
}

func TestPartialResultsAreStillIngested(t *testing.T) {
	ctx := context.Background()
	partial := &fakeSource{name: "partial", interval: time.Hour, jobs: []*domain.Job{job("1")}, err: errors.New("board x: 404")}
	s, st, _ := setup(t, partial)

	s.RunOnce(ctx)
	last, _ := st.LastPoll(ctx, "partial")
	if last == nil || last.Fetched != 1 || last.NewJobs != 1 || last.Error == "" {
		t.Fatalf("poll = %+v, want 1 fetched, 1 new, and the error kept", last)
	}
}

func TestNewJobsCountedOncePerJob(t *testing.T) {
	ctx := context.Background()
	src := &fakeSource{name: "a", interval: time.Minute, jobs: []*domain.Job{job("1"), job("2")}}
	s, st, now := setup(t, src)

	s.RunOnce(ctx)
	*now = now.Add(time.Hour)
	src.jobs = []*domain.Job{job("1"), job("2"), job("3")} // one new listing appeared
	s.RunOnce(ctx)

	last, _ := st.LastPoll(ctx, "a")
	if last.Fetched != 3 || last.NewJobs != 1 {
		t.Fatalf("second poll = %+v, want 3 fetched and 1 new", last)
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	src := &fakeSource{name: "a", interval: time.Hour, jobs: []*domain.Job{job("1")}}
	s, _, _ := setup(t, src)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
