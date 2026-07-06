// Package tui wires the top-level lazys3 TUI model, layout, and key dispatch.
package tui

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"

	"github.com/LinPr/lazys3/internal/history"
	"github.com/LinPr/lazys3/internal/tui/components/bucketlist"
	"github.com/LinPr/lazys3/internal/tui/components/help"
	"github.com/LinPr/lazys3/internal/tui/components/historyview"
	"github.com/LinPr/lazys3/internal/tui/components/modal"
	"github.com/LinPr/lazys3/internal/tui/components/objectlist"
	"github.com/LinPr/lazys3/internal/tui/components/preview"
	"github.com/LinPr/lazys3/internal/tui/components/profilelist"
	"github.com/LinPr/lazys3/internal/tui/components/statusbar"
	"github.com/LinPr/lazys3/internal/tui/components/style"
	"github.com/LinPr/lazys3/internal/tui/components/syncmodal"
	"github.com/LinPr/lazys3/internal/tui/components/transferpanel"
	"github.com/LinPr/lazys3/internal/tui/keybinding"
	"github.com/LinPr/lazys3/internal/tui/state"
	"github.com/LinPr/lazys3/internal/tui/types"
)

type size struct {
	width  int
	height int
}

type Model struct {
	state           state.State
	profileList     profilelist.Model
	bucketList      bucketlist.Model
	objectlist      objectlist.Model
	previewPanel    preview.Model
	transferPanel   transferpanel.Model
	modal           modal.Model
	statusBar       statusbar.Model
	help            help.Model
	historyView     historyview.Model
	historyStore    *history.Store
	selectedProfile string
	selectedBucket  string
	selectedObject  string
	// lastStatus is the last StatusUpdateMsg emitted. emitStatusUpdate
	// only publishes when the status actually changed; emitting
	// unconditionally would make every StatusUpdateMsg pass spawn the
	// next one — an infinite self-perpetuating message loop.
	lastStatus types.StatusUpdateMsg
	size
}

