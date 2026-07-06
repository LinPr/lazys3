// Dual-pane (local ⇄ remote) mode: 'l' toggles the layout, 'tab' moves
// focus between the remote browser and the local-filesystem pane, and the
// file-op keys act on the FOCUSED pane's selection with the OTHER pane's
// location as the destination: 'c' copies across (either direction), 'u'
// uploads from local focus, 'd' downloads from remote focus — all through
// the existing transfer machinery (downloadCmds / uploadCmds). The local-
// only ops (D/r/B/y with local focus) live in localops.go.
package tui

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea/v2"

	"github.com/LinPr/lazys3/internal/tui/components/locallist"
	"github.com/LinPr/lazys3/internal/tui/components/objectlist"
	"github.com/LinPr/lazys3/internal/tui/components/transferpanel"
	"github.com/LinPr/lazys3/internal/tui/keybinding"
	"github.com/LinPr/lazys3/internal/tui/state"
)

// paneFocus identifies which pane owns list-navigation keys in dual mode.
type paneFocus int

const (
	focusRemote paneFocus = iota // left pane: existing profile/bucket/object browser
	focusLocal                   // right pane: local filesystem browser
)

// minDualPaneWidth is the narrowest terminal dual-pane mode renders
// legibly in: 40-col panes minus 2 border cols leave 38, which keeps the
// delegate's marker+name+size columns readable (mtime degrades off).
const minDualPaneWidth = 80

// remotePaneKeyHint is the status-bar nudge shown when a remote-only key
// (v/V) is pressed while the local pane has focus.
const remotePaneKeyHint = "remote-pane key — press tab to switch"

// localFocused reports whether the local pane owns list-navigation keys.
func (m Model) localFocused() bool {
	return m.dualPane && m.paneFocus == focusLocal
}

// applyPaneFocus pushes the current focus onto every pane's border color.
// Outside dual mode the remote lists are always focused (the single-pane
// render is unchanged) and the local pane is not.
func (m *Model) applyPaneFocus() {
	remote := !m.dualPane || m.paneFocus == focusRemote
	m.profileList.SetFocused(remote)
	m.bucketList.SetFocused(remote)
	m.objectlist.SetFocused(remote)
	m.localList.SetFocused(m.dualPane && m.paneFocus == focusLocal)
}

// handleDualPaneToggle enters or exits dual-pane mode ('l').
func (m *Model) handleDualPaneToggle() tea.Cmd {
	if m.dualPane {
		m.exitDualPane()
		return nil
	}
	return m.enterDualPane()
}

// enterDualPane switches to the dual layout, starting on the remote pane.
// The preview is closed (matching exit/switch) so 'l' visibly swaps the
// preview for the local pane rather than rendering an identical layout.
// The local pane's first directory fetch is lazy (EnsureLoaded), so its
// last visited directory persists across toggles.
func (m *Model) enterDualPane() tea.Cmd {
	if m.width < minDualPaneWidth {
		m.statusBar.SetInfo(fmt.Sprintf("terminal too narrow for dual-pane (needs ≥%d cols)", minDualPaneWidth))
		return nil
	}
	m.dualPane = true
	m.paneFocus = focusRemote
	m.applyPaneFocus()
	m.closePreview()
	m.resizeLists()
	return m.localList.EnsureLoaded()
}

// exitDualPane restores the single-pane layout. The preview is closed so
// single-pane never renders with a stale dual-sized preview.
func (m *Model) exitDualPane() {
	m.dualPane = false
	m.paneFocus = focusRemote
	m.applyPaneFocus()
	m.closePreview()
	m.resizeLists()
}

