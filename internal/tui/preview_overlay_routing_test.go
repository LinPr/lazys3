// Tests for the floating 'p' (content preview) and 'm' (metadata) overlay
// routing in tui.go's Update: what each key opens per state/focus, the
// swallow-everything-while-visible contract, and their place at the tail of
// the overlay precedence chain.
package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LinPr/lazys3/internal/tui/components/bucketlist"
	"github.com/LinPr/lazys3/internal/tui/components/locallist"
	"github.com/LinPr/lazys3/internal/tui/components/objectlist"
	"github.com/LinPr/lazys3/internal/tui/components/preview"
	"github.com/LinPr/lazys3/internal/tui/components/profilelist"
	"github.com/LinPr/lazys3/internal/tui/state"
)

// TestContentPreviewOnDirectoryHints pins that 'p' over a directory row
// never opens the overlay and leaves the status-bar hint instead.
func TestContentPreviewOnDirectoryHints(t *testing.T) {
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.state = state.ActiveObjectList
	m.selectedBucket = "bkt"
	m.objectlist.SetObjects([]objectlist.Object{objectlist.NewDirObject("dir/")})

	m = updateModel(t, m, keyPress('p'))
	if m.contentView.IsVisible() {
		t.Fatal("'p' opened the content overlay for a directory")
	}
	if !strings.Contains(m.statusBar.Info(), "preview works on files") {
		t.Fatalf("status info = %q, want the files-only hint", m.statusBar.Info())
	}
}

// TestContentPreviewOutsideObjectListHints pins 'p' on the profile and
// bucket lists: no overlay, a hint to open a bucket first.
func TestContentPreviewOutsideObjectListHints(t *testing.T) {
	for _, st := range []state.State{state.ActiveProfileList, state.ActiveBucketList} {
		m := NewLazyS3Model()
		m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
		m.state = st
		m = updateModel(t, m, keyPress('p'))
		if m.contentView.IsVisible() {
			t.Fatalf("'p' opened the content overlay in state %v", st)
		}
		if !strings.Contains(m.statusBar.Info(), "open a bucket") {
			t.Fatalf("status info = %q in state %v, want the open-a-bucket hint", m.statusBar.Info(), st)
		}
	}
}

