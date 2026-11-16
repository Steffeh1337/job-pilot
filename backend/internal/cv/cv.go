// Package cv turns an uploaded CV into plain text. Everything happens in
// process: nothing is sent anywhere.
package cv

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"
)

// MaxBytes is the largest upload accepted.
const MaxBytes = 8 << 20

// maxText bounds the extracted text, so a hostile file can't fill memory.
const maxText = 200_000

// minText is how little text counts as "nothing readable", which is what a
// scanned PDF (a picture of a page) looks like.
const minText = 20

type Kind string

const (
	KindPDF  Kind = "pdf"
	KindDOCX Kind = "docx"
	KindText Kind = "text"
)

var (
	ErrTooLarge    = fmt.Errorf("file is larger than %d MB", MaxBytes>>20)
	ErrEmpty       = errors.New("file is empty")
	ErrUnsupported = errors.New("unsupported file type: upload a PDF, a DOCX or a text file")
	ErrLegacyDoc   = errors.New("legacy Word (.doc) files are not supported: save it as DOCX or PDF")
	ErrNoText      = errors.New("no readable text found: the file may be a scan or an image, so export a text-based PDF or DOCX")
	ErrEncrypted   = errors.New("the PDF is password protected")
)

// Result is the text of a CV and the kind of file it came from.
type Result struct {
	Text string
	Kind Kind
}

// Extract reads a CV. The file type is decided from its content, never from
// its name.
func Extract(data []byte) (Result, error) {
	switch {
	case len(data) == 0:
		return Result{}, ErrEmpty
	case len(data) > MaxBytes:
		return Result{}, ErrTooLarge
	}

	var (
		text string
		kind Kind
		err  error
	)
	switch {
	case bytes.Contains(data[:min(len(data), 1024)], []byte("%PDF-")):
		kind = KindPDF
		text, err = pdfText(data)
	case bytes.HasPrefix(data, []byte("PK\x03\x04")):
		kind = KindDOCX
		text, err = docxText(data)
	case bytes.HasPrefix(data, []byte{0xD0, 0xCF, 0x11, 0xE0}):
		return Result{}, ErrLegacyDoc
	case looksLikeText(data):
		kind = KindText
		text = string(data)
	default:
		return Result{}, ErrUnsupported
	}
	if err != nil {
		return Result{}, err
	}

	text = Clean(text)
	if len(strings.Fields(text)) == 0 || utf8.RuneCountInString(text) < minText {
		return Result{}, ErrNoText
	}
	return Result{Text: text, Kind: kind}, nil
}

// looksLikeText accepts UTF-8 without NUL bytes or a high share of control
// characters.
func looksLikeText(data []byte) bool {
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return false
	}
	control := 0
	for _, r := range string(data) {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			control++
		}
	}
	return control*50 < len(data)
}

func pdfText(data []byte) (text string, err error) {
	// The parser panics on some malformed files.
	defer func() {
		if r := recover(); r != nil {
			text, err = "", fmt.Errorf("could not read the PDF: %v", r)
		}
	}()
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "encrypt") || strings.Contains(strings.ToLower(err.Error()), "password") {
			return "", ErrEncrypted
		}
		return "", fmt.Errorf("could not read the PDF: %w", err)
	}
	plain, err := r.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("could not read the PDF text: %w", err)
	}
	b, err := io.ReadAll(io.LimitReader(plain, maxText*4))
	if err != nil {
		return "", fmt.Errorf("could not read the PDF text: %w", err)
	}
	return string(b), nil
}

// maxDocx bounds the unzipped size of word/document.xml (zip bombs).
const maxDocx = 20 << 20

func docxText(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", ErrUnsupported
	}
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		if f.UncompressedSize64 > maxDocx {
			return "", ErrTooLarge
		}
		rc, err := f.Open()
		if err != nil {
			return "", fmt.Errorf("could not read the DOCX: %w", err)
		}
		defer rc.Close()
		return wordXMLText(io.LimitReader(rc, maxDocx))
	}
	return "", ErrUnsupported // a zip, but not a Word document
}

// wordXMLText flattens WordprocessingML: text runs, tabs, breaks and
// paragraph ends.
func wordXMLText(r io.Reader) (string, error) {
	dec := xml.NewDecoder(r)
	var (
		b      strings.Builder
		inText bool
	)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("could not read the DOCX: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inText = true
			case "tab":
				b.WriteByte('\t')
			case "br", "cr":
				b.WriteByte('\n')
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				b.WriteByte('\n')
			}
		case xml.CharData:
			if inText {
				b.Write(t)
			}
		}
		if b.Len() > maxText*4 {
			break
		}
	}
	return b.String(), nil
}

var (
	spaceRun   = regexp.MustCompile(`[ \t\x{a0}]+`)
	blankLines = regexp.MustCompile(`\n{3,}`)
)

// Clean normalises extracted text: line endings, control characters,
// repeated spaces and blank lines.
func Clean(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\f", "\n", " ", "\n", " ", "\n").Replace(s)
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, s)
	s = spaceRun.ReplaceAllString(s, " ")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	s = blankLines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n")
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > maxText {
		s = string(r[:maxText])
	}
	return s
}
