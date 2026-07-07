// Package locallist renders the local-filesystem browser pane used in
// dual-pane mode. It mirrors objectlist's shapes (list wrapper,
// pointer-map selection, dirs-first sort, substring filter, column
// delegate) but carries no S3 plumbing: it only lists, navigates,
// selects, sorts and filters.
package locallist

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/LinPr/lazys3/internal/strutil"
	tea "github.com/charmbracelet/bubbletea/v2"
)

// Entry is one directory entry. Directory names carry a trailing "/" in
// Name()/Title() so selection-map keys and rendering mirror objectlist's
// convention. Path() is always the absolute path WITHOUT trailing slash.
type Entry struct {
	name    string
	path    string
	size    int64
	modTime time.Time
	isDir   bool
	// isSymlink records the link itself (isDir/size/modTime describe the
	// stat'ed target): deleting one only unlinks it, and the local-delete
	// confirm text says so.
	isSymlink bool
}

// Name returns the base name; directories end with "/".
func (e Entry) Name() string { return e.name }

// Path returns the absolute path on disk (no trailing slash).
func (e Entry) Path() string { return e.path }

func (e Entry) Size() int64        { return e.size }
func (e Entry) ModTime() time.Time { return e.modTime }
func (e Entry) IsDir() bool        { return e.isDir }

// IsSymlink reports whether the entry is itself a symlink (the other
// fields describe its target).
func (e Entry) IsSymlink() bool { return e.isSymlink }

func (e Entry) Title() string { return e.name }

// Description renders a one-line metadata summary for the list delegate.
func (e Entry) Description() string {
	if e.isDir {
		return "<dir>"
	}
	return fmt.Sprintf("%s  %s",
		strutil.HumanizeBytes(e.size),
		e.modTime.Format("2006-01-02 15:04"))
}

func (e Entry) FilterValue() string { return e.name }

// LoadedMsg is the result of FetchDirCmd. Dir echoes the directory that
// was read so the Model only commits a navigation on success. Gen carries
// the fetch generation stamped by Model.fetch; the Model's Update drops
// messages from superseded fetches. Gen 0 (a bare FetchDirCmd) is always
// accepted.
type LoadedMsg struct {
	Dir     string
	Entries []Entry
	Err     error
	Gen     int
}

// FetchDirCmd reads dir with os.ReadDir off the Update goroutine.
// Symlinks are os.Stat'ed so a symlink-to-dir lists (and enters) as a
// directory; broken symlinks list as 0-byte files. Entries arrive
// unsorted; the Model applies the active sort in its LoadedMsg handler.
func FetchDirCmd(dir string) tea.Cmd {
	return func() tea.Msg {
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
		dirents, err := os.ReadDir(dir)
		if err != nil {
			return LoadedMsg{Dir: dir, Err: err}
		}
		entries := make([]Entry, 0, len(dirents))
		for _, de := range dirents {
			p := filepath.Join(dir, de.Name())
			isLink := de.Type()&fs.ModeSymlink != 0
			var info fs.FileInfo
			if isLink {
				// Follow the link target; a broken symlink keeps info nil
				// and lists as a 0-byte file.
				info, _ = os.Stat(p)
			} else {
				info, _ = de.Info()
			}
			e := Entry{name: de.Name(), path: p, isSymlink: isLink}
			if info != nil {
				e.isDir = info.IsDir()
				e.modTime = info.ModTime()
				if !e.isDir {
					e.size = info.Size()
				}
			}
			if e.isDir {
				e.name += "/"
			}
			entries = append(entries, e)
		}
		return LoadedMsg{Dir: dir, Entries: entries}
	}
}
