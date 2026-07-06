// Local-filesystem operations for the dual-pane local pane (dispatched by
// handleDualFileOp with local focus): D deletes, r renames, B creates a
// directory, y copies the highlighted path to the clipboard. Delete runs
// through the transfer panel (row + history) like the remote ops; rename
// and mkdir are instant and report back via localFSDoneMsg, which tui.go
// turns into a pane refresh (keeping the cursor on the touched entry).
package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea/v2"

	"github.com/LinPr/lazys3/internal/tui/components/locallist"
	"github.com/LinPr/lazys3/internal/tui/components/transferpanel"
)

// localFSDoneMsg is the result of a local rename/mkdir cmd. name, when
// non-empty, is the entry Name() the cursor should land on after the
// refresh (directories carry their trailing "/").
type localFSDoneMsg struct {
	op   string // "rename" | "mkdir"
	dir  string
	name string
	err  error
}

// localTargets returns the local pane's multi-selection, falling back to
// the highlighted entry.
func (m *Model) localTargets() []locallist.Entry {
	if sel := m.localList.SelectedEntries(); len(sel) > 0 {
		return sel
	}
	if e := m.localList.GetSelectedEntry(); e != nil {
		return []locallist.Entry{*e}
	}
	return nil
}

// validLocalName rejects entry names that would escape the pane's current
// directory: empty, "." / "..", or anything containing a path separator.
func validLocalName(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("invalid name %q", name)
	}
	if strings.ContainsRune(name, '/') || strings.ContainsRune(name, os.PathSeparator) {
		return fmt.Errorf("name must not contain path separators")
	}
	return nil
}

// promptLocalDelete ('D' with the local pane focused) confirms and deletes
// the local selection (the highlighted entry when nothing is marked).
// Files use os.Remove; directories os.RemoveAll — the confirm body calls
// out every directory as a recursive delete so the modal never hides that
// a whole tree is going away (a symlink-to-dir is called out as a link
// removal instead: only the link goes). There is no trash: this is
// permanent.
func (m *Model) promptLocalDelete() tea.Cmd {
	entries := m.localTargets()
	if len(entries) == 0 {
		return nil
	}
	body := fmt.Sprintf("Delete %d item(s) from %s? (permanent, no trash)", len(entries), m.localList.Dir())
	for _, e := range entries {
		switch {
		case e.IsDir() && e.IsSymlink():
			// RemoveAll on a symlink only unlinks it; the confirm must not
			// claim the target tree is going away.
			body += fmt.Sprintf("\nremove symlink %s (target untouched)", strings.TrimSuffix(e.Name(), "/"))
		case e.IsDir():
			body += fmt.Sprintf("\nrecursively delete directory %s", strings.TrimSuffix(e.Name(), "/"))
		}
	}
	label := fmt.Sprintf("local: %d item(s)", len(entries))
	m.modal.ShowConfirm(
		"Delete local files",
		body,
		func() tea.Cmd {
			id := transferpanel.NewID()
			ctx, cancel := context.WithCancel(context.Background())
			// Sequence (not Batch): the row must exist before the op can
			// emit its TransferDoneMsg, or a fast-failing op leaves a
			// permanently-running row.
			return tea.Sequence(
				addTransferCmd(transferpanel.Transfer{
					ID:     id,
					Op:     transferpanel.OpDelete,
					Label:  label,
					Status: transferpanel.StatusRunning,
					Cancel: cancel,
				}),
				localDeleteCmd(ctx, entries, id, label),
			)
		},
	)
	return nil
}