// TestContentPreviewOnRemoteFileOpensAndSwallows pins the overlay
// lifecycle on the object list: 'p' opens it loading, every unrelated key
// is swallowed ('q' must not quit, '?' must not open help), and p/esc
// close it.
func TestContentPreviewOnRemoteFileOpensAndSwallows(t *testing.T) {
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.state = state.ActiveObjectList
	m.selectedBucket = "bkt"
	m.objectlist.SetObjects([]objectlist.Object{objectlist.NewFileObject("file.txt")})

	nm, _ := m.Update(keyPress('p'))
	m = nm.(Model)
	if !m.contentView.IsVisible() {
		t.Fatal("'p' did not open the content overlay for a file")
	}

	nm, cmd := m.Update(keyPress('q'))
	m = nm.(Model)
	for _, msg := range collectMsgs(cmd) {
		if _, ok := msg.(tea.QuitMsg); ok {
			t.Fatal("'q' quit the program while the content overlay was open")
		}
	}
	m = updateModel(t, m, keyPress('?'))
	if m.help.IsVisible() {
		t.Fatal("'?' opened help behind the content overlay")
	}
	// t/v/m are swallowed too: the precedence chain is untouched.
	for _, r := range []rune{'t', 'v', 'm'} {
		m = updateModel(t, m, keyPress(r))
	}
	if m.transferView.IsVisible() || m.versionView.IsVisible() || m.metaView.IsVisible() {
		t.Fatal("a swallowed key opened another overlay behind the content preview")
	}
	if !m.contentView.IsVisible() {
		t.Fatal("overlay closed by a swallowed key")
	}

	m = updateModel(t, m, keyPress('p'))
	if m.contentView.IsVisible() {
		t.Fatal("'p' did not close the content overlay")
	}
	m = updateModel(t, m, keyPress('p'))
	m = updateModel(t, m, tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if m.contentView.IsVisible() {
		t.Fatal("esc did not close the content overlay")
	}
}

// TestContentPreviewLocalFileRendersContent pins the dual-pane local
// source: 'p' with local focus reads the highlighted file and renders its
// text in the floating box over the live layout.
func TestContentPreviewLocalFileRendersContent(t *testing.T) {
	m, dir := dualModel(t, "a.txt")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("LOCAL PREVIEW BODY"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = updateModel(t, m, tabPress()) // focus local

	nm, cmd := m.Update(keyPress('p'))
	m = nm.(Model)
	if !m.contentView.IsVisible() {
		t.Fatal("'p' did not open the content overlay with local focus")
	}
	for _, msg := range collectMsgs(cmd) {
		if _, ok := msg.(preview.ContentMsg); ok {
			m = updateModel(t, m, msg)
		}
	}
	out := ansi.Strip(m.viewContent())
	if !strings.Contains(out, "LOCAL PREVIEW BODY") {
		t.Fatalf("overlay does not render the local file content:\n%s", out)
	}
	// The floating box sits over the live layout: the status bar row is
	// still part of the render.
	if !strings.Contains(out, "a.txt") {
		t.Fatalf("overlay title/path missing:\n%s", out)
	}
}

// TestContentPreviewLocalDirectoryHints pins 'p' over a local directory.
func TestContentPreviewLocalDirectoryHints(t *testing.T) {
	m, dir := dualModel(t, "a.txt")
	if err := os.Mkdir(filepath.Join(dir, "0sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	m = updateModel(t, m, locallist.FetchDirCmd(dir)()) // pick up the new dir
	m = updateModel(t, m, tabPress())                   // focus local; dirs sort first
	m = updateModel(t, m, keyPress('p'))
	if m.contentView.IsVisible() {
		t.Fatal("'p' opened the content overlay for a local directory")
	}
	if !strings.Contains(m.statusBar.Info(), "preview works on files") {
		t.Fatalf("status info = %q, want the files-only hint", m.statusBar.Info())
	}
}

// TestStaleContentMsgDroppedAcrossReopen pins the fetch race guard end to
// end: a sample fetched for a previous 'p' target must never populate the
// overlay after it was closed and reopened on another file.
func TestStaleContentMsgDroppedAcrossReopen(t *testing.T) {
	m, dir := dualModel(t, "a.txt", "b.txt")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("STALE AAA"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("FRESH BBB"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = updateModel(t, m, tabPress()) // focus local, cursor on a.txt

	nm, staleCmd := m.Update(keyPress('p'))
	m = nm.(Model)
	m = updateModel(t, m, keyPress('p')) // close before the fetch lands
	m = updateModel(t, m, keyPress('j')) // cursor -> b.txt
	nm, freshCmd := m.Update(keyPress('p'))
	m = nm.(Model)

	// The stale fetch completes last-ish: feed it first, then the fresh one.
	for _, msg := range collectMsgs(staleCmd) {
		if _, ok := msg.(preview.ContentMsg); ok {
			m = updateModel(t, m, msg)
		}
	}
	out := ansi.Strip(m.viewContent())
	if strings.Contains(out, "STALE AAA") {
		t.Fatalf("stale sample populated the reopened overlay:\n%s", out)
	}
	for _, msg := range collectMsgs(freshCmd) {
		if _, ok := msg.(preview.ContentMsg); ok {
			m = updateModel(t, m, msg)
		}
	}
	if out := ansi.Strip(m.viewContent()); !strings.Contains(out, "FRESH BBB") {
		t.Fatalf("fresh sample not applied:\n%s", out)
	}
}

// TestMetadataOpensInEveryState pins 'm' across all four sources: profile
// list, bucket list, object list (file and prefix) and the dual-pane local
// pane.
func TestMetadataOpensInEveryState(t *testing.T) {
	// Profile list (synchronous rows — no fetch).
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updateModel(t, m, profilelist.ReadAwsConfigResult{Profiles: []profilelist.Profile{
		profilelist.NewProfile("oss", "https://example.com"),
	}})
	m = updateModel(t, m, keyPress('m'))
	if !m.metaView.IsVisible() {
		t.Fatal("'m' did not open the metadata overlay on the profile list")
	}
	out := ansi.Strip(m.viewContent())
	if !strings.Contains(out, "oss") || !strings.Contains(out, "https://example.com") {
		t.Fatalf("profile metadata missing name/endpoint:\n%s", out)
	}
	m = updateModel(t, m, tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if m.metaView.IsVisible() {
		t.Fatal("esc did not close the metadata overlay")
	}

	// Bucket list (async fetch: overlay opens loading).
	m.state = state.ActiveBucketList
	m.bucketList.SetBuckets([]bucketlist.Bucket{bucketlist.NewBucket("mybucket")})
	m = updateModel(t, m, keyPress('m'))
	if !m.metaView.IsVisible() {
		t.Fatal("'m' did not open the metadata overlay on the bucket list")
	}
	m = updateModel(t, m, keyPress('m'))
	if m.metaView.IsVisible() {
		t.Fatal("'m' did not close the metadata overlay")
	}

	// Object list: file and directory rows both open.
	m.state = state.ActiveObjectList
	m.selectedBucket = "bkt"
	m.objectlist.SetObjects([]objectlist.Object{objectlist.NewFileObject("f.txt")})
	m = updateModel(t, m, keyPress('m'))
	if !m.metaView.IsVisible() {
		t.Fatal("'m' did not open the metadata overlay for an object file")
	}
	m = updateModel(t, m, keyPress('m'))
	m.objectlist.SetObjects([]objectlist.Object{objectlist.NewDirObject("dir/")})
	m = updateModel(t, m, keyPress('m'))
	if !m.metaView.IsVisible() {
		t.Fatal("'m' did not open the metadata overlay for a prefix row")
	}
	m = updateModel(t, m, keyPress('m'))

	// Local pane: the fetch cmd is a real lstat — run it and check rows.
	m2, dir := dualModel(t, "a.txt")
	m2 = updateModel(t, m2, tabPress())
	nm, cmd := m2.Update(keyPress('m'))
	m2 = nm.(Model)
	if !m2.metaView.IsVisible() {
		t.Fatal("'m' did not open the metadata overlay with local focus")
	}
	for _, msg := range collectMsgs(cmd) {
		m2 = updateModel(t, m2, msg)
	}
	out = ansi.Strip(m2.viewContent())
	if !strings.Contains(out, filepath.Join(dir, "a.txt")) || !strings.Contains(out, "Permissions:") {
		t.Fatalf("local metadata missing path/permissions:\n%s", out)
	}
}

// TestMetadataOnEmptyListingHints pins 'm' with nothing selected.
func TestMetadataOnEmptyListingHints(t *testing.T) {
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.state = state.ActiveObjectList
	m = updateModel(t, m, keyPress('m'))
	if m.metaView.IsVisible() {
		t.Fatal("'m' opened the metadata overlay with nothing selected")
	}
	if !strings.Contains(m.statusBar.Info(), "nothing selected") {
		t.Fatalf("status info = %q, want the nothing-selected hint", m.statusBar.Info())
	}
}

// TestMetadataOverlaySwallowsGlobalKeys pins the swallow contract for 'm':
// 'q' must not quit, '?' must not open help, 'p' must not open the content
// preview, and m/esc close it.
func TestMetadataOverlaySwallowsGlobalKeys(t *testing.T) {
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updateModel(t, m, profilelist.ReadAwsConfigResult{Profiles: []profilelist.Profile{
		profilelist.NewProfile("p1", ""),
	}})
	m = updateModel(t, m, keyPress('m'))
	if !m.metaView.IsVisible() {
		t.Fatal("'m' did not open the metadata overlay")
	}

	nm, cmd := m.Update(keyPress('q'))
	m = nm.(Model)
	for _, msg := range collectMsgs(cmd) {
		if _, ok := msg.(tea.QuitMsg); ok {
			t.Fatal("'q' quit the program while the metadata overlay was open")
		}
	}
	for _, r := range []rune{'?', 'p', 't', 'v'} {
		m = updateModel(t, m, keyPress(r))
	}
	if m.help.IsVisible() || m.contentView.IsVisible() || m.transferView.IsVisible() || m.versionView.IsVisible() {
		t.Fatal("a swallowed key opened another overlay behind the metadata overlay")
	}
	if !m.metaView.IsVisible() {
		t.Fatal("overlay closed by a swallowed key")
	}
	m = updateModel(t, m, keyPress('m'))
	if m.metaView.IsVisible() {
		t.Fatal("'m' did not close the metadata overlay")
	}
}

// TestFullScreenOverlaysSwallowPAndM pins the precedence chain: while a
// full-screen overlay (help, transfers) is up, p/m are swallowed and never
// open the floating overlays underneath.
func TestFullScreenOverlaysSwallowPAndM(t *testing.T) {
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.state = state.ActiveObjectList
	m.selectedBucket = "bkt"
	m.objectlist.SetObjects([]objectlist.Object{objectlist.NewFileObject("f.txt")})

	m = updateModel(t, m, keyPress('?'))
	m = updateModel(t, m, keyPress('p'))
	m = updateModel(t, m, keyPress('m'))
	if m.contentView.IsVisible() || m.metaView.IsVisible() {
		t.Fatal("p/m opened a floating overlay behind help")
	}
	if !m.help.IsVisible() {
		t.Fatal("help closed by a swallowed key")
	}
	m = updateModel(t, m, tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))

	m = updateModel(t, m, keyPress('t'))
	m = updateModel(t, m, keyPress('p'))
	m = updateModel(t, m, keyPress('m'))
	if m.contentView.IsVisible() || m.metaView.IsVisible() {
		t.Fatal("p/m opened a floating overlay behind the transfers overlay")
	}
	if !m.transferView.IsVisible() {
		t.Fatal("transfers overlay closed by a swallowed key")
	}
}
