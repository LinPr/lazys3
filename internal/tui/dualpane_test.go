package tui

import (
	"context"
	"fmt"
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
// mode already entered (ResetToStartDir's start-dir fetch cmd is dropped)
// and the local pane committed to a temp dir containing files. Entering
// focuses the LOCAL pane (pinned by TestEnterDualPaneFocusesLocalAtStartDir);
// the helper tabs back to the remote pane so the tests keep the remote-
// first tab choreography they were written with.
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
	m = updateModel(t, m, keyPress('l'))
	if !m.dualPane {
		t.Fatal("'l' did not enter dual-pane mode at 100 cols")
	}
	if m.paneFocus != focusLocal {
		t.Fatal("entering dual-pane did not focus the local pane")
	}
	m = updateModel(t, m, tabPress())
	// Commit the temp dir as the local pane's listing (bypassing the
	// dropped start-dir fetch).
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

// TestDualPaneRefusedWhenNarrow pins the 80-col minimum: 'l' at 79 cols
// must not enter dual mode and must leave a status-bar hint.
func TestDualPaneRefusedWhenNarrow(t *testing.T) {
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 79, Height: 30})
	m = updateModel(t, m, keyPress('l'))
	if m.dualPane {
		t.Fatal("dual-pane entered below the 80-col minimum")
	}
	if !strings.Contains(m.statusBar.Info(), "narrow") {
		t.Fatalf("status info = %q, want a too-narrow hint", m.statusBar.Info())
	}
}

