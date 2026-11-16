// Package scheduler polls sources on a fixed tick, respecting each source's
// own minimum interval, and can also poll them on demand.
package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Steffeh1337/job-pilot/backend/internal/pipeline"
	"github.com/Steffeh1337/job-pilot/backend/internal/source"
	"github.com/Steffeh1337/job-pilot/backend/internal/store"
)

// DefaultRefreshMinAge is how recently a source must have been polled for a
// manual refresh to leave it alone. It keeps repeated clicks from hammering
// free APIs that ask for sparing use.
const DefaultRefreshMinAge = 5 * time.Minute

type Scheduler struct {
	Sources  []source.Source
	Pipeline *pipeline.Pipeline
	Store    *store.Store
	Tick     time.Duration
	Log      *slog.Logger
	Now      func() time.Time
	// Ctx bounds background refreshes; they stop when it is cancelled. Nil
	// means context.Background().
	Ctx context.Context
	// RefreshMinAge overrides DefaultRefreshMinAge when positive.
	RefreshMinAge time.Duration

	runMu sync.Mutex // one poll round at a time, scheduled or manual

	mu      sync.Mutex // guards refresh
	refresh RefreshStatus
}

// PollResult is the outcome of one source in one round.
type PollResult struct {
	Source  string `json:"source"`
	Fetched int    `json:"fetched"`
	NewJobs int    `json:"new_jobs"`
	// Skipped is set when the source was left alone because it was polled
	// very recently.
	Skipped bool   `json:"skipped,omitempty"`
	Error   string `json:"error,omitempty"`
}

// RefreshStatus describes the latest manual refresh.
type RefreshStatus struct {
	Running    bool         `json:"running"`
	StartedAt  *time.Time   `json:"started_at"`
	FinishedAt *time.Time   `json:"finished_at"`
	NewJobs    int          `json:"new_jobs"`
	NewMatches int          `json:"new_matches"`
	Results    []PollResult `json:"results"`
}

func (s *Scheduler) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Run polls immediately, then on every tick, until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	s.RunOnce(ctx)
	t := time.NewTicker(s.Tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.RunOnce(ctx)
		}
	}
}

// RunOnce polls every source that is due, concurrently, and waits for them.
func (s *Scheduler) RunOnce(ctx context.Context) []PollResult {
	return s.round(ctx, func(src source.Source) (bool, error) {
		last, err := s.Store.LastPoll(ctx, src.Name())
		if err != nil || last == nil {
			return true, err
		}
		// Failed polls count too, so a broken source is retried at its
		// normal pace instead of every tick.
		return s.now().Sub(last.StartedAt) >= src.Interval(), nil
	})
}

// Refresh polls every source now, regardless of its interval, except those
// polled within RefreshMinAge. It waits for them to finish.
func (s *Scheduler) Refresh(ctx context.Context) []PollResult {
	minAge := s.RefreshMinAge
	if minAge <= 0 {
		minAge = DefaultRefreshMinAge
	}
	return s.round(ctx, func(src source.Source) (bool, error) {
		last, err := s.Store.LastPoll(ctx, src.Name())
		if err != nil || last == nil {
			return true, err
		}
		return s.now().Sub(last.StartedAt) >= minAge, nil
	})
}

// StartRefresh begins a background refresh and returns at once. If one is
// already running it does nothing and reports that one.
func (s *Scheduler) StartRefresh() RefreshStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refresh.Running {
		return s.snapshot()
	}
	started := s.now()
	s.refresh = RefreshStatus{Running: true, StartedAt: &started, Results: []PollResult{}}

	ctx := s.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		results := s.Refresh(ctx)
		newJobs := 0
		for _, r := range results {
			newJobs += r.NewJobs
		}
		// New matches are jobs first seen during this refresh that cleared
		// every filter.
		_, newMatches, err := s.Store.Search(ctx, store.Search{Status: store.StatusMatch, Since: started, Limit: 1})
		if err != nil {
			s.Log.Error("count new matches", "err", err)
		}
		finished := s.now()

		s.mu.Lock()
		defer s.mu.Unlock()
		s.refresh = RefreshStatus{
			Running: false, StartedAt: &started, FinishedAt: &finished,
			NewJobs: newJobs, NewMatches: newMatches, Results: results,
		}
	}()
	return s.snapshot()
}

// RefreshStatus returns the state of the latest manual refresh.
func (s *Scheduler) RefreshStatus() RefreshStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshot()
}

// snapshot copies the status so callers can't race with the background run.
// The caller must hold s.mu.
func (s *Scheduler) snapshot() RefreshStatus {
	out := s.refresh
	// Always a non-nil slice, so the JSON is [] and never null.
	out.Results = make([]PollResult, len(s.refresh.Results))
	copy(out.Results, s.refresh.Results)
	return out
}

// round polls the sources that shouldPoll accepts. Rounds never overlap, so a
// manual refresh that lands during a scheduled tick waits for it.
func (s *Scheduler) round(ctx context.Context, shouldPoll func(source.Source) (bool, error)) []PollResult {
	s.runMu.Lock()
	defer s.runMu.Unlock()

	results := make([]PollResult, len(s.Sources))
	var wg sync.WaitGroup
	for i, src := range s.Sources {
		ok, err := shouldPoll(src)
		switch {
		case err != nil:
			s.Log.Error("check last poll", "source", src.Name(), "err", err)
			results[i] = PollResult{Source: src.Name(), Error: err.Error()}
		case !ok:
			results[i] = PollResult{Source: src.Name(), Skipped: true}
		default:
			wg.Add(1)
			go func() {
				defer wg.Done()
				results[i] = s.poll(ctx, src)
			}()
		}
	}
	wg.Wait()
	return results
}

func (s *Scheduler) poll(ctx context.Context, src source.Source) PollResult {
	start := s.now()
	jobs, fetchErr := src.Fetch(ctx)
	// Partial results are still ingested.
	added, ingestErr := s.Pipeline.Ingest(ctx, jobs)

	p := store.Poll{
		Source: src.Name(), StartedAt: start, Duration: s.now().Sub(start),
		Fetched: len(jobs), NewJobs: added,
	}
	if err := firstErr(fetchErr, ingestErr); err != nil {
		p.Error = err.Error()
		s.Log.Warn("poll", "source", p.Source, "fetched", p.Fetched, "new", p.NewJobs, "err", err)
	} else {
		s.Log.Info("poll", "source", p.Source, "fetched", p.Fetched, "new", p.NewJobs, "took", p.Duration.Round(time.Millisecond))
	}
	if err := s.Store.RecordPoll(ctx, p); err != nil {
		s.Log.Error("record poll", "source", p.Source, "err", err)
	}
	return PollResult{Source: p.Source, Fetched: p.Fetched, NewJobs: p.NewJobs, Error: p.Error}
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
