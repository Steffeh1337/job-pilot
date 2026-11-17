package store

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestViewedJobsSortLast(t *testing.T) {
	st, ctx := open(t), context.Background()
	a := insert(t, st, job("a", "Alpha"))
	b := insert(t, st, job("b", "Beta"))
	c := insert(t, st, job("c", "Gamma"))
	order := func(q Search) []int64 {
		t.Helper()
		got, err := st.SearchIDs(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	if got := order(Search{}); !slices.Equal(got, []int64{c, b, a}) {
		t.Fatalf("nothing viewed: %v, want newest first", got)
	}

	// Viewing the newest job moves it below the ones still to look at.
	if err := st.MarkViewed(ctx, c, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkViewed(ctx, c, time.Now()); err != nil {
		t.Errorf("viewing twice should be fine: %v", err)
	}
	if got := order(Search{}); !slices.Equal(got, []int64{b, a, c}) {
		t.Errorf("c viewed: %v, want [b a c]", got)
	}
	// Viewed jobs keep newest-first order among themselves.
	st.MarkViewed(ctx, a, time.Now())
	if got := order(Search{}); !slices.Equal(got, []int64{b, c, a}) {
		t.Errorf("a and c viewed: %v, want [b c a]", got)
	}
	// Paging sees the same order.
	page, total, err := st.Search(ctx, Search{Limit: 1, Offset: 2})
	if err != nil || total != 3 || len(page) != 1 || page[0].ID != a {
		t.Errorf("page 3 = %v of %d, err %v; want a last", page, total, err)
	}

	got, err := st.ViewedAmong(ctx, []int64{a, b, c, 999})
	if err != nil || len(got) != 2 || !got[a] || !got[c] || got[b] {
		t.Errorf("viewed = %v, err %v", got, err)
	}

	// Marking unread brings it back to its place, and doing it twice is fine.
	if err := st.MarkUnread(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkUnread(ctx, c); err != nil {
		t.Errorf("marking unread twice should be fine: %v", err)
	}
	if got := order(Search{}); !slices.Equal(got, []int64{c, b, a}) {
		t.Errorf("c unread again: %v, want [c b a]", got)
	}
}

func TestTrackerIgnoresViewed(t *testing.T) {
	st, ctx := open(t), context.Background()
	a := insert(t, st, job("a", "Alpha"))
	b := insert(t, st, job("b", "Beta"))
	st.SaveApplication(ctx, Application{JobID: a, Status: "applied", AppliedOn: "2026-11-12"})
	st.SaveApplication(ctx, Application{JobID: b, Status: "applied", AppliedOn: "2026-11-01"})
	st.MarkViewed(ctx, a, time.Now())

	got, err := st.SearchIDs(ctx, Search{Status: StatusAll, Applied: AppliedOnly})
	if err != nil || !slices.Equal(got, []int64{a, b}) {
		t.Errorf("tracker = %v, %v; want latest applied first even though a was viewed", got, err)
	}
}

func TestViewedSurvivesReopening(t *testing.T) {
	st, ctx := open(t), context.Background()
	id := insert(t, st, job("1", "Go dev"))
	if err := st.MarkViewed(ctx, id, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.ViewedAmong(ctx, []int64{id}); !got[id] {
		t.Errorf("not remembered: %v", got)
	}
}
