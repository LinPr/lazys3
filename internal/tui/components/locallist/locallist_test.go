package locallist

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/LinPr/lazys3/internal/tui/types"
	"github.com/charmbracelet/x/ansi"
)

// press feeds a single key press into the model and drains any resulting
// filter-refresh commands so VisibleItems reflects the new state.
func press(m Model, k tea.Key) Model {
	newModel, cmd := m.Update(tea.KeyPressMsg(k))
	return drain(newModel, cmd)
}

func typeString(m Model, s string) Model {
	for _, r := range s {
		m = press(m, tea.Key{Code: r, Text: string(r)})
	}
	return m
}

// drain executes cmds, feeding list.FilterMatchesMsg back into the model
// (the async part of bubbles' filtering). Other messages are dropped.
func drain(m Model, cmd tea.Cmd) Model {
	if cmd == nil {
		return m
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			m = drain(m, c)
		}
	case list.FilterMatchesMsg:
		newModel, cmd := m.Update(msg)
		m = drain(newModel, cmd)
	}
	return m
}

func visibleNames(m Model) []string {
	var names []string
	for _, it := range m.list.VisibleItems() {
		names = append(names, it.(Entry).Name())
	}
	return names
}

func mustEqual(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("order mismatch:\n got:  %v\n want: %v", got, want)
	}
}

// writeFile creates a file with the given size and mtime.
func writeFile(t *testing.T, path string, size int, mtime time.Time) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

// sampleDir builds a fixture directory mirroring objectlist's sample set.
func sampleDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t0 := time.Date(2024, 3, 1, 10, 30, 0, 0, time.UTC)
	writeFile(t, filepath.Join(dir, "zeta.txt"), 10, t0.Add(3*time.Hour))
	writeFile(t, filepath.Join(dir, "Alpha.txt"), 300, t0.Add(1*time.Hour))
	writeFile(t, filepath.Join(dir, "mid.txt"), 50, t0.Add(2*time.Hour))
	for _, d := range []string{"logs", "data"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// load fetches dir synchronously and feeds the result into the model.
func load(t *testing.T, m Model, dir string) Model {
	t.Helper()
	msg := FetchDirCmd(dir)()
	newModel, _ := m.Update(msg)
	return newModel
}

func TestLoadDirsFirstNameSort(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 20)
	dir := sampleDir(t)
	m = load(t, m, dir)
	if m.Dir() != dir {
		t.Fatalf("Dir() = %q, want %q", m.Dir(), dir)
	}
	mustEqual(t, visibleNames(m),
		[]string{"data/", "logs/", "Alpha.txt", "mid.txt", "zeta.txt"})
}

func TestCycleSortFieldAndDirection(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 20)
	m = load(t, m, sampleDir(t))

	// o -> size ascending, dirs still first.
	m = press(m, tea.Key{Code: 'o', Text: "o"})
	mustEqual(t, visibleNames(m),
		[]string{"data/", "logs/", "zeta.txt", "mid.txt", "Alpha.txt"})
	if !strings.Contains(m.list.Title, "size ↑") {
		t.Errorf("title should show 'size ↑', got %q", m.list.Title)
	}

	// O -> size descending, dirs still on top.
	m = press(m, tea.Key{Code: 'O', Text: "O"})
	mustEqual(t, visibleNames(m),
		[]string{"logs/", "data/", "Alpha.txt", "mid.txt", "zeta.txt"})
	if !strings.Contains(m.list.Title, "size ↓") {
		t.Errorf("title should show 'size ↓', got %q", m.list.Title)
	}

	// o -> time descending (direction persists across field change).
	m = press(m, tea.Key{Code: 'o', Text: "o"})
	mustEqual(t, visibleNames(m),
		[]string{"logs/", "data/", "zeta.txt", "mid.txt", "Alpha.txt"})
}

