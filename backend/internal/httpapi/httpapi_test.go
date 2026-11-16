package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Steffeh1337/job-pilot/backend/internal/config"
	"github.com/Steffeh1337/job-pilot/backend/internal/domain"
	"github.com/Steffeh1337/job-pilot/backend/internal/pipeline"
	"github.com/Steffeh1337/job-pilot/backend/internal/scheduler"
	"github.com/Steffeh1337/job-pilot/backend/internal/store"
)

func newAPI(t *testing.T) (*API, *store.Store) {
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
	_, err = pipeline.New(f, st).Ingest(context.Background(), []*domain.Job{
		{Source: "remoteok", ExternalID: "1", URL: "https://x/1", Title: "Senior Go Engineer", Company: "Acme",
			Remote: true, Regions: []string{"Worldwide"}, Description: "Go"},
		{Source: "remoteok", ExternalID: "2", URL: "https://x/2", Title: "Sales Lead", Company: "Beta",
			Remote: true, Regions: []string{"Worldwide"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &API{Store: st, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, st
}

func get(t *testing.T, a *API, path string, into any) {
	t.Helper()
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", path, rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
		t.Fatalf("GET %s: bad JSON: %v", path, err)
	}
}

func TestFunnelEndpoint(t *testing.T) {
	a, _ := newAPI(t)
	var res struct {
		Rows     []store.FunnelRow `json:"rows"`
		Rejected map[string]int    `json:"rejected"`
	}
	get(t, a, "/api/funnel?days=7", &res)
	total := res.Rows[len(res.Rows)-1]
	if total.Source != "TOTAL" || total.New != 2 || total.Senior != 1 {
		t.Errorf("total = %+v", total)
	}
	if res.Rejected["role"] != 1 {
		t.Errorf("rejected = %v, want one role rejection", res.Rejected)
	}
}

func TestFunnelWindowExcludesOldJobs(t *testing.T) {
	a, _ := newAPI(t)
	a.Now = func() time.Time { return time.Now().AddDate(0, 0, 30) }
	var res struct {
		Rows []store.FunnelRow `json:"rows"`
	}
	get(t, a, "/api/funnel?days=7", &res)
	if total := res.Rows[len(res.Rows)-1]; total.New != 0 {
		t.Errorf("jobs seen 30 days ago counted in a 7-day window: %+v", total)
	}
}

func TestMatchesEndpoint(t *testing.T) {
	a, _ := newAPI(t)
	var res []jobView
	get(t, a, "/api/matches", &res)
	if len(res) != 1 || res[0].Title != "Senior Go Engineer" {
		t.Errorf("matches = %+v", res)
	}
}

func TestEmptyListsAreArraysNotNull(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	a := &API{Store: st, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for _, path := range []string{"/api/matches", "/api/polls"} {
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if got := rec.Body.String(); got != "[]\n" {
			t.Errorf("GET %s = %q, want []", path, got)
		}
	}
}

func TestHealthAndMethods(t *testing.T) {
	a, _ := newAPI(t)
	var res map[string]string
	get(t, a, "/api/health", &res)
	if res["status"] != "ok" {
		t.Errorf("health = %v", res)
	}
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/funnel", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/funnel = %d, want 405", rec.Code)
	}
}

func TestJobsEndpointFilters(t *testing.T) {
	a, _ := newAPI(t)
	type page struct {
		Total int       `json:"total"`
		Jobs  []jobView `json:"jobs"`
	}
	tests := []struct {
		query     string
		wantTotal int
		wantTitle string
	}{
		{"", 1, "Senior Go Engineer"},
		{"?status=all", 2, ""},
		{"?status=rejected", 1, "Sales Lead"},
		{"?status=rejected&reason=role", 1, "Sales Lead"},
		{"?status=rejected&reason=stack", 0, ""},
		{"?status=all&q=acme", 1, "Senior Go Engineer"},
		{"?b2b=1", 0, ""},
		{"?status=all&source=remoteok&limit=1", 2, ""},
		{"?status=all&days=7", 2, ""},
	}
	for _, tt := range tests {
		var res page
		get(t, a, "/api/jobs"+tt.query, &res)
		if res.Total != tt.wantTotal {
			t.Errorf("%q: total = %d, want %d", tt.query, res.Total, tt.wantTotal)
		}
		if tt.wantTitle != "" && (len(res.Jobs) == 0 || res.Jobs[0].Title != tt.wantTitle) {
			t.Errorf("%q: jobs = %+v, want first %q", tt.query, res.Jobs, tt.wantTitle)
		}
		for _, j := range res.Jobs {
			if j.Description != "" {
				t.Errorf("%q: list responses must not carry descriptions", tt.query)
			}
		}
	}

	var limited page
	get(t, a, "/api/jobs?status=all&limit=1", &limited)
	if len(limited.Jobs) != 1 || limited.Total != 2 {
		t.Errorf("limit=1: %d jobs, total %d; want 1 and 2", len(limited.Jobs), limited.Total)
	}
}

func TestJobsRejectsBadStatus(t *testing.T) {
	a, _ := newAPI(t)
	if code := status(a, "/api/jobs?status=bogus"); code != http.StatusBadRequest {
		t.Errorf("bad status = %d, want 400", code)
	}
}

func TestJobDetail(t *testing.T) {
	a, _ := newAPI(t)
	var j jobView
	get(t, a, "/api/jobs/1", &j)
	if j.Description != "Go" || j.Stage != "senior" || j.Title != "Senior Go Engineer" {
		t.Errorf("detail = %+v", j)
	}
	if code := status(a, "/api/jobs/9999"); code != http.StatusNotFound {
		t.Errorf("missing job = %d, want 404", code)
	}
	if code := status(a, "/api/jobs/abc"); code != http.StatusBadRequest {
		t.Errorf("bad id = %d, want 400", code)
	}
}

func TestPollsIncludeSourcesThatHaveNotRun(t *testing.T) {
	a, st := newAPI(t)
	a.Intervals = map[string]time.Duration{"remoteok": 30 * time.Minute, "lever": time.Hour}
	if err := st.RecordPoll(context.Background(), store.Poll{Source: "remoteok", StartedAt: time.Now(), Fetched: 99, NewJobs: 5}); err != nil {
		t.Fatal(err)
	}
	var res []pollView
	get(t, a, "/api/polls", &res)
	if len(res) != 2 || res[0].Source != "lever" || res[1].Source != "remoteok" {
		t.Fatalf("polls = %+v, want lever then remoteok", res)
	}
	if res[0].StartedAt != nil || res[0].IntervalSeconds != 3600 {
		t.Errorf("unpolled source = %+v", res[0])
	}
	if res[1].StartedAt == nil || res[1].Fetched != 99 || res[1].IntervalSeconds != 1800 {
		t.Errorf("polled source = %+v", res[1])
	}
}

func TestServesDashboardWithClientRouteFallback(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"index.html": "<html>app</html>", "assets/app.js": "console.log(1)"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a, _ := newAPI(t)
	a.WebDir = dir
	for path, want := range map[string]string{
		"/":              "<html>app</html>",
		"/matches":       "<html>app</html>", // client-side route
		"/assets/app.js": "console.log(1)",
	} {
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != want {
			t.Errorf("GET %s = %d %q, want %q", path, rec.Code, rec.Body, want)
		}
	}
	// API paths must never fall through to the app.
	if code := status(a, "/api/nope"); code != http.StatusNotFound {
		t.Errorf("/api/nope with web dir = %d, want 404", code)
	}
}

func TestNoDashboardWhenWebDirMissing(t *testing.T) {
	a, _ := newAPI(t)
	a.WebDir = filepath.Join(t.TempDir(), "missing")
	if code := status(a, "/"); code != http.StatusNotFound {
		t.Errorf("GET / = %d, want 404 when there is no build", code)
	}
}

func status(a *API, path string) int {
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code
}

type fakeRefresher struct {
	started int
	status  scheduler.RefreshStatus
}

func (f *fakeRefresher) StartRefresh() scheduler.RefreshStatus {
	f.started++
	f.status.Running = true
	return f.status
}

func (f *fakeRefresher) RefreshStatus() scheduler.RefreshStatus { return f.status }

func TestRefreshEndpoints(t *testing.T) {
	a, _ := newAPI(t)
	fr := &fakeRefresher{status: scheduler.RefreshStatus{NewJobs: 3, NewMatches: 1, Results: []scheduler.PollResult{{Source: "lever", Skipped: true}}}}
	a.Refresher = fr

	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/refresh", nil))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST /api/refresh = %d, want 202", rec.Code)
	}
	var started scheduler.RefreshStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &started); err != nil || !started.Running {
		t.Fatalf("POST body = %s (err %v), want running", rec.Body, err)
	}
	if fr.started != 1 {
		t.Errorf("StartRefresh called %d times, want 1", fr.started)
	}

	var st scheduler.RefreshStatus
	get(t, a, "/api/refresh", &st)
	if st.NewJobs != 3 || st.NewMatches != 1 || len(st.Results) != 1 || !st.Results[0].Skipped {
		t.Errorf("GET /api/refresh = %+v", st)
	}
	if fr.started != 1 {
		t.Error("GET must not start a refresh")
	}
}

func TestRefreshUnavailableWithoutAPoller(t *testing.T) {
	a, _ := newAPI(t)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, httptest.NewRequest(method, "/api/refresh", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s /api/refresh = %d, want 503", method, rec.Code)
		}
	}
}

func TestRefreshRejectsOtherMethods(t *testing.T) {
	a, _ := newAPI(t)
	a.Refresher = &fakeRefresher{}
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/refresh", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE /api/refresh = %d, want 405", rec.Code)
	}
}
