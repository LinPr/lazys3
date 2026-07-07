package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/LinPr/lazys3/internal/tui/components/locallist"
	"github.com/LinPr/lazys3/internal/tui/components/transferpanel"
	"github.com/LinPr/lazys3/internal/tui/types"
)

// drainAll executes a cmd tree in order — tea.Batch by type, tea.Sequence
// via reflection (its sequenceMsg is an unexported []tea.Cmd) — and
// returns every produced message. Unlike transferAdds this RUNS the ops,
// which is safe here because local ops touch only the test's temp dirs.
func drainAll(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if msg == nil {
		return nil
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, drainAll(c)...)
		}
		return out
	}
	rv := reflect.ValueOf(msg)
	if rv.Kind() == reflect.Slice && rv.Type().Elem() == reflect.TypeOf(tea.Cmd(nil)) {
		var out []tea.Msg
		for i := 0; i < rv.Len(); i++ {
			if c, ok := rv.Index(i).Interface().(tea.Cmd); ok {
				out = append(out, drainAll(c)...)
			}
		}
		return out
	}
	return []tea.Msg{msg}
}

func enterPress() tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
}

// typeInModal feeds each rune of s into the model (the open modal's
// textinput consumes them).
func typeInModal(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m = updateModel(t, m, keyPress(r))
	}
	return m
}

// TestLocalDeleteFileFlow pins the local 'D' flow end to end: confirm
// modal (permanent, no trash), a delete transfer row labelled with the
// item count, the file actually removed, and the pane refreshed with the
// selection cleared once the done message lands.
func TestLocalDeleteFileFlow(t *testing.T) {
	m, dir := dualModel(t, "a.txt", "b.txt")
	m = updateModel(t, m, tabPress())

	m = updateModel(t, m, keyPress('D'))
	if !m.modal.IsVisible() {
		t.Fatal("'D' with local focus did not open the confirm modal")
	}
	if !strings.Contains(m.modal.Body(), "permanent, no trash") {
		t.Fatalf("modal body = %q, want the permanent-delete warning", m.modal.Body())
	}
	if !strings.Contains(m.modal.Body(), "1 item(s)") {
		t.Fatalf("modal body = %q, want the highlighted-entry count", m.modal.Body())
	}

	nm, cmd := m.Update(keyPress('y'))
	m = nm.(Model)
	var add *transferpanel.TransferAddMsg
	var done *transferpanel.TransferDoneMsg
	for _, msg := range drainAll(cmd) {
		switch mm := msg.(type) {
		case transferpanel.TransferAddMsg:
			add = &mm
		case transferpanel.TransferDoneMsg:
			done = &mm
		}
	}
	if add == nil || done == nil {
		t.Fatal("confirm did not produce a transfer row + done message")
	}
	if add.Transfer.Op != transferpanel.OpDelete || add.Transfer.Label != "local: 1 item(s)" {
		t.Fatalf("row op/label = %q/%q, want delete/local: 1 item(s)", add.Transfer.Op, add.Transfer.Label)
	}
	if done.Err != nil || !done.Local {
		t.Fatalf("done = {err: %v, local: %v}, want {nil, true}", done.Err, done.Local)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); !os.IsNotExist(err) {
		t.Fatal("a.txt was not deleted")
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); err != nil {
		t.Fatalf("b.txt must survive: %v", err)
	}

	// The done message refreshes the pane and clears the selection.
	m = pump(t, m, *done)
	if names := localVisibleNames(m); strings.Join(names, ",") != "b.txt" {
		t.Fatalf("listing after delete = %v, want [b.txt]", names)
	}
	if m.localList.SelectedCount() != 0 {
		t.Fatal("selection not cleared after the local delete")
	}
}

