package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"

	"github.com/LinPr/lazys3/internal/tui/components/locallist"
	"github.com/LinPr/lazys3/internal/tui/components/objectlist"
	"github.com/LinPr/lazys3/internal/tui/components/transferpanel"
	"github.com/LinPr/lazys3/internal/tui/state"
	"github.com/LinPr/lazys3/internal/tui/types"
)

// dualModel returns a model sized wide enough for dual-pane mode with the
// mode already entered (EnsureLoaded's home fetch cmd is dropped) and the
// local pane committed to a temp dir containing files.
func dualModel(t *testing.T, files ...string) (Model, string) {
	t.Helper()
	dir := t.TempDir()
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updateModel(t, m, keyPress('w'))
	if !m.dualPane {
		t.Fatal("'w' did not enter dual-pane mode at 100 cols")
	}
	// Commit the temp dir as the local pane's listing (bypassing the
	// EnsureLoaded home fetch).
	m = updateModel(t, m, locallist.FetchDirCmd(dir)())
	if m.localList.Dir() != dir {
		t.Fatalf("local dir = %q, want %q", m.localList.Dir(), dir)
	}
	return m, dir
}

func tabPress() tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyTab})
}

// transferAdds walks a cmd tree and returns every TransferAddMsg without
// running the network ops: tea.Sequence produces an unexported
// sequenceMsg ([]tea.Cmd) whose first element is always the row-add (the
// op is sequenced after it), so only that first element is executed.
func transferAdds(cmd tea.Cmd) []transferpanel.TransferAddMsg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if msg == nil {
		return nil
	}
	switch mm := msg.(type) {
	case tea.BatchMsg:
		var out []transferpanel.TransferAddMsg
		for _, c := range mm {
			out = append(out, transferAdds(c)...)
		}
		return out
	case transferpanel.TransferAddMsg:
		return []transferpanel.TransferAddMsg{mm}
	}
	rv := reflect.ValueOf(msg)
	if rv.Kind() == reflect.Slice && rv.Len() > 0 {
		if c, ok := rv.Index(0).Interface().(tea.Cmd); ok {
			return transferAdds(c)
		}
	}
	return nil
}

// TestDualPaneRefusedWhenNarrow pins the 80-col minimum: 'w' at 79 cols
// must not enter dual mode and must leave a status-bar hint.
func TestDualPaneRefusedWhenNarrow(t *testing.T) {
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 79, Height: 30})
	m = updateModel(t, m, keyPress('w'))
	if m.dualPane {
		t.Fatal("dual-pane entered below the 80-col minimum")
	}
	if !strings.Contains(m.statusBar.Info(), "narrow") {
		t.Fatalf("status info = %q, want a too-narrow hint", m.statusBar.Info())
	}
}

// TestDualPaneToggleAndAutoExit pins 'w' enter/exit and the auto-exit on a
// narrow resize while dual mode is active.
func TestDualPaneToggleAndAutoExit(t *testing.T) {
	m, _ := dualModel(t, "a.txt")
	if m.paneFocus != focusRemote {
		t.Fatal("dual-pane did not start on the remote pane")
	}
	if m.localList.Focused() {
		t.Fatal("local pane focused on entry")
	}

	// 'w' again exits.
	m = updateModel(t, m, keyPress('w'))
	if m.dualPane {
		t.Fatal("'w' did not exit dual-pane mode")
	}
	if !m.objectlist.Focused() {
		t.Fatal("remote lists must return to focused after exit")
	}

	// Re-enter, then shrink below the minimum: auto-exit.
	m = updateModel(t, m, keyPress('w'))
	if !m.dualPane {
		t.Fatal("re-entering dual-pane failed")
	}
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 60, Height: 30})
	if m.dualPane {
		t.Fatal("narrow resize did not auto-exit dual-pane mode")
	}
	if !strings.Contains(m.statusBar.Info(), "narrow") {
		t.Fatalf("status info = %q, want the auto-exit hint", m.statusBar.Info())
	}
}

