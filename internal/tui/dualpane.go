// Dual-pane (local ⇄ remote) mode: 'l' toggles the layout, 'tab' moves
// focus between the remote browser and the local-filesystem pane, and the
// file-op keys act on the FOCUSED pane's selection with the OTHER pane's
// location as the destination: 'c' copies across (either direction), 'u'
// uploads from local focus, 'd' downloads from remote focus. Files go
// through the existing transfer machinery (downloadCmds / uploadCmds);
// directories become one recursive sync row each (a one-way sync without
// --delete through the storage sync engine). The local-only ops (D/r/B/y
// with local focus) live in localops.go.
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
	"github.com/LinPr/lazys3/internal/tui/components/syncmodal"
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
// (v/V/Y) is pressed while the local pane has focus.
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

// enterDualPane switches to the dual layout, focusing the local pane and
// loading the start directory fresh (ResetToStartDir) — 'l' always opens
// the same view, no matter where a previous dual session navigated to.
// Navigation within one open session persists as usual; only close→reopen
// resets. resizeLists applies the dual layout for the current terminal
// size immediately, so both panes render at matching sizes without
// waiting for the next WindowSizeMsg or tab.
func (m *Model) enterDualPane() tea.Cmd {
	// width 0 means no WindowSizeMsg has arrived yet (bubbletea v2 on
	// Windows delivers no resize events after startup, and the initial
	// size message is sent asynchronously) — an unknown size is not a
	// narrow one, so enter and let the next WindowSizeMsg lay the panes
	// out (or auto-exit via initComponentsSize if genuinely too narrow).
	if m.width > 0 && m.width < minDualPaneWidth {
		m.statusBar.SetInfo(fmt.Sprintf("terminal too narrow for dual-pane (needs ≥%d cols)", minDualPaneWidth))
		return nil
	}
	m.dualPane = true
	m.paneFocus = focusLocal
	m.applyPaneFocus()
	m.resizeLists()
	return m.localList.ResetToStartDir()
}

// exitDualPane restores the single-pane layout (resizeLists puts the
// remote lists back at full width for the current terminal size).
func (m *Model) exitDualPane() {
	m.dualPane = false
	m.paneFocus = focusRemote
	m.applyPaneFocus()
	m.resizeLists()
}

// handlePaneSwitch moves focus between the panes ('tab'). Outside dual
// mode it is a handled no-op so tab never leaks into the active list.
func (m *Model) handlePaneSwitch() {
	if !m.dualPane {
		return
	}
	// The status bar's persistent pane indicator reflects the new focus;
	// no transient info note needed.
	if m.paneFocus == focusRemote {
		m.paneFocus = focusLocal
	} else {
		m.paneFocus = focusRemote
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
		case keybinding.YankURI:
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

// dirSync is one directory-level recursive transfer built by the dual-pane
// copy flows: a one-way sync (no --delete) through the sync engine.
type dirSync struct {
	src, dst, label string
}

// dirSyncCmds turns the specs into sync transfer rows with the full
// syncmodal wiring (cancellable ctx, 200ms poll loop, files-done note,
// summary note, history record).
func dirSyncCmds(specs []dirSync, conn connParams) []tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(specs))
	for _, d := range specs {
		cmds = append(cmds, syncTransferCmd(d.src, d.dst, d.label, syncmodal.Flags{}, conn))
	}
	return cmds
}

// transferCountPhrase renders "N file(s)", "M folder(s)" or "N file(s) and
// M folder(s)" for the dual-pane confirm bodies.
func transferCountPhrase(files, dirs int) string {
	switch {
	case dirs == 0:
		return fmt.Sprintf("%d file(s)", files)
	case files == 0:
		return fmt.Sprintf("%d folder(s)", dirs)
	default:
		return fmt.Sprintf("%d file(s) and %d folder(s)", files, dirs)
	}
}

// localDirSyncs builds the specs for a local→remote directory copy: each
// directory syncs recursively to s3://bucket/prefix/<name>/. A '?' or '*'
// anywhere in the destination key would be parsed as a glob wildcard by
// storage.NewStorageURL (truncating the prefix and uploading to the wrong
// keys), so those entries are refused and returned in skipped. A symlink
// to a directory is resolved to its target — the sync engine walks real
// directories only.
func localDirSyncs(dirs []locallist.Entry, bucket, prefix string) (specs []dirSync, skipped []string) {
	p := prefix
	if p != "" && !strings.HasSuffix(p, "/") {
		p += "/"
	}
	for _, e := range dirs {
		name := strings.TrimSuffix(e.Name(), "/")
		if strings.ContainsAny(p+name, "?*") {
			skipped = append(skipped, name+"/")
			continue
		}
		src := e.Path()
		if e.IsSymlink() {
			if resolved, err := filepath.EvalSymlinks(src); err == nil {
				src = resolved
			}
		}
		dst := fmt.Sprintf("s3://%s/%s%s/", bucket, p, name)
		specs = append(specs, dirSync{
			src:   src,
			dst:   dst,
			label: fmt.Sprintf("dir: %s/ -> %s", name, dst),
		})
	}
	return specs, skipped
}