// TestLocalDeleteDirRecursiveModal pins the directory case: the confirm
// body explicitly announces the recursive delete, and confirming removes
// the whole tree.
func TestLocalDeleteDirRecursiveModal(t *testing.T) {
	m, dir := dualModel(t, "keep.txt")
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "inner.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Reload so the pane sees sub/ (dirs sort first: the cursor lands on it).
	m = updateModel(t, m, locallist.FetchDirCmd(dir)())
	m = updateModel(t, m, tabPress())
	if e := m.localList.GetSelectedEntry(); e == nil || e.Name() != "sub/" {
		t.Fatalf("cursor = %v, want sub/", e)
	}

	m = updateModel(t, m, keyPress('D'))
	if !m.modal.IsVisible() {
		t.Fatal("'D' on a directory did not open the confirm modal")
	}
	if !strings.Contains(m.modal.Body(), "recursively delete directory sub") {
		t.Fatalf("modal body = %q, want the explicit recursive warning", m.modal.Body())
	}

	nm, cmd := m.Update(keyPress('y'))
	m = nm.(Model)
	var done *transferpanel.TransferDoneMsg
	for _, msg := range drainAll(cmd) {
		if mm, ok := msg.(transferpanel.TransferDoneMsg); ok {
			done = &mm
		}
	}
	if done == nil || done.Err != nil {
		t.Fatalf("done = %v, want a successful done message", done)
	}
	if _, err := os.Stat(sub); !os.IsNotExist(err) {
		t.Fatal("sub/ was not recursively deleted")
	}
	if _, err := os.Stat(filepath.Join(dir, "keep.txt")); err != nil {
		t.Fatalf("keep.txt must survive: %v", err)
	}
}

