// Package history persists finished transfers to a JSONL state file so
// the TUI can browse transfer history across sessions. Each terminal
// transfer (done/failed/canceled) is appended as one JSON line; the file
// is rotated in place once it grows past rotateAt lines.
package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Record is one finished transfer — one JSON line in the state file.
// Bytes is the total transferred (-1 when unknown, e.g. deletes). Error
// is empty on success.
type Record struct {
	Time       string `json:"time"` // RFC3339
	Op         string `json:"op"`
	Label      string `json:"label"`
	Status     string `json:"status"`
	Bytes      int64  `json:"bytes"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
	Note       string `json:"note,omitempty"`
}

const (
	// rotateAt is the line count past which Append rewrites the file;
	// rotateKeep is how many newest lines the rewrite retains.
	rotateAt   = 2000
	rotateKeep = 1000
)

// Store appends and loads records at a fixed file path. The mutex
// serializes Append (including rotation) and Load: each finished transfer
// appends from its own tea.Cmd goroutine, and an unserialized append racing
// rotate's read-rewrite-rename would be silently dropped.
type Store struct {
	mu   sync.Mutex
	path string
}

// NewStore returns a Store persisting to path. The parent directory is
// created lazily on the first Append.
func NewStore(path string) *Store { return &Store{path: path} }

// Path returns the store's file path.
func (s *Store) Path() string { return s.path }

// DefaultPath returns $XDG_STATE_HOME/lazys3/history.jsonl, falling back
// to ~/.local/state/lazys3/history.jsonl (and the temp dir when the home
// directory cannot be resolved).
func DefaultPath() string {
	// Per the XDG spec, non-absolute values must be ignored (a relative
	// path would scatter history files across working directories).
	if dir := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(dir) {
		return filepath.Join(dir, "lazys3", "history.jsonl")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "lazys3", "history.jsonl")
	}
	return filepath.Join(home, ".local", "state", "lazys3", "history.jsonl")
}

// Append writes rec as one JSON line (creating the directory and file as
// needed), then rotates the file if it grew past rotateAt lines. Callers
// run it from a tea.Cmd so the file IO never blocks Update.
func (s *Store) Append(rec Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	// Heal a torn tail (crash or disk-full mid-write): appending straight
	// onto a partial line would merge this record into it, and Load's
	// corrupt-line skip would then drop both.
	if st, serr := f.Stat(); serr == nil && st.Size() > 0 {
		var last [1]byte
		if _, rerr := f.ReadAt(last[:], st.Size()-1); rerr == nil && last[0] != '\n' {
			line = append([]byte{'\n'}, line...)
		}
	}
	_, werr := f.Write(append(line, '\n'))
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	if cerr != nil {
		return cerr
	}
	return s.rotate()
}

// Load reads the file and returns at most limit records, newest first.
// Corrupt or unparseable lines are skipped, never surfaced as an error;
// a missing or empty file loads as empty.
func (s *Store) Load(limit int) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var recs []Record
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var r Record
		if json.Unmarshal([]byte(line), &r) != nil {
			continue
		}
		recs = append(recs, r)
	}
	if limit > 0 && len(recs) > limit {
		recs = recs[len(recs)-limit:]
	}
	// The file is oldest-first; reverse so callers get newest first.
	for i, j := 0, len(recs)-1; i < j; i, j = i+1, j-1 {
		recs[i], recs[j] = recs[j], recs[i]
	}
	return recs, nil
}

// rotate rewrites the file keeping only the newest rotateKeep lines once
// it exceeds rotateAt lines. The rewrite is atomic (temp file in the same
// directory + rename) so a crash mid-rotation never loses the live file.
func (s *Store) rotate() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) <= rotateAt {
		return nil
	}
	kept := lines[len(lines)-rotateKeep:]
	tmp, err := os.CreateTemp(filepath.Dir(s.path), "history-*.tmp")
	if err != nil {
		return err
	}
	_, werr := tmp.WriteString(strings.Join(kept, "\n") + "\n")
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp.Name())
		if werr != nil {
			return werr
		}
		return cerr
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}