func TestFilterNarrowsAndExposesState(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 20)
	m = load(t, m, sampleDir(t))

	m = press(m, tea.Key{Code: '/', Text: "/"})
	if !m.Filtering() {
		t.Fatal("expected Filtering() true after '/'")
	}
	m = typeString(m, "ALPHA")
	mustEqual(t, visibleNames(m), []string{"Alpha.txt"})

	// enter confirms the filter and returns focus to the list.
	m = press(m, tea.Key{Code: tea.KeyEnter})
	if m.Filtering() {
		t.Error("expected Filtering() false after enter")
	}
	mustEqual(t, visibleNames(m), []string{"Alpha.txt"})

	// esc clears the applied filter.
	m = press(m, tea.Key{Code: tea.KeyEscape})
	if len(visibleNames(m)) != 5 {
		t.Errorf("expected all 5 items after clearing filter, got %v", visibleNames(m))
	}
}

func TestSelectedPathsDisplayOrderAndClear(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 20)
	dir := sampleDir(t)
	m = load(t, m, dir)

	// Select zeta.txt (index 4) then data/ (index 0).
	m.list.Select(4)
	m.ToggleSelected()
	m.list.Select(0)
	m.ToggleSelected()

	mustEqual(t, m.SelectedPaths(), []string{
		filepath.Join(dir, "data"),
		filepath.Join(dir, "zeta.txt"),
	})
	if m.SelectedCount() != 2 {
		t.Errorf("expected SelectedCount 2, got %d", m.SelectedCount())
	}
	if got := m.SelectedEntries(); len(got) != 2 || !got[0].IsDir() || got[1].Name() != "zeta.txt" {
		t.Errorf("unexpected SelectedEntries: %+v", got)
	}
	if !strings.Contains(m.list.Title, "2 selected") {
		t.Errorf("title should show selection count, got %q", m.list.Title)
	}

	// Selection survives re-sorting and follows display order.
	m = press(m, tea.Key{Code: 'o', Text: "o"}) // size asc
	mustEqual(t, m.SelectedPaths(), []string{
		filepath.Join(dir, "data"),
		filepath.Join(dir, "zeta.txt"),
	})

	// A reload clears the selection.
	m = load(t, m, dir)
	if m.SelectedCount() != 0 {
		t.Errorf("expected selection cleared on reload, got %d", m.SelectedCount())
	}
	if m.SelectedPaths() != nil {
		t.Errorf("expected nil SelectedPaths, got %v", m.SelectedPaths())
	}
}

func TestInvertSelection(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 20)
	m = load(t, m, sampleDir(t))

	m.list.Select(2) // Alpha.txt
	m.ToggleSelected()
	m.InvertSelection()
	if m.SelectedCount() != 4 {
		t.Errorf("expected 4 selected after invert, got %d", m.SelectedCount())
	}
	for _, e := range m.SelectedEntries() {
		if e.Name() == "Alpha.txt" {
			t.Error("Alpha.txt should be deselected by invert")
		}
	}
}

func TestEnterUpNavigationAndCursorRestore(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 20)
	dir := sampleDir(t)
	child := filepath.Join(dir, "logs")
	writeFile(t, filepath.Join(child, "app.log"), 5, time.Now())
	m = load(t, m, dir)

	// Enter is a no-op on files.
	m.list.Select(2) // Alpha.txt
	if cmd := m.Enter(); cmd != nil {
		t.Error("Enter on a file should return nil")
	}

	// Enter the logs/ dir (index 1).
	m.list.Select(1)
	cmd := m.Enter()
	if cmd == nil {
		t.Fatal("Enter on a dir should return a fetch cmd")
	}
	m, _ = m.Update(cmd())
	if m.Dir() != child {
		t.Fatalf("Dir() = %q, want %q", m.Dir(), child)
	}
	mustEqual(t, visibleNames(m), []string{"app.log"})
	if m.list.Index() != 0 {
		t.Errorf("cursor should reset in a new dir, got %d", m.list.Index())
	}

	// Up restores the memoised cursor on logs/.
	cmd = m.Up()
	if cmd == nil {
		t.Fatal("Up should return a fetch cmd")
	}
	m, _ = m.Update(cmd())
	if m.Dir() != dir {
		t.Fatalf("Dir() = %q, want %q", m.Dir(), dir)
	}
	if m.list.Index() != 1 {
		t.Errorf("expected cursor restored to 1, got %d", m.list.Index())
	}
}

