package store

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const cvMarker = "ZZ-UNIQUE-CV-MARKER-8f3a1c-ZZ"

func saveMarkerProfile(t *testing.T, st *Store) {
	t.Helper()
	err := st.SaveProfile(context.Background(), ProfileRow{
		Data: []byte(`{"headline":"Engineer"}`), CVText: "Senior engineer. " + cvMarker, CVFilename: "cv.pdf",
		ExtractedBy: "keywords", UploadedAt: time.Now(), UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
}

// copyMainFileOnly copies the database file without its write-ahead log or
// shared-memory file. That is what a power cut leaves behind if the log never
// reached the disk.
func copyMainFileOnly(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "survivor.db")
	if err := os.WriteFile(dst, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return dst
}

func TestSavedProfileIsInTheMainDatabaseFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobpilot.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	saveMarkerProfile(t, st)

	// The process is still running and has not closed the database, as when
	// the machine loses power. Only the main file is guaranteed to survive.
	survivor, err := Open(copyMainFileOnly(t, path))
	if err != nil {
		t.Fatal(err)
	}
	defer survivor.Close()
	got, err := survivor.LoadProfile(context.Background())
	if err != nil || got == nil || got.CVFilename != "cv.pdf" || !bytes.Contains([]byte(got.CVText), []byte(cvMarker)) {
		t.Fatalf("the saved profile did not reach the main database file: %+v, %v", got, err)
	}
}

func TestProfileEditsAlsoReachTheMainFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobpilot.db")
	st, _ := Open(path)
	defer st.Close()
	saveMarkerProfile(t, st)
	st.SaveProfile(context.Background(), ProfileRow{Data: []byte(`{"headline":"Edited"}`), CVText: "cv " + cvMarker, CVFilename: "cv.pdf", UploadedAt: time.Now(), UpdatedAt: time.Now()})

	survivor, _ := Open(copyMainFileOnly(t, path))
	defer survivor.Close()
	got, _ := survivor.LoadProfile(context.Background())
	if got == nil || string(got.Data) != `{"headline":"Edited"}` {
		t.Errorf("the edit was lost: %+v", got)
	}
}

func TestDeletedCVTextIsErasedFromDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobpilot.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	saveMarkerProfile(t, st)
	if b, _ := os.ReadFile(path); !bytes.Contains(b, []byte(cvMarker)) {
		t.Fatal("test setup: the CV text should be in the database file before it is deleted")
	}

	if err := st.DeleteProfile(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{path, path + "-wal"} {
		b, err := os.ReadFile(name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte(cvMarker)) {
			t.Errorf("the deleted CV text is still readable in %s", filepath.Base(name))
		}
	}
	survivor, _ := Open(copyMainFileOnly(t, path))
	defer survivor.Close()
	if got, _ := survivor.LoadProfile(context.Background()); got != nil {
		t.Error("a deleted profile must stay deleted")
	}
}

func TestReplacingACVErasesTheOldText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobpilot.db")
	st, _ := Open(path)
	defer st.Close()
	saveMarkerProfile(t, st)
	st.SaveProfile(context.Background(), ProfileRow{Data: []byte(`{}`), CVText: "a completely different cv", CVFilename: "new.pdf", UploadedAt: time.Now(), UpdatedAt: time.Now()})

	for _, name := range []string{path, path + "-wal"} {
		if b, err := os.ReadFile(name); err == nil && bytes.Contains(b, []byte(cvMarker)) {
			t.Errorf("the replaced CV text is still readable in %s", filepath.Base(name))
		}
	}
}

func TestProfileSurvivesReopeningTheDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobpilot.db")
	st, _ := Open(path)
	saveMarkerProfile(t, st)
	st.Close()

	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if got, _ := again.LoadProfile(context.Background()); got == nil || got.CVFilename != "cv.pdf" {
		t.Errorf("profile after a restart = %+v", got)
	}
}
