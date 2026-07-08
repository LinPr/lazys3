package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/LinPr/lazys3/internal/tui/components/bucketlist"
	"github.com/LinPr/lazys3/internal/tui/components/objectlist"
	"github.com/LinPr/lazys3/internal/tui/components/versionview"
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

// TestVersionsKeyOnObjectFileOpensOverlay pins one half of the merged 'v'
// key: on a highlighted object FILE it opens the versions overlay.
func TestVersionsKeyOnObjectFileOpensOverlay(t *testing.T) {
	m := NewLazyS3Model()
	m.state = state.ActiveObjectList
	m.selectedBucket = "bkt"
	m.objectlist.SetObjects([]objectlist.Object{objectlist.NewFileObject("f.txt")})

	m = updateModel(t, m, keyPress('v'))
	if !m.versionView.IsVisible() {
		t.Fatal("'v' on an object file did not open the versions overlay")
	}
	if m.modal.IsVisible() {
		t.Fatal("'v' on an object file opened a modal")
	}
}

// TestVersionsKeyInBucketListStartsVersioningFlow pins the other half: in
// the bucket list 'v' kicks off the bucket-versioning toggle flow (the old
// 'V' binding) — an async status probe whose BucketStatusMsg opens the
// confirm modal.
func TestVersionsKeyInBucketListStartsVersioningFlow(t *testing.T) {
	m := NewLazyS3Model()
	m.state = state.ActiveBucketList
	m.bucketList.SetBuckets([]bucketlist.Bucket{bucketlist.NewBucket("bkt")})

	// The dispatch seam: 'v' must reach the versioning status probe (a
	// non-nil async cmd; not executed here — it would hit the network).
	if cmd := m.handleVersionsKey(); cmd == nil {
		t.Fatal("'v' in the bucket list did not start the versioning flow")
	}
	// Never the object-versions overlay.
	m = updateModel(t, m, keyPress('v'))
	if m.versionView.IsVisible() {
		t.Fatal("'v' in the bucket list opened the object-versions overlay")
	}

	// The probe result opens the confirm modal.
	m = updateModel(t, m, versionview.BucketStatusMsg{Bucket: "bkt", Status: ""})
	if !m.modal.IsVisible() {
		t.Fatal("BucketStatusMsg did not open the versioning confirm modal")
	}
	if m.modal.Title() != "Bucket versioning" {
		t.Fatalf("modal title = %q, want Bucket versioning", m.modal.Title())
	}
}

// TestShiftVIsUnbound pins that shift+v does nothing since the v/V merge:
// no overlay, no modal, in either remote list.
func TestShiftVIsUnbound(t *testing.T) {
	shiftV := tea.KeyPressMsg(tea.Key{Code: 'v', Text: "V", Mod: tea.ModShift})
	for _, press := range []tea.KeyPressMsg{keyPress('V'), shiftV} {
		m := NewLazyS3Model()
		m.state = state.ActiveBucketList
		m.bucketList.SetBuckets([]bucketlist.Bucket{bucketlist.NewBucket("bkt")})
		m = updateModel(t, m, press)
		if m.modal.IsVisible() || m.versionView.IsVisible() {
			t.Fatalf("%q opened a modal/overlay in the bucket list", press.String())
		}

		m.state = state.ActiveObjectList
		m.selectedBucket = "bkt"
		m.objectlist.SetObjects([]objectlist.Object{objectlist.NewFileObject("f.txt")})
		m = updateModel(t, m, press)
		if m.modal.IsVisible() || m.versionView.IsVisible() {
			t.Fatalf("%q opened a modal/overlay in the object list", press.String())
		}
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