func TestUpStopsAtRoot(t *testing.T) {
	m := NewModel()
	if cmd := m.Up(); cmd != nil {
		t.Error("Up before first load should return nil")
	}
	m.dir = "/"
	if cmd := m.Up(); cmd != nil {
		t.Error("Up at / should return nil")
	}
}

func TestResetToStartDirWithoutStartDirFallsBack(t *testing.T) {
	m := NewModel()
	if cmd := m.ResetToStartDir(); cmd == nil {
		t.Fatal("ResetToStartDir without a start dir should still return a fetch cmd (home fallback)")
	}
}

// TestResetToStartDirAlwaysFetchesStartDir pins the close→reopen reset:
// the fetch targets the start dir even after navigating elsewhere, and the
// selection is cleared up front.
func TestResetToStartDirAlwaysFetchesStartDir(t *testing.T) {
	start := sampleDir(t)
	m := NewModel()
	m.SetSize(80, 20)
	m.SetStartDir(start)
	cmd := m.ResetToStartDir()
	if cmd == nil {
		t.Fatal("first ResetToStartDir should return a fetch cmd")
	}
	msg, ok := cmd().(LoadedMsg)
	if !ok {
		t.Fatalf("fetch produced %T, want LoadedMsg", cmd())
	}
	if msg.Err != nil {
		t.Fatal(msg.Err)
	}
	if msg.Dir != start {
		t.Fatalf("first fetch dir = %q, want the start dir %q", msg.Dir, start)
	}

	// Navigate elsewhere and mark an entry: the next reset re-fetches the
	// start dir and drops the selection immediately.
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = load(t, m, other)
	m.ToggleSelected()
	if m.SelectedCount() != 1 {
		t.Fatal("selection setup failed")
	}
	cmd = m.ResetToStartDir()
	if cmd == nil {
		t.Fatal("ResetToStartDir after navigation should re-fetch the start dir")
	}
	if m.SelectedCount() != 0 {
		t.Fatalf("selection = %d after reset, want 0", m.SelectedCount())
	}
	msg = cmd().(LoadedMsg)
	if msg.Dir != start {
		t.Fatalf("re-fetch dir = %q, want the start dir %q", msg.Dir, start)
	}
}

