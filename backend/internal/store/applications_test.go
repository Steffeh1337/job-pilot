package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Steffeh1337/job-pilot/backend/internal/domain"
)

func job(ext, title string) domain.Job {
	return domain.Job{Source: "test", ExternalID: ext, Title: title, Company: "Co", Stage: domain.StageSenior, Fingerprint: ext}
}

func TestApplicationRoundTrip(t *testing.T) {
	st, ctx := open(t), context.Background()
	id := insert(t, st, job("1", "Go dev"))

	if err := st.SaveApplication(ctx, Application{JobID: id, Status: "applied", AppliedOn: "2026-11-10", Notes: "via LinkedIn"}); err != nil {
		t.Fatal(err)
	}
	got, err := st.ApplicationsFor(ctx, []int64{id, 999})
	if err != nil || len(got) != 1 || got[id].Status != "applied" || got[id].AppliedOn != "2026-11-10" || got[id].Notes != "via LinkedIn" {
		t.Fatalf("got %+v, err %v", got, err)
	}

	// Saving again updates the same record rather than adding a second.
	if err := st.SaveApplication(ctx, Application{JobID: id, Status: "interviewing", AppliedOn: "2026-11-10"}); err != nil {
		t.Fatal(err)
	}
	got, _ = st.ApplicationsFor(ctx, []int64{id})
	if n, _ := st.ApplicationCount(ctx); n != 1 || got[id].Status != "interviewing" || got[id].Notes != "" {
		t.Errorf("count %d, got %+v", n, got[id])
	}

	if err := st.DeleteApplication(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteApplication(ctx, id); err != nil {
		t.Errorf("deleting twice should be fine: %v", err)
	}
	if got, _ := st.ApplicationsFor(ctx, []int64{id}); len(got) != 0 {
		t.Errorf("still there: %+v", got)
	}
}

func TestSearchByApplied(t *testing.T) {
	st, ctx := open(t), context.Background()
	a := insert(t, st, job("a", "Alpha"))
	b := insert(t, st, job("b", "Beta"))
	c := insert(t, st, job("c", "Gamma"))
	dropped := job("d", "Delta")
	dropped.Stage, dropped.RejectReason = domain.StageRegion, "role"
	d := insert(t, st, dropped)
	st.SaveApplication(ctx, Application{JobID: a, Status: "applied", AppliedOn: "2026-11-01"})
	st.SaveApplication(ctx, Application{JobID: c, Status: "applied", AppliedOn: "2026-11-12"})
	st.SaveApplication(ctx, Application{JobID: d, Status: "rejected", AppliedOn: "2026-11-05"})

	ids := func(q Search) []int64 {
		t.Helper()
		got, err := st.SearchIDs(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	eq := func(got, want []int64) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	// The tracker lists the most recently applied first, and includes jobs that
	// the filters would otherwise hide.
	if got := ids(Search{Status: StatusAll, Applied: AppliedOnly}); !eq(got, []int64{c, d, a}) {
		t.Errorf("applied only = %v, want %v", got, []int64{c, d, a})
	}
	if got := ids(Search{Status: StatusMatch, Applied: AppliedHide}); !eq(got, []int64{b}) {
		t.Errorf("hide applied = %v, want %v", got, []int64{b})
	}
	if got := ids(Search{Status: StatusMatch}); len(got) != 3 {
		t.Errorf("no filter = %v", got)
	}
	page, total, err := st.Search(ctx, Search{Status: StatusAll, Applied: AppliedOnly, Limit: 2})
	if err != nil || total != 3 || len(page) != 2 || page[0].ID != c {
		t.Errorf("paged tracker: total %d, %v, err %v", total, page, err)
	}
}

// Applications are the one record that can't be fetched again, so a save must
// be in the main database file, not only in the write-ahead log.
func TestApplicationSurvivesLosingTheLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobpilot.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	id := insert(t, st, job("1", "Go dev"))
	if err := st.SaveApplication(context.Background(), Application{JobID: id, Status: "applied", AppliedOn: "2026-11-10", Notes: "ZZ-NOTE-MARKER"}); err != nil {
		t.Fatal(err)
	}

	survivor, err := Open(copyMainFileOnly(t, path))
	if err != nil {
		t.Fatal(err)
	}
	defer survivor.Close()
	got, _ := survivor.ApplicationsFor(context.Background(), []int64{id})
	if got[id].Notes != "ZZ-NOTE-MARKER" {
		t.Errorf("the application was lost with the log: %+v", got)
	}
}