// TestTabSwitchesFocus pins that tab moves focus (border ownership) in
// dual mode, closes an open preview, and is a swallowed no-op single-pane.
func TestTabSwitchesFocus(t *testing.T) {
	m, _ := dualModel(t, "a.txt")

	// Preview open on the remote focus...
	m = updateModel(t, m, keyPress('p'))
	if !m.previewPanel.IsVisible() {
		t.Fatal("'p' did not open the preview in dual mode")
	}

	// ...tab closes it and hands focus to the local pane.
	m = updateModel(t, m, tabPress())
	if m.paneFocus != focusLocal {
		t.Fatal("tab did not focus the local pane")
	}
	if m.previewPanel.IsVisible() {
		t.Fatal("tab did not close the preview before switching")
	}
	if !m.localList.Focused() || m.objectlist.Focused() {
		t.Fatal("border focus flags do not match the local focus")
	}

	m = updateModel(t, m, tabPress())
	if m.paneFocus != focusRemote {
		t.Fatal("second tab did not focus the remote pane")
	}
	if m.localList.Focused() || !m.objectlist.Focused() {
		t.Fatal("border focus flags do not match the remote focus")
	}

	// Single-pane: tab is a handled no-op (no crash, no mode change).
	m = updateModel(t, m, keyPress('w')) // exit dual
	m = updateModel(t, m, tabPress())
	if m.dualPane {
		t.Fatal("tab must not enter dual-pane mode")
	}
}

// TestKeysRouteToFocusedPane pins that 'j' moves the focused pane's cursor
// and leaves the other pane's cursor alone.
func TestKeysRouteToFocusedPane(t *testing.T) {
	m, _ := dualModel(t, "a.txt", "b.txt", "c.txt")
	m.state = state.ActiveObjectList
	m.selectedBucket = "bkt"
	m.objectlist.SetObjects([]objectlist.Object{
		objectlist.NewDirObject("d1/"),
		objectlist.NewDirObject("d2/"),
	})

	// Remote focus: 'j' moves the remote cursor, not the local one.
	m = updateModel(t, m, keyPress('j'))
	if got := m.objectlist.GetSelectedObject(); got == nil || got.Name() != "d2/" {
		t.Fatalf("remote cursor = %v, want d2/", got)
	}
	if got := m.localList.GetSelectedEntry(); got == nil || got.Name() != "a.txt" {
		t.Fatalf("local cursor = %v, want a.txt (unmoved)", got)
	}

	// Local focus: 'j' moves the local cursor, not the remote one.
	m = updateModel(t, m, tabPress())
	m = updateModel(t, m, keyPress('j'))
	if got := m.localList.GetSelectedEntry(); got == nil || got.Name() != "b.txt" {
		t.Fatalf("local cursor = %v, want b.txt", got)
	}
	if got := m.objectlist.GetSelectedObject(); got == nil || got.Name() != "d2/" {
		t.Fatalf("remote cursor = %v, want d2/ (unmoved)", got)
	}
}

// TestDualCopyLocalToRemote pins the local→remote 'c' flow: a confirm
// modal opens against the remote bucket/prefix, and confirming it emits
// one upload transfer row per selected file.
func TestDualCopyLocalToRemote(t *testing.T) {
	m, dir := dualModel(t, "a.txt")
	m.state = state.ActiveObjectList
	m.selectedBucket = "bkt"
	m.selectedObject = "pre/"
	m = updateModel(t, m, tabPress())

	m = updateModel(t, m, keyPress('c'))
	if !m.modal.IsVisible() {
		t.Fatal("'c' with local focus did not open the confirm modal")
	}
	if !strings.Contains(m.modal.Body(), "s3://bkt/pre/") {
		t.Fatalf("modal body = %q, want the remote destination", m.modal.Body())
	}

	nm, cmd := m.Update(keyPress('y'))
	m = nm.(Model)
	adds := transferAdds(cmd)
	if len(adds) != 1 {
		t.Fatalf("confirm produced %d transfer rows, want 1", len(adds))
	}
	tr := adds[0].Transfer
	if tr.Op != transferpanel.OpUpload {
		t.Fatalf("transfer op = %q, want upload", tr.Op)
	}
	wantLabel := filepath.Join(dir, "a.txt") + " -> s3://bkt/pre/a.txt"
	if tr.Label != wantLabel {
		t.Fatalf("transfer label = %q, want %q", tr.Label, wantLabel)
	}
	if tr.Cancel == nil {
		t.Fatal("transfer row carries no cancellable context")
	}
	tr.Cancel()
}

