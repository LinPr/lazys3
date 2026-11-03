package fsstore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateWritesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	fs := NewFileStore(context.Background(), LocalOption{})

	f, err := fs.Create(path)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.WriteString("hello"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("content = %q, want hello", got)
	}
}

// TestCreateTruncatesExisting pins the os.Create semantics the sync engine
// relies on: re-downloading over a longer existing file must not leave the
// old tail behind.
func TestCreateTruncatesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(path, []byte("a much longer old content"), 0o644); err != nil {
		t.Fatal(err)
	}

	fs := NewFileStore(context.Background(), LocalOption{})
	f, err := fs.Create(path)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.WriteString("new"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Errorf("content = %q, want new (old bytes must be truncated)", got)
	}
}

// TestCreateMissingDirectorySurfacesError pins that a bad destination is
// reported rather than silently ignored: the sync engine turns this into a
// per-file failure.
func TestCreateMissingDirectorySurfacesError(t *testing.T) {
	fs := NewFileStore(context.Background(), LocalOption{})
	if _, err := fs.Create(filepath.Join(t.TempDir(), "no-such-dir", "f.txt")); err == nil {
		t.Fatal("Create in a missing directory should error")
	}
}