// handlePaneSwitch moves focus between the panes ('tab'). Outside dual
// mode it is a handled no-op so tab never leaks into the active list. The
// preview is closed before switching (matching the closePreview-on-
// navigation precedent) — 'p' re-opens it against the new focus.
func (m *Model) handlePaneSwitch() {
	if !m.dualPane {
		return
	}
	m.closePreview()
	if m.paneFocus == focusRemote {
		m.paneFocus = focusLocal
		m.statusBar.SetInfo("pane: local")
	} else {
		m.paneFocus = focusRemote
		m.statusBar.SetInfo("pane: remote")
	}
	m.applyPaneFocus()
	m.resizeLists()
}

// handleDualFileOp dispatches the file-op keys while dual-pane mode is
// active. Keys act on the FOCUSED pane's selection with the OTHER pane's
// current location as the destination, never asking the user to type a
// path both panes already know:
//
//	local focus:  c/u upload to the remote bucket/prefix, D deletes,
//	              r renames, B mkdirs, y yanks the path (localops.go);
//	              d hints (downloads come from the remote pane).
//	remote focus: c/d download into the local pane's directory; the other
//	              keys keep their remote meaning (handleFileOp);
//	              u hints (uploads come from the local pane).
//	both:         's' prefills the sync flow focused pane → other pane.
func (m *Model) handleDualFileOp(key string) tea.Cmd {
	if m.paneFocus == focusLocal {
		switch key {
		case "c", "u":
			return m.promptCopyToRemote()
		case "d":
			m.statusBar.SetInfo("press tab: d downloads from the remote pane")
			return nil
		case "s":
			return m.promptDualSync()
		case "D":
			return m.promptLocalDelete()
		case "r":
			return m.promptLocalRename()
		case "B":
			return m.promptLocalMkdir()
		case keybinding.PresignYank:
			return m.localYankPath()
		default:
			m.statusBar.SetInfo(remotePaneKeyHint)
			return nil
		}
	}
	switch key {
	case "c", "d":
		return m.promptCopyToLocal()
	case "u":
		m.statusBar.SetInfo("press tab: u uploads from the local pane")
		return nil
	case "s":
		return m.promptDualSync()
	default:
		return m.handleFileOp(key)
	}
}

// promptCopyToLocal ('c' or 'd' with the remote pane focused) confirms and
// downloads the remote selection (files only; the highlighted item when
// nothing is marked) into the local pane's current directory. Rows,
// progress and cancellation are identical to the single-pane 'd' flow.
func (m *Model) promptCopyToLocal() tea.Cmd {
	if m.state != state.ActiveObjectList {
		m.statusBar.SetInfo("open a bucket to copy from")
		return nil
	}
	var files []objectlist.Object
	skipped := 0
	for _, o := range m.objectlist.SelectedObjects() {
		if o.IsDir() {
			skipped++
			continue
		}
		files = append(files, o)
	}
	if len(files) == 0 && skipped == 0 {
		obj := m.objectlist.GetSelectedObject()
		if obj == nil {
			return nil
		}
		if obj.IsDir() {
			skipped++
		} else {
			files = append(files, *obj)
		}
	}
	if len(files) == 0 {
		if skipped > 0 {
			return errCmd(fmt.Errorf("copy: directories are skipped in dual-pane copy (v1)"))
		}
		return nil
	}
	localDir := m.localList.Dir()
	if localDir == "" {
		m.statusBar.SetInfo("local pane is still loading — try again")
		return nil
	}
	opt := m.objectListOptionFromState()
	bucket := m.selectedBucket
	body := fmt.Sprintf("Download %d file(s) to %s?", len(files), localDir)
	if skipped > 0 {
		body += fmt.Sprintf(" (%d dir(s) skipped)", skipped)
	}
	m.modal.ShowConfirm(
		"Copy to local",
		body,
		func() tea.Cmd {
			return downloadCmds(opt, bucket, files, func(o objectlist.Object) string {
				return filepath.Join(localDir, path.Base(o.Name()))
			})
		},
	)
	return nil
}

