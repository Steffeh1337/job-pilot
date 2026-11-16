// Package skills finds known technical skills in free text, such as a CV or a
// job description. It is deterministic and needs no model, so matching works
// even when Ollama is not running.
package skills

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

//go:embed vocab.txt
var defaultSource string

// Skill is one entry of the vocabulary.
type Skill struct {
	Name     string `json:"name"`
	Category string `json:"category,omitempty"`
}

// Hit is a skill found in a text, and how many times it was mentioned.
type Hit struct {
	Skill
	Count int `json:"count"`
}

type Vocabulary struct {
	skills  map[string]Skill  // canonical name -> skill
	byLower map[string]string // lower-case canonical name -> canonical name
	fold    map[string]string // lower-case alias -> canonical (case-insensitive aliases)
	exact   map[string]string // alias -> canonical (case-sensitive aliases)
	guarded map[string]bool   // case-sensitive aliases that need the sentence-start guard
	reFold  *regexp.Regexp
	reExact *regexp.Regexp
	version string
}

var (
	defaultOnce  sync.Once
	defaultVocab *Vocabulary
)

// Default returns the vocabulary embedded in the binary.
func Default() *Vocabulary {
	defaultOnce.Do(func() {
		v, err := Parse(defaultSource)
		if err != nil {
			panic("skills: embedded vocabulary is invalid: " + err.Error())
		}
		defaultVocab = v
	})
	return defaultVocab
}

// Parse reads a vocabulary in the format documented at the top of vocab.txt.
func Parse(src string) (*Vocabulary, error) {
	v := &Vocabulary{
		skills: map[string]Skill{}, byLower: map[string]string{},
		fold: map[string]string{}, exact: map[string]string{}, guarded: map[string]bool{},
	}
	sum := sha256.Sum256([]byte(src))
	v.version = hex.EncodeToString(sum[:])[:12]

	category := ""
	for n, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
			continue
		case strings.HasPrefix(line, "["):
			category = strings.Trim(line, "[] ")
			continue
		}
		parts := strings.Split(line, "|")
		name := strings.TrimSpace(parts[0])
		if name == "" {
			return nil, fmt.Errorf("line %d: missing skill name", n+1)
		}
		if _, dup := v.skills[name]; dup {
			return nil, fmt.Errorf("line %d: duplicate skill %q", n+1, name)
		}
		v.skills[name] = Skill{Name: name, Category: category}
		v.byLower[strings.ToLower(name)] = name

		for _, raw := range parts[1:] {
			alias := strings.TrimSpace(raw)
			if alias == "" {
				continue
			}
			switch alias[0] {
			case '=', '^':
				guard := alias[0] == '^'
				alias = alias[1:]
				if other, taken := v.exact[alias]; taken && other != name {
					return nil, fmt.Errorf("line %d: alias %q already belongs to %q", n+1, alias, other)
				}
				v.exact[alias] = name
				v.guarded[alias] = guard
			default:
				key := strings.ToLower(alias)
				if other, taken := v.fold[key]; taken && other != name {
					return nil, fmt.Errorf("line %d: alias %q already belongs to %q", n+1, alias, other)
				}
				v.fold[key] = name
			}
		}
	}
	v.reFold = build(keys(v.fold), true)
	v.reExact = build(keys(v.exact), false)
	return v, nil
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// build makes one regular expression for a set of aliases. Words end where a
// letter, digit or underscore stops, and "+" and "#" count as part of a word
// so that "C" is not found inside "C++" or "C#". Longer aliases come first so
// "Node.js" wins over "Node".
func build(aliases []string, foldCase bool) *regexp.Regexp {
	if len(aliases) == 0 {
		return nil
	}
	sort.Slice(aliases, func(i, j int) bool {
		if len(aliases[i]) != len(aliases[j]) {
			return len(aliases[i]) > len(aliases[j])
		}
		return aliases[i] < aliases[j]
	})
	quoted := make([]string, len(aliases))
	for i, a := range aliases {
		quoted[i] = regexp.QuoteMeta(a)
	}
	flags := ""
	if foldCase {
		flags = "(?i)"
	}
	return regexp.MustCompile(flags + `(?:^|[^A-Za-z0-9_])(` + strings.Join(quoted, "|") + `)(?:[^A-Za-z0-9_+#]|$)`)
}

