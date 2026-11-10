package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Steffeh1337/job-pilot/backend/internal/config"
	"github.com/Steffeh1337/job-pilot/backend/internal/domain"
	"github.com/Steffeh1337/job-pilot/backend/internal/pipeline"
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
	var res []matchView
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