// promptCopyToRemote ('c' or 'u' with the local pane focused) confirms and
// uploads the local selection (files only; the highlighted entry when
// nothing is marked) to the remote pane's current s3://bucket/prefix.
func (m *Model) promptCopyToRemote() tea.Cmd {
	if m.state != state.ActiveObjectList || m.selectedBucket == "" {
		m.statusBar.SetInfo("open a bucket in the remote pane first (tab)")
		return nil
	}
	var files []locallist.Entry
	skipped := 0
	for _, e := range m.localList.SelectedEntries() {
		if e.IsDir() {
			skipped++
			continue
		}
		files = append(files, e)
	}
	if len(files) == 0 && skipped == 0 {
		e := m.localList.GetSelectedEntry()
		if e == nil {
			return nil
		}
		if e.IsDir() {
			skipped++
		} else {
			files = append(files, *e)
		}
	}
	if len(files) == 0 {
		if skipped > 0 {
			return errCmd(fmt.Errorf("copy: directories are skipped in dual-pane copy (v1)"))
		}
		return nil
	}
	paths := make([]string, 0, len(files))
	for _, e := range files {
		paths = append(paths, e.Path())
	}
	opt := m.objectListOptionFromState()
	bucket := m.selectedBucket
	prefix := strings.TrimPrefix(m.selectedObject, "/")
	body := fmt.Sprintf("Upload %d file(s) to s3://%s/%s?", len(paths), bucket, prefix)
	if skipped > 0 {
		body += fmt.Sprintf(" (%d dir(s) skipped)", skipped)
	}
	m.modal.ShowConfirm(
		"Copy to remote",
		body,
		func() tea.Cmd {
			return uploadCmds(opt, bucket, prefix, paths)
		},
	)
	return nil
}

// uploadCmds builds one transfer row + UploadCmd pair per local path
// (mirror of downloadCmds). Each transfer owns a cancellable context
// (stored on the row for the 'x' key) and a shared Progress counter the
// panel's tick loop renders. UploadCmd derives the object key from
// opt.S3Uri's prefix + the path's basename; the label mirrors that.
func uploadCmds(opt objectlist.Option, bucket, prefix string, paths []string) tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(paths))
	for _, localPath := range paths {
		id := transferpanel.NewID()
		prog := transferpanel.NewProgress()
		ctx, cancel := context.WithCancel(context.Background())
		key := path.Base(localPath)
		if prefix != "" {
			p := prefix
			if !strings.HasSuffix(p, "/") {
				p += "/"
			}
			key = p + key
		}
		// Sequence each add+op pair so the row exists before the op can
		// emit its TransferDoneMsg (a fast-failing op would otherwise
		// leave a permanently-running row).
		cmds = append(cmds, tea.Sequence(
			addTransferCmd(transferpanel.Transfer{
				ID:       id,
				Op:       transferpanel.OpUpload,
				Label:    fmt.Sprintf("%s -> s3://%s/%s", localPath, bucket, key),
				Status:   transferpanel.StatusRunning,
				Progress: prog,
				Cancel:   cancel,
			}),
			objectlist.UploadCmd(ctx, opt, localPath, id, prog),
		))
	}
	return tea.Batch(cmds...)
}

// promptDualSync ('s' in dual mode) opens the sync modal chain prefilled
// with the focused pane's location as source and the other pane's as
// destination. Both prefills are editable in the modals.
func (m *Model) promptDualSync() tea.Cmd {
	remoteLoc := m.remoteLocation()
	localDir := m.localList.Dir()
	if m.paneFocus == focusLocal {
		return m.promptSync(localDir, remoteLoc)
	}
	return m.promptSync(remoteLoc, localDir)
}

// remoteLocation returns the remote pane's current location as an s3 URI
// (s3://bucket[/prefix]), or "" when no bucket is open.
func (m *Model) remoteLocation() string {
	if m.state != state.ActiveObjectList || m.selectedBucket == "" {
		return ""
	}
	if m.selectedObject != "" {
		return fmt.Sprintf("s3://%s/%s", m.selectedBucket, m.selectedObject)
	}
	return fmt.Sprintf("s3://%s", m.selectedBucket)
}
