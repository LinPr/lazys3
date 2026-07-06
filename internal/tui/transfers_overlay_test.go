// Tests for the 't' transfers overlay routing and the y/Y yank/presign key
// split in tui.go's Update.
package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea/v2"

	"github.com/LinPr/lazys3/internal/tui/components/bucketlist"
	"github.com/LinPr/lazys3/internal/tui/components/objectlist"
	"github.com/LinPr/lazys3/internal/tui/components/transferpanel"
	"github.com/LinPr/lazys3/internal/tui/state"
)

// TestTransfersOverlayToggleAndSwallow pins the overlay lifecycle: 't'
// opens it, every unrelated key is swallowed ('q' must not quit, '?' must
// not open help), and 't'/esc close it.
func TestTransfersOverlayToggleAndSwallow(t *testing.T) {
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	m = updateModel(t, m, keyPress('t'))
	if !m.transferView.IsVisible() {
		t.Fatal("'t' did not open the transfers overlay")
	}

	nm, cmd := m.Update(keyPress('q'))
	m = nm.(Model)
	for _, msg := range collectMsgs(cmd) {
		if _, ok := msg.(tea.QuitMsg); ok {
			t.Fatal("'q' quit the program while the transfers overlay was open")
		}
	}
	m = updateModel(t, m, keyPress('?'))
	if m.help.IsVisible() {
		t.Fatal("'?' opened help behind the transfers overlay")
	}
	if !m.transferView.IsVisible() {
		t.Fatal("overlay closed by a swallowed key")
	}

	m = updateModel(t, m, keyPress('t'))
	if m.transferView.IsVisible() {
		t.Fatal("'t' did not close the transfers overlay")
	}
	m = updateModel(t, m, keyPress('t'))
	m = updateModel(t, m, tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if m.transferView.IsVisible() {
		t.Fatal("esc did not close the transfers overlay")
	}
}

// TestTransfersOverlayCancelsHighlighted pins the overlay 'x': it cancels
// the HIGHLIGHTED row (newest first), not the latest running transfer.
func TestTransfersOverlayCancelsHighlighted(t *testing.T) {
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	canceled := map[string]bool{}
	for i := 1; i <= 3; i++ {
		id := fmt.Sprintf("t%d", i)
		m = updateModel(t, m, transferpanel.TransferAddMsg{Transfer: transferpanel.Transfer{
			ID:     id,
			Op:     transferpanel.OpDownload,
			Label:  "s3://b/" + id,
			Status: transferpanel.StatusRunning,
			Cancel: func() { canceled[id] = true },
		}})
	}

	m = updateModel(t, m, keyPress('t'))
	// Rows are newest first: cursor 0 = t3; move to t2.
	m = updateModel(t, m, keyPress('j'))
	m = updateModel(t, m, keyPress('x'))

	if !canceled["t2"] || canceled["t1"] || canceled["t3"] {
		t.Fatalf("canceled = %v, want only t2", canceled)
	}
	if st, _ := m.transferPanel.Status("t2"); st != transferpanel.StatusCanceled {
		t.Fatalf("t2 status = %q, want canceled", st)
	}
	if st, _ := m.transferPanel.Status("t3"); st != transferpanel.StatusRunning {
		t.Fatalf("t3 status = %q, want running (untouched)", st)
	}
	if !m.transferView.IsVisible() {
		t.Fatal("'x' closed the overlay")
	}
}

// TestTransfersOverlayCancelAfterPrune pins the cursor clamp on 'x': when
// pruning shrinks Rows() while the overlay is open, 'x' cancels the row the
// View renders highlighted (the clamped last row) instead of silently
// no-oping on a stale out-of-range cursor.
func TestTransfersOverlayCancelAfterPrune(t *testing.T) {
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	canceled := map[string]bool{}
	addRunning := func(id string) {
		m = updateModel(t, m, transferpanel.TransferAddMsg{Transfer: transferpanel.Transfer{
			ID:     id,
			Op:     transferpanel.OpDownload,
			Label:  "s3://b/" + id,
			Status: transferpanel.StatusRunning,
			Cancel: func() { canceled[id] = true },
		}})
	}
	// 120 running rows: active rows are never pruned, so the history may
	// exceed maxHistory while they are all in flight.
	for i := 1; i <= 120; i++ {
		addRunning(fmt.Sprintf("t%d", i))
	}
	m = updateModel(t, m, keyPress('t'))
	m = updateModel(t, m, keyPress('G')) // cursor -> 119 (oldest row, t1)
	// Finish the 15 oldest; the next add's prune evicts all of them
	// (excess 21 > 15 finished), shrinking Rows() from 121 to 106 while
	// the persisted cursor still sits at 119.
	for i := 1; i <= 15; i++ {
		m = updateModel(t, m, transferpanel.TransferDoneMsg{ID: fmt.Sprintf("t%d", i)})
	}
	addRunning("t121")

	m = updateModel(t, m, keyPress('x'))
	if !canceled["t16"] {
		t.Fatalf("'x' after prune canceled %v, want the visibly-highlighted oldest row t16", canceled)
	}
	if st, _ := m.transferPanel.Status("t16"); st != transferpanel.StatusCanceled {
		t.Fatalf("t16 status = %q, want canceled", st)
	}
}

// TestTransfersOverlayCancelOnEmptyRows pins that 'x' with no transfers is
// a safe no-op (no panic on the empty snapshot).
func TestTransfersOverlayCancelOnEmptyRows(t *testing.T) {
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updateModel(t, m, keyPress('t'))
	m = updateModel(t, m, keyPress('x'))
	if !m.transferView.IsVisible() {
		t.Fatal("'x' on an empty overlay closed it")
	}
}

// clipboardContent walks a cmd tree and returns the payload of the first
// tea.SetClipboard message, if any.
func clipboardContent(cmd tea.Cmd) (string, bool) {
	for _, msg := range collectMsgs(cmd) {
		if strings.Contains(fmt.Sprintf("%T", msg), "setClipboardMsg") {
			return reflect.ValueOf(msg).String(), true
		}
	}
	return "", false
}

// TestYankURIOnObjectList pins the reworked 'y': it copies the highlighted
// object's s3:// URI to the clipboard (directories yield their prefix URI)
// with a status-bar note — presign moved to 'Y'.
func TestYankURIOnObjectList(t *testing.T) {
	m := NewLazyS3Model()
	m.state = state.ActiveObjectList
	m.selectedBucket = "bkt"
	m.objectlist.SetObjects([]objectlist.Object{objectlist.NewFileObject("dir/file.txt")})

	nm, cmd := m.Update(keyPress('y'))
	m = nm.(Model)
	if m.modal.IsVisible() {
		t.Fatal("'y' opened a modal; presign should live on 'Y' now")
	}
	got, ok := clipboardContent(cmd)
	if !ok {
		t.Fatal("'y' emitted no SetClipboard cmd")
	}
	if want := "s3://bkt/dir/file.txt"; got != want {
		t.Fatalf("clipboard = %q, want %q", got, want)
	}
	if !strings.Contains(m.statusBar.Info(), "uri copied: s3://bkt/dir/file.txt") {
		t.Fatalf("status info = %q, want the uri-copied note", m.statusBar.Info())
	}

	// A directory row yields its prefix URI.
	m.objectlist.SetObjects([]objectlist.Object{objectlist.NewDirObject("dir/sub/")})
	_, cmd = m.Update(keyPress('y'))
	if got, _ := clipboardContent(cmd); got != "s3://bkt/dir/sub/" {
		t.Fatalf("directory clipboard = %q, want s3://bkt/dir/sub/", got)
	}
}

// TestPresignMovedToShiftY pins that 'Y' (and its "shift+y" report) opens
// the presign expiry modal that used to live on 'y'.
func TestPresignMovedToShiftY(t *testing.T) {
	m := NewLazyS3Model()
	m.state = state.ActiveObjectList
	m.selectedBucket = "bkt"
	m.objectlist.SetObjects([]objectlist.Object{objectlist.NewFileObject("file.txt")})

	m = updateModel(t, m, tea.KeyPressMsg(tea.Key{Code: 'Y', Text: "Y"}))
	if !m.modal.IsVisible() || !strings.Contains(m.modal.Title(), "Presign URL expiry") {
		t.Fatalf("'Y' did not open the presign modal (title=%q)", m.modal.Title())
	}

	m2 := NewLazyS3Model()
	m2.state = state.ActiveObjectList
	m2.selectedBucket = "bkt"
	m2.objectlist.SetObjects([]objectlist.Object{objectlist.NewFileObject("file.txt")})
	m2 = updateModel(t, m2, tea.KeyPressMsg(tea.Key{Code: 'y', Mod: tea.ModShift}))
	if !m2.modal.IsVisible() || !strings.Contains(m2.modal.Title(), "Presign URL expiry") {
		t.Fatalf("shift+y did not open the presign modal (title=%q)", m2.modal.Title())
	}
}

// TestYankURIOnBucketList pins 'y' in the bucket list: the bucket's s3://
// URI lands on the clipboard.
func TestYankURIOnBucketList(t *testing.T) {
	m := NewLazyS3Model()
	m.state = state.ActiveBucketList
	m.bucketList.SetBuckets([]bucketlist.Bucket{bucketlist.NewBucket("mybucket")})

	_, cmd := m.Update(keyPress('y'))
	got, ok := clipboardContent(cmd)
	if !ok {
		t.Fatal("'y' on the bucket list emitted no SetClipboard cmd")
	}
	if want := "s3://mybucket"; got != want {
		t.Fatalf("clipboard = %q, want %q", got, want)
	}
}

// TestListsReclaimTransferPanelRows pins the layout rework: with the bottom
// panel gone, the main content plus the one-line status bar fill the
// terminal exactly, transfers or not.
func TestListsReclaimTransferPanelRows(t *testing.T) {
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if h := strings.Count(m.View(), "\n") + 1; h != 24 {
		t.Fatalf("view is %d lines, want 24", h)
	}
	// A queued transfer must not re-grow a bottom panel.
	m = updateModel(t, m, transferpanel.TransferAddMsg{Transfer: transferpanel.Transfer{
		ID: "t1", Op: transferpanel.OpDownload, Label: "s3://b/k -> ./k",
		Status: transferpanel.StatusRunning,
	}})
	if h := strings.Count(m.View(), "\n") + 1; h != 24 {
		t.Fatalf("view with a transfer is %d lines, want 24", h)
	}
	if strings.Contains(m.View(), "Transfers\n") {
		t.Fatal("bottom transfer panel rendered in the layout")
	}
}