// TestDualCopyLocalToRemoteNeedsBucket pins the guard: without an open
// bucket the copy surfaces a status-bar hint instead of a modal.
func TestDualCopyLocalToRemoteNeedsBucket(t *testing.T) {
	m, _ := dualModel(t, "a.txt")
	m = updateModel(t, m, tabPress())
	m = updateModel(t, m, keyPress('c'))
	if m.modal.IsVisible() {
		t.Fatal("'c' opened a modal without an open bucket")
	}
	if !strings.Contains(m.statusBar.Info(), "open a bucket") {
		t.Fatalf("status info = %q, want the open-a-bucket hint", m.statusBar.Info())
	}
}

// TestDualCopyRemoteToLocal pins the remote→local 'c' flow: the confirm
// modal targets the local pane's directory and confirming emits download
// rows.
func TestDualCopyRemoteToLocal(t *testing.T) {
	m, dir := dualModel(t, "existing.txt")
	m.state = state.ActiveObjectList
	m.selectedBucket = "bkt"
	// Directory-only selection errors out (v1 skips directories).
	m.objectlist.SetObjects([]objectlist.Object{objectlist.NewDirObject("d1/")})
	nm, cmd := m.Update(keyPress('c'))
	m = nm.(Model)
	if m.modal.IsVisible() {
		t.Fatal("'c' opened a modal for a directory-only selection")
	}
	foundErr := false
	for _, msg := range collectMsgs(cmd) {
		if em, ok := msg.(types.ErrMsg); ok && strings.Contains(em.Err.Error(), "directories are skipped") {
			foundErr = true
		}
	}
	if !foundErr {
		t.Fatal("directory-only 'c' did not surface the skip error")
	}
	if m.localList.Dir() != dir {
		t.Fatalf("local dir changed to %q", m.localList.Dir())
	}
}

// TestDualSyncPrefill pins the 's' prefill: focused pane is the source,
// the other pane the destination. The prefills land as modal placeholders
// (enter on an empty input submits them), so the chained-modal flow is
// walked with plain enters and the final sync row's label is checked.
func TestDualSyncPrefill(t *testing.T) {
	enter := tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	m, dir := dualModel(t, "a.txt")
	m.state = state.ActiveObjectList
	m.selectedBucket = "bkt"
	m.selectedObject = "pre/"

	// Remote focus: src = s3 URI, dst = local dir.
	m = updateModel(t, m, keyPress('s'))
	if !m.modal.IsVisible() || !strings.Contains(m.modal.Title(), "Sync source") {
		t.Fatalf("'s' did not open the sync source modal (title %q)", m.modal.Title())
	}
	nm, cmd := m.Update(enter) // submit the src prefill
	m = nm.(Model)
	var dstMsg *types.ShowInputModalMsg
	for _, msg := range collectMsgs(cmd) {
		if sm, ok := msg.(types.ShowInputModalMsg); ok {
			dstMsg = &sm
		}
	}
	if dstMsg == nil {
		t.Fatal("submitting the source did not chain into the destination modal")
	}
	if dstMsg.Placeholder != dir {
		t.Fatalf("dst prefill = %q, want the local dir %q", dstMsg.Placeholder, dir)
	}
	m = updateModel(t, m, *dstMsg)
	nm, cmd = m.Update(enter) // submit the dst prefill
	m = nm.(Model)
	var flagsMsg *types.ShowInputModalMsg
	for _, msg := range collectMsgs(cmd) {
		if sm, ok := msg.(types.ShowInputModalMsg); ok {
			flagsMsg = &sm
		}
	}
	if flagsMsg == nil {
		t.Fatal("submitting the destination did not chain into the flags modal")
	}
	m = updateModel(t, m, *flagsMsg)
	nm, cmd = m.Update(enter) // submit empty flags -> start sync
	m = nm.(Model)
	adds := transferAdds(cmd)
	if len(adds) != 1 {
		t.Fatalf("sync confirm produced %d rows, want 1", len(adds))
	}
	tr := adds[0].Transfer
	if tr.Op != transferpanel.OpSync {
		t.Fatalf("transfer op = %q, want sync", tr.Op)
	}
	if want := "sync s3://bkt/pre/ -> " + dir; tr.Label != want {
		t.Fatalf("sync label = %q, want %q", tr.Label, want)
	}
	tr.Cancel()

	// Local focus: the destination prefill is the remote location.
	m = updateModel(t, m, tabPress())
	m = updateModel(t, m, keyPress('s'))
	nm, cmd = m.Update(enter)
	m = nm.(Model)
	dstMsg = nil
	for _, msg := range collectMsgs(cmd) {
		if sm, ok := msg.(types.ShowInputModalMsg); ok {
			dstMsg = &sm
		}
	}
	if dstMsg == nil {
		t.Fatal("local-focus 's': no destination modal chained")
	}
	if dstMsg.Placeholder != "s3://bkt/pre/" {
		t.Fatalf("dst prefill = %q, want s3://bkt/pre/", dstMsg.Placeholder)
	}
	if m.modal.IsVisible() {
		t.Fatal("stale modal left visible")
	}
}