// remoteDirSyncs builds the specs for a remote→local directory copy: each
// prefix syncs recursively into <localDir>/<name> (the sync engine creates
// the destination directory). Keys containing '?' or '*' are refused (see
// localDirSyncs) — the source URL would list a truncated prefix and sync
// the wrong subtree.
func remoteDirSyncs(dirs []objectlist.Object, bucket, localDir string) (specs []dirSync, skipped []string) {
	for _, o := range dirs {
		name := path.Base(strings.TrimSuffix(o.Name(), "/"))
		if strings.ContainsAny(o.Name(), "?*") {
			skipped = append(skipped, name+"/")
			continue
		}
		dst := filepath.Join(localDir, name)
		specs = append(specs, dirSync{
			src:   fmt.Sprintf("s3://%s/%s", bucket, o.Name()),
			dst:   dst,
			label: fmt.Sprintf("dir: %s/ -> %s", name, dst),
		})
	}
	return specs, skipped
}

// wildcardSkipNote renders the confirm-body suffix / error text for
// entries refused by the dir-sync builders.
func wildcardSkipNote(skipped []string) string {
	return fmt.Sprintf("%d folder(s) skipped ('?'/'*' in the s3 path is not supported): %s",
		len(skipped), strings.Join(skipped, ", "))
}

// promptCopyToLocal ('c' or 'd' with the remote pane focused) confirms and
// downloads the remote selection (the highlighted item when nothing is
// marked) into the local pane's current directory. Files go through the
// single-pane 'd' machinery (one download row each); each directory prefix
// becomes one recursive sync row (the engine lists the prefix recursively
// and creates the destination directory itself).
func (m *Model) promptCopyToLocal() tea.Cmd {
	if m.state != state.ActiveObjectList {
		m.statusBar.SetInfo("open a bucket to copy from")
		return nil
	}
	var files, dirs []objectlist.Object
	for _, o := range m.objectlist.SelectedObjects() {
		if o.IsDir() {
			dirs = append(dirs, o)
		} else {
			files = append(files, o)
		}
	}
	if len(files) == 0 && len(dirs) == 0 {
		obj := m.objectlist.GetSelectedObject()
		if obj == nil {
			return nil
		}
		if obj.IsDir() {
			dirs = append(dirs, *obj)
		} else {
			files = append(files, *obj)
		}
	}
	localDir := m.localList.Dir()
	if localDir == "" {
		m.statusBar.SetInfo("local pane is still loading — try again")
		return nil
	}
	opt := m.objectListOptionFromState()
	bucket := m.selectedBucket
	// Resolve the dir sync specs and connection params now — the confirm
	// callback runs against a stale model.
	syncs, skipped := remoteDirSyncs(dirs, bucket, localDir)
	if len(files) == 0 && len(syncs) == 0 {
		return errCmd(fmt.Errorf("copy: %s", wildcardSkipNote(skipped)))
	}
	conn := m.syncConnParams()
	body := fmt.Sprintf("Download %s to %s?", transferCountPhrase(len(files), len(syncs)), localDir)
	if len(skipped) > 0 {
		body += " (" + wildcardSkipNote(skipped) + ")"
	}
	m.modal.ShowConfirm(
		"Copy to local",
		body,
		func() tea.Cmd {
			var cmds []tea.Cmd
			if len(files) > 0 {
				cmds = append(cmds, downloadCmds(opt, bucket, files, func(o objectlist.Object) string {
					return filepath.Join(localDir, path.Base(o.Name()))
				}))
			}
			cmds = append(cmds, dirSyncCmds(syncs, conn)...)
			return tea.Batch(cmds...)
		},
	)
	return nil
}

// promptCopyToRemote ('c' or 'u' with the local pane focused) confirms and
// uploads the local selection (the highlighted entry when nothing is
// marked) to the remote pane's current s3://bucket/prefix. Files go
// through uploadCmds (one upload row each); each directory becomes one
// recursive sync row targeting s3://bucket/prefix/<name>/.
func (m *Model) promptCopyToRemote() tea.Cmd {
	if m.state != state.ActiveObjectList || m.selectedBucket == "" {
		m.statusBar.SetInfo("open a bucket in the remote pane first (tab)")
		return nil
	}
	var files, dirs []locallist.Entry
	for _, e := range m.localList.SelectedEntries() {
		if e.IsDir() {
			dirs = append(dirs, e)
		} else {
			files = append(files, e)
		}
	}
	if len(files) == 0 && len(dirs) == 0 {
		e := m.localList.GetSelectedEntry()
		if e == nil {
			return nil
		}
		if e.IsDir() {
			dirs = append(dirs, *e)
		} else {
			files = append(files, *e)
		}
	}
	paths := make([]string, 0, len(files))
	for _, e := range files {
		paths = append(paths, e.Path())
	}
	opt := m.objectListOptionFromState()
	bucket := m.selectedBucket
	prefix := strings.TrimPrefix(m.selectedObject, "/")
	// Resolve the dir sync specs and connection params now — the confirm
	// callback runs against a stale model.
	syncs, skipped := localDirSyncs(dirs, bucket, prefix)
	if len(paths) == 0 && len(syncs) == 0 {
		return errCmd(fmt.Errorf("copy: %s", wildcardSkipNote(skipped)))
	}
	conn := m.syncConnParams()
	body := fmt.Sprintf("Upload %s to s3://%s/%s?", transferCountPhrase(len(paths), len(syncs)), bucket, prefix)
	if len(skipped) > 0 {
		body += " (" + wildcardSkipNote(skipped) + ")"
	}
	m.modal.ShowConfirm(
		"Copy to remote",
		body,
		func() tea.Cmd {
			var cmds []tea.Cmd
			if len(paths) > 0 {
				cmds = append(cmds, uploadCmds(opt, bucket, prefix, paths))
			}
			cmds = append(cmds, dirSyncCmds(syncs, conn)...)
			return tea.Batch(cmds...)
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
