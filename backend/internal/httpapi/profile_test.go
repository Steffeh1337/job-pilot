package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Steffeh1337/job-pilot/backend/internal/cv"
	"github.com/Steffeh1337/job-pilot/backend/internal/matcher"
	"github.com/Steffeh1337/job-pilot/backend/internal/skills"
	"github.com/Steffeh1337/job-pilot/backend/internal/store"
)

const apiCV = `Jane Developer
Senior Backend Engineer
8 years of experience. Go, TypeScript, PostgreSQL and Docker. Go and Docker daily.`

// matchingAPI is an API with a real matcher that uses keywords only (no Ollama).
func matchingAPI(t *testing.T) (*API, *matcher.Matcher, *store.Store) {
	t.Helper()
	a, st := newAPI(t)
	m := &matcher.Matcher{
		Store: st, Vocab: skills.Default(), Cfg: matcher.DefaultConfig(),
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	a.Matcher = m
	return a, m, st
}

func multipartBody(t *testing.T, field, filename string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	part.Write(data)
	w.Close()
	return &buf, w.FormDataContentType()
}

func upload(t *testing.T, a *API, field, filename string, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	body, ctype := multipartBody(t, field, filename, data)
	req := httptest.NewRequest(http.MethodPost, "/api/profile/cv", body)
	req.Header.Set("Content-Type", ctype)
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	return rec
}

func do(a *API, method, path string, body io.Reader) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("bad JSON %q: %v", rec.Body, err)
	}
	return v
}

func errMsg(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	return decode[map[string]string](t, rec)["error"]
}

func TestUploadAndReadBackAProfile(t *testing.T) {
	a, _, _ := matchingAPI(t)

	if p := decode[matcher.ProfileView](t, do(a, http.MethodGet, "/api/profile", nil)); p.Exists {
		t.Fatal("no profile yet")
	}
	rec := upload(t, a, "cv", "Jane CV.txt", []byte(apiCV))
	if rec.Code != http.StatusOK {
		t.Fatalf("upload = %d: %s", rec.Code, rec.Body)
	}
	res := decode[profileResponse](t, rec)
	if !res.Profile.Exists || res.Profile.CVFilename != "Jane CV.txt" || res.Profile.YearsExperience != 8 {
		t.Errorf("profile = %+v", res.Profile)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "keywords only") {
		t.Errorf("warnings = %v", res.Warnings)
	}
	names := map[string]bool{}
	for _, s := range res.Profile.Skills {
		names[s.Name] = true
	}
	if !names["Go"] || !names["PostgreSQL"] || !names["Docker"] {
		t.Errorf("skills = %v", names)
	}

	got := decode[matcher.ProfileView](t, do(a, http.MethodGet, "/api/profile", nil))
	if !got.Exists || got.CVChars == 0 || !strings.Contains(got.CVPreview, "Senior Backend Engineer") {
		t.Errorf("GET /api/profile = %+v", got)
	}
}

func TestUploadErrors(t *testing.T) {
	a, _, _ := matchingAPI(t)

	if rec := upload(t, a, "resume", "cv.txt", []byte(apiCV)); rec.Code != http.StatusBadRequest || !strings.Contains(errMsg(t, rec), "named cv") {
		t.Errorf("wrong field name = %d %s", rec.Code, rec.Body)
	}
	if rec := do(a, http.MethodPost, "/api/profile/cv", strings.NewReader(`{"cv":"text"}`)); rec.Code != http.StatusBadRequest {
		t.Errorf("not multipart = %d", rec.Code)
	}
	if rec := upload(t, a, "cv", "logo.png", append([]byte{0x89, 'P', 'N', 'G', 0}, make([]byte, 80)...)); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(errMsg(t, rec), "unsupported") {
		t.Errorf("binary file = %d %s", rec.Code, rec.Body)
	}
	if rec := upload(t, a, "cv", "empty.pdf", nil); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("empty file = %d %s", rec.Code, rec.Body)
	}
	if rec := upload(t, a, "cv", "big.txt", bytes.Repeat([]byte("a "), (cv.MaxBytes+(2<<20))/2)); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("huge upload = %d, want 413", rec.Code)
	}
	if rec := upload(t, a, "cv", "tiny.txt", []byte("hi")); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(errMsg(t, rec), "no readable text") {
		t.Errorf("no text = %d %s", rec.Code, rec.Body)
	}
	// None of the failures left a profile behind.
	if p := decode[matcher.ProfileView](t, do(a, http.MethodGet, "/api/profile", nil)); p.Exists {
		t.Error("a rejected upload created a profile")
	}
}

