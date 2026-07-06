package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(filepath.Join(t.TempDir(), "state", "lazys3", "history.jsonl"))
}

func rec(label string) Record {
	return Record{
		Time:   "2026-07-06T12:00:00Z",
		Op:     "download",
		Label:  label,
		Status: "done",
		Bytes:  1024,
	}
}

func TestAppendLoadRoundtrip(t *testing.T) {
	s := testStore(t)
	for i := 0; i < 3; i++ {
		if err := s.Append(rec(fmt.Sprintf("file-%d", i))); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	got, err := s.Load(10)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("Load returned %d records, want 3", len(got))
	}
	// Newest first: the last appended record comes back first.
	for i, want := range []string{"file-2", "file-1", "file-0"} {
		if got[i].Label != want {
			t.Errorf("got[%d].Label = %q, want %q", i, got[i].Label, want)
		}
	}
}

func TestLoadLimitKeepsNewest(t *testing.T) {
	s := testStore(t)
	for i := 0; i < 5; i++ {
		if err := s.Append(rec(fmt.Sprintf("file-%d", i))); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	got, err := s.Load(2)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Load returned %d records, want 2", len(got))
	}
	if got[0].Label != "file-4" || got[1].Label != "file-3" {
		t.Errorf("Load(2) = [%q, %q], want [file-4, file-3]", got[0].Label, got[1].Label)
	}
}

func TestLoadSkipsCorruptLines(t *testing.T) {
	s := testStore(t)
	if err := s.Append(rec("good-1")); err != nil {
		t.Fatalf("Append: %v", err)
	}
	f, err := os.OpenFile(s.Path(), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.WriteString("{not json\n\n42\n"); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}
	f.Close()
	if err := s.Append(rec("good-2")); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, err := s.Load(10)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Load returned %d records, want 2 (corrupt lines skipped)", len(got))
	}
	if got[0].Label != "good-2" || got[1].Label != "good-1" {
		t.Errorf("Load = [%q, %q], want [good-2, good-1]", got[0].Label, got[1].Label)
	}
}

func TestRotationKeepsNewestAndCleansTemp(t *testing.T) {
	s := testStore(t)
	// Pre-seed rotateAt lines directly (Append per line would be O(n^2)
	// in the test), then push it over the threshold with one real Append.
	if err := os.MkdirAll(filepath.Dir(s.Path()), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	var sb strings.Builder
	for i := 0; i < rotateAt; i++ {
		line, _ := json.Marshal(rec(fmt.Sprintf("file-%d", i)))
		sb.Write(line)
		sb.WriteByte('\n')
	}
	if err := os.WriteFile(s.Path(), []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := s.Append(rec("newest")); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, err := s.Load(0)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != rotateKeep {
		t.Fatalf("after rotation: %d records, want %d", len(got), rotateKeep)
	}
	if got[0].Label != "newest" {
		t.Errorf("got[0].Label = %q, want newest", got[0].Label)
	}
	if got[len(got)-1].Label != fmt.Sprintf("file-%d", rotateAt-rotateKeep+1) {
		t.Errorf("oldest kept = %q, want file-%d", got[len(got)-1].Label, rotateAt-rotateKeep+1)
	}
	// The atomic rewrite must not leave its temp file behind.
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(s.Path()), "history-*.tmp"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("temp files left after rotation: %v", matches)
	}
}

func TestAppendHealsTornTail(t *testing.T) {
	s := testStore(t)
	if err := s.Append(rec("good-1")); err != nil {
		t.Fatalf("Append: %v", err)
	}
	// Simulate a crash mid-write: a partial line without a trailing newline.
	f, err := os.OpenFile(s.Path(), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.WriteString(`{"time":"2026-07-06T12:0`); err != nil {
		t.Fatalf("write partial: %v", err)
	}
	f.Close()
	if err := s.Append(rec("good-2")); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, err := s.Load(10)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Load returned %d records, want 2 (torn tail healed)", len(got))
	}
	if got[0].Label != "good-2" || got[1].Label != "good-1" {
		t.Errorf("Load = [%q, %q], want [good-2, good-1]", got[0].Label, got[1].Label)
	}
}

func TestConcurrentAppendLosesNothing(t *testing.T) {
	// Each finished transfer appends from its own tea.Cmd goroutine; keep
	// the file over the rotation threshold so concurrent appends also race
	// rotate's read-rewrite-rename.
	s := testStore(t)
	if err := os.MkdirAll(filepath.Dir(s.Path()), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	var sb strings.Builder
	for i := 0; i < rotateAt; i++ {
		line, _ := json.Marshal(rec(fmt.Sprintf("seed-%d", i)))
		sb.Write(line)
		sb.WriteByte('\n')
	}
	if err := os.WriteFile(s.Path(), []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := s.Append(rec(fmt.Sprintf("conc-%d", i))); err != nil {
				t.Errorf("Append: %v", err)
			}
		}(i)
	}
	wg.Wait()
	got, err := s.Load(0)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	found := 0
	for _, r := range got {
		if strings.HasPrefix(r.Label, "conc-") {
			found++
		}
	}
	if found != n {
		t.Errorf("found %d concurrent records after rotation, want %d", found, n)
	}
}

func TestDefaultPathIgnoresRelativeXDGStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "relative/state")
	if p := DefaultPath(); !filepath.IsAbs(p) || strings.Contains(p, "relative") {
		t.Errorf("DefaultPath honored relative XDG_STATE_HOME: %q", p)
	}
	t.Setenv("XDG_STATE_HOME", "~/state")
	if p := DefaultPath(); strings.Contains(p, "~") {
		t.Errorf("DefaultPath honored unexpanded ~ in XDG_STATE_HOME: %q", p)
	}
	abs := t.TempDir()
	t.Setenv("XDG_STATE_HOME", abs)
	if want := filepath.Join(abs, "lazys3", "history.jsonl"); DefaultPath() != want {
		t.Errorf("DefaultPath = %q, want %q", DefaultPath(), want)
	}
}

func TestAppendCreatesMissingDir(t *testing.T) {
	s := testStore(t)
	if _, err := os.Stat(filepath.Dir(s.Path())); !os.IsNotExist(err) {
		t.Fatalf("dir unexpectedly exists before Append")
	}
	if err := s.Append(rec("first")); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if _, err := os.Stat(s.Path()); err != nil {
		t.Fatalf("state file missing after Append: %v", err)
	}
}

func TestLoadMissingAndEmptyFile(t *testing.T) {
	s := testStore(t)
	got, err := s.Load(10)
	if err != nil {
		t.Fatalf("Load missing file: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Load missing file returned %d records, want 0", len(got))
	}
	if err := os.MkdirAll(filepath.Dir(s.Path()), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(s.Path(), nil, 0o644); err != nil {
		t.Fatalf("write empty: %v", err)
	}
	got, err = s.Load(10)
	if err != nil {
		t.Fatalf("Load empty file: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Load empty file returned %d records, want 0", len(got))
	}
}
