package httpapi

import (
	"context"
	"net/http"
	"slices"
	"testing"
)

func TestViewingAndUnreadingAJob(t *testing.T) {
	a, _ := newAPI(t)
	type list struct {
		Total int       `json:"total"`
		Jobs  []jobView `json:"jobs"`
	}

	var one jobView
	get(t, a, "/api/jobs/1", &one)
	if one.Viewed {
		t.Fatal("a job nobody opened is not viewed")
	}
	// Fetching a job doesn't count as viewing it: the page says so explicitly.
	get(t, a, "/api/jobs/1", &one)
	if one.Viewed {
		t.Error("reading the job must not mark it viewed")
	}

	if rec := do(a, http.MethodPut, "/api/jobs/1/viewed", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("mark viewed = %d %s", rec.Code, rec.Body)
	}
	get(t, a, "/api/jobs/1", &one)
	if !one.Viewed {
		t.Error("detail should say viewed")
	}
	var l list
	get(t, a, "/api/jobs?status=all", &l)
	for _, j := range l.Jobs {
		if j.Viewed != (j.ID == 1) {
			t.Errorf("job %d viewed = %v", j.ID, j.Viewed)
		}
	}
	// The viewed job is last even though it was seen first.
	if l.Total != 2 || l.Jobs[len(l.Jobs)-1].ID != 1 {
		t.Errorf("order = %v, want job 1 last", []int64{l.Jobs[0].ID, l.Jobs[1].ID})
	}

	if rec := do(a, http.MethodDelete, "/api/jobs/1/viewed", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("mark unread = %d", rec.Code)
	}
	if rec := do(a, http.MethodDelete, "/api/jobs/1/viewed", nil); rec.Code != http.StatusNoContent {
		t.Errorf("marking unread twice = %d, want 204", rec.Code)
	}
	get(t, a, "/api/jobs/1", &one)
	if one.Viewed {
		t.Error("should be unread again")
	}

	if rec := do(a, http.MethodPut, "/api/jobs/999/viewed", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown job = %d, want 404", rec.Code)
	}
	if rec := do(a, http.MethodPut, "/api/jobs/x/viewed", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("bad id = %d, want 400", rec.Code)
	}
}

func TestUnviewedFirstKeepsOrderInsideEachGroup(t *testing.T) {
	a, st := newAPI(t)
	ctx := context.Background()
	st.MarkViewed(ctx, 2, a.now())
	st.MarkViewed(ctx, 5, a.now())

	got, err := a.unviewedFirst(ctx, []int64{5, 1, 2, 4, 3})
	if err != nil || !slices.Equal(got, []int64{1, 4, 3, 5, 2}) {
		t.Errorf("got %v, %v; want [1 4 3 5 2]", got, err)
	}
	same, _ := a.unviewedFirst(ctx, []int64{3, 1})
	if !slices.Equal(same, []int64{3, 1}) {
		t.Errorf("nothing viewed must leave the order alone: %v", same)
	}
}
