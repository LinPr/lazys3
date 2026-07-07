// Package tui wires the top-level lazys3 TUI model, layout, and key dispatch.
package tui

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"strings"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/LinPr/lazys3/internal/config"
	"github.com/LinPr/lazys3/internal/history"
	"github.com/LinPr/lazys3/internal/tui/components/bucketlist"
	"github.com/LinPr/lazys3/internal/tui/components/help"
	"github.com/LinPr/lazys3/internal/tui/components/historyview"
	"github.com/LinPr/lazys3/internal/tui/components/locallist"
	"github.com/LinPr/lazys3/internal/tui/components/metaview"
	"github.com/LinPr/lazys3/internal/tui/components/modal"
	"github.com/LinPr/lazys3/internal/tui/components/objectlist"
	"github.com/LinPr/lazys3/internal/tui/components/preview"
	"github.com/LinPr/lazys3/internal/tui/components/profilelist"
	"github.com/LinPr/lazys3/internal/tui/components/statusbar"
	"github.com/LinPr/lazys3/internal/tui/components/style"
	"github.com/LinPr/lazys3/internal/tui/components/syncmodal"
	"github.com/LinPr/lazys3/internal/tui/components/transferpanel"
	"github.com/LinPr/lazys3/internal/tui/components/transferview"
	"github.com/LinPr/lazys3/internal/tui/components/versionview"
	"github.com/LinPr/lazys3/internal/tui/keybinding"
	"github.com/LinPr/lazys3/internal/tui/state"
	"github.com/LinPr/lazys3/internal/tui/types"
)

type size struct {
	width  int
	height int
}

type Model struct {
	state         state.State
	profileList   profilelist.Model
	bucketList    bucketlist.Model
	objectlist    objectlist.Model
	contentView   preview.Model
	metaView      metaview.Model
	transferPanel transferpanel.Model
	modal         modal.Model
	statusBar     statusbar.Model
	help          help.Model
	historyView   historyview.Model
	transferView  transferview.Model
	versionView   versionview.Model
	historyStore  *history.Store
	// awsFiles are the resolved AWS shared config/credentials paths
	// (--aws-config/--aws-credentials > env > ~/.aws default), threaded
	// into every S3 option so ops target the same files as the listings.
	awsFiles        config.AWSFiles
	selectedProfile string
	selectedBucket  string
	selectedObject  string
	// Dual-pane (local ⇄ remote) mode; see dualpane.go. paneFocus is
	// meaningful only while dualPane: entering focuses the local pane,
	// exiting resets to focusRemote. Focus and state are orthogonal:
	// m.state keeps meaning "which remote list is active".
	localList locallist.Model
	dualPane  bool
	paneFocus paneFocus
	// lastStatus is the last StatusUpdateMsg emitted. emitStatusUpdate
	// only publishes when the status actually changed; emitting
	// unconditionally would make every StatusUpdateMsg pass spawn the
	// next one — an infinite self-perpetuating message loop.
	lastStatus types.StatusUpdateMsg
	size
}

func NewLazyS3Model() Model {
	return NewLazyS3ModelWithConfig(config.Config{}, config.ResolveAWSFiles("", ""))
}

