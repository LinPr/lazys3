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
	"strings"
	"time"

	"github.com/LinPr/lazys3/internal/strutil"
	"github.com/LinPr/lazys3/internal/tui/components/preview"
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
	mode    fs.FileMode // kept for the preview metadata block
	isDir   bool
}

// Name returns the base name; directories end with "/".
func (e Entry) Name() string { return e.name }

// Path returns the absolute path on disk (no trailing slash).
func (e Entry) Path() string { return e.path }

func (e Entry) Size() int64        { return e.size }
func (e Entry) ModTime() time.Time { return e.modTime }
func (e Entry) IsDir() bool        { return e.isDir }

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

// PreviewKey gives the preview panel a path-unique identity: FilterValue
// is the base name only, which collides across directories (and with
// same-named profiles) and would leave a stale preview memo.
func (e Entry) PreviewKey() string { return e.path }

// GetPreviewContent returns the metadata block shown by the preview
// panel. Local entries always use the preview's synchronous path.
func (e Entry) GetPreviewContent() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Path:      %s\n", e.path)
	if e.isDir {
		sb.WriteString("Type:      directory\n")
	} else {
		fmt.Fprintf(&sb, "Size:      %s\n", strutil.HumanizeBytes(e.size))
	}
	if !e.modTime.IsZero() {
		fmt.Fprintf(&sb, "Modified:  %s\n", e.modTime.Format(time.RFC3339))
	}
	if e.mode != 0 {
		fmt.Fprintf(&sb, "Mode:      %s\n", e.mode.String())
	}
	return sb.String()
}

// GetPreviewRequest always returns nil: there is no async fetch for
// local files in v1, so the preview falls back to GetPreviewContent.
func (e Entry) GetPreviewRequest() *preview.PreviewRequest { return nil }

// LoadedMsg is the result of FetchDirCmd. Dir echoes the directory that
// was read so the Model only commits a navigation on success.
type LoadedMsg struct {
	Dir     string
	Entries []Entry
	Err     error
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
			var info fs.FileInfo
			if de.Type()&fs.ModeSymlink != 0 {
				// Follow the link target; a broken symlink keeps info nil
				// and lists as a 0-byte file.
				info, _ = os.Stat(p)
			} else {
				info, _ = de.Info()
			}
			e := Entry{name: de.Name(), path: p}
			if info != nil {
				e.isDir = info.IsDir()
				e.mode = info.Mode()
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
