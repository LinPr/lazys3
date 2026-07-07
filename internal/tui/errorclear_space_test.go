package tui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/LinPr/lazys3/internal/tui/components/objectlist"
	"github.com/LinPr/lazys3/internal/tui/components/transferpanel"
	"github.com/LinPr/lazys3/internal/tui/state"
	"github.com/LinPr/lazys3/internal/tui/types"
)

// spacePress is the space key exactly as bubbletea v2 delivers it.
func spacePress() tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeySpace, Text: " "})
}

// objectListModel builds a model sitting on the object list with a small
// listing, sized so the list renders.
func objectListModel(t *testing.T) Model {
	t.Helper()
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.state = state.ActiveObjectList
	m.objectlist.SetObjects([]objectlist.Object{
		objectlist.NewFileObject("aaa.txt"),
		objectlist.NewFileObject("bbb.txt"),
	})
	return m
}

// TestErrorClearsOnKeyNotOnAsyncMsgs pins the error lifecycle: an ErrMsg
// persists across async (non-key) messages and is dismissed by the very
// next key press — and a NEW failure with the same text still shows again.
func TestErrorClearsOnKeyNotOnAsyncMsgs(t *testing.T) {
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})

	m = updateModel(t, m, types.ErrMsg{Err: errors.New("boom")})
	if m.statusBar.LastError() != "boom" {
		t.Fatalf("error = %q after ErrMsg, want boom", m.statusBar.LastError())
	}

	// Async messages (transfer ticks, fetch results) keep it visible.
	m = updateModel(t, m, transferpanel.TickMsg{})
	if m.statusBar.LastError() != "boom" {
		t.Fatal("a non-key message cleared the error")
	}

	// Any key press dismisses it.
	m = updateModel(t, m, keyPress('j'))
	if m.statusBar.LastError() != "" {
		t.Fatalf("error = %q after a key press, want cleared", m.statusBar.LastError())
	}

	// The same error text re-raised by a NEW failure shows again.
	m = updateModel(t, m, types.ErrMsg{Err: errors.New("boom")})
	if m.statusBar.LastError() != "boom" {
		t.Fatal("a re-raised identical error was suppressed")
	}
}

// TestErrorClearsOnKeySwallowedByOverlayOrModal pins the placement of the
// clearing (top of the KeyMsg handling): an error that lands WHILE the
// help overlay or a modal is open still shows, and the next key clears it
// even though the overlay/modal swallows that key.
func TestErrorClearsOnKeySwallowedByOverlayOrModal(t *testing.T) {
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})

	m = updateModel(t, m, keyPress('?'))
	if !m.help.IsVisible() {
		t.Fatal("'?' did not open the help overlay")
	}
	m = updateModel(t, m, types.ErrMsg{Err: errors.New("late failure")})
	if m.statusBar.LastError() != "late failure" {
		t.Fatal("an error arriving behind the help overlay was dropped")
	}
	m = updateModel(t, m, keyPress('j')) // swallowed by the help scroll
	if !m.help.IsVisible() {
		t.Fatal("'j' closed the help overlay")
	}
	if m.statusBar.LastError() != "" {
		t.Fatalf("error = %q after a key swallowed by the overlay, want cleared", m.statusBar.LastError())
	}

	// Same behind a modal.
	m2 := NewLazyS3Model()
	m2 = updateModel(t, m2, tea.WindowSizeMsg{Width: 100, Height: 30})
	m2.modal.ShowInfo("Note", "body")
	m2 = updateModel(t, m2, types.ErrMsg{Err: errors.New("modal-time failure")})
	if m2.statusBar.LastError() != "modal-time failure" {
		t.Fatal("an error arriving behind a modal was dropped")
	}
	m2 = updateModel(t, m2, keyPress('j')) // swallowed by the modal
	if m2.statusBar.LastError() != "" {
		t.Fatalf("error = %q after a key swallowed by the modal, want cleared", m2.statusBar.LastError())
	}
}