func NewLazyS3Model() Model {
	// Work around a bubbletea-beta1 renderer bug under GNU screen / the
	// Linux console before tea.Program picks its renderer (see renderer.go).
	ensureCompatRenderer()
	return Model{
		state:         state.ActiveProfileList,
		profileList:   profilelist.NewModel(),
		bucketList:    bucketlist.NewModel(),
		objectlist:    objectlist.NewModel(),
		previewPanel:  preview.NewPreviewModel(),
		transferPanel: transferpanel.NewModel(),
		modal:         modal.NewModel(),
		statusBar:     statusbar.NewModel(),
		help:          help.NewModel(),
		historyView:   historyview.NewModel(),
		historyStore:  history.NewStore(history.DefaultPath()),
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.profileList.Init(),
		m.bucketList.Init(),
		m.objectlist.Init(),
		m.transferPanel.Init(),
		m.modal.Init(),
		m.statusBar.Init(),
		m.help.Init(),
		m.historyView.Init(),
	)
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.initComponentsSize(msg)
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
		// modal and skip list/preview dispatch entirely. The modal's
		// onConfirm callback returns the tea.Cmd that starts the op.
		if m.modal.IsVisible() {
			newModal, cmd := m.modal.Update(msg)
			m.modal = newModal
			cmds = append(cmds, cmd)
			return m, tea.Batch(cmds...)
		}
		// Help overlay is handled before any list dispatch so '?' works
		// in every state. When the help is visible, '?' closes it and
		// every other key is ignored (so the user can read the help
		// without triggering a file op by accident).
		if m.help.IsVisible() {
			if msg.String() == "?" || msg.String() == "esc" {
				m.help.Hide()
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

			case "enter", "right":
				cmds = append(cmds, m.handleForward(msg), m.emitStatusUpdate())
				return m, tea.Batch(cmds...)

			case "backspace", "left":
				cmds = append(cmds, m.handleBackward(), m.emitStatusUpdate())
				return m, tea.Batch(cmds...)

			case "p":
				m.handlePreviewToggle()
				// Kick off the fetch for the highlighted item when the
				// preview just opened (SetContent skips hidden panels).
				cmds = append(cmds, m.previewCmdForSelection(), m.emitStatusUpdate())
				return m, tea.Batch(cmds...)

			// t toggles the transfer panel visibility.
			case "t":
				m.transferPanel.Toggle()
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

			// Multi-select: space toggles the current object's selection.
			// We handle this here (before forwarding to the list) because
			// the bubbles list does not treat space as a selection toggle
			// by default. bubbletea v2 stringifies the key as "space"; the
			// legacy " " spelling is kept for compatibility. After
			// toggling, we move the cursor down so the user can mark
			// several items in a row (the standard mc/nnn UX).
			case " ", "space":
				if m.state == state.ActiveObjectList {
					m.objectlist.ToggleSelected()
					// Synthesise a down-arrow and forward it to the list
					// so the cursor advances after the toggle.
					newList, cmd := m.objectlist.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
					m.objectlist = newList
					cmds = append(cmds, cmd)
					// Refresh the preview for the newly-highlighted row.
					if obj := m.objectlist.GetSelectedObject(); obj != nil {
						if pc := m.previewPanel.SetContent(obj); pc != nil {
							cmds = append(cmds, pc)
						}
					}
				}
				cmds = append(cmds, m.emitStatusUpdate())
				return m, tea.Batch(cmds...)

			// a inverts the selection on the active object list. Pressed
			// once, it selects all visible items; pressed again, it clears
			// them. (We use invert rather than a strict "select all" so a
			// user can recover a partial selection by pressing 'a' again
			// instead of having to clear first.)
			case "a":
				if m.state == state.ActiveObjectList {
					m.objectlist.InvertSelection()
				}
				return m, m.emitStatusUpdate()

			// File-op branches (delegated to handler.go so all the key switch
			// logic stays in one place). Only ActiveObjectList /
			// ActiveBucketList react to these; the handler returns nil for
			// other states.
			case "d", "u", "D", "r", "c", "B", "s", keybinding.PresignYank:
				if cmd := m.handleFileOp(keybinding.KeyString(msg.String())); cmd != nil {
					cmds = append(cmds, cmd)
				}
				cmds = append(cmds, m.emitStatusUpdate())
				return m, tea.Batch(cmds...)

			default:
				log.Println("key string:", msg.String())
			}
		}
	}

	// Forward live-preview fetch results to the preview panel. This must
	// happen outside the state switch below so the preview panel receives
	// PreviewContentMsg even when the active list is a different component.
	if _, ok := msg.(preview.PreviewContentMsg); ok {
		newPreviewModel, cmd := m.previewPanel.Update(msg)
		m.previewPanel = newPreviewModel.(preview.Model)
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
			// error banner, no refresh.
		case tmsg.Err != nil:
			cmds = append(cmds, func() tea.Msg {
				return types.ErrMsg{Err: tmsg.Err}
			})
		default:
			// A finished download batch keeps the listing unchanged, so
			// refreshAfterOp skips it; clear the multi-selection here so
			// the marks don't linger after the op.
			if tmsg.Op == transferpanel.OpDownload {
				m.objectlist.ClearSelection()
			}
			// Refresh whichever list the completed op touched.
			if cmd := m.refreshAfterOp(tmsg); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	case historyview.LoadedMsg:
		newHV, cmd := m.historyView.Update(tmsg)
		m.historyView = newHV
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
		if m.modal.IsVisible() || m.help.IsVisible() || m.historyView.IsVisible() {
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
		m.modal.ShowConfirm("Presigned URL", body, nil)
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

	// dispatch message to the active component
	switch m.state {
	case state.ActiveProfileList:
		newProfileListModel, cmd := m.profileList.Update(msg)
		m.profileList = newProfileListModel
		profileItem := m.profileList.GetSelectedProfile()
		if profileItem != nil {
			if cmd := m.previewPanel.SetContent(profileItem); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		cmds = append(cmds, cmd)

	case state.ActiveBucketList:
		newBucketListModel, cmd := m.bucketList.Update(msg)
		m.bucketList = newBucketListModel

		bucketItem := m.bucketList.GetSelectedBucket()
		if bucketItem != nil {
			if cmd := m.previewPanel.SetContent(bucketItem); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		cmds = append(cmds, cmd)

	case state.ActiveObjectList:
		newObjectListModel, cmd := m.objectlist.Update(msg)
		m.objectlist = newObjectListModel

		objectItem := m.objectlist.GetSelectedObject()
		if objectItem != nil {
			if cmd := m.previewPanel.SetContent(objectItem); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
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
	upd := types.StatusUpdateMsg{
		Profile:       m.selectedProfile,
		Bucket:        m.selectedBucket,
		Prefix:        prefix,
		SelectedCount: m.objectlist.SelectedCount(),
	}
	if upd == m.lastStatus {
		return nil
	}
	m.lastStatus = upd
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

// filtering reports whether the active list's filter input is focused (the
// user is typing a filter pattern), in which case global hotkeys must not
// fire.
func (m Model) filtering() bool {
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

// previewCmdForSelection feeds the active list's highlighted item to the
// preview panel and returns the live-fetch cmd (nil when nothing is
// selected or the item is unchanged).
func (m *Model) previewCmdForSelection() tea.Cmd {
	switch m.state {
	case state.ActiveProfileList:
		if it := m.profileList.GetSelectedProfile(); it != nil {
			return m.previewPanel.SetContent(it)
		}
	case state.ActiveBucketList:
		if it := m.bucketList.GetSelectedBucket(); it != nil {
			return m.previewPanel.SetContent(it)
		}
	case state.ActiveObjectList:
		if it := m.objectlist.GetSelectedObject(); it != nil {
			return m.previewPanel.SetContent(it)
		}
	}
	return nil
}

func (m Model) View() string {
	var mainContent string
	switch m.state {
	case state.ActiveProfileList:
		mainContent = lipgloss.JoinHorizontal(
			lipgloss.Top,
			m.profileList.View(),
			m.previewPanel.View(),
		)

	case state.ActiveBucketList:
		mainContent = lipgloss.JoinHorizontal(
			lipgloss.Top,
			m.bucketList.View(),
			m.previewPanel.View(),
		)

	case state.ActiveObjectList:
		mainContent = lipgloss.JoinHorizontal(
			lipgloss.Top,
			m.objectlist.View(),
			m.previewPanel.View(),
		)

	default:
		mainContent = style.ErrorStyle.Render("Unknown component")
	}

	// Stack the main content above the transfer panel and the status bar
	// at the bottom. The transfer panel returns "" when hidden and the
	// status bar always renders one line, so JoinVertical collapses the
	// transfer panel cleanly when there are no transfers.
	bottom := lipgloss.JoinVertical(
		lipgloss.Left,
		m.transferPanel.View(),
		m.statusBar.View(),
	)
	layout := lipgloss.JoinVertical(
		lipgloss.Top,
		mainContent,
		bottom,
	)

	// Modal and help are full-canvas overlays (their View() returns a
	// width×height canvas with the content centered via lipgloss.Place).
	// When either is visible we return its canvas directly so the overlay
	// lands in the centre of the screen rather than being concatenated
	// below the layout (which would double the canvas height and push the
	// overlay off-screen). Help takes precedence over the modal so the
	// user can always summon the cheat sheet, even with a modal open.
	if m.help.IsVisible() {
		return m.help.View()
	}
	if m.historyView.IsVisible() {
		return m.historyView.View()
	}
	if m.modal.IsVisible() {
		return m.modal.View()
	}

	return layout
}
