// Package pipeline turns fetched listings into classified, stored jobs.
package pipeline

import (
	"context"
	"sync"
	"time"

	"github.com/Steffeh1337/job-pilot/backend/internal/domain"
	"github.com/Steffeh1337/job-pilot/backend/internal/store"
)

type Pipeline struct {
	filter *Filter
	store  *store.Store
	now    func() time.Time

	// Sources are polled concurrently; ingest is serialised so two sources
	// carrying the same role can't both miss the duplicate check.
	mu sync.Mutex
}

func New(f *Filter, s *store.Store) *Pipeline {
	return &Pipeline{filter: f, store: s, now: time.Now}
}

// Ingest stores jobs not seen before and returns how many were new. Jobs
// already stored only get their last_seen_at refreshed, so their funnel stage
// stays as first judged; use Reclassify after changing filters.
func (p *Pipeline) Ingest(ctx context.Context, jobs []*domain.Job) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := p.now()
	added := 0
	for _, j := range jobs {
		if j.ExternalID == "" || j.Title == "" {
			continue
		}
		seen, err := p.store.Seen(ctx, j.Source, j.ExternalID, now)
		if err != nil {
			return added, err
		}
		if seen {
			continue
		}
		j.Fingerprint = domain.Fingerprint(j.Company, j.Title)
		if j.DupOf, err = p.store.FindDuplicate(ctx, j.Fingerprint); err != nil {
			return added, err
		}
		j.FirstSeenAt, j.LastSeenAt = now, now
		p.filter.Classify(j)
		if err := p.store.Insert(ctx, j); err != nil {
			return added, err
		}
		added++
	}
	return added, nil
}

// Reclassify re-runs the filters over every stored job, for tuning them
// against what has already been collected. It returns the number of jobs
// whose stage changed.
func (p *Pipeline) Reclassify(ctx context.Context) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	jobs, err := p.store.AllJobs(ctx)
	if err != nil {
		return 0, err
	}
	changed := 0
	for _, j := range jobs {
		before := j.Stage
		p.filter.Classify(j)
		if err := p.store.UpdateClassification(ctx, j); err != nil {
			return changed, err
		}
		if j.Stage != before {
			changed++
		}
	}
	return changed, nil
}