// TestLocalFilterSwallowsGlobalHotkeys pins the pane-aware filtering
// guard: while typing a filter in the local pane, 'd'/'q' are filter
// input, not hotkeys.
func TestLocalFilterSwallowsGlobalHotkeys(t *testing.T) {
	m, _ := dualModel(t, "a.txt", "b.txt")
	m = updateModel(t, m, tabPress())

	m = updateModel(t, m, keyPress('/'))
	if !m.localList.Filtering() {
		t.Fatal("'/' did not start the local filter")
	}
	nm, cmd := m.Update(keyPress('q'))
	m = nm.(Model)
	for _, msg := range collectMsgs(cmd) {
		if _, ok := msg.(tea.QuitMsg); ok {
			t.Fatal("'q' quit while typing a local filter")
		}
	}
	m = updateModel(t, m, keyPress('d'))
	if m.modal.IsVisible() {
		t.Fatal("'d' opened a modal while typing a local filter")
	}
	if !m.localList.Filtering() {
		t.Fatal("filter input lost focus to a global hotkey")
	}
}

// TestRemoteOnlyKeyHintWhenLocalFocused pins that remote-only file-op keys
// pressed with local focus produce a status-bar hint and never leak into
// the local list (where 'D' would page).
func TestRemoteOnlyKeyHintWhenLocalFocused(t *testing.T) {
	m, _ := dualModel(t, "a.txt")
	m = updateModel(t, m, tabPress())

	for _, k := range []rune{'D', 'd', 'u', 'r', 'B', 'y', 'v', 'V'} {
		m.statusBar.SetInfo("")
		m = updateModel(t, m, keyPress(k))
		if m.modal.IsVisible() {
			t.Fatalf("%q opened a modal with local focus", k)
		}
		if !strings.Contains(m.statusBar.Info(), "tab") {
			t.Fatalf("%q: status info = %q, want the remote-pane hint", k, m.statusBar.Info())
		}
	}
}

// TestDualPaneViewRendersBothPanes pins the dual View composition: two
// side-by-side panes filling the full width, and the local pane gone
// again after exiting the mode.
func TestDualPaneViewRendersBothPanes(t *testing.T) {
	m, _ := dualModel(t, "a.txt")
	out := m.View()
	if w := lipgloss.Width(out); w != 100 {
		t.Fatalf("dual view width = %d, want 100", w)
	}
	// Left pane: the remote profile list; right pane: the local list
	// (its title carries the sort status).
	if !strings.Contains(out, "AWS Profiles") {
		t.Fatal("dual view missing the remote pane")
	}
	if !strings.Contains(out, "a.txt") {
		t.Fatal("dual view missing the local pane")
	}

	m = updateModel(t, m, keyPress('w'))
	out = m.View()
	if strings.Contains(out, "a.txt") {
		t.Fatal("single-pane view still renders the local pane")
	}
	if w := lipgloss.Width(out); w != 100 {
		t.Fatalf("single view width = %d, want 100", w)
	}
}