func TestEditingAndDeletingTheProfile(t *testing.T) {
	a, _, _ := matchingAPI(t)

	edit := `{"headline":"Lead","seniority":"staff","years_experience":10,"skills":[{"name":"golang","core":true},{"name":"Zig","core":false}],"languages":["English"],"summary":"s"}`
	if rec := do(a, http.MethodPut, "/api/profile", strings.NewReader(edit)); rec.Code != http.StatusNotFound {
		t.Errorf("editing with no profile = %d, want 404", rec.Code)
	}
	upload(t, a, "cv", "cv.txt", []byte(apiCV))

	rec := do(a, http.MethodPut, "/api/profile", strings.NewReader(edit))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d %s", rec.Code, rec.Body)
	}
	p := decode[matcher.ProfileView](t, rec)
	if p.Headline != "Lead" || p.Seniority != "staff" || len(p.Skills) != 2 || p.Skills[0].Name != "Go" {
		t.Errorf("profile = %+v", p)
	}

	if rec := do(a, http.MethodPut, "/api/profile", strings.NewReader(`{"seniority":"wizard"}`)); rec.Code != http.StatusBadRequest || !strings.Contains(errMsg(t, rec), "seniority") {
		t.Errorf("invalid edit = %d %s", rec.Code, rec.Body)
	}
	if rec := do(a, http.MethodPut, "/api/profile", strings.NewReader(`not json`)); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed edit = %d", rec.Code)
	}

	if rec := do(a, http.MethodDelete, "/api/profile", nil); rec.Code != http.StatusNoContent {
		t.Errorf("DELETE = %d", rec.Code)
	}
	if p := decode[matcher.ProfileView](t, do(a, http.MethodGet, "/api/profile", nil)); p.Exists {
		t.Error("the profile survived deletion")
	}
	if rec := do(a, http.MethodDelete, "/api/profile", nil); rec.Code != http.StatusNoContent {
		t.Errorf("deleting twice = %d", rec.Code)
	}
}

func TestReextractEndpoint(t *testing.T) {
	a, _, _ := matchingAPI(t)
	if rec := do(a, http.MethodPost, "/api/profile/reextract", nil); rec.Code != http.StatusNotFound {
		t.Errorf("no CV = %d, want 404", rec.Code)
	}
	upload(t, a, "cv", "cv.txt", []byte(apiCV))
	rec := do(a, http.MethodPost, "/api/profile/reextract", nil)
	if rec.Code != http.StatusOK || !decode[profileResponse](t, rec).Profile.Exists {
		t.Errorf("reextract = %d %s", rec.Code, rec.Body)
	}
}

func TestJobsCarryFitAndCanBeRankedByIt(t *testing.T) {
	a, m, st := matchingAPI(t)
	ctx := context.Background()

	// Before a CV: no fit, and sorting by fit changes nothing.
	m.Sync(ctx)
	type page struct {
		Total int       `json:"total"`
		Jobs  []jobView `json:"jobs"`
	}
	before := decode[page](t, do(a, http.MethodGet, "/api/jobs?status=all&sort=fit", nil))
	for _, j := range before.Jobs {
		if j.Fit != nil {
			t.Errorf("job %d has a fit without a CV: %+v", j.ID, j.Fit)
		}
	}

	upload(t, a, "cv", "cv.txt", []byte(apiCV))
	if _, err := m.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	all := decode[page](t, do(a, http.MethodGet, "/api/jobs?status=all", nil))
	var goJob, salesJob *jobView
	for i := range all.Jobs {
		switch all.Jobs[i].Title {
		case "Senior Go Engineer":
			goJob = &all.Jobs[i]
		case "Sales Lead":
			salesJob = &all.Jobs[i]
		}
	}
	if goJob == nil || goJob.Fit == nil || goJob.Fit.Score == 0 || len(goJob.Fit.Matched) == 0 {
		t.Fatalf("the Go job should carry a fit: %+v", goJob)
	}
	if salesJob == nil || salesJob.Fit != nil {
		t.Errorf("a job that names no known skill can't be scored: %+v", salesJob)
	}

	// Best fit first, and unscorable jobs last.
	ranked := decode[page](t, do(a, http.MethodGet, "/api/jobs?status=all&sort=fit", nil))
	if len(ranked.Jobs) != 2 || ranked.Jobs[0].Title != "Senior Go Engineer" || ranked.Jobs[1].Fit != nil {
		t.Errorf("ranked = %+v", ranked.Jobs)
	}
	// min_fit drops what is below it, and total reflects that.
	min := decode[page](t, do(a, http.MethodGet, "/api/jobs?status=all&min_fit=1", nil))
	if min.Total != 1 || len(min.Jobs) != 1 || min.Jobs[0].Title != "Senior Go Engineer" {
		t.Errorf("min_fit=1 = %+v", min)
	}
	// Paging applies after ranking.
	p2 := decode[page](t, do(a, http.MethodGet, "/api/jobs?status=all&sort=fit&limit=1&offset=1", nil))
	if p2.Total != 2 || len(p2.Jobs) != 1 || p2.Jobs[0].Title != "Sales Lead" {
		t.Errorf("page 2 = %+v", p2)
	}
	if rec := do(a, http.MethodGet, "/api/jobs?sort=sideways", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("bad sort = %d", rec.Code)
	}

	// The detail view has it too.
	detail := decode[jobView](t, do(a, http.MethodGet, "/api/jobs/"+itoa(goJob.ID), nil))
	if detail.Fit == nil || detail.Fit.Score != goJob.Fit.Score {
		t.Errorf("detail fit = %+v", detail.Fit)
	}
	_ = st
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestFitStatusAndSyncEndpoints(t *testing.T) {
	a, _, _ := matchingAPI(t)
	st := decode[matcher.Status](t, do(a, http.MethodGet, "/api/fit/status", nil))
	if st.Ollama.Enabled || st.HasProfile {
		t.Errorf("status = %+v", st)
	}
	rec := do(a, http.MethodPost, "/api/fit/sync", nil)
	if rec.Code != http.StatusAccepted || !decode[matcher.Status](t, rec).Index.Syncing {
		t.Errorf("sync = %d %s", rec.Code, rec.Body)
	}
}

func TestMatchingIsUnavailableWithoutAMatcher(t *testing.T) {
	a, _ := newAPI(t)
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/profile"}, {http.MethodPut, "/api/profile"}, {http.MethodDelete, "/api/profile"},
		{http.MethodPost, "/api/profile/cv"}, {http.MethodPost, "/api/profile/reextract"},
		{http.MethodGet, "/api/fit/status"}, {http.MethodPost, "/api/fit/sync"},
	} {
		if rec := do(a, c.method, c.path, nil); rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s = %d, want 503", c.method, c.path, rec.Code)
		}
	}
	// Jobs still work, with the fit options ignored.
	rec := do(a, http.MethodGet, "/api/jobs?status=all&sort=fit&min_fit=50", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("jobs without a matcher = %d", rec.Code)
	}
}