// TestSpaceTogglesWithoutMovingCursorObjectList pins the reworked space:
// selecting and deselecting never moves the object-list cursor.
func TestSpaceTogglesWithoutMovingCursorObjectList(t *testing.T) {
	m := objectListModel(t)
	if o := m.objectlist.GetSelectedObject(); o == nil || o.Name() != "aaa.txt" {
		t.Fatalf("cursor = %v before space, want aaa.txt", o)
	}

	m = updateModel(t, m, spacePress())
	if n := m.objectlist.SelectedCount(); n != 1 {
		t.Fatalf("selection = %d after space, want 1", n)
	}
	if o := m.objectlist.GetSelectedObject(); o == nil || o.Name() != "aaa.txt" {
		t.Fatalf("cursor moved to %v after select-toggle, want aaa.txt", o)
	}

	// Deselecting keeps it put too.
	m = updateModel(t, m, spacePress())
	if n := m.objectlist.SelectedCount(); n != 0 {
		t.Fatalf("selection = %d after second space, want 0", n)
	}
	if o := m.objectlist.GetSelectedObject(); o == nil || o.Name() != "aaa.txt" {
		t.Fatalf("cursor moved to %v after deselect-toggle, want aaa.txt", o)
	}
}

// TestSpaceTogglesWithoutMovingCursorLocalList mirrors the pin for the
// dual-pane local pane.
func TestSpaceTogglesWithoutMovingCursorLocalList(t *testing.T) {
	m, _ := dualModel(t, "a.txt", "b.txt")
	m = updateModel(t, m, tabPress())
	if !m.localFocused() {
		t.Fatal("tab did not focus the local pane")
	}
	if e := m.localList.GetSelectedEntry(); e == nil || e.Name() != "a.txt" {
		t.Fatalf("cursor = %v before space, want a.txt", e)
	}

	m = updateModel(t, m, spacePress())
	if n := m.localList.SelectedCount(); n != 1 {
		t.Fatalf("selection = %d after space, want 1", n)
	}
	if e := m.localList.GetSelectedEntry(); e == nil || e.Name() != "a.txt" {
		t.Fatalf("cursor moved to %v after select-toggle, want a.txt", e)
	}

	m = updateModel(t, m, spacePress())
	if n := m.localList.SelectedCount(); n != 0 {
		t.Fatalf("selection = %d after second space, want 0", n)
	}
	if e := m.localList.GetSelectedEntry(); e == nil || e.Name() != "a.txt" {
		t.Fatalf("cursor moved to %v after deselect-toggle, want a.txt", e)
	}
}

// TestSortKeyNoteReachesStatusBar pins the o/O plumbing end to end: the
// component emits types.InfoMsg and tui.go routes it onto the status bar,
// for both the remote object list and the focused local pane.
func TestSortKeyNoteReachesStatusBar(t *testing.T) {
	m := objectListModel(t)
	m = pump(t, m, keyPress('o'))
	if got := m.statusBar.Info(); got != "sort: size ↑" {
		t.Fatalf("info = %q after 'o' on the object list, want 'sort: size ↑'", got)
	}
	m = pump(t, m, keyPress('O'))
	if got := m.statusBar.Info(); got != "sort: size ↓" {
		t.Fatalf("info = %q after 'O', want 'sort: size ↓'", got)
	}

	lm, _ := dualModel(t, "a.txt")
	lm = updateModel(t, lm, tabPress())
	lm = pump(t, lm, keyPress('o'))
	if got := lm.statusBar.Info(); got != "sort: size ↑" {
		t.Fatalf("info = %q after 'o' on the local pane, want 'sort: size ↑'", got)
	}
}

// TestTickPassEmitsNoStatusUpdate pins the no-infinite-loop property with
// the transfer tallies gone from StatusUpdateMsg: a 200ms tick moving
// byte counters changes nothing the dedup compares, so no StatusUpdateMsg
// is re-emitted (the bar reads live stats on the render path instead).
func TestTickPassEmitsNoStatusUpdate(t *testing.T) {
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})

	p := transferpanel.NewProgress()
	p.Report(10, 100)
	m = updateModel(t, m, transferpanel.TransferAddMsg{Transfer: transferpanel.Transfer{
		ID: "t1", Op: transferpanel.OpUpload, Status: transferpanel.StatusRunning, Progress: p,
	}})
	// Settle the bar (the add pass may legitimately publish once for the
	// selection-independent fields).
	if cmd := m.emitStatusUpdate(); cmd != nil {
		m = updateModel(t, m, cmd())
	}

	p.Report(50, 100)
	m = updateModel(t, m, transferpanel.TickMsg{})
	if cmd := m.emitStatusUpdate(); cmd != nil {
		t.Fatal("a tick with moving byte counters re-emitted StatusUpdateMsg (dedupe defeated)")
	}
}
