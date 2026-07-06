package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea/v2"

	"github.com/LinPr/lazys3/internal/tui/components/objectlist"
	"github.com/LinPr/lazys3/internal/tui/state"
	"github.com/LinPr/lazys3/internal/tui/types"
)

// collectMsgs executes a cmd tree (flattening tea.Batch) and returns every
// produced message. Nil cmds and nil messages are skipped.
func collectMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, collectMsgs(c)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

func keyPress(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: r, Text: string(r)})
}

// TestVersionsKeyOnDirectorySurfacesStatusbarError pins that 'v' over a
// directory row never opens the overlay (a prefix has no version history)
// and surfaces a status-bar error instead of panicking.
func TestVersionsKeyOnDirectorySurfacesStatusbarError(t *testing.T) {
	m := NewLazyS3Model()
	m.state = state.ActiveObjectList
	m.selectedBucket = "bkt"
	m.objectlist.SetObjects([]objectlist.Object{objectlist.NewDirObject("dir/")})

	nm, cmd := m.Update(keyPress('v'))
	m = nm.(Model)
	if m.versionView.IsVisible() {
		t.Fatal("versions overlay opened for a directory selection")
	}

	var errMsg *types.ErrMsg
	for _, msg := range collectMsgs(cmd) {
		if em, ok := msg.(types.ErrMsg); ok {
			errMsg = &em
			break
		}
	}
	if errMsg == nil {
		t.Fatal("no types.ErrMsg produced for 'v' on a directory")
	}
	if !strings.Contains(errMsg.Err.Error(), "versions") {
		t.Fatalf("error = %q, want a versions hint", errMsg.Err)
	}

	// Feed the error back like the program loop would and check it lands
	// on the status bar.
	m = updateModel(t, m, *errMsg)
	if !strings.Contains(m.statusBar.LastError(), "versions") {
		t.Fatalf("status bar error = %q, want the versions error", m.statusBar.LastError())
	}
}

// TestVersionsKeyOnNilSelectionSurfacesStatusbarError covers the empty
// listing: no highlighted row at all.
func TestVersionsKeyOnNilSelectionSurfacesStatusbarError(t *testing.T) {
	m := NewLazyS3Model()
	m.state = state.ActiveObjectList

	nm, cmd := m.Update(keyPress('v'))
	m = nm.(Model)
	if m.versionView.IsVisible() {
		t.Fatal("versions overlay opened with nothing selected")
	}
	found := false
	for _, msg := range collectMsgs(cmd) {
		if _, ok := msg.(types.ErrMsg); ok {
			found = true
		}
	}
	if !found {
		t.Fatal("no types.ErrMsg produced for 'v' with nothing selected")
	}
}

// TestVersionOverlaySwallowsGlobalKeys pins that a visible overlay swallows
// global hotkeys ('q' must not quit, '?' must not open help).
func TestVersionOverlaySwallowsGlobalKeys(t *testing.T) {
	m := NewLazyS3Model()
	m.versionView.Show(objectlist.Option{}, "bkt", "file.txt")

	nm, cmd := m.Update(keyPress('q'))
	m = nm.(Model)
	for _, msg := range collectMsgs(cmd) {
		if _, ok := msg.(tea.QuitMsg); ok {
			t.Fatal("'q' quit the program while the versions overlay was open")
		}
	}
	nm, _ = m.Update(keyPress('?'))
	m = nm.(Model)
	if m.help.IsVisible() {
		t.Fatal("'?' opened help behind the versions overlay")
	}
	if !m.versionView.IsVisible() {
		t.Fatal("overlay closed by a swallowed key")
	}

	// esc closes it.
	nm, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	m = nm.(Model)
	if m.versionView.IsVisible() {
		t.Fatal("esc did not close the versions overlay")
	}
}
