package cv

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// makePDF builds a minimal single-page text PDF, with correct xref offsets.
func makePDF(lines ...string) []byte {
	var buf bytes.Buffer
	var offsets []int
	buf.WriteString("%PDF-1.4\n")
	obj := func(s string) {
		offsets = append(offsets, buf.Len())
		buf.WriteString(s)
	}
	esc := strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`)
	var content strings.Builder
	content.WriteString("BT /F1 12 Tf 72 720 Td 16 TL\n")
	for _, l := range lines {
		fmt.Fprintf(&content, "(%s) Tj T*\n", esc.Replace(l))
	}
	content.WriteString("ET")

	obj("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	obj("2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n")
	obj("3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>\nendobj\n")
	obj(fmt.Sprintf("4 0 obj\n<< /Length %d >>\nstream\n%s\nendstream\nendobj\n", content.Len(), content.String()))
	obj("5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n")

	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, o := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	return buf.Bytes()
}

func makeDOCX(t *testing.T, bodyXML string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>%s</w:body></w:document>`, bodyXML)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractPDF(t *testing.T) {
	res, err := Extract(makePDF("Jane Developer", "Senior Backend Engineer", "Go, TypeScript, PostgreSQL and Kubernetes", "8 years of experience"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != KindPDF {
		t.Errorf("kind = %v", res.Kind)
	}
	for _, want := range []string{"Senior Backend Engineer", "PostgreSQL", "8 years"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("text %q is missing %q", res.Text, want)
		}
	}
}

func TestExtractPDFWithEscapedCharacters(t *testing.T) {
	res, err := Extract(makePDF("Skills (Go) and C++ \\ more text so it is long enough"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "(Go)") || !strings.Contains(res.Text, "C++") {
		t.Errorf("text = %q", res.Text)
	}
}

func TestExtractDOCX(t *testing.T) {
	doc := makeDOCX(t, `
		<w:p><w:r><w:t>Jane Developer</w:t></w:r></w:p>
		<w:p><w:r><w:t xml:space="preserve">Senior </w:t></w:r><w:r><w:t>Engineer</w:t></w:r></w:p>
		<w:p><w:r><w:t>Go</w:t></w:r><w:r><w:tab/></w:r><w:r><w:t>TypeScript</w:t></w:r><w:r><w:br/></w:r><w:r><w:t>Docker and Kubernetes</w:t></w:r></w:p>`)
	res, err := Extract(doc)
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != KindDOCX {
		t.Errorf("kind = %v", res.Kind)
	}
	for _, want := range []string{"Jane Developer", "Senior Engineer", "Go TypeScript", "Docker and Kubernetes"} {
		if !strings.Contains(res.Text, want) {
			t.Errorf("text %q is missing %q", res.Text, want)
		}
	}
	if !strings.Contains(res.Text, "\n") {
		t.Error("paragraphs should be separated by newlines")
	}
}

func TestExtractPlainText(t *testing.T) {
	res, err := Extract([]byte("Jane Developer\r\n\r\n\r\n\r\nSenior Engineer — Go & TypeScript  \t  café"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != KindText {
		t.Errorf("kind = %v", res.Kind)
	}
	if strings.Contains(res.Text, "\r") || strings.Contains(res.Text, "\n\n\n") || strings.Contains(res.Text, "  ") {
		t.Errorf("text was not normalised: %q", res.Text)
	}
	if !strings.Contains(res.Text, "café") || !strings.Contains(res.Text, "—") {
		t.Errorf("non-ASCII text was damaged: %q", res.Text)
	}
}

func TestExtractRejects(t *testing.T) {
	notWord := func() []byte {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w, _ := zw.Create("other.txt")
		w.Write([]byte("hello"))
		zw.Close()
		return buf.Bytes()
	}()
	tests := []struct {
		name string
		data []byte
		want error
	}{
		{"empty", nil, ErrEmpty},
		{"too large", bytes.Repeat([]byte("a"), MaxBytes+1), ErrTooLarge},
		{"legacy doc", append([]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, make([]byte, 200)...), ErrLegacyDoc},
		{"binary", append([]byte{0x89, 'P', 'N', 'G', 0, 1, 2, 3}, make([]byte, 100)...), ErrUnsupported},
		{"zip that is not Word", notWord, ErrUnsupported},
		{"whitespace only", []byte("   \n\n  \t "), ErrNoText},
		{"too little text", []byte("hi"), ErrNoText},
		{"scanned pdf with no text", makePDF(), ErrNoText},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Extract(tt.data); !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestExtractDecidesByContentNotName(t *testing.T) {
	// A PDF is a PDF whatever the upload was called; Extract never sees the name.
	if res, err := Extract(makePDF("Senior Engineer with Go experience for many years")); err != nil || res.Kind != KindPDF {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestExtractSurvivesGarbagePDF(t *testing.T) {
	// A truncated or corrupt PDF must produce an error, never a panic.
	for _, data := range [][]byte{
		[]byte("%PDF-1.4\n1 0 obj\n<<"),
		append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte{0xFF, 0x00, 0x13}, 400)...),
		makePDF("some readable text for this cv and more words")[:120],
	} {
		if _, err := Extract(data); err == nil {
			t.Errorf("expected an error for corrupt PDF %q…", data[:min(len(data), 20)])
		}
	}
}

func TestCleanBoundsLength(t *testing.T) {
	got := Clean(strings.Repeat("word ", maxText))
	if n := len([]rune(got)); n > maxText {
		t.Errorf("length = %d, want at most %d", n, maxText)
	}
}

func TestCleanRemovesControlCharacters(t *testing.T) {
	got := Clean("a\x00b\x07c\x1bde�")
	if strings.ContainsAny(got, "\x00\x07\x1b") {
		t.Errorf("control characters survived: %q", got)
	}
}
