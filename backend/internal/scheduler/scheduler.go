// Package scheduler polls sources on a fixed tick, respecting each source's
// own minimum interval.
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

type Scheduler struct {
	Sources  []source.Source
	Pipeline *pipeline.Pipeline
	Store    *store.Store
	Tick     time.Duration
	Log      *slog.Logger
	Now      func() time.Time
}

// Run polls immediately, then on every tick, until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	if s.Now == nil {
		s.Now = time.Now
	}
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
func (s *Scheduler) RunOnce(ctx context.Context) {
	if s.Now == nil {
		s.Now = time.Now
	}
	var wg sync.WaitGroup
	for _, src := range s.Sources {
		due, err := s.due(ctx, src)
		if err != nil {
			s.Log.Error("check last poll", "source", src.Name(), "err", err)
			continue
		}
		if !due {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.poll(ctx, src)
		}()
	}
	wg.Wait()
}

func (s *Scheduler) due(ctx context.Context, src source.Source) (bool, error) {
	last, err := s.Store.LastPoll(ctx, src.Name())
	if err != nil || last == nil {
		return true, err
	}
	// Failed polls count too, so a broken source is retried at its normal
	// pace instead of every tick.
	return s.Now().Sub(last.StartedAt) >= src.Interval(), nil
}

func (s *Scheduler) poll(ctx context.Context, src source.Source) {
	start := s.Now()
	jobs, fetchErr := src.Fetch(ctx)
	// Partial results are still ingested.
	added, ingestErr := s.Pipeline.Ingest(ctx, jobs)

	p := store.Poll{
		Source: src.Name(), StartedAt: start, Duration: s.Now().Sub(start),
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
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