// TestLocalRenameFlow pins the local 'r' flow: input modal prefilled with
// the current name, os.Rename within the same dir, refresh with the cursor
// following the renamed entry.
func TestLocalRenameFlow(t *testing.T) {
	m, dir := dualModel(t, "a.txt", "b.txt")
	m = updateModel(t, m, tabPress())

	m = updateModel(t, m, keyPress('r'))
	if !m.modal.IsVisible() || !strings.Contains(m.modal.Title(), "Rename a.txt") {
		t.Fatalf("'r' modal title = %q, want Rename a.txt …", m.modal.Title())
	}
	m = typeInModal(t, m, "z.txt")
	nm, cmd := m.Update(enterPress())
	m = nm.(Model)
	var doneMsg tea.Msg
	for _, msg := range drainAll(cmd) {
		if _, ok := msg.(localFSDoneMsg); ok {
			doneMsg = msg
		}
	}
	if doneMsg == nil {
		t.Fatal("rename confirm produced no localFSDoneMsg")
	}
	if err := doneMsg.(localFSDoneMsg).err; err != nil {
		t.Fatalf("rename failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "z.txt")); err != nil {
		t.Fatalf("z.txt missing after rename: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); !os.IsNotExist(err) {
		t.Fatal("a.txt still present after rename")
	}

	m = pump(t, m, doneMsg)
	if e := m.localList.GetSelectedEntry(); e == nil || e.Name() != "z.txt" {
		t.Fatalf("cursor after rename = %v, want z.txt", e)
	}
}

// TestLocalRenameRejectsSeparatorsAndMultiSelect pins the guards: a new
// name with a path separator errors without touching the file, and a
// multi-selection never opens the modal.
func TestLocalRenameRejectsSeparatorsAndMultiSelect(t *testing.T) {
	m, dir := dualModel(t, "a.txt", "b.txt")
	m = updateModel(t, m, tabPress())

	m = updateModel(t, m, keyPress('r'))
	m = typeInModal(t, m, "x/y")
	nm, cmd := m.Update(enterPress())
	m = nm.(Model)
	foundErr := false
	for _, msg := range drainAll(cmd) {
		if em, ok := msg.(types.ErrMsg); ok && strings.Contains(em.Err.Error(), "separator") {
			foundErr = true
		}
	}
	if !foundErr {
		t.Fatal("separator in the new name did not surface an error")
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatalf("a.txt must be untouched: %v", err)
	}

	// Multi-selection: status-bar error, no modal. Space no longer
	// advances the cursor, so move down explicitly between toggles.
	space := tea.KeyPressMsg(tea.Key{Code: tea.KeySpace, Text: " "})
	down := tea.KeyPressMsg(tea.Key{Code: tea.KeyDown})
	m = updateModel(t, m, space)
	m = updateModel(t, m, down)
	m = updateModel(t, m, space)
	if m.localList.SelectedCount() != 2 {
		t.Fatalf("selection = %d, want 2", m.localList.SelectedCount())
	}
	nm, cmd = m.Update(keyPress('r'))
	m = nm.(Model)
	if m.modal.IsVisible() {
		t.Fatal("'r' opened a modal for a multi-selection")
	}
	foundErr = false
	for _, msg := range drainAll(cmd) {
		if em, ok := msg.(types.ErrMsg); ok && strings.Contains(em.Err.Error(), "single entry") {
			foundErr = true
		}
	}
	if !foundErr {
		t.Fatal("multi-selection 'r' did not surface the single-entry error")
	}
}

// TestLocalMkdirFlow pins the local 'B' flow: input modal, os.MkdirAll
// under the pane's current dir, refresh with the cursor on the new dir.
// Empty / separators-only names are rejected.
func TestLocalMkdirFlow(t *testing.T) {
	m, dir := dualModel(t, "a.txt")
	m = updateModel(t, m, tabPress())

	m = updateModel(t, m, keyPress('B'))
	if !m.modal.IsVisible() || !strings.Contains(m.modal.Title(), "New directory") {
		t.Fatalf("'B' modal title = %q, want New directory …", m.modal.Title())
	}
	m = typeInModal(t, m, "subdir")
	nm, cmd := m.Update(enterPress())
	m = nm.(Model)
	var doneMsg tea.Msg
	for _, msg := range drainAll(cmd) {
		if _, ok := msg.(localFSDoneMsg); ok {
			doneMsg = msg
		}
	}
	if doneMsg == nil || doneMsg.(localFSDoneMsg).err != nil {
		t.Fatalf("mkdir done = %v, want a successful localFSDoneMsg", doneMsg)
	}
	if fi, err := os.Stat(filepath.Join(dir, "subdir")); err != nil || !fi.IsDir() {
		t.Fatalf("subdir not created: %v", err)
	}
	m = pump(t, m, doneMsg)
	if e := m.localList.GetSelectedEntry(); e == nil || e.Name() != "subdir/" {
		t.Fatalf("cursor after mkdir = %v, want subdir/", e)
	}

	// Separators-only name: error, nothing created.
	m = updateModel(t, m, keyPress('B'))
	m = typeInModal(t, m, "/")
	nm, cmd = m.Update(enterPress())
	m = nm.(Model)
	foundErr := false
	for _, msg := range drainAll(cmd) {
		if em, ok := msg.(types.ErrMsg); ok && strings.Contains(em.Err.Error(), "empty directory name") {
			foundErr = true
		}
	}
	if !foundErr {
		t.Fatal("separators-only mkdir did not surface an error")
	}
}

// TestLocalRenameRefusesOverwrite pins the clobber guard: renaming onto
// an existing name fails with an error instead of silently replacing the
// target (rename(2) would), and both files survive untouched.
func TestLocalRenameRefusesOverwrite(t *testing.T) {
	m, dir := dualModel(t, "a.txt", "b.txt")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = updateModel(t, m, tabPress())

	m = updateModel(t, m, keyPress('r'))
	m = typeInModal(t, m, "b.txt")
	nm, cmd := m.Update(enterPress())
	m = nm.(Model)
	var done *localFSDoneMsg
	for _, msg := range drainAll(cmd) {
		if mm, ok := msg.(localFSDoneMsg); ok {
			done = &mm
		}
	}
	if done == nil || done.err == nil || !strings.Contains(done.err.Error(), "already exists") {
		t.Fatalf("done = %+v, want an already-exists error", done)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatalf("a.txt must be untouched: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "b.txt")); err != nil || string(got) != "keep" {
		t.Fatalf("b.txt content = %q, %v; want the original content preserved", got, err)
	}
}

// TestLocalMkdirRejectsEscape pins the ".." guard: a name whose cleaned
// form leaves the pane's directory errors and creates nothing anywhere.
func TestLocalMkdirRejectsEscape(t *testing.T) {
	m, dir := dualModel(t, "a.txt")
	m = updateModel(t, m, tabPress())

	m = updateModel(t, m, keyPress('B'))
	m = typeInModal(t, m, "x/../../escaped")
	nm, cmd := m.Update(enterPress())
	m = nm.(Model)
	foundErr := false
	for _, msg := range drainAll(cmd) {
		if em, ok := msg.(types.ErrMsg); ok && strings.Contains(em.Err.Error(), "escapes") {
			foundErr = true
		}
	}
	if !foundErr {
		t.Fatal("escaping mkdir name did not surface an error")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escaped")); !os.IsNotExist(err) {
		t.Fatal("directory was created outside the pane's dir")
	}
	if _, err := os.Stat(filepath.Join(dir, "x")); !os.IsNotExist(err) {
		t.Fatal("partial path was created inside the pane's dir")
	}
}

// TestLocalDeleteSymlinkConfirmText pins the symlink phrasing: a
// symlink-to-dir is announced as a link removal (the target survives),
// never as a recursive directory delete.
func TestLocalDeleteSymlinkConfirmText(t *testing.T) {
	m, dir := dualModel(t)
	target := filepath.Join(dir, "real")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "inner.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	m = updateModel(t, m, locallist.FetchDirCmd(dir)())
	m = updateModel(t, m, tabPress())
	if e := m.localList.GetSelectedEntry(); e == nil || e.Name() != "link/" {
		t.Fatalf("cursor = %v, want link/", e)
	}

	m = updateModel(t, m, keyPress('D'))
	if !strings.Contains(m.modal.Body(), "remove symlink link (target untouched)") {
		t.Fatalf("modal body = %q, want the symlink phrasing", m.modal.Body())
	}
	if strings.Contains(m.modal.Body(), "recursively") {
		t.Fatalf("modal body = %q, must not claim a recursive delete", m.modal.Body())
	}

	nm, cmd := m.Update(keyPress('y'))
	m = nm.(Model)
	drainAll(cmd)
	if _, err := os.Lstat(filepath.Join(dir, "link")); !os.IsNotExist(err) {
		t.Fatal("link was not removed")
	}
	if _, err := os.Stat(filepath.Join(target, "inner.txt")); err != nil {
		t.Fatalf("target tree must survive: %v", err)
	}
}

// TestLocalDeletePartialFailureCount pins the batch-error aggregation:
// when several entries fail, the done error carries the failure count
// instead of naming only the first path.
func TestLocalDeletePartialFailureCount(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; permission bits are not enforced")
	}
	m, dir := dualModel(t, "f1.txt", "f2.txt")
	m = updateModel(t, m, tabPress())
	// Space no longer advances the cursor; move down between toggles.
	space := tea.KeyPressMsg(tea.Key{Code: tea.KeySpace, Text: " "})
	m = updateModel(t, m, space)
	m = updateModel(t, m, tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	m = updateModel(t, m, space)
	if m.localList.SelectedCount() != 2 {
		t.Fatalf("selection = %d, want 2", m.localList.SelectedCount())
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	m = updateModel(t, m, keyPress('D'))
	nm, cmd := m.Update(keyPress('y'))
	m = nm.(Model)
	var done *transferpanel.TransferDoneMsg
	for _, msg := range drainAll(cmd) {
		if mm, ok := msg.(transferpanel.TransferDoneMsg); ok {
			done = &mm
		}
	}
	if done == nil || done.Err == nil || !strings.Contains(done.Err.Error(), "2 of 2 items failed") {
		t.Fatalf("done = %v, want the 2-of-2 failure count in the error", done)
	}
}

// TestLocalYankPath pins the local 'y': the highlighted entry's absolute
// path lands on the clipboard via tea.SetClipboard (OSC52) with a
// status-bar note.
func TestLocalYankPath(t *testing.T) {
	m, dir := dualModel(t, "a.txt")
	m = updateModel(t, m, tabPress())

	nm, cmd := m.Update(keyPress('y'))
	m = nm.(Model)
	want := filepath.Join(dir, "a.txt")
	found := false
	for _, msg := range collectMsgs(cmd) {
		if !strings.Contains(fmt.Sprintf("%T", msg), "setClipboardMsg") {
			continue
		}
		found = true
		if got := reflect.ValueOf(msg).String(); got != want {
			t.Fatalf("clipboard content = %q, want %q", got, want)
		}
	}
	if !found {
		t.Fatal("'y' with local focus emitted no SetClipboard cmd")
	}
	if !strings.Contains(m.statusBar.Info(), "path copied") {
		t.Fatalf("status info = %q, want the path-copied note", m.statusBar.Info())
	}
}
