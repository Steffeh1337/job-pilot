package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/Steffeh1337/job-pilot/backend/internal/domain"
)

func open(t *testing.T) *Store {
	t.Helper()
	st, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func insert(t *testing.T, st *Store, j domain.Job) int64 {
	t.Helper()
	now := time.Now()
	j.FirstSeenAt, j.LastSeenAt, j.PostedAt = now, now, now
	if err := st.Insert(context.Background(), &j); err != nil {
		t.Fatal(err)
	}
	return j.ID
}

func TestSearchFilters(t *testing.T) {
	ctx := context.Background()
	st := open(t)
	insert(t, st, domain.Job{Source: "a", ExternalID: "1", Title: "Senior Go Engineer", Company: "Acme", Stage: domain.StageSenior, B2B: true, RegionVerified: true})
	insert(t, st, domain.Job{Source: "a", ExternalID: "2", Title: "Backend Engineer", Company: "Beta", Stage: domain.StageSenior})
	insert(t, st, domain.Job{Source: "b", ExternalID: "3", Title: "Sales Lead", Company: "Gamma 100%", Stage: domain.StageRegion, RejectReason: "role"})
	insert(t, st, domain.Job{Source: "b", ExternalID: "4", Title: "Java Dev", Company: "Delta", Stage: domain.StageRegion, RejectReason: "role"})
	insert(t, st, domain.Job{Source: "b", ExternalID: "5", Title: "Rust Dev", Company: "Eps", Stage: domain.StageRole, RejectReason: "stack"})

	tests := []struct {
		name  string
		q     Search
		total int
	}{
		{"default is matches", Search{}, 2},
		{"all", Search{Status: StatusAll}, 5},
		{"rejected", Search{Status: StatusRejected}, 3},
		{"reason", Search{Status: StatusRejected, Reason: "role"}, 2},
		{"source", Search{Status: StatusAll, Source: "b"}, 3},
		{"b2b only", Search{B2BOnly: true}, 1},
		{"unverified only", Search{Unverified: true}, 1},
		{"query on title, case-insensitive", Search{Status: StatusAll, Query: "go eng"}, 1},
		{"query on company", Search{Status: StatusAll, Query: "BETA"}, 1},
		{"percent is literal, not a wildcard", Search{Status: StatusAll, Query: "100%"}, 1},
		{"underscore is literal", Search{Status: StatusAll, Query: "_"}, 0},
		{"since in the future", Search{Status: StatusAll, Since: time.Now().Add(time.Hour)}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobs, total, err := st.Search(ctx, tt.q)
			if err != nil {
				t.Fatal(err)
			}
			if total != tt.total || len(jobs) != tt.total {
				t.Errorf("total = %d, rows = %d, want %d", total, len(jobs), tt.total)
			}
		})
	}
}

func TestSearchPagination(t *testing.T) {
	ctx := context.Background()
	st := open(t)
	for i := 0; i < 5; i++ {
		insert(t, st, domain.Job{Source: "a", ExternalID: string(rune('a' + i)), Title: "T", Stage: domain.StageSenior})
	}
	page1, total, err := st.Search(ctx, Search{Limit: 2})
	if err != nil || total != 5 || len(page1) != 2 {
		t.Fatalf("page1: %d rows, total %d, err %v", len(page1), total, err)
	}
	page3, _, _ := st.Search(ctx, Search{Limit: 2, Offset: 4})
	if len(page3) != 1 {
		t.Errorf("last page has %d rows, want 1", len(page3))
	}
	if page1[0].ID == page3[0].ID {
		t.Error("pages overlap")
	}
}

func TestJobLookup(t *testing.T) {
	ctx := context.Background()
	st := open(t)
	id := insert(t, st, domain.Job{Source: "a", ExternalID: "1", Title: "T", Description: "full text", RegionVerified: true})
	j, err := st.Job(ctx, id)
	if err != nil || j == nil || j.Description != "full text" || !j.RegionVerified {
		t.Fatalf("Job = %+v, %v", j, err)
	}
	if j, err := st.Job(ctx, 9999); j != nil || err != nil {
		t.Errorf("missing job = %+v, %v; want nil, nil", j, err)
	}
}

// A database written by the first release has no region_verified column.
func TestOpenMigratesOlderDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`CREATE TABLE jobs (
		id INTEGER PRIMARY KEY, source TEXT NOT NULL, external_id TEXT NOT NULL, url TEXT NOT NULL,
		title TEXT NOT NULL, company TEXT NOT NULL, location TEXT NOT NULL, regions TEXT NOT NULL,
		remote INTEGER NOT NULL, employment_type TEXT NOT NULL, tags TEXT NOT NULL, description TEXT NOT NULL,
		salary_min INTEGER NOT NULL, salary_max INTEGER NOT NULL, currency TEXT NOT NULL,
		posted_at INTEGER NOT NULL, first_seen_at INTEGER NOT NULL, last_seen_at INTEGER NOT NULL,
		fingerprint TEXT NOT NULL, dup_of INTEGER NOT NULL DEFAULT 0, stage INTEGER NOT NULL,
		b2b INTEGER NOT NULL, reject_reason TEXT NOT NULL, UNIQUE (source, external_id));
	INSERT INTO jobs VALUES (1,'a','1','u','Old job','Acme','','[]',1,'','[]','d',0,0,'',0,0,0,'acme|old job',0,5,0,'');`)
	if err != nil {
		t.Fatal(err)
	}
	raw.Close()

	st, err := Open(path)
	if err != nil {
		t.Fatalf("open old database: %v", err)
	}
	defer st.Close()
	j, err := st.Job(context.Background(), 1)
	if err != nil || j == nil || j.Title != "Old job" || j.RegionVerified {
		t.Fatalf("migrated row = %+v, %v", j, err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st2, err := Open(path) // opening twice must not try to add the column again
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	st2.Close()
}
