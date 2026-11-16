package skills

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

func names(t *testing.T, text string) []string {
	t.Helper()
	out := Default().Names(text)
	sort.Strings(out)
	return out
}

func TestDefaultVocabularyIsValid(t *testing.T) {
	v := Default()
	if len(v.Skills()) < 100 {
		t.Errorf("only %d skills loaded", len(v.Skills()))
	}
	if v.Version() == "" {
		t.Error("no version")
	}
}

func TestExtract(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{"stack sentence", "Built services in Go and TypeScript with React and Node.js.", []string{"Go", "Node.js", "React", "TypeScript"}},
		{"aliases and symbols", "We use Golang, Rust/Go and Kubernetes (k8s).", []string{"Go", "Kubernetes", "Rust"}},
		{"upper-case alias", "GOLANG developer", []string{"Go"}},
		{"no comma after list item", "Go,Rust and Python", []string{"Go", "Python", "Rust"}},
		{"Java is not inside JavaScript", "JavaScript, not Java.", []string{"Java", "JavaScript"}},
		{"JavaScript alone", "Senior JavaScript engineer", []string{"JavaScript"}},
		{"C++ and C#", "C++ and C# developer", []string{"C#", "C++"}},
		{"dot net", "Maintain .NET and ASP.NET services", []string{".NET"}},
		{"NoSQL is not SQL", "NoSQL stores, PostgreSQL, MySQL and SQL", []string{"MySQL", "PostgreSQL", "SQL"}},
		{"react the verb", "We react quickly to change", nil},
		{"React.js spelling", "Experience with React.js", []string{"React"}},
		{"CI/CD", "CI/CD pipelines with GitHub Actions", []string{"CI/CD", "GitHub Actions"}},
		{"Go at sentence start, not a skill", "Go beyond expectations. Go to market with us.", nil},
		{"Go-to-market mid sentence", "Our go-to-market team and Go-to-market plan", nil},
		{"Go at line start as a list", "Languages\nGo, TypeScript\nRust", []string{"Go", "Rust", "TypeScript"}},
		{"Go alone on a line", "Skills\nGo\nPython", []string{"Go", "Python"}},
		{"Go after a bullet", "• Go, Docker", []string{"Docker", "Go"}},
		{"Go mid sentence", "Experienced in Go for backend services.", []string{"Go"}},
		{"similar words are not skills", "Django, MongoDB and an algorithm", []string{"Django", "MongoDB"}},
		{"lowercase Node is not Node.js", "no node here", nil},
		{"capital Node is", "We run Node in production", []string{"Node.js"}},
		{"empty", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := names(t, tt.text)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Extract(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestExtractCountsMentions(t *testing.T) {
	hits := Default().Extract("Go, Go, Go and Docker. kubernetes k8s")
	counts := map[string]int{}
	for _, h := range hits {
		counts[h.Name] = h.Count
	}
	if counts["Go"] != 3 || counts["Docker"] != 1 || counts["Kubernetes"] != 2 {
		t.Errorf("counts = %v", counts)
	}
	if hits[0].Name != "Go" {
		t.Errorf("most mentioned should come first, got %v", hits[0])
	}
}

func TestExtractKeepsCategory(t *testing.T) {
	hits := Default().Extract("PostgreSQL")
	if len(hits) != 1 || hits[0].Category != "Data stores" {
		t.Errorf("hits = %+v", hits)
	}
}

func TestLookup(t *testing.T) {
	v := Default()
	for in, want := range map[string]string{
		"golang": "Go", "Go": "Go", "typescript": "TypeScript", "TYPESCRIPT": "TypeScript",
		"k8s": "Kubernetes", "Postgres": "PostgreSQL", "react.js": "React", "node.js": "Node.js",
		// Lookup is about naming only: a model may say "go". Whether the CV
		// really mentions the skill is checked separately, against the text.
		"go": "Go",
	} {
		got, ok := v.Lookup(in)
		if !ok || got.Name != want {
			t.Errorf("Lookup(%q) = %v, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"cobol", "", "  "} {
		if _, ok := v.Lookup(in); ok {
			t.Errorf("Lookup(%q) should not resolve", in)
		}
	}
}

func TestMentions(t *testing.T) {
	text := "Led a team using Cobol and COBOL tooling; also Rust."
	for term, want := range map[string]bool{"cobol": true, "Rust": true, "rus": false, "tool": false, "": false, "C++": false} {
		if got := Mentions(text, term); got != want {
			t.Errorf("Mentions(%q) = %v, want %v", term, got, want)
		}
	}
	if !Mentions("We write C++ daily", "C++") {
		t.Error("symbol terms should match")
	}
}

func TestParseRejectsBadVocabularies(t *testing.T) {
	for name, src := range map[string]string{
		"missing name":    "[X]\n | alias",
		"duplicate skill": "[X]\nGo | golang\nGo | other",
		"alias conflict":  "[X]\nGo | golang\nGolang | golang",
		"exact conflict":  "[X]\nA | =Same\nB | =Same",
	} {
		if _, err := Parse(src); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestParseCustomVocabulary(t *testing.T) {
	v, err := Parse("# comment\n[Custom]\nCOBOL | cobol\nFoo | =Foo\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(v.Names("We hire cobol people. And Foo, but not foo."), ","); got != "COBOL,Foo" {
		t.Errorf("got %q", got)
	}
	if v.Version() == Default().Version() {
		t.Error("different sources should have different versions")
	}
}