// TestStaleLoadedMsgDroppedAfterReset is the regression for the fetch-
// generation guard: an Enter fetch still in flight when ResetToStartDir
// re-arms the start dir must not commit its directory when its LoadedMsg
// lands after the reset's (bubbletea runs both cmds concurrently, so a
// slow read can finish last).
func TestStaleLoadedMsgDroppedAfterReset(t *testing.T) {
	start := t.TempDir()
	if err := os.Mkdir(filepath.Join(start, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := NewModel()
	m.SetSize(80, 20)
	m.SetStartDir(start)
	m, _ = m.Update(m.ResetToStartDir()())
	if m.Dir() != start {
		t.Fatalf("Dir() = %q after the first load, want %q", m.Dir(), start)
	}

	// Arm the slow navigation (the cursor sits on "sub/", the only
	// entry), then reset before it lands.
	slow := m.Enter()
	if slow == nil {
		t.Fatal("Enter did not arm a fetch")
	}
	m, _ = m.Update(m.ResetToStartDir()())
	if m.Dir() != start {
		t.Fatalf("Dir() = %q after the reset load, want %q", m.Dir(), start)
	}
	m, _ = m.Update(slow())
	if m.Dir() != start {
		t.Fatalf("Dir() = %q after the stale LoadedMsg, want %q (superseded fetch committed)", m.Dir(), start)
	}
	if m.Loading() {
		t.Error("Loading() still true after the current-generation load committed")
	}

	// A bare FetchDirCmd (Gen 0, the unguarded external path used by
	// tests and refresh hooks) is still accepted.
	m = load(t, m, filepath.Join(start, "sub"))
	if m.Dir() != filepath.Join(start, "sub") {
		t.Fatalf("Dir() = %q after an unguarded load, want the sub dir", m.Dir())
	}
}

// TestViewHeightStableAfterMultiPageLoad is the regression for the dual-
// pane height mismatch: committing a listing that spans more than one page
// must not grow the rendered pane past the height from SetSize (bubbles'
// SetItems computes the page size against the pagination line's previous
// height; setEntries re-applies the size to converge).
func TestViewHeightStableAfterMultiPageLoad(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 40; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%02d.txt", i)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := NewModel()
	m.SetSize(50, 24)
	m = load(t, m, dir)
	if h := lipgloss.Height(m.View()); h != 24 {
		t.Fatalf("View height = %d after a multi-page load, want 24", h)
	}
}

func TestSelectOnLoadPlacesCursorByName(t *testing.T) {
	dir := sampleDir(t)
	m := NewModel()
	m.SetSize(80, 20)
	m.SelectOnLoad("mid.txt")
	m = load(t, m, dir)
	if e := m.GetSelectedEntry(); e == nil || e.Name() != "mid.txt" {
		t.Fatalf("cursor = %v, want mid.txt", e)
	}
	// An unknown name leaves the cursor alone (top after a plain load).
	m.SelectOnLoad("missing.txt")
	m = load(t, m, dir)
	if e := m.GetSelectedEntry(); e == nil || e.Name() != "data/" {
		t.Fatalf("cursor = %v, want the top entry data/", e)
	}
}

// TestFailedLoadClearsPendingSelect pins that a failed fetch drops an
// armed SelectOnLoad: it must not fire on the next unrelated load.
func TestFailedLoadClearsPendingSelect(t *testing.T) {
	dir := sampleDir(t)
	m := NewModel()
	m.SetSize(80, 20)
	m = load(t, m, dir)
	m.SelectOnLoad("mid.txt")
	newModel, _ := m.Update(LoadedMsg{Dir: dir, Err: errors.New("boom")})
	m = newModel
	m = load(t, m, dir)
	if e := m.GetSelectedEntry(); e == nil || e.Name() != "data/" {
		t.Fatalf("cursor = %v, want the top entry data/ (stale pendingSelect fired)", e)
	}
}

func TestPermissionDeniedKeepsListing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; permission bits are not enforced")
	}
	m := NewModel()
	m.SetSize(80, 20)
	dir := sampleDir(t)
	locked := filepath.Join(dir, "data")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	m = load(t, m, dir)
	m.list.Select(0) // data/
	cmd := m.Enter()
	if cmd == nil {
		t.Fatal("Enter on a dir should return a fetch cmd")
	}
	newModel, errCmd := m.Update(cmd())
	m = newModel
	if errCmd == nil {
		t.Fatal("expected an error cmd from a failed load")
	}
	errMsg, ok := errCmd().(types.ErrMsg)
	if !ok {
		t.Fatalf("expected types.ErrMsg, got %T", errCmd())
	}
	if !strings.Contains(errMsg.Error(), "read dir") {
		t.Errorf("unexpected error text: %q", errMsg.Error())
	}

	// The failed navigation is not committed: dir, listing and rendering
	// are unchanged.
	if m.Dir() != dir {
		t.Errorf("Dir() = %q, want unchanged %q", m.Dir(), dir)
	}
	mustEqual(t, visibleNames(m),
		[]string{"data/", "logs/", "Alpha.txt", "mid.txt", "zeta.txt"})
	if out := ansi.Strip(m.View()); !strings.Contains(out, "zeta.txt") {
		t.Errorf("view should still render the previous listing, got:\n%s", out)
	}
}

func TestSymlinkToDirListsAsDir(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 20)
	dir := t.TempDir()
	target := filepath.Join(dir, "real")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(dir, "gone"), filepath.Join(dir, "broken")); err != nil {
		t.Fatal(err)
	}

	m = load(t, m, dir)
	mustEqual(t, visibleNames(m), []string{"link/", "real/", "broken"})
	for _, e := range m.entries {
		if e.Name() == "broken" && (e.IsDir() || e.Size() != 0) {
			t.Errorf("broken symlink should list as 0-byte file, got %+v", e)
		}
	}
}

