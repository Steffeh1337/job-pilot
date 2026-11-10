package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Steffeh1337/job-pilot/backend/internal/domain"
)

// Fixtures in testdata/ are real API responses trimmed to a few jobs.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustParse(t *testing.T, jobs []*domain.Job, err error, wantLen int) []*domain.Job {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != wantLen {
		t.Fatalf("parsed %d jobs, want %d", len(jobs), wantLen)
	}
	for _, j := range jobs {
		if j.Source == "" || j.ExternalID == "" || j.Title == "" || j.URL == "" {
			t.Errorf("incomplete job: %+v", j)
		}
		if strings.Contains(j.Description, "<p>") || strings.Contains(j.Description, "&lt;") {
			t.Errorf("job %s: description still has markup: %.80q", j.ExternalID, j.Description)
		}
	}
	return jobs
}

func TestStripHTML(t *testing.T) {
	tests := []struct{ in, want string }{
		{"<p>Hi&nbsp;there</p><br/>x", "Hi there\nx"},
		{"&lt;p&gt;Hello&lt;/p&gt;&lt;ul&gt;&lt;li&gt;A&lt;/li&gt;&lt;li&gt;B &amp;amp; C&lt;/li&gt;&lt;/ul&gt;", "Hello\nA\nB & C"},
		{"plain text", "plain text"},
		{"  <div> a  \t b </div>\n\n\n <div>c</div> ", "a b\nc"},
	}
	for _, tt := range tests {
		if got := StripHTML(tt.in); got != tt.want {
			t.Errorf("StripHTML(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	if got := StripHTML(strings.Repeat("a", maxDescription*2)); len(got) != maxDescription {
		t.Errorf("long input not clipped: %d", len(got))
	}
}

func TestParseRemoteOK(t *testing.T) {
	jobs, err := parseRemoteOK(fixture(t, "remoteok.json"))
	jobs = mustParse(t, jobs, err, 3) // element 0 is the legal notice
	if jobs[0].ExternalID != "1137708" || !jobs[0].Remote || jobs[0].Company != "Pebl" {
		t.Errorf("unexpected first job: %+v", jobs[0])
	}
	if jobs[0].PostedAt.Year() != 2026 {
		t.Errorf("posted at %v", jobs[0].PostedAt)
	}
}

func TestParseRemotive(t *testing.T) {
	jobs, err := parseRemotive(fixture(t, "remotive.json"))
	jobs = mustParse(t, jobs, err, 4)
	if got := jobs[2].Regions; len(got) != 1 || got[0] != "USA" {
		t.Errorf("regions = %v", got)
	}
	if jobs[3].EmploymentType != "freelance" || jobs[0].PostedAt.IsZero() {
		t.Errorf("unexpected: %+v", jobs[3])
	}
}

func TestParseHimalayas(t *testing.T) {
	jobs, err := parseHimalayas(fixture(t, "himalayas.json"))
	jobs = mustParse(t, jobs, err, 4)
	if jobs[0].Regions[0] != "Egypt" || jobs[0].ExternalID != jobs[0].URL || jobs[0].PostedAt.IsZero() {
		t.Errorf("unexpected first job: %+v", jobs[0])
	}
}

func TestParseWWR(t *testing.T) {
	jobs, err := parseWWR(fixture(t, "wwr.xml"))
	jobs = mustParse(t, jobs, err, 3)
	if jobs[0].Company != "Toptal" || jobs[0].Title != "CAD scripting Expert" {
		t.Errorf("title not split into company and role: %q / %q", jobs[0].Company, jobs[0].Title)
	}
	if len(jobs[0].Regions) != 1 || jobs[0].Regions[0] != "Anywhere in the World" {
		t.Errorf("regions = %v", jobs[0].Regions)
	}
	if jobs[0].PostedAt.Year() != 2026 {
		t.Errorf("posted at %v", jobs[0].PostedAt)
	}
}

func TestParseJobicy(t *testing.T) {
	jobs, err := parseJobicy(fixture(t, "jobicy.json"))
	jobs = mustParse(t, jobs, err, 4)
	j := jobs[0]
	if j.Title != "Senior Cloud Architect" || j.Regions[0] != "USA" || j.SalaryMin != 150000 || j.Currency != "USD" {
		t.Errorf("unexpected first job: %+v", j)
	}
}

func TestParseArbeitnow(t *testing.T) {
	jobs, err := parseArbeitnow(fixture(t, "arbeitnow.json"))
	jobs = mustParse(t, jobs, err, 4)
	if jobs[0].Remote || len(jobs[0].Regions) != 1 {
		t.Errorf("unexpected first job: %+v", jobs[0])
	}
}

func TestParseHNThread(t *testing.T) {
	jobs, err := parseHNThread(fixture(t, "hn_item.json"), hnThreads[0])
	if err != nil {
		t.Fatal(err)
	}
	byCompany := map[string]*domain.Job{}
	for _, j := range jobs {
		byCompany[j.Company] = j
		if !strings.HasPrefix(j.URL, "https://news.ycombinator.com/item?id=") {
			t.Errorf("bad url %q", j.URL)
		}
	}
	sa := byCompany["Simple Analytics"]
	if sa == nil || !sa.Remote || !strings.Contains(sa.Location, "worldwide") || !strings.HasPrefix(sa.Title, "DevOps Engineer") {
		t.Fatalf("Simple Analytics not parsed as remote worldwide DevOps: %+v", sa)
	}
	if sh := byCompany["Shepherd (Series B)"]; sh == nil || sh.Remote {
		t.Errorf("on-site post parsed as remote: %+v", sh)
	}
}

func TestParseHNFreelancerThreadKeepsOnlyClients(t *testing.T) {
	body := `{"id": 2, "children": [
		{"id": 10, "created_at": "2026-11-02T10:00:00Z", "text": "SEEKING FREELANCER | Acme | Go developer | Remote<p>Details"},
		{"id": 11, "created_at": "2026-11-02T10:00:00Z", "text": "SEEKING WORK | Bob | Go | Remote<p>Hire me"},
		{"id": 12, "created_at": "2026-11-02T10:00:00Z", "text": ""}
	]}`
	jobs, err := parseHNThread([]byte(body), hnThreads[1])
	if err != nil || len(jobs) != 1 {
		t.Fatalf("got %d jobs, err %v; want 1", len(jobs), err)
	}
	if jobs[0].EmploymentType != "freelance" || jobs[0].ExternalID != "10" {
		t.Errorf("unexpected: %+v", jobs[0])
	}
}

func TestParseGreenhouse(t *testing.T) {
	jobs, err := parseGreenhouse(fixture(t, "greenhouse.json"), "gitlab")
	jobs = mustParse(t, jobs, err, 4)
	if jobs[0].ExternalID != "gitlab:8638232002" || !jobs[0].Remote || jobs[0].Company != "GitLab" || jobs[0].Description == "" {
		t.Errorf("unexpected first job: %+v", jobs[0])
	}
}

func TestParseLever(t *testing.T) {
	jobs, err := parseLever(fixture(t, "lever.json"), "palantir")
	jobs = mustParse(t, jobs, err, 4)
	if jobs[0].Remote || !jobs[1].Remote {
		t.Errorf("workplaceType not mapped: %v / %v", jobs[0].Remote, jobs[1].Remote)
	}
	if jobs[0].PostedAt.Year() != 2026 || !strings.HasPrefix(jobs[0].ExternalID, "palantir:") {
		t.Errorf("unexpected: %+v", jobs[0])
	}
}

func TestParseAshby(t *testing.T) {
	jobs, err := parseAshby(fixture(t, "ashby.json"), "ashby")
	jobs = mustParse(t, jobs, err, 4)
	j := jobs[0]
	if !j.Remote || !strings.Contains(j.Location, "Romania") {
		t.Errorf("secondary locations not merged into Location: %q", j.Location)
	}
	if j.SalaryMin != 150000 || j.SalaryMax != 250000 || j.Currency != "EUR" {
		t.Errorf("salary = %d-%d %s", j.SalaryMin, j.SalaryMax, j.Currency)
	}
}

func TestLocationRegions(t *testing.T) {
	tests := []struct {
		in   []string
		want []string
	}{
		{[]string{"Remote"}, nil},
		{[]string{"remote", " Remote - "}, nil},
		{[]string{""}, nil},
		{[]string{"San Francisco"}, []string{"San Francisco"}},
		{[]string{"Remote - US"}, []string{"Remote - US"}},
		{[]string{"Remote", "Romania"}, []string{"Romania"}},
	}
	for _, tt := range tests {
		got := locationRegions(tt.in...)
		if strings.Join(got, "|") != strings.Join(tt.want, "|") {
			t.Errorf("locationRegions(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// Ashby marks hybrid roles isRemote:true; only workplaceType tells them apart.
func TestAshbyHybridIsNotRemote(t *testing.T) {
	body := `{"jobs": [
		{"id": "1", "title": "A", "location": "San Francisco", "isRemote": true, "workplaceType": "Hybrid", "jobUrl": "u"},
		{"id": "2", "title": "B", "location": "Remote", "isRemote": true, "workplaceType": "Remote", "jobUrl": "u"},
		{"id": "3", "title": "C", "location": "Remote", "isRemote": true, "jobUrl": "u"},
		{"id": "4", "title": "D", "location": "Remote", "isRemote": true, "workplaceType": "Remote", "isListed": false, "jobUrl": "u"}
	]}`
	jobs, err := parseAshby([]byte(body), "co")
	if err != nil || len(jobs) != 3 {
		t.Fatalf("got %d jobs, err %v; want 3 (unlisted skipped)", len(jobs), err)
	}
	if jobs[0].Remote || !jobs[1].Remote || !jobs[2].Remote {
		t.Errorf("remote = %v %v %v, want false true true", jobs[0].Remote, jobs[1].Remote, jobs[2].Remote)
	}
	if len(jobs[1].Regions) != 0 {
		t.Errorf("bare Remote should carry no region restriction, got %v", jobs[1].Regions)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	for name, fn := range map[string]func([]byte) ([]*domain.Job, error){
		"remoteok": parseRemoteOK, "remotive": parseRemotive, "himalayas": parseHimalayas,
		"wwr": parseWWR, "jobicy": parseJobicy, "arbeitnow": parseArbeitnow,
	} {
		if _, err := fn([]byte("<html>Just a moment...</html>")); err == nil {
			t.Errorf("%s: expected an error for a non-API response", name)
		}
	}
}

func TestGreenhouseKeepsGoingWhenOneBoardFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/boards/good/jobs":
			_, _ = w.Write(fixture(t, "greenhouse.json"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	g := NewGreenhouse(NewClient(""), []string{"missing", "good"})
	g.BaseURL = srv.URL
	jobs, err := g.Fetch(context.Background())
	if len(jobs) != 4 {
		t.Errorf("got %d jobs, want 4 from the good board", len(jobs))
	}
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Errorf("error should name the failing board, got %v", err)
	}
}

func TestHimalayasPagesUntilEmpty(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Query().Get("offset") == "0" {
			_, _ = w.Write(fixture(t, "himalayas.json"))
			return
		}
		_, _ = w.Write([]byte(`{"jobs": []}`))
	}))
	defer srv.Close()

	h := NewHimalayas(NewClient(""))
	h.BaseURL = srv.URL
	jobs, err := h.Fetch(context.Background())
	if err != nil || len(jobs) != 4 {
		t.Fatalf("got %d jobs, err %v; want 4", len(jobs), err)
	}
	if n := requests.Load(); n != 2 {
		t.Errorf("made %d requests, want 2 (stop at the first empty page)", n)
	}
}

func TestHackerNewsFetchFindsLatestThreads(t *testing.T) {
	freelancer, _ := json.Marshal(map[string]any{"id": 2, "children": []map[string]any{
		{"id": 10, "created_at": "2026-11-02T10:00:00Z", "text": "SEEKING FREELANCER | Acme | Go developer | Remote"},
	}})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/search_by_date" && strings.Contains(r.URL.Query().Get("query"), "Freelancer"):
			fmt.Fprint(w, `{"hits":[{"objectID":"2","title":"Ask HN: Freelancer? Seeking freelancer? (November 2026)"}]}`)
		case r.URL.Path == "/search_by_date":
			// A "wants to be hired" thread comes first and must be skipped.
			fmt.Fprint(w, `{"hits":[{"objectID":"9","title":"Ask HN: Who wants to be hired? (November 2026)"},{"objectID":"1","title":"Ask HN: Who is hiring? (November 2026)"}]}`)
		case r.URL.Path == "/items/1":
			_, _ = w.Write(fixture(t, "hn_item.json"))
		case r.URL.Path == "/items/2":
			_, _ = w.Write(freelancer)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	h := NewHackerNews(NewClient(""))
	h.BaseURL = srv.URL
	jobs, err := h.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var freelance int
	for _, j := range jobs {
		if j.EmploymentType == "freelance" {
			freelance++
		}
	}
	if len(jobs) < 2 || freelance != 1 {
		t.Errorf("got %d jobs (%d freelance), want the hiring thread plus one freelance post", len(jobs), freelance)
	}
}

func TestBuildHonoursConfig(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.Sources["remoteok"] = srcCfg(false, 0)
	cfg.Sources["remotive"] = srcCfg(true, 90*60*1e9)
	got := map[string]Source{}
	for _, s := range Build(cfg, NewClient("")) {
		got[s.Name()] = s
	}
	if _, ok := got["remoteok"]; ok {
		t.Error("disabled source was built")
	}
	if s := got["remotive"]; s == nil || s.Interval().Minutes() != 90 {
		t.Errorf("interval override not applied: %v", s)
	}
	if got["jobicy"] == nil {
		t.Error("enabled source missing")
	}
}
