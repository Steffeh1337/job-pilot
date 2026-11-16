package httpapi

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMarkingAJobApplied(t *testing.T) {
	a, _ := newAPI(t)
	a.Now = func() time.Time { return time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC) }
	put := func(id, body string) int {
		return do(a, http.MethodPut, "/api/jobs/"+id+"/application", strings.NewReader(body)).Code
	}

	// An empty body means "applied today".
	rec := do(a, http.MethodPut, "/api/jobs/1/application", strings.NewReader(`{}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("mark applied = %d %s", rec.Code, rec.Body)
	}
	app := decode[applicationView](t, rec)
	if app.Status != "applied" || app.AppliedOn != "2026-11-20" {
		t.Errorf("application = %+v", app)
	}

	// It shows on the job, in the list, and can be filtered on.
	var one jobView
	get(t, a, "/api/jobs/1", &one)
	if one.Application == nil || one.Application.Status != "applied" {
		t.Errorf("detail = %+v", one.Application)
	}
	var list struct {
		Total int       `json:"total"`
		Jobs  []jobView `json:"jobs"`
	}
	get(t, a, "/api/jobs?status=all", &list)
	for _, j := range list.Jobs {
		if (j.ID == 1) != (j.Application != nil) {
			t.Errorf("job %d application = %+v", j.ID, j.Application)
		}
	}
	get(t, a, "/api/jobs?status=all&applied=1", &list)
	if list.Total != 1 || list.Jobs[0].ID != 1 {
		t.Errorf("applied=1 -> %+v", list)
	}
	get(t, a, "/api/jobs?status=all&applied=0", &list)
	if list.Total != 1 || list.Jobs[0].ID == 1 {
		t.Errorf("applied=0 -> %+v", list)
	}

	// Moving it along and adding notes.
	rec = do(a, http.MethodPut, "/api/jobs/1/application", strings.NewReader(`{"status":"Interviewing","applied_on":"2026-11-15","notes":"  call on Monday "}`))
	app = decode[applicationView](t, rec)
	if rec.Code != 200 || app.Status != "interviewing" || app.AppliedOn != "2026-11-15" || app.Notes != "call on Monday" {
		t.Errorf("update = %d %+v", rec.Code, app)
	}

	// Removing it puts the job back as not applied.
	if rec := do(a, http.MethodDelete, "/api/jobs/1/application", nil); rec.Code != http.StatusNoContent {
		t.Errorf("delete = %d", rec.Code)
	}
	if rec := do(a, http.MethodDelete, "/api/jobs/1/application", nil); rec.Code != http.StatusNoContent {
		t.Errorf("deleting twice = %d, want 204", rec.Code)
	}
	one = jobView{} // decoding into a used struct would keep the old value
	get(t, a, "/api/jobs/1", &one)
	if one.Application != nil {
		t.Errorf("still applied: %+v", one.Application)
	}

	for name, tc := range map[string]struct {
		id, body string
		want     int
	}{
		"unknown job":    {"9999", `{}`, 404},
		"bad id":         {"abc", `{}`, 400},
		"bad status":     {"1", `{"status":"hired"}`, 400},
		"bad date":       {"1", `{"applied_on":"yesterday"}`, 400},
		"future date":    {"1", `{"applied_on":"2026-12-25"}`, 400},
		"not JSON":       {"1", `nope`, 400},
		"too many notes": {"1", `{"notes":"` + strings.Repeat("x", 2001) + `"}`, 400},
	} {
		if got := put(tc.id, tc.body); got != tc.want {
			t.Errorf("%s: %d, want %d", name, got, tc.want)
		}
	}
	if rec := do(a, http.MethodGet, "/api/jobs?applied=yes", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("applied=yes = %d, want 400", rec.Code)
	}
}