func TestHighlightsEndpoints(t *testing.T) {
	a, _, _ := matchingAPI(t)
	if rec := do(a, http.MethodPost, "/api/profile/highlights", nil); rec.Code != http.StatusNotFound {
		t.Errorf("no CV = %d, want 404", rec.Code)
	}
	upload(t, a, "cv", "cv.txt", []byte(apiCV))

	// This matcher has no local model, so drafting is unavailable, and says why.
	rec := do(a, http.MethodPost, "/api/profile/highlights", nil)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "local model") {
		t.Errorf("no model = %d %s, want 503 explaining it", rec.Code, rec.Body)
	}

	// Highlights are saved with the rest of the profile.
	put := func(body string) *httptest.ResponseRecorder {
		return do(a, http.MethodPut, "/api/profile", strings.NewReader(body))
	}
	rec = put(`{"seniority":"senior","skills":[{"name":"Go","core":true}],"highlights":["Led the Go migration","Ran the platform team"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("save = %d %s", rec.Code, rec.Body)
	}
	got := decode[matcher.ProfileView](t, do(a, http.MethodGet, "/api/profile", nil))
	if len(got.Highlights) != 2 || got.Highlights[0] != "Led the Go migration" {
		t.Errorf("highlights = %q", got.Highlights)
	}
	if rec := put(`{"seniority":"senior","highlights":["a","b","c","d"]}`); rec.Code != http.StatusBadRequest {
		t.Errorf("four highlights = %d, want 400", rec.Code)
	}
}

func TestAskEndpoint(t *testing.T) {
	a, _, _ := matchingAPI(t)
	post := func(body string) *httptest.ResponseRecorder {
		return do(a, http.MethodPost, "/api/ask", strings.NewReader(body))
	}
	if rec := post(`not json`); rec.Code != http.StatusBadRequest {
		t.Errorf("junk body = %d, want 400", rec.Code)
	}
	if rec := post(`{"question":"  "}`); rec.Code != http.StatusBadRequest {
		t.Errorf("blank question = %d, want 400", rec.Code)
	}
	if rec := post(`{"question":"Tell me about yourself","use_cv":true}`); rec.Code != http.StatusNotFound {
		t.Errorf("CV context without a CV = %d, want 404", rec.Code)
	}
	// The matching API has no Ollama, so a question can't be answered.
	rec := post(`{"question":"What is a goroutine?"}`)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "ollama") {
		t.Errorf("no Ollama = %d %s, want 503 saying so", rec.Code, rec.Body)
	}
}