// NewLazyS3ModelWithConfig constructs the top-level model from the loaded
// user config (cmd/root.go loads it once and applies the theme to the
// style package before calling this) plus the resolved AWS shared file
// paths. The zero Config keeps every default.
func NewLazyS3ModelWithConfig(cfg config.Config, awsFiles config.AWSFiles) Model {
	// The bubbletea-beta1 renderer workaround (renderer.go, removed) is
	// gone: v2.0.8 dropped the TEA_STANDARD_RENDERER escape hatch, so no
	// pre-Run env tweak is possible. It forced the standard renderer on
	// two grounds, both addressed by the ultraviolet backend:
	//   - REP-less unix terminals (GNU screen, Linux console): ultraviolet
	//     strips the REP capability for those TERMs itself (ultraviolet
	//     terminal_renderer.go xtermCaps; pinned by renderer_compat_test.go).
	//   - all of Windows (conhost's partial VT support, beta1's missing
	//     resize events): ultraviolet delivers Windows resize via console
	//     input records (terminal_reader_windows.go WINDOW_BUFFER_SIZE_EVENT
	//     -> uv.WindowSizeEvent, mapped in bubbletea input.go), renders
	//     with the empty capability set when TERM is unset (xtermCaps("")
	//     emits no REP/HPA/CHT/...; conhost's usual state, also pinned by
	//     renderer_compat_test.go), and disables scroll optimizations on
	//     Windows entirely (microsoft/terminal#19016). Resize delivery
	//     under real conhost has not been live-verified on this stack.
	// The local pane always opens at [local] start_dir when configured
	// (config.Load only keeps it when the directory exists), otherwise the
	// lazys3 process's working directory, captured once here
	// (ResetToStartDir falls back to $HOME then "/" when Getwd fails).
	localList := locallist.NewModel()
	startDir := cfg.Local.StartDir
	if startDir == "" {
		if wd, err := os.Getwd(); err == nil && wd != "" {
			startDir = wd
		}
	}
	if startDir != "" {
		localList.SetStartDir(startDir)
	}
	objectList := objectlist.NewModel()
	if cfg.UI.DefaultSort != "" || cfg.UI.SortDesc {
		objectList.SetSortMode(cfg.UI.DefaultSort, cfg.UI.SortDesc)
		localList.SetSortMode(cfg.UI.DefaultSort, cfg.UI.SortDesc)
	}
	return Model{
		state:         state.ActiveProfileList,
		awsFiles:      awsFiles,
		profileList:   profilelist.NewModelWithFiles(awsFiles),
		bucketList:    bucketlist.NewModel(),
		objectlist:    objectList,
		contentView:   preview.NewModel(),
		metaView:      metaview.NewModel(),
		transferPanel: transferpanel.NewModel(),
		modal:         modal.NewModel(),
		statusBar:     statusbar.NewModel(),
		help:          help.NewModel(),
		historyView:   historyview.NewModel(),
		transferView:  transferview.NewModel(),
		versionView:   versionview.NewModel(),
		historyStore:  history.NewStore(history.DefaultPath()),
		localList:     localList,
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		// Ask the terminal for its size once at startup. bubbletea sends
		// the initial WindowSizeMsg asynchronously, and on Windows no
		// further resize events arrive at all (bubbletea#1601), so this
		// explicit query is a cheap second chance for the layout to learn
		// the real size before the user starts pressing keys.
		tea.RequestWindowSize,
		m.profileList.Init(),
		m.bucketList.Init(),
		m.objectlist.Init(),
		m.contentView.Init(),
		m.metaView.Init(),
		m.transferPanel.Init(),
		m.modal.Init(),
		m.statusBar.Init(),
		m.help.Init(),
		m.historyView.Init(),
		m.transferView.Init(),
		m.versionView.Init(),
		m.localList.Init(),
	)
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// initComponentsSize may auto-exit dual mode on a narrow resize;
		// publish the pane/selection change synchronously so the bar never
		// keeps a stale dual-pane chip. A resize is not navigation, so the
		// info note (including the "dual-pane closed" one the auto-exit
		// just set) is restored over the update's ClearInfo.
		m.initComponentsSize(msg)
		if cmd := m.emitStatusUpdate(); cmd != nil {
			note := m.statusBar.Info()
			newBar, _ := m.statusBar.Update(cmd())
			m.statusBar = newBar
			m.statusBar.SetInfo(note)
		}
		return m, nil
	case tea.KeyMsg:
		// ctrl+c is the global emergency exit. Handled before the modal
		// and help branches so the user can always force-quit, even while
		// an overlay is swallowing every other key.
		if msg.String() == "ctrl+c" {
			m.cancelAllOnQuit()
			return m, tea.Quit
		}
		// Modal takes over key dispatch when visible: forward to the
		// modal and skip list dispatch entirely. The modal's
		// onConfirm callback returns the tea.Cmd that starts the op.
		if m.modal.IsVisible() {
			newModal, cmd := m.modal.Update(msg)
			m.modal = newModal
			cmds = append(cmds, cmd)
			return m, tea.Batch(cmds...)
		}
		// Help overlay is handled before any list dispatch so '?' works
		// in every state. When the help is visible, '?'/esc closes it,
		// j/k/pgup/pgdown (and g/G) scroll it, and every other key is
		// swallowed (so the user can read the help without triggering a
		// file op by accident).
		if m.help.IsVisible() {
			if msg.String() == "?" || msg.String() == "esc" {
				m.help.Hide()
			} else {
				// Fold "shift+g" -> "G" so the jump-to-bottom key works
				// on every terminal (see keybinding.KeyString).
				m.help.HandleKey(keybinding.KeyString(msg.String()))
			}
			cmds = append(cmds, m.emitStatusUpdate())
			return m, tea.Batch(cmds...)
		}
		// History overlay mirrors the help overlay: 'T'/esc closes it,
		// j/k/pgup/pgdown scroll, everything else is swallowed so reading
		// the history can't trigger a file op.
		if m.historyView.IsVisible() {
			if keybinding.KeyString(msg.String()) == keybinding.HistoryToggle || msg.String() == "esc" {
				m.historyView.Hide()
			} else {
				m.historyView.HandleKey(msg.String())
			}
			cmds = append(cmds, m.emitStatusUpdate())
			return m, tea.Batch(cmds...)
		}
		// Transfers overlay: 't'/esc closes, j/k/pgup/pgdown/g/G move the
		// cursor over the live rows, and 'x' cancels the HIGHLIGHTED
		// transfer (outside the overlay 'x' keeps its cancel-latest
		// meaning). Everything else is swallowed. The overlay itself is
		// stateless about transfers: composeView renders it from the
		// transferpanel's live rows each frame, so the 200ms tick loop
		// keeps its progress moving while it is open.
		if m.transferView.IsVisible() {
			key := keybinding.KeyString(msg.String())
			switch {
			case key == keybinding.TransfersToggle || msg.String() == "esc":
				m.transferView.Hide()
			case key == keybinding.TransferCancel:
				// Clamp the cursor the same way View clamps the highlight:
				// pruning can shrink the rows between keys, and 'x' must
				// cancel the row the user SEES highlighted, not no-op.
				rows := m.transferPanel.Rows()
				if c := min(m.transferView.Cursor(), len(rows)-1); c >= 0 {
					if m.transferPanel.CancelByID(rows[c].ID) {
						log.Println("cancelled transfer:", rows[c].ID)
					}
				}
			default:
				m.transferView.HandleKey(key, len(m.transferPanel.Rows()))
			}
			cmds = append(cmds, m.emitStatusUpdate())
			return m, tea.Batch(cmds...)
		}
		// Versions overlay: 'v'/esc closes, j/k/pgup/pgdown move the
		// cursor, d/R/D act on the highlighted row (routed back as an
		// ActionMsg that opens the matching modal flow). 'x' keeps its
		// global cancel meaning — version ops are launched from this
		// overlay, so a running download must stay cancellable behind it.
		// Everything else is swallowed so global hotkeys never fire
		// behind the overlay.
		if m.versionView.IsVisible() {
			key := keybinding.KeyString(msg.String())
			if key == keybinding.VersionsToggle || msg.String() == "esc" {
				m.versionView.Hide()
			} else if key == keybinding.TransferCancel {
				if id, ok := m.transferPanel.CancelLatest(); ok {
					log.Println("cancelled transfer:", id)
				}
			} else if cmd := m.versionView.HandleKey(key); cmd != nil {
				cmds = append(cmds, cmd)
			}
			cmds = append(cmds, m.emitStatusUpdate())
			return m, tea.Batch(cmds...)
		}
		// Content-preview overlay ('p', floating): p/esc closes it (dropping
		// any in-flight fetch), j/k/pgup/pgdown/g/G scroll the sample, and
		// every other key is swallowed so global hotkeys never fire behind
		// the overlay. Slotted after the versions overlay in the precedence
		// chain (ctrl+c > modal > help > history > transfers > versions >
		// preview > metadata); the full-screen overlays swallow 'p'/'m'
		// while visible, so the floating pair can never stack on them.
		if m.contentView.IsVisible() {
			key := keybinding.KeyString(msg.String())
			if key == keybinding.ContentPreview || msg.String() == "esc" {
				m.contentView.Hide()
			} else {
				m.contentView.HandleKey(key)
			}
			cmds = append(cmds, m.emitStatusUpdate())
			return m, tea.Batch(cmds...)
		}
		// Metadata overlay ('m', floating): same routing as the content
		// preview — m/esc closes, scroll keys move, the rest is swallowed.
		if m.metaView.IsVisible() {
			key := keybinding.KeyString(msg.String())
			if key == keybinding.Metadata || msg.String() == "esc" {
				m.metaView.Hide()
			} else {
				m.metaView.HandleKey(key)
			}
			cmds = append(cmds, m.emitStatusUpdate())
			return m, tea.Batch(cmds...)
		}
		// While a list's filter input is focused, every key belongs to
		// the list (the user is typing a pattern): skip the global hotkey
		// switch and let the state dispatch below forward the key to the
		// active list. Without this guard, typing "data" would trigger
		// the download modal on 'd', invert the selection on 'a', and
		// quit on 'q'. ctrl+c (handled above) keeps its global meaning as
		// the emergency exit.
		//
		// Every fully-handled global key returns early so the same key
		// press is not also fed to the active list (where e.g. '?'
		// toggles the built-in full-help footer and 'right'/'left' page
		// the list).
		if !m.filtering() {
			switch keybinding.KeyString(msg.String()) {
			case "q":
				// Cancel every outstanding transfer context so no op
				// goroutine outlives the TUI.
				m.cancelAllOnQuit()
				return m, tea.Quit

			// l toggles the dual-pane (local ⇄ remote) layout; tab moves
			// focus between the panes while it is active. Outside dual
			// mode tab is a handled no-op so it never pages the list.
			case keybinding.DualPaneToggle:
				if cmd := m.handleDualPaneToggle(); cmd != nil {
					cmds = append(cmds, cmd)
				}
				cmds = append(cmds, m.emitStatusUpdate())
				return m, tea.Batch(cmds...)

			case keybinding.PaneSwitch:
				m.handlePaneSwitch()
				return m, m.emitStatusUpdate()

			case "enter", "right":
				if m.localFocused() {
					if cmd := m.localList.Enter(); cmd != nil {
						cmds = append(cmds, cmd)
					}
					cmds = append(cmds, m.emitStatusUpdate())
					return m, tea.Batch(cmds...)
				}
				cmds = append(cmds, m.handleForward(msg), m.emitStatusUpdate())
				return m, tea.Batch(cmds...)

			case "backspace", "left":
				if m.localFocused() {
					if cmd := m.localList.Up(); cmd != nil {
						cmds = append(cmds, cmd)
					}
					cmds = append(cmds, m.emitStatusUpdate())
					return m, tea.Batch(cmds...)
				}
				cmds = append(cmds, m.handleBackward(), m.emitStatusUpdate())
				return m, tea.Batch(cmds...)

			// p opens the floating content-preview overlay for the focused
			// pane's highlighted FILE (first 256 KiB, remote or local);
			// directories and non-file lists get a status-bar hint. Closing
			// is handled in the contentView.IsVisible branch above.
			case keybinding.ContentPreview:
				if cmd := m.handleContentPreview(); cmd != nil {
					cmds = append(cmds, cmd)
				}
				cmds = append(cmds, m.emitStatusUpdate())
				return m, tea.Batch(cmds...)

			// m opens the floating metadata overlay for the focused pane's
			// highlighted item: HeadObject fields for objects, the live
			// region/versioning probes for buckets, lstat facts for local
			// entries, the shared-config facts for profiles. Closing is
			// handled in the metaView.IsVisible branch above.
			case keybinding.Metadata:
				if cmd := m.handleMetadataOpen(); cmd != nil {
					cmds = append(cmds, cmd)
				}
				cmds = append(cmds, m.emitStatusUpdate())
				return m, tea.Batch(cmds...)

			// t opens the live transfers overlay (closing is handled in
			// the transferView.IsVisible branch above).
			case keybinding.TransfersToggle:
				m.transferView.Show()
				return m, m.emitStatusUpdate()

			// x cancels the most recent running transfer (its context is
			// cancelled; the op returns context.Canceled and the row renders
			// as "canceled").
			case keybinding.TransferCancel:
				if id, ok := m.transferPanel.CancelLatest(); ok {
					log.Println("cancelled transfer:", id)
				}
				return m, m.emitStatusUpdate()

			// ? toggles the help overlay. Handled here (rather than in the
			// help.IsVisible branch above) so the first '?' press opens it.
			case "?":
				m.help.Toggle()
				return m, m.emitStatusUpdate()

			// T opens the persistent transfer-history overlay. The records
			// are re-read from the state file on every open (via a tea.Cmd,
			// so the read never blocks Update); closing is handled in the
			// historyView.IsVisible branch above.
			case keybinding.HistoryToggle:
				m.historyView.Show()
				return m, tea.Batch(historyview.LoadCmd(m.historyStore), m.emitStatusUpdate())

			// v opens the object-versions overlay for the highlighted file
			// (object list only; directories error on the status bar).
			case keybinding.VersionsToggle:
				if m.localFocused() {
					m.statusBar.SetInfo(remotePaneKeyHint)
					return m, m.emitStatusUpdate()
				}
				if cmd := m.handleVersionsOpen(); cmd != nil {
					cmds = append(cmds, cmd)
				}
				cmds = append(cmds, m.emitStatusUpdate())
				return m, tea.Batch(cmds...)

			// V toggles bucket versioning on the highlighted bucket. The
			// current status is fetched first; the confirm modal opens when
			// the BucketStatusMsg arrives.
			case keybinding.VersioningToggle:
				if m.localFocused() {
					m.statusBar.SetInfo(remotePaneKeyHint)
					return m, m.emitStatusUpdate()
				}
				if cmd := m.handleVersioningToggle(); cmd != nil {
					cmds = append(cmds, cmd)
				}
				cmds = append(cmds, m.emitStatusUpdate())
				return m, tea.Batch(cmds...)

			// Multi-select: space toggles the current object's selection.
			// We handle this here (before forwarding to the list) because
			// the bubbles list does not treat space as a selection toggle
			// by default. bubbletea v2 stringifies the key as "space"; the
			// legacy " " spelling is kept for compatibility. After
			// toggling, we move the cursor down so the user can mark
			// several items in a row (the standard mc/nnn UX).
			case " ", "space":
				if m.localFocused() {
					m.localList.ToggleSelected()
					newLocal, cmd := m.localList.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
					m.localList = newLocal
					cmds = append(cmds, cmd)
					cmds = append(cmds, m.emitStatusUpdate())
					return m, tea.Batch(cmds...)
				}
				if m.state == state.ActiveObjectList {
					m.objectlist.ToggleSelected()
					// Synthesise a down-arrow and forward it to the list
					// so the cursor advances after the toggle.
					newList, cmd := m.objectlist.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
					m.objectlist = newList
					cmds = append(cmds, cmd)
				}
				cmds = append(cmds, m.emitStatusUpdate())
				return m, tea.Batch(cmds...)

			// a inverts the selection on the active object list. Pressed
			// once, it selects all visible items; pressed again, it clears
			// them. (We use invert rather than a strict "select all" so a
			// user can recover a partial selection by pressing 'a' again
			// instead of having to clear first.)
			case "a":
				if m.localFocused() {
					m.localList.InvertSelection()
					return m, m.emitStatusUpdate()
				}
				if m.state == state.ActiveObjectList {
					m.objectlist.InvertSelection()
				}
				return m, m.emitStatusUpdate()

			// File-op branches (delegated to handler.go so all the key switch
			// logic stays in one place). Only ActiveObjectList /
			// ActiveBucketList react to these; the handler returns nil for
			// other states.
			case "d", "u", "D", "r", "c", "B", "s", keybinding.YankURI, keybinding.Presign:
				// In dual mode, 'c' copies across panes and 's' prefills the
				// sync flow with both pane locations; local focus turns the
				// remaining remote-only keys into a status-bar hint.
				fileOp := m.handleFileOp
				if m.dualPane {
					fileOp = m.handleDualFileOp
				}
				if cmd := fileOp(keybinding.KeyString(msg.String())); cmd != nil {
					cmds = append(cmds, cmd)
				}
				cmds = append(cmds, m.emitStatusUpdate())
				return m, tea.Batch(cmds...)

			default:
				log.Println("key string:", msg.String())
			}
		}
	}

	// Forward overlay fetch results by TYPE (never by active state): the
	// content sample / metadata rows must reach their overlay no matter
	// which list is active. Each overlay drops stale messages via its seq.
	switch msg.(type) {
	case preview.ContentMsg:
		newCV, cmd := m.contentView.Update(msg)
		m.contentView = newCV
		cmds = append(cmds, cmd)
	case metaview.LoadedMsg:
		newMV, cmd := m.metaView.Update(msg)
		m.metaView = newMV
		cmds = append(cmds, cmd)
	}

	// Drain transfer-panel messages. The file-op Cmds emit
	// TransferAddMsg/TransferStartMsg/TransferProgressMsg/TransferDoneMsg;
	// the panel updates its rows here, and on TransferDoneMsg we also
	// surface errors via types.ErrMsg and refresh the active list so the
	// UI reflects the change.
	switch tmsg := msg.(type) {
	case transferpanel.TransferAddMsg,
		transferpanel.TransferStartMsg,
		transferpanel.TransferProgressMsg,
		transferpanel.TickMsg:
		newTP, cmd := m.transferPanel.Update(tmsg)
		m.transferPanel = newTP
		cmds = append(cmds, cmd)
	case transferpanel.TransferDoneMsg:
		newTP, cmd := m.transferPanel.Update(tmsg)
		m.transferPanel = newTP
		cmds = append(cmds, cmd)
		// The row just turned terminal: snapshot it (final status, bytes,
		// FinishedAt, note) and append it to the persistent history file.
		// The record is built here on the Update goroutine (cheap) but the
		// file IO runs inside the returned tea.Cmd.
		if hcmd := m.appendHistoryCmd(tmsg); hcmd != nil {
			cmds = append(cmds, hcmd)
		}
		switch {
		case errors.Is(tmsg.Err, context.Canceled):
			// User-cancelled: the row already renders "canceled"; no
			// error banner, no remote refresh. A cancelled LOCAL delete
			// may still have removed some entries, so the local pane is
			// refreshed to reflect whatever actually happened.
			if tmsg.Local {
				if cmd := m.localList.Refresh(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
			// A cancelled sync usually transferred part of its files:
			// refresh the touched listings so they show what landed.
			if tmsg.Op == transferpanel.OpSync {
				if cmd := m.refreshAfterOp(tmsg); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		case tmsg.Err != nil:
			cmds = append(cmds, func() tea.Msg {
				return types.ErrMsg{Err: tmsg.Err}
			})
			// A partially-failed local delete removed everything before
			// the failing entry: refresh the pane anyway.
			if tmsg.Local {
				if cmd := m.localList.Refresh(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
			// Same for a partially-failed sync (the engine keeps going
			// past per-file errors, so most files may have transferred).
			if tmsg.Op == transferpanel.OpSync {
				if cmd := m.refreshAfterOp(tmsg); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		default:
			// A finished download batch keeps the listing unchanged, so
			// refreshAfterOp skips it; clear the multi-selection here so
			// the marks don't linger after the op.
			if tmsg.Op == transferpanel.OpDownload {
				m.objectlist.ClearSelection()
			}
			// Mirror for a finished upload or local delete: clear the
			// local pane's marks (harmless in single-pane, where the
			// selection is empty).
			if tmsg.Op == transferpanel.OpUpload || tmsg.Local {
				m.localList.ClearSelection()
			}
			// Refresh whichever list the completed op touched.
			if cmd := m.refreshAfterOp(tmsg); cmd != nil {
				cmds = append(cmds, cmd)
			}
			// A restore (copy) or version delete completed while the
			// versions overlay is open changes its listing: re-fetch it.
			if m.versionView.IsVisible() && (tmsg.Op == transferpanel.OpCopy || tmsg.Op == transferpanel.OpDelete) {
				if cmd := m.versionView.Refresh(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}
	case versionview.LoadedMsg:
		newVV, cmd := m.versionView.Update(tmsg)
		m.versionView = newVV
		cmds = append(cmds, cmd)
	case versionview.ActionMsg:
		// d/R/D on an overlay row: open the matching modal flow on the
		// live model. The overlay stays open behind the modal (View gives
		// the modal render precedence while it is visible).
		if cmd := m.handleVersionAction(tmsg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case versionview.BucketStatusMsg:
		if cmd := m.handleBucketStatus(tmsg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case historyview.LoadedMsg:
		newHV, cmd := m.historyView.Update(tmsg)
		m.historyView = newHV
		cmds = append(cmds, cmd)
	case bucketlist.FetchBucketListResultMsg:
		// Routed by TYPE, not by active state: the result of a delayed
		// refresh (e.g. the re-fetch after make-bucket) can land after the
		// user already entered a bucket or backed out to the profile list.
		// The state dispatch below only feeds the ACTIVE list, so without
		// this the result would be silently dropped on another component,
		// leaving the bucket list stale and marked loading forever. When
		// the bucket list IS the active state, the state dispatch below
		// delivers it exactly as before (no double delivery).
		if m.state != state.ActiveBucketList {
			newBL, cmd := m.bucketList.Update(tmsg)
			m.bucketList = newBL
			cmds = append(cmds, cmd)
		}
	case localFSDoneMsg:
		// A local rename/mkdir finished: surface a failure, keep the
		// cursor on the touched entry on success, and refresh the pane
		// either way (the op may have partially applied).
		if tmsg.err != nil {
			cmds = append(cmds, errCmd(fmt.Errorf("%s: %w", tmsg.op, tmsg.err)))
		} else if tmsg.name != "" && tmsg.dir == m.localList.Dir() {
			m.localList.SelectOnLoad(tmsg.name)
		}
		if cmd := m.localList.Refresh(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	case locallist.LoadedMsg:
		// Local directory fetch results are routed by type (never by
		// focus): the pane commits the navigation on success and keeps
		// the previous listing (surfacing an ErrMsg) on failure.
		newLocal, cmd := m.localList.Update(tmsg)
		m.localList = newLocal
		cmds = append(cmds, cmd)
	case types.SyncPollMsg:
		// syncmodal.PollCmd snapshots the sync's shared progress state
		// into the message. Forward it to the panel and re-arm the
		// ticker while the sync is still registered (Active).
		// A snapshot taken just before the sync finished can arrive after
		// TransferDoneMsg; never let it overwrite a terminal row's final
		// summary note.
		if st, ok := m.transferPanel.Status(tmsg.TransferID); ok && st != transferpanel.StatusRunning && st != transferpanel.StatusQueued {
			break
		}
		if tmsg.Active {
			m.transferPanel.UpdateProgress(tmsg.TransferID, tmsg.Bytes, tmsg.Total, "", nil)
			note := fmt.Sprintf("%d file(s) done", tmsg.FilesDone)
			if tmsg.CurrentFile != "" {
				note += " · " + path.Base(tmsg.CurrentFile)
			}
			m.transferPanel.SetNote(tmsg.TransferID, note)
			cmds = append(cmds, tea.Every(200*time.Millisecond, syncmodal.PollCmd(tmsg.TransferID)))
		}
	case types.ShowInputModalMsg:
		// Chained modal flows (sync src → dst → flags) reopen the next
		// modal message-style so it lands on the live model.
		m.modal.Show(tmsg.Title, tmsg.Placeholder, tmsg.OnConfirm)
	case objectlist.PresignDoneMsg:
		// Presign is instant (no transfer row): failures land on the
		// status bar; a success shows the URL in a confirm modal (the
		// modal hard-wraps long bodies) and copies it to the system
		// clipboard via OSC52.
		if tmsg.Err != nil {
			cmds = append(cmds, func() tea.Msg {
				return types.ErrMsg{Err: tmsg.Err}
			})
			break
		}
		cmds = append(cmds, tea.SetClipboard(tmsg.URL))
		insecure := strings.HasPrefix(tmsg.URL, "http://")
		// PresignCmd is async (credential resolution can take seconds), so
		// the result can land while another modal, the help overlay, or the
		// history overlay is open. Never clobber those (a pending confirm or
		// half-typed input would be silently discarded, and a modal opened
		// behind a full-screen overlay would swallow keys invisibly): fall
		// back to a status-bar note — the URL is on the clipboard either way.
		// The floating p/m overlays count too: a modal popping over the text
		// the user is reading would steal its keys mid-scroll.
		if m.overlayActive() {
			note := fmt.Sprintf("presigned URL for %s copied to clipboard (valid %s)", path.Base(tmsg.Key), tmsg.Expiry)
			if insecure {
				note += " — plain HTTP"
			}
			m.statusBar.SetInfo(note)
			break
		}
		body := fmt.Sprintf("s3://%s/%s\nvalid for %s — copied to clipboard\n\n%s",
			tmsg.Bucket, tmsg.Key, tmsg.Expiry, tmsg.URL)
		if insecure {
			body += "\n\nwarning: plain-HTTP endpoint — anyone fetching this link sends the URL (a bearer credential) and receives the object unencrypted"
		}
		m.modal.ShowInfo("Presigned URL", body)
	}

	// Forward errors and status updates to the status bar. ErrMsg arrives
	// from file-op Cmds (and from sync); we surface it on the bar so the
	// user sees the failure without a separate log. StatusUpdateMsg is
	// emitted by emitStatusUpdate below; we handle it here too so the
	// bar's state stays in sync with the model's state.
	switch tmsg := msg.(type) {
	case types.ErrMsg:
		newBar, _ := m.statusBar.Update(tmsg)
		m.statusBar = newBar
	case types.StatusUpdateMsg:
		newBar, _ := m.statusBar.Update(tmsg)
		m.statusBar = newBar
	}

	// dispatch message to the active component. Key presses belong to the
	// focused pane: with the local pane focused they go to the local list
	// instead of the remote state switch. Non-key messages keep their
	// existing routes,
	// with one exception: bubbles' filtering is asynchronous — typing in
	// the filter input returns a tea.Cmd whose list.FilterMatchesMsg must
	// be fed back into the SAME list to actually narrow it. That message
	// belongs to the focused pane (only the focused pane's filter can be
	// running), so with the local pane focused it must reach the local
	// list rather than fall through to the remote state switch below —
	// otherwise the local filter input echoes keys but never narrows.
	_, isKey := msg.(tea.KeyMsg)
	_, isFilterMatches := msg.(list.FilterMatchesMsg)
	if (isKey || isFilterMatches) && m.localFocused() {
		newLocal, cmd := m.localList.Update(msg)
		m.localList = newLocal
		cmds = append(cmds, cmd, m.emitStatusUpdate())
		return m, tea.Batch(cmds...)
	}
	switch m.state {
	case state.ActiveProfileList:
		newProfileListModel, cmd := m.profileList.Update(msg)
		m.profileList = newProfileListModel
		cmds = append(cmds, cmd)

	case state.ActiveBucketList:
		newBucketListModel, cmd := m.bucketList.Update(msg)
		m.bucketList = newBucketListModel
		cmds = append(cmds, cmd)

	case state.ActiveObjectList:
		newObjectListModel, cmd := m.objectlist.Update(msg)
		m.objectlist = newObjectListModel
		cmds = append(cmds, cmd)

	default:
		log.Println("Unknown state:", m.state)
	}

	// Emit a StatusUpdateMsg reflecting the post-dispatch state so the
	// status bar always shows the current profile/bucket/prefix and the
	// current selection count. We do this last so the bar sees the
	// updated model state (e.g. after a forward navigation moves us into
	// a new prefix, the bar shows the new prefix rather than the old one).
	cmds = append(cmds, m.emitStatusUpdate())

	return m, tea.Batch(cmds...)
}

// emitStatusUpdate returns a tea.Cmd that publishes a StatusUpdateMsg
// reflecting the model's current state. It is called at the end of every
// Update pass so the status bar stays in sync with navigation and
// selection changes. It returns nil when nothing changed since the last
// emission, so a StatusUpdateMsg pass does not produce another one.
func (m *Model) emitStatusUpdate() tea.Cmd {
	// Strip the trailing slash from the selected object so the bar shows
	// a clean s3://bucket/prefix rather than s3://bucket/prefix/. When
	// no object is selected, only the bucket is shown.
	prefix := m.selectedObject
	if prefix != "" {
		prefix = strings.TrimSuffix(prefix, "/")
	}
	// The count mirrors the focused pane's selection in dual mode.
	selected := m.objectlist.SelectedCount()
	if m.localFocused() {
		selected = m.localList.SelectedCount()
	}
	// The pane indicator names the focused pane while dual mode is active.
	pane := ""
	if m.dualPane {
		pane = "remote"
		if m.paneFocus == focusLocal {
			pane = "local"
		}
	}
	// The transfer tallies participate in the dedup below, so a
	// TransferAddMsg/TransferDoneMsg pass (which falls through to the
	// final emitStatusUpdate) always refreshes the bar's summary.
	running, done, failed := m.transferPanel.Counts()
	upd := types.StatusUpdateMsg{
		Profile:          m.selectedProfile,
		Bucket:           m.selectedBucket,
		Prefix:           prefix,
		SelectedCount:    selected,
		Pane:             pane,
		TransfersRunning: running,
		TransfersDone:    done,
		TransfersFailed:  failed,
	}
	if upd == m.lastStatus {
		return nil
	}
	prev := m.lastStatus
	m.lastStatus = upd
	// Only a navigation-ish change dismisses the transient info note; a
	// transfer tally moving in the background must not wipe a note the
	// user is still reading. ClearInfo is set after the lastStatus
	// snapshot so it never participates in the dedup above.
	upd.ClearInfo = upd.Profile != prev.Profile || upd.Bucket != prev.Bucket ||
		upd.Prefix != prev.Prefix || upd.SelectedCount != prev.SelectedCount ||
		upd.Pane != prev.Pane
	return func() tea.Msg {
		return upd
	}
}

// appendHistoryCmd builds a history.Record for the transfer that tmsg just
// turned terminal and returns a tea.Cmd that appends it to the persistent
// history file. The record is snapshotted from the panel row (which
// already carries the final status, byte counts, note and FinishedAt); a
// row that was pruned before the message arrived falls back to the fields
// echoed on the message itself. The file IO happens inside the Cmd so
// Update never blocks on it.
func (m *Model) appendHistoryCmd(tmsg transferpanel.TransferDoneMsg) tea.Cmd {
	if m.historyStore == nil {
		return nil
	}
	rec := history.Record{
		Time:  time.Now().Format(time.RFC3339),
		Op:    string(tmsg.Op),
		Label: tmsg.Label,
		Bytes: -1,
		Note:  tmsg.Note,
	}
	switch {
	case errors.Is(tmsg.Err, context.Canceled):
		rec.Status = string(transferpanel.StatusCanceled)
	case tmsg.Err != nil:
		rec.Status = string(transferpanel.StatusFailed)
		rec.Error = tmsg.Err.Error()
	default:
		rec.Status = string(transferpanel.StatusDone)
	}
	if t, ok := m.transferPanel.Transfer(tmsg.ID); ok {
		rec.Status = string(t.Status)
		rec.Note = t.Note
		if t.Err != nil && t.Status == transferpanel.StatusFailed {
			rec.Error = t.Err.Error()
		}
		if t.Done > 0 {
			rec.Bytes = t.Done
		}
		if !t.FinishedAt.IsZero() {
			rec.Time = t.FinishedAt.Format(time.RFC3339)
			if !t.StartedAt.IsZero() {
				rec.DurationMs = t.FinishedAt.Sub(t.StartedAt).Milliseconds()
			}
		}
	}
	store := m.historyStore
	return func() tea.Msg {
		if err := store.Append(rec); err != nil {
			log.Println("history append:", err)
		}
		return nil
	}
}

// cancelAllOnQuit cancels every outstanding transfer and synchronously
// appends a canceled record for each row that was still queued/running.
// Quit returns tea.Quit right after this, so the op goroutines'
// TransferDoneMsg (and its append Cmd) never runs — without this, a
// transfer interrupted by quitting would leave no trace in the history
// even though the same cancellation via 'x' would.
func (m *Model) cancelAllOnQuit() {
	active := m.transferPanel.Active()
	m.transferPanel.CancelAll()
	if m.historyStore == nil {
		return
	}
	now := time.Now()
	for _, t := range active {
		if t.Progress != nil {
			t.Done, _ = t.Progress.Load()
		}
		rec := history.Record{
			Time:   now.Format(time.RFC3339),
			Op:     string(t.Op),
			Label:  t.Label,
			Status: string(transferpanel.StatusCanceled),
			Bytes:  -1,
			Note:   t.Note,
		}
		if t.Done > 0 {
			rec.Bytes = t.Done
		}
		if !t.StartedAt.IsZero() {
			rec.DurationMs = now.Sub(t.StartedAt).Milliseconds()
		}
		if err := m.historyStore.Append(rec); err != nil {
			log.Println("history append:", err)
		}
	}
}

// filtering reports whether the FOCUSED pane's filter input is focused
// (the user is typing a filter pattern), in which case global hotkeys must
// not fire. In dual mode with the local pane focused, that is the local
// list's filter; otherwise the active remote list's.
func (m Model) filtering() bool {
	if m.localFocused() {
		return m.localList.Filtering()
	}
	switch m.state {
	case state.ActiveProfileList:
		return m.profileList.Filtering()
	case state.ActiveBucketList:
		return m.bucketList.Filtering()
	case state.ActiveObjectList:
		return m.objectlist.Filtering()
	}
	return false
}

// overlayActive reports whether any key-swallowing surface (modal or
// overlay) is up. Async results that would open a modal (presign, the
// versioning-status probe) fall back to a status-bar note while one is
// active, so they never clobber a live modal or steal keys from an overlay
// the user is reading.
func (m Model) overlayActive() bool {
	return m.modal.IsVisible() || m.help.IsVisible() || m.historyView.IsVisible() ||
		m.transferView.IsVisible() || m.versionView.IsVisible() ||
		m.contentView.IsVisible() || m.metaView.IsVisible()
}

// remotePaneView renders the active remote list; the single-pane layout
// and the dual-pane left column both build on it.
func (m Model) remotePaneView() string {
	switch m.state {
	case state.ActiveProfileList:
		return m.profileList.View()
	case state.ActiveBucketList:
		return m.bucketList.View()
	case state.ActiveObjectList:
		return m.objectlist.View()
	}
	return style.ErrorStyle.Render("Unknown component")
}

// View implements tea.Model for bubbletea v2.0.8: the frame string is
// wrapped in a tea.View, which now also carries the terminal modes that
// used to be tea.NewProgram options (alt screen, focus reporting, cell-
// motion mouse) — same behavior as before, new API surface.
func (m Model) View() tea.View {
	view := tea.NewView(m.viewContent())
	view.AltScreen = true
	view.ReportFocus = true
	view.MouseMode = tea.MouseModeCellMotion
	return view
}

// viewContent renders the whole frame as a styled string (the pre-v2.0.8
// View body); tests assert against this directly.
func (m Model) viewContent() string {
	if m.dualPane {
		// Dual layout: remote pane left, local pane right.
		return m.composeView(lipgloss.JoinHorizontal(lipgloss.Top,
			m.remotePaneView(), m.localList.View()))
	}
	// Single-pane layout: the active list owns the full width.
	return m.composeView(m.remotePaneView())
}

// composeView stacks the main content above the status bar, then applies
// the overlay precedence (help > history > transfers > versions, modal on
// top). Shared by the single- and dual-pane branches of View. The lists
// own every row above the one-line status bar; transfers are ambient in
// the bar's tallies and on demand in the 't' overlay.
func (m Model) composeView(mainContent string) string {
	layout := lipgloss.JoinVertical(
		lipgloss.Top,
		mainContent,
		m.statusBar.View(),
	)

	// Help and history are full-canvas overlays (their View() returns a
	// width×height canvas with the content centered via lipgloss.Place),
	// so they replace the layout outright. Help takes precedence over the
	// modal so the user can always summon the cheat sheet, even with a
	// modal open.
	if m.help.IsVisible() {
		return m.help.View()
	}
	if m.historyView.IsVisible() {
		return m.historyView.View()
	}
	// The transfers and versions overlays are also full-canvas, but the
	// modal outranks them: a modal opened from an overlay row action (or
	// landing async, like the presign result) must be the visible, key-
	// receiving surface, floating over the still-rendered overlay
	// underneath. The transfers overlay renders from the transferpanel's
	// live rows, so every tick/progress pass repaints it current.
	if m.transferView.IsVisible() {
		layout = m.transferView.View(m.transferPanel.Rows())
	} else if m.versionView.IsVisible() {
		layout = m.versionView.View()
	}
	// The content-preview ('p') and metadata ('m') overlays are floating
	// boxes composited centered over the live layout, like the modal but
	// larger. They are mutually exclusive (each swallows the other's key
	// while visible) and can never stack on the full-screen overlays above
	// (which swallow p/m too).
	if m.contentView.IsVisible() {
		layout = placeCentered(layout, m.contentView.View(), m.width, m.height)
	} else if m.metaView.IsVisible() {
		layout = placeCentered(layout, m.metaView.View(), m.width, m.height)
	}
	// The modal is a floating box composited centered ON TOP of the live
	// layout, so the panes and status bar stay visible around it; closing
	// it just removes the box, revealing the untouched layout.
	if m.modal.IsVisible() {
		layout = placeCentered(layout, m.modal.View(), m.width, m.height)
	}

	return layout
}

// placeCentered composites the floating box over the layout, centered on
// the w×h canvas (ANSI-aware and CJK-safe; see style.PlaceOverlay).
func placeCentered(layout, box string, w, h int) string {
	x := (w - lipgloss.Width(box)) / 2
	y := (h - lipgloss.Height(box)) / 2
	return style.PlaceOverlay(layout, box, x, y)
}