// localDeleteCmd removes the entries off the Update goroutine. A failing
// entry does not stop the batch; the done message carries the first error,
// wrapped with the failure count when more than one entry failed, so a
// partially-applied batch is never reported as a single failure (tui.go
// surfaces it and refreshes the pane either way, so the listing reflects
// whatever was actually removed).
func localDeleteCmd(ctx context.Context, entries []locallist.Entry, id, label string) tea.Cmd {
	return func() tea.Msg {
		var firstErr error
		failed := 0
		for _, e := range entries {
			if err := ctx.Err(); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				break
			}
			var err error
			if e.IsDir() {
				err = os.RemoveAll(e.Path())
			} else {
				err = os.Remove(e.Path())
			}
			if err != nil {
				failed++
				if firstErr == nil {
					firstErr = err
				}
			}
		}
		if failed > 1 {
			firstErr = fmt.Errorf("%d of %d items failed, first: %w", failed, len(entries), firstErr)
		}
		return transferpanel.TransferDoneMsg{ID: id, Err: firstErr, Op: transferpanel.OpDelete, Label: label, Local: true}
	}
}

// promptLocalRename ('r' with the local pane focused) renames the
// highlighted entry within its directory. Single entry only: a multi-
// selection surfaces a status-bar error instead of guessing a target.
func (m *Model) promptLocalRename() tea.Cmd {
	if n := m.localList.SelectedCount(); n > 1 {
		return errCmd(fmt.Errorf("rename: works on a single entry (%d selected)", n))
	}
	e := m.localList.GetSelectedEntry()
	if e == nil {
		return nil
	}
	oldName := strings.TrimSuffix(e.Name(), "/")
	oldPath := e.Path()
	dir := m.localList.Dir()
	isDir := e.IsDir()
	m.modal.Show(
		fmt.Sprintf("Rename %s to", oldName),
		oldName,
		func(newName string) tea.Cmd {
			if err := validLocalName(newName); err != nil {
				return errCmd(fmt.Errorf("rename: %w", err))
			}
			if newName == oldName {
				return nil
			}
			selectName := newName
			if isDir {
				selectName += "/"
			}
			return func() tea.Msg {
				newPath := filepath.Join(dir, newName)
				// rename(2) silently replaces an existing target; refuse
				// instead (Lstat so a symlink at the name still counts).
				if _, err := os.Lstat(newPath); err == nil {
					return localFSDoneMsg{op: "rename", dir: dir, err: fmt.Errorf("%s already exists", newName)}
				}
				err := os.Rename(oldPath, newPath)
				return localFSDoneMsg{op: "rename", dir: dir, name: selectName, err: err}
			}
		},
	)
	return nil
}

// promptLocalMkdir ('B' with the local pane focused) creates a directory
// under the pane's current dir. Nested names (a/b) are allowed via
// os.MkdirAll; empty, separators-only and non-local (".." escaping)
// names are rejected.
func (m *Model) promptLocalMkdir() tea.Cmd {
	dir := m.localList.Dir()
	if dir == "" {
		m.statusBar.SetInfo("local pane is still loading — try again")
		return nil
	}
	m.modal.Show(
		fmt.Sprintf("New directory in %s", dir),
		"new-dir",
		func(name string) tea.Cmd {
			trimmed := strings.Trim(strings.TrimSpace(name), "/")
			if trimmed == "" {
				return errCmd(fmt.Errorf("mkdir: empty directory name"))
			}
			// ".." segments would escape the pane's directory (the modal
			// promises the new dir lives under it).
			if !filepath.IsLocal(trimmed) {
				return errCmd(fmt.Errorf("mkdir: %q escapes %s", trimmed, dir))
			}
			// The cursor lands on the first created segment after the
			// refresh (the deeper segments are not in this listing).
			first, _, _ := strings.Cut(trimmed, "/")
			return func() tea.Msg {
				err := os.MkdirAll(filepath.Join(dir, trimmed), 0o755)
				return localFSDoneMsg{op: "mkdir", dir: dir, name: first + "/", err: err}
			}
		},
	)
	return nil
}

// localYankPath ('y' with the local pane focused) copies the highlighted
// entry's absolute path to the system clipboard via OSC52 — the local
// mirror of the remote s3:// URI yank (yankRemoteURI).
func (m *Model) localYankPath() tea.Cmd {
	e := m.localList.GetSelectedEntry()
	if e == nil {
		return nil
	}
	m.statusBar.SetInfo("path copied: " + e.Path())
	return tea.SetClipboard(e.Path())
}