func TestViewLinesFitWidthIncludingCJK(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Date(2024, 3, 1, 10, 30, 0, 0, time.UTC)
	writeFile(t, filepath.Join(dir, "一个非常长的中文文件名用来测试终端单元格宽度是否会溢出边界.txt"), 123456789, t0)
	writeFile(t, filepath.Join(dir, "a-very-long-ascii-file-name-that-used-to-wrap-at-eighty-columns.log"), 42, t0)
	if err := os.Mkdir(filepath.Join(dir, "中文目录名也很长很长很长很长很长"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, width := range []int{40, 60, 80, 100} {
		m := NewModel()
		m.SetSize(width, 20)
		m = load(t, m, dir)
		for i, line := range strings.Split(ansi.Strip(m.View()), "\n") {
			if w := ansi.StringWidth(line); w > width {
				t.Errorf("width %d: line %d is %d cells wide:\n%q", width, i, w, line)
			}
		}
	}
}

func TestColumnsNarrowDegrade(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Date(2024, 3, 1, 10, 30, 0, 0, time.UTC)
	writeFile(t, filepath.Join(dir, "report.txt"), 1536, t0)

	m := NewModel()
	m.SetSize(100, 20)
	m = load(t, m, dir)
	out := ansi.Strip(m.View())
	for _, want := range []string{"1.5K", "2024-03-01"} {
		if !strings.Contains(out, want) {
			t.Errorf("wide view should contain %q, got:\n%s", want, out)
		}
	}

	// At 40 cols the mtime column is dropped; the size column stays.
	m.SetSize(40, 20)
	out = ansi.Strip(m.View())
	if strings.Contains(out, "2024-03-01") {
		t.Errorf("narrow view should drop the mtime, got:\n%s", out)
	}
	if !strings.Contains(out, "1.5K") {
		t.Errorf("narrow view should keep the size column, got:\n%s", out)
	}
}

func TestViewHeightMatchesSetSize(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 15)
	m = load(t, m, sampleDir(t))
	if h := strings.Count(m.View(), "\n") + 1; h != 15 {
		t.Errorf("View() is %d lines, want exactly 15", h)
	}
}

func TestFocusChangesBorderOnly(t *testing.T) {
	m := NewModel()
	m.SetSize(40, 10)
	m = load(t, m, sampleDir(t))
	if m.Focused() {
		t.Error("a new locallist should start unfocused (remote pane owns focus)")
	}
	unfocused := m.View()
	m.SetFocused(true)
	focused := m.View()
	if !m.Focused() {
		t.Error("Focused() should be true after SetFocused(true)")
	}
	if ansi.Strip(unfocused) != ansi.Strip(focused) {
		t.Error("focus must only change border styling, not content")
	}
}

// TestTitleFitsNarrowPane pins the dual-pane title guard for the local
// pane: a deep 60-char directory path at a 40-col pane must be middle-
// truncated, never word-wrapped — no rendered line may exceed the width
// AND the view must keep exactly SetSize's height (a wrapped title bar
// adds lines, pushing every row down and misaligning the two panes).
func TestTitleFitsNarrowPane(t *testing.T) {
	deepDir := "/" + strings.Repeat("d", 59) // 60-char path
	m := NewModel()
	m.SetSize(40, 20)
	newModel, _ := m.Update(LoadedMsg{Dir: deepDir, Entries: []Entry{
		{name: "tmp/", path: deepDir + "/tmp", isDir: true},
		{name: "app.txt", path: deepDir + "/app.txt", size: 10},
	}})
	m = newModel
	if m.Dir() != deepDir {
		t.Fatalf("Dir() = %q, want %q", m.Dir(), deepDir)
	}

	lines := strings.Split(m.View(), "\n")
	if len(lines) != 20 {
		t.Errorf("view has %d lines, want exactly 20 (title bar wrapped?)", len(lines))
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w > 40 {
			t.Errorf("line %d is %d cells wide, exceeds 40:\n%q", i, w, ansi.Strip(line))
		}
	}
	// Middle truncation keeps the path tail and the sort suffix readable.
	if out := ansi.Strip(m.View()); !strings.Contains(out, "↑]") {
		t.Errorf("truncated title lost the sort suffix:\n%s", out)
	}
}