// TestLocalPreviewSurvivesNonKeyMsgs pins that with the local pane focused
// and the preview open, non-key messages (transfer ticks, status updates)
// never re-feed the remote pane's highlighted item to the preview — which
// would flip the panel's content and kick off a spurious remote fetch.
func TestLocalPreviewSurvivesNonKeyMsgs(t *testing.T) {
	m, _ := dualModel(t, "a.txt")
	m.state = state.ActiveObjectList
	m.selectedBucket = "bkt"
	m.objectlist.SetObjects([]objectlist.Object{objectlist.NewDirObject("d1/")})

	m = updateModel(t, m, tabPress())
	m = updateModel(t, m, keyPress('p'))
	if !m.previewPanel.IsVisible() {
		t.Fatal("'p' did not open the preview with local focus")
	}
	// Local entry previews carry a Mode: line; object previews a Key: line.
	if out := m.previewPanel.View(); !strings.Contains(out, "Mode:") {
		t.Fatalf("preview does not show the local entry:\n%s", out)
	}
	for _, msg := range []tea.Msg{transferpanel.TickMsg{}, types.StatusUpdateMsg{Profile: "x"}} {
		m = updateModel(t, m, msg)
		if out := m.previewPanel.View(); strings.Contains(out, "Key:") || !strings.Contains(out, "Mode:") {
			t.Fatalf("%T flipped the preview to the remote item:\n%s", msg, out)
		}
	}
}

// TestLocalLoadRefreshesPreview pins that committing a local directory
// load retargets the open preview at the new listing's highlighted entry
// without another key press.
func TestLocalLoadRefreshesPreview(t *testing.T) {
	m, _ := dualModel(t, "a.txt")
	m = updateModel(t, m, tabPress())
	m = updateModel(t, m, keyPress('p'))
	if out := m.previewPanel.View(); !strings.Contains(out, "Size:") {
		t.Fatalf("preview does not show the local file entry:\n%s", out)
	}

	dirB := t.TempDir()
	if err := os.Mkdir(filepath.Join(dirB, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	m = updateModel(t, m, locallist.FetchDirCmd(dirB)())
	if m.localList.Dir() != dirB {
		t.Fatalf("local dir = %q, want %q", m.localList.Dir(), dirB)
	}
	if out := m.previewPanel.View(); !strings.Contains(out, "directory") {
		t.Fatalf("preview did not follow the local load:\n%s", out)
	}
}

// TestEnterDualPaneClosesPreview pins that 'w' with the single-pane
// preview open closes it (matching exit/switch), so entering dual mode is
// visible rather than rendering an identical half-width layout.
func TestEnterDualPaneClosesPreview(t *testing.T) {
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updateModel(t, m, keyPress('p'))
	if !m.previewPanel.IsVisible() {
		t.Fatal("'p' did not open the preview")
	}
	m = updateModel(t, m, keyPress('w'))
	if !m.dualPane {
		t.Fatal("'w' did not enter dual-pane mode")
	}
	if m.previewPanel.IsVisible() {
		t.Fatal("entering dual-pane left the preview open")
	}
}

// TestStatusBarCountsFocusedPane pins that the status bar's selection
// count follows the focused pane in dual mode.
func TestStatusBarCountsFocusedPane(t *testing.T) {
	m, _ := dualModel(t, "a.txt", "b.txt")
	m = updateModel(t, m, tabPress())
	m = updateModel(t, m, tea.KeyPressMsg(tea.Key{Code: tea.KeySpace, Text: " "}))
	if m.lastStatus.SelectedCount != 1 {
		t.Fatalf("status count = %d after local space, want 1", m.lastStatus.SelectedCount)
	}
	m = updateModel(t, m, tabPress())
	if m.lastStatus.SelectedCount != 0 {
		t.Fatalf("status count = %d with remote focus, want 0", m.lastStatus.SelectedCount)
	}
}

// TestLocalSelectionSpaceAndInvert pins space/a routing to the local pane.
func TestLocalSelectionSpaceAndInvert(t *testing.T) {
	m, _ := dualModel(t, "a.txt", "b.txt")
	m = updateModel(t, m, tabPress())

	m = updateModel(t, m, tea.KeyPressMsg(tea.Key{Code: tea.KeySpace, Text: " "}))
	if m.localList.SelectedCount() != 1 {
		t.Fatalf("local selection = %d after space, want 1", m.localList.SelectedCount())
	}
	if m.objectlist.SelectedCount() != 0 {
		t.Fatal("space with local focus marked a remote object")
	}
	m = updateModel(t, m, keyPress('a'))
	if m.localList.SelectedCount() != 1 {
		t.Fatalf("local selection = %d after invert, want 1", m.localList.SelectedCount())
	}
	paths := m.localList.SelectedPaths()
	if len(paths) != 1 || filepath.Base(paths[0]) != "b.txt" {
		t.Fatalf("selected paths = %v, want [.../b.txt]", paths)
	}
}