// TestDualPaneToggleAndAutoExit pins 'l' enter/exit and the auto-exit on a
// narrow resize while dual mode is active. (Entry focus — local pane — is
// pinned by TestEnterDualPaneFocusesLocalAtStartDir; dualModel tabs back
// to the remote pane.)
func TestDualPaneToggleAndAutoExit(t *testing.T) {
	m, _ := dualModel(t, "a.txt")
	if m.paneFocus != focusRemote {
		t.Fatal("dualModel did not normalize focus to the remote pane")
	}
	if m.localList.Focused() {
		t.Fatal("local pane focused after the helper's tab")
	}

	// 'l' again exits.
	m = updateModel(t, m, keyPress('l'))
	if m.dualPane {
		t.Fatal("'l' did not exit dual-pane mode")
	}
	if !m.objectlist.Focused() {
		t.Fatal("remote lists must return to focused after exit")
	}

	// Re-enter, then shrink below the minimum: auto-exit.
	m = updateModel(t, m, keyPress('l'))
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
	m = updateModel(t, m, keyPress('l')) // exit dual
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
// modal targets the local pane's directory, and a directory-only selection
// is no longer skipped — it confirms as a folder download (recursive sync).
func TestDualCopyRemoteToLocal(t *testing.T) {
	m, dir := dualModel(t, "existing.txt")
	m.state = state.ActiveObjectList
	m.selectedBucket = "bkt"
	m.objectlist.SetObjects([]objectlist.Object{objectlist.NewDirObject("d1/")})
	m = updateModel(t, m, keyPress('c'))
	if !m.modal.IsVisible() {
		t.Fatal("'c' did not open the confirm modal for a directory selection")
	}
	if want := "Download 1 folder(s) to " + dir + "?"; !strings.Contains(m.modal.Body(), want) {
		t.Fatalf("modal body = %q, want %q", m.modal.Body(), want)
	}
	if m.localList.Dir() != dir {
		t.Fatalf("local dir changed to %q", m.localList.Dir())
	}
	m = updateModel(t, m, tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
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

// TestRemoteOnlyKeyHintWhenLocalFocused pins that the remaining remote-only
// keys (v/V) pressed with local focus produce a status-bar hint and never
// leak into the local list.
func TestRemoteOnlyKeyHintWhenLocalFocused(t *testing.T) {
	m, _ := dualModel(t, "a.txt")
	m = updateModel(t, m, tabPress())

	for _, k := range []rune{'v', 'V'} {
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

// TestDualMismatchedComboHints pins the cross-direction hints: 'u' needs
// the local pane (upload source) and 'd' the remote pane (download source);
// pressing them on the wrong focus nudges toward tab instead of opening a
// modal.
func TestDualMismatchedComboHints(t *testing.T) {
	m, _ := dualModel(t, "a.txt")
	m.state = state.ActiveObjectList
	m.selectedBucket = "bkt"
	m.objectlist.SetObjects([]objectlist.Object{objectlist.NewFileObject("f.txt")})

	// Remote focus: 'u' hints instead of opening the upload modal.
	m = updateModel(t, m, keyPress('u'))
	if m.modal.IsVisible() {
		t.Fatal("'u' opened a modal with remote focus in dual mode")
	}
	if info := m.statusBar.Info(); !strings.Contains(info, "press tab") || !strings.Contains(info, "uploads from the local pane") {
		t.Fatalf("'u' status info = %q, want the upload hint", info)
	}

	// Local focus: 'd' hints instead of opening the download modal.
	m = updateModel(t, m, tabPress())
	m = updateModel(t, m, keyPress('d'))
	if m.modal.IsVisible() {
		t.Fatal("'d' opened a modal with local focus in dual mode")
	}
	if info := m.statusBar.Info(); !strings.Contains(info, "press tab") || !strings.Contains(info, "downloads from the remote pane") {
		t.Fatalf("'d' status info = %q, want the download hint", info)
	}
}

// TestDualUploadKeyLocalFocused pins the focus-scoped 'u': with the local
// pane focused it is the same cross-pane upload as 'c' — confirm modal
// against the remote bucket/prefix, one upload row per file, no path typing.
func TestDualUploadKeyLocalFocused(t *testing.T) {
	m, dir := dualModel(t, "a.txt")
	m.state = state.ActiveObjectList
	m.selectedBucket = "bkt"
	m.selectedObject = "pre/"
	m = updateModel(t, m, tabPress())

	m = updateModel(t, m, keyPress('u'))
	if !m.modal.IsVisible() {
		t.Fatal("'u' with local focus did not open the confirm modal")
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
	tr.Cancel()
}

// TestDualDownloadKeyRemoteFocused pins the focus-scoped 'd': with the
// remote pane focused it downloads the selection into the local pane's
// current directory (same as 'c'), skipping the destination-typing modal.
func TestDualDownloadKeyRemoteFocused(t *testing.T) {
	m, dir := dualModel(t, "existing.txt")
	m.state = state.ActiveObjectList
	m.selectedBucket = "bkt"
	m.objectlist.SetObjects([]objectlist.Object{objectlist.NewFileObject("f.txt")})

	m = updateModel(t, m, keyPress('d'))
	if !m.modal.IsVisible() {
		t.Fatal("'d' with remote focus did not open the confirm modal")
	}
	if !strings.Contains(m.modal.Body(), dir) {
		t.Fatalf("modal body = %q, want the local pane dir %q", m.modal.Body(), dir)
	}

	nm, cmd := m.Update(keyPress('y'))
	m = nm.(Model)
	adds := transferAdds(cmd)
	if len(adds) != 1 {
		t.Fatalf("confirm produced %d transfer rows, want 1", len(adds))
	}
	tr := adds[0].Transfer
	if tr.Op != transferpanel.OpDownload {
		t.Fatalf("transfer op = %q, want download", tr.Op)
	}
	wantLabel := "s3://bkt/f.txt -> " + filepath.Join(dir, "f.txt")
	if tr.Label != wantLabel {
		t.Fatalf("transfer label = %q, want %q", tr.Label, wantLabel)
	}
	tr.Cancel()
}

// TestSinglePaneDownloadUploadUnchanged pins that outside dual mode the
// 'd'/'u' keys keep their original path-typing modal flows.
func TestSinglePaneDownloadUploadUnchanged(t *testing.T) {
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.state = state.ActiveObjectList
	m.selectedBucket = "bkt"
	m.objectlist.SetObjects([]objectlist.Object{objectlist.NewFileObject("f.txt")})

	m = updateModel(t, m, keyPress('d'))
	if m.modal.Title() != "Download to" {
		t.Fatalf("single-pane 'd' modal title = %q, want Download to", m.modal.Title())
	}
	m = updateModel(t, m, tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if m.modal.IsVisible() {
		t.Fatal("esc did not close the download modal")
	}

	m = updateModel(t, m, keyPress('u'))
	if m.modal.Title() != "Upload from" {
		t.Fatalf("single-pane 'u' modal title = %q, want Upload from", m.modal.Title())
	}
}

// TestWKeyNoLongerTogglesDualPane pins the rebind: 'w' is unclaimed and
// must neither enter nor exit dual-pane mode.
func TestWKeyNoLongerTogglesDualPane(t *testing.T) {
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updateModel(t, m, keyPress('w'))
	if m.dualPane {
		t.Fatal("'w' still enters dual-pane mode")
	}
	m = updateModel(t, m, keyPress('l'))
	if !m.dualPane {
		t.Fatal("'l' did not enter dual-pane mode")
	}
	m = updateModel(t, m, keyPress('w'))
	if !m.dualPane {
		t.Fatal("'w' still exits dual-pane mode")
	}
}

// TestLocalPaneStartsInProcessCwd pins the local pane's first load: the
// lazys3 process's working directory, captured at NewLazyS3Model.
func TestLocalPaneStartsInProcessCwd(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	nm, cmd := m.Update(keyPress('l'))
	m = nm.(Model)
	var loaded *locallist.LoadedMsg
	for _, msg := range collectMsgs(cmd) {
		if lm, ok := msg.(locallist.LoadedMsg); ok {
			loaded = &lm
		}
	}
	if loaded == nil {
		t.Fatal("'l' did not kick off the local pane's first load")
	}
	m = updateModel(t, m, *loaded)
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := filepath.EvalSymlinks(m.localList.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("local pane first dir = %q, want the process cwd %q", got, want)
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

	m = updateModel(t, m, keyPress('l'))
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

// TestEnterDualPaneClosesPreview pins that 'l' with the single-pane
// preview open closes it (matching exit/switch), so entering dual mode is
// visible rather than rendering an identical half-width layout.
func TestEnterDualPaneClosesPreview(t *testing.T) {
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updateModel(t, m, keyPress('p'))
	if !m.previewPanel.IsVisible() {
		t.Fatal("'p' did not open the preview")
	}
	m = updateModel(t, m, keyPress('l'))
	if !m.dualPane {
		t.Fatal("'l' did not enter dual-pane mode")
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

// findLoaded executes a cmd tree and returns the locallist.LoadedMsg it
// produced, failing the test when there is none.
func findLoaded(t *testing.T, cmd tea.Cmd) locallist.LoadedMsg {
	t.Helper()
	for _, msg := range collectMsgs(cmd) {
		if lm, ok := msg.(locallist.LoadedMsg); ok {
			return lm
		}
	}
	t.Fatal("no locallist.LoadedMsg produced")
	return locallist.LoadedMsg{}
}

// assertSameDir compares two paths after resolving symlinks (t.TempDir may
// sit behind one, e.g. on macOS).
func assertSameDir(t *testing.T, got, want string) {
	t.Helper()
	g, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatal(err)
	}
	w, err := filepath.EvalSymlinks(want)
	if err != nil {
		t.Fatal(err)
	}
	if g != w {
		t.Fatalf("dir = %q, want %q", g, w)
	}
}

// TestEnterDualPaneFocusesLocalAtStartDir pins the 'l' entry behavior:
// the local pane opens FOCUSED and ALWAYS at the start directory —
// navigation within an open session persists, but close→reopen resets the
// directory and clears the selection.
func TestEnterDualPaneFocusesLocalAtStartDir(t *testing.T) {
	start := t.TempDir()
	if err := os.WriteFile(filepath.Join(start, "home.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(start)
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})

	nm, cmd := m.Update(keyPress('l'))
	m = nm.(Model)
	if !m.dualPane || m.paneFocus != focusLocal || !m.localList.Focused() {
		t.Fatal("'l' did not enter dual-pane mode focused on the local pane")
	}
	m = updateModel(t, m, findLoaded(t, cmd))
	assertSameDir(t, m.localList.Dir(), start)

	// Navigate elsewhere and mark an entry: within the open session the
	// directory persists.
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = updateModel(t, m, locallist.FetchDirCmd(other)())
	assertSameDir(t, m.localList.Dir(), other)
	m.localList.ToggleSelected()
	if m.localList.SelectedCount() != 1 {
		t.Fatal("selection setup failed")
	}

	// Close and reopen: focus is local again and the pane reloads the
	// start dir with a clean selection.
	m = updateModel(t, m, keyPress('l'))
	nm, cmd = m.Update(keyPress('l'))
	m = nm.(Model)
	if m.paneFocus != focusLocal || !m.localList.Focused() {
		t.Fatal("re-entering dual-pane did not focus the local pane")
	}
	loaded := findLoaded(t, cmd)
	assertSameDir(t, loaded.Dir, start)
	m = updateModel(t, m, loaded)
	assertSameDir(t, m.localList.Dir(), start)
	if m.localList.SelectedCount() != 0 {
		t.Fatalf("selection = %d after reopen, want 0", m.localList.SelectedCount())
	}
}

// TestDualPaneHeightsMatchOnEntry is the regression for the entry-layout
// mismatch: on a fresh session, 'l' alone (no tab, no extra resize) must
// render both panes at equal heights and complementary widths, even when
// the local listing spans more than one page (bubbles' SetItems used to
// compute the page size against the pagination line's stale height, making
// the local pane one line taller than its box until the next SetSize).
func TestDualPaneHeightsMatchOnEntry(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 40; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%02d.txt", i)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
	m := NewLazyS3Model()
	m = pump(t, m, tea.WindowSizeMsg{Width: 101, Height: 31})
	m = pump(t, m, keyPress('l'))
	if !m.dualPane {
		t.Fatal("'l' did not enter dual-pane mode")
	}
	left, right := m.remotePaneView(), m.localList.View()
	if lh, rh := lipgloss.Height(left), lipgloss.Height(right); lh != rh {
		t.Fatalf("pane heights differ right after entry: remote %d, local %d", lh, rh)
	}
	if lw, rw := lipgloss.Width(left), lipgloss.Width(right); lw+rw != 101 {
		t.Fatalf("pane widths %d+%d do not sum to the terminal width 101", lw, rw)
	}

	// Exiting restores full-width single-pane sizing just as immediately.
	m = pump(t, m, keyPress('l'))
	if m.dualPane {
		t.Fatal("'l' did not exit dual-pane mode")
	}
	if w := lipgloss.Width(m.remotePaneView()); w != 101 {
		t.Fatalf("single-pane width = %d after exit, want 101", w)
	}
}

// TestStatusUpdatePaneAndTransferCounts pins the status bar plumbing: the
// pane indicator follows the dual-pane focus (empty single-pane), and the
// transfer tallies refresh on TransferAddMsg/TransferDoneMsg without any
// key press, so the bar's summary never goes stale.
func TestStatusUpdatePaneAndTransferCounts(t *testing.T) {
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})

	m = updateModel(t, m, keyPress('l'))
	if m.lastStatus.Pane != "local" {
		t.Fatalf("pane = %q after entering dual mode, want local", m.lastStatus.Pane)
	}
	m = updateModel(t, m, tabPress())
	if m.lastStatus.Pane != "remote" {
		t.Fatalf("pane = %q after tab, want remote", m.lastStatus.Pane)
	}
	m = updateModel(t, m, keyPress('l'))
	if m.lastStatus.Pane != "" {
		t.Fatalf("pane = %q after exit, want empty", m.lastStatus.Pane)
	}

	m = updateModel(t, m, transferpanel.TransferAddMsg{Transfer: transferpanel.Transfer{
		ID: "c1", Op: transferpanel.OpDownload, Status: transferpanel.StatusRunning,
	}})
	if m.lastStatus.TransfersRunning != 1 {
		t.Fatalf("running = %d after add, want 1", m.lastStatus.TransfersRunning)
	}
	m = updateModel(t, m, transferpanel.TransferDoneMsg{ID: "c1", Op: transferpanel.OpDownload})
	if m.lastStatus.TransfersRunning != 0 || m.lastStatus.TransfersDone != 1 {
		t.Fatalf("running/done = %d/%d after done, want 0/1",
			m.lastStatus.TransfersRunning, m.lastStatus.TransfersDone)
	}
	m = updateModel(t, m, transferpanel.TransferAddMsg{Transfer: transferpanel.Transfer{
		ID: "c2", Op: transferpanel.OpUpload, Status: transferpanel.StatusRunning,
	}})
	m = updateModel(t, m, transferpanel.TransferDoneMsg{ID: "c2", Op: transferpanel.OpUpload, Err: context.Canceled})
	if m.lastStatus.TransfersFailed != 1 {
		t.Fatalf("failed = %d after cancel, want 1 (canceled counts as failed)", m.lastStatus.TransfersFailed)
	}
}

// TestNarrowResizeAutoExitRefreshesStatusBar is the regression for the
// stale pane chip: a resize below minDualPaneWidth auto-exits dual mode,
// and the status bar must drop the pane indicator in the same pass — with
// no key press or transfer event needed — while the "dual-pane closed"
// info note stays readable (a resize is not navigation).
func TestNarrowResizeAutoExitRefreshesStatusBar(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	m := NewLazyS3Model()
	m = pump(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = pump(t, m, keyPress('l'))
	if !m.dualPane {
		t.Fatal("'l' did not enter dual-pane mode")
	}
	if got := m.statusBar.Pane(); got != "local" {
		t.Fatalf("pane chip = %q in dual mode, want local", got)
	}

	m = pump(t, m, tea.WindowSizeMsg{Width: 70, Height: 30})
	if m.dualPane {
		t.Fatal("narrow resize did not auto-exit dual mode")
	}
	if got := m.statusBar.Pane(); got != "" {
		t.Fatalf("pane chip = %q after the narrow auto-exit, want empty", got)
	}
	if info := m.statusBar.Info(); !strings.Contains(info, "dual-pane closed") {
		t.Fatalf("info = %q after the narrow auto-exit, want the dual-pane closed note", info)
	}
}

// TestInfoNoteSurvivesTransferCompletion pins the ClearInfo plumbing end
// to end: a note set while a transfer runs must still be readable after
// the transfer turns terminal in the background (the tally-only status
// update carries ClearInfo=false), while a navigation-ish change (tab
// pane switch) still dismisses it.
func TestInfoNoteSurvivesTransferCompletion(t *testing.T) {
	m, _ := dualModel(t, "a.txt")
	m = pump(t, m, transferpanel.TransferAddMsg{Transfer: transferpanel.Transfer{
		ID: "c1", Op: transferpanel.OpDownload, Status: transferpanel.StatusRunning,
	}})
	m.statusBar.SetInfo("path copied: /tmp/x")
	m = pump(t, m, transferpanel.TransferDoneMsg{ID: "c1", Op: transferpanel.OpDownload})
	if got := m.statusBar.Info(); got != "path copied: /tmp/x" {
		t.Fatalf("info = %q after a background transfer completed, want the note preserved", got)
	}

	m = pump(t, m, tabPress())
	if got := m.statusBar.Info(); got != "" {
		t.Fatalf("info = %q after a pane switch, want cleared", got)
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