// Version identifies the vocabulary contents; features computed with another
// version are stale.
func (v *Vocabulary) Version() string { return v.version }

// Skills returns every skill in the vocabulary.
func (v *Vocabulary) Skills() []Skill {
	out := make([]Skill, 0, len(v.skills))
	for _, s := range v.skills {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Extract returns the skills mentioned in text, most mentioned first.
func (v *Vocabulary) Extract(text string) []Hit {
	counts := map[string]int{}
	if v.reFold != nil {
		scan(v.reFold, text, func(s, e int) {
			if name, ok := v.fold[strings.ToLower(text[s:e])]; ok {
				counts[name]++
			}
		})
	}
	if v.reExact != nil {
		scan(v.reExact, text, func(s, e int) {
			alias := text[s:e]
			name, ok := v.exact[alias]
			if !ok || (v.guarded[alias] && !guardOK(text, s, e)) {
				return
			}
			counts[name]++
		})
	}
	hits := make([]Hit, 0, len(counts))
	for name, n := range counts {
		hits = append(hits, Hit{Skill: v.skills[name], Count: n})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Count != hits[j].Count {
			return hits[i].Count > hits[j].Count
		}
		return hits[i].Name < hits[j].Name
	})
	return hits
}

// Names is Extract reduced to canonical names.
func (v *Vocabulary) Names(text string) []string {
	hits := v.Extract(text)
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.Name
	}
	return out
}

// Lookup resolves a skill name or alias, in any spelling the vocabulary knows,
// to its canonical entry.
func (v *Vocabulary) Lookup(name string) (Skill, bool) {
	n := strings.TrimSpace(name)
	if c, ok := v.exact[n]; ok {
		return v.skills[c], true
	}
	l := strings.ToLower(n)
	if c, ok := v.fold[l]; ok {
		return v.skills[c], true
	}
	if c, ok := v.byLower[l]; ok {
		return v.skills[c], true
	}
	return Skill{}, false
}

// Mentions reports whether term appears in text as a whole word, ignoring case.
// It is used to check that something a model claimed is really in the CV.
func Mentions(text, term string) bool {
	term = strings.TrimSpace(term)
	if term == "" {
		return false
	}
	re, err := regexp.Compile(`(?i)(?:^|[^A-Za-z0-9_])` + regexp.QuoteMeta(term) + `(?:[^A-Za-z0-9_+#]|$)`)
	return err == nil && re.MatchString(text)
}

// scan calls fn with the span of every alias match. Scanning resumes right
// after the alias, not after the boundary character that ended it, so "Go,Rust"
// and "Go/Rust" find both.
func scan(re *regexp.Regexp, text string, fn func(start, end int)) {
	pos := 0
	for pos <= len(text) {
		loc := re.FindStringSubmatchIndex(text[pos:])
		if loc == nil {
			return
		}
		start, end := pos+loc[2], pos+loc[3]
		fn(start, end)
		if end == pos {
			end++
		}
		pos = end
	}
}

// guardOK decides whether a capitalised word that is also ordinary English
// ("Go") is really the skill. In the middle of a sentence it is. At the start
// of one it only counts when it reads like a list item: "Go, Rust", "Go/Python"
// or "Go" alone on a line, never "Go beyond" or "Go-to-market".
func guardOK(text string, start, end int) bool {
	before := strings.TrimRight(text[:start], " \t•·*-–—")
	atStart := before == "" || strings.HasSuffix(before, "\n") ||
		strings.HasSuffix(before, ".") || strings.HasSuffix(before, "!") || strings.HasSuffix(before, "?")
	if !atStart {
		return !strings.HasPrefix(strings.ToLower(text[end:]), "-to-market")
	}
	rest := strings.TrimLeft(text[end:], " \t")
	return rest == "" || rest[0] == '\n' || rest[0] == '\r' || strings.IndexByte(",/;:)|&", rest[0]) >= 0
}
