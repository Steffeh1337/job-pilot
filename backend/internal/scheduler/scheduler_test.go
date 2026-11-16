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
	// started and block let a test hold Fetch open to observe a running refresh.
	started chan struct{}
	block   chan struct{}
}

func (f *fakeSource) Name() string            { return f.name }
func (f *fakeSource) Interval() time.Duration { return f.interval }
func (f *fakeSource) Fetch(context.Context) ([]*domain.Job, error) {
	f.calls++
	if f.started != nil {
		f.started <- struct{}{}
	}
	if f.block != nil {
		<-f.block
	}
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

func TestRefreshPollsSourcesThatAreNotDue(t *testing.T) {
	ctx := context.Background()
	slow := &fakeSource{name: "slow", interval: 6 * time.Hour, jobs: []*domain.Job{job("1")}}
	s, _, now := setup(t, slow)

	s.RunOnce(ctx)
	*now = now.Add(10 * time.Minute)
	s.RunOnce(ctx) // not due: the interval is 6h
	if slow.calls != 1 {
		t.Fatalf("scheduled round polled %d times, want 1", slow.calls)
	}

	res := s.Refresh(ctx)
	if slow.calls != 2 {
		t.Fatalf("refresh did not force the poll: %d calls", slow.calls)
	}
	if len(res) != 1 || res[0].Skipped || res[0].Fetched != 1 || res[0].NewJobs != 0 {
		t.Errorf("result = %+v, want fetched 1, nothing new, not skipped", res)
	}
}

func TestRefreshLeavesRecentlyPolledSourcesAlone(t *testing.T) {
	ctx := context.Background()
	src := &fakeSource{name: "a", interval: time.Hour, jobs: []*domain.Job{job("1")}}
	s, _, now := setup(t, src)

	s.RunOnce(ctx)
	*now = now.Add(time.Minute)
	res := s.Refresh(ctx)
	if src.calls != 1 || len(res) != 1 || !res[0].Skipped {
		t.Fatalf("calls = %d, result = %+v; want 1 call and a skipped result", src.calls, res)
	}

	*now = now.Add(5 * time.Minute) // now past the default cooldown
	s.Refresh(ctx)
	if src.calls != 2 {
		t.Errorf("calls = %d, want 2 once the cooldown has passed", src.calls)
	}
}

func TestRefreshCooldownIsConfigurable(t *testing.T) {
	ctx := context.Background()
	src := &fakeSource{name: "a", interval: time.Hour, jobs: []*domain.Job{job("1")}}
	s, _, now := setup(t, src)
	s.RefreshMinAge = time.Second

	s.RunOnce(ctx)
	*now = now.Add(2 * time.Second)
	s.Refresh(ctx)
	if src.calls != 2 {
		t.Errorf("calls = %d, want 2", src.calls)
	}
}

func TestRefreshStatusBeforeAnyRefresh(t *testing.T) {
	s, _, _ := setup(t)
	st := s.RefreshStatus()
	if st.Running || st.StartedAt != nil || st.FinishedAt != nil || st.Results == nil || len(st.Results) != 0 {
		t.Errorf("idle status = %+v", st)
	}
}

func waitUntilIdle(t *testing.T, s *Scheduler) RefreshStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if st := s.RefreshStatus(); !st.Running {
			return st
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("refresh did not finish")
	return RefreshStatus{}
}

func TestStartRefreshRunsOnceAndReportsTheOutcome(t *testing.T) {
	src := &fakeSource{
		name: "a", interval: 6 * time.Hour,
		jobs:    []*domain.Job{job("1"), job("2")},
		started: make(chan struct{}, 1), block: make(chan struct{}),
	}
	s, _, _ := setup(t, src)
	s.Now = time.Now // new matches are counted against real first-seen times

	first := s.StartRefresh()
	if !first.Running || first.StartedAt == nil {
		t.Fatalf("StartRefresh = %+v, want running with a start time", first)
	}
	<-src.started // the poll is now in flight

	// A second click while one runs must not start another.
	if second := s.StartRefresh(); !second.Running {
		t.Errorf("second StartRefresh = %+v, want it to report the running refresh", second)
	}
	if st := s.RefreshStatus(); !st.Running || st.FinishedAt != nil {
		t.Errorf("status while running = %+v", st)
	}

	close(src.block)
	done := waitUntilIdle(t, s)
	if src.calls != 1 {
		t.Errorf("source fetched %d times, want 1", src.calls)
	}
	if done.NewJobs != 2 || done.NewMatches != 2 || done.FinishedAt == nil || len(done.Results) != 1 {
		t.Errorf("finished status = %+v, want 2 new jobs, 2 new matches, one result", done)
	}
}

func TestStartRefreshCanRunAgainAfterFinishing(t *testing.T) {
	src := &fakeSource{name: "a", interval: time.Hour, jobs: []*domain.Job{job("1")}}
	s, _, _ := setup(t, src)
	s.Now = time.Now
	s.RefreshMinAge = time.Nanosecond

	s.StartRefresh()
	waitUntilIdle(t, s)
	time.Sleep(5 * time.Millisecond)
	s.StartRefresh()
	waitUntilIdle(t, s)
	if src.calls != 2 {
		t.Errorf("calls = %d, want 2 (one per refresh)", src.calls)
	}
}

func TestRefreshReportsFailuresPerSource(t *testing.T) {
	good := &fakeSource{name: "good", interval: time.Hour, jobs: []*domain.Job{job("1")}}
	bad := &fakeSource{name: "bad", interval: time.Hour, err: errors.New("503")}
	s, _, _ := setup(t, good, bad)
	s.Now = time.Now

	s.StartRefresh()
	done := waitUntilIdle(t, s)
	if len(done.Results) != 2 {
		t.Fatalf("results = %+v", done.Results)
	}
	if r := done.Results[0]; r.Error != "" || r.NewJobs != 1 {
		t.Errorf("good = %+v", r)
	}
	if r := done.Results[1]; r.Error != "503" || r.NewJobs != 0 {
		t.Errorf("bad = %+v", r)
	}
	if done.NewJobs != 1 {
		t.Errorf("new jobs = %d, want 1", done.NewJobs)
	}
}
