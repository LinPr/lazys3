package objectlist

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/v2/list"
	tea "github.com/charmbracelet/bubbletea/v2"
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
	for _, it := range m.objectlist.VisibleItems() {
		names = append(names, it.(Object).Name())
	}
	return names
}

func mustEqual(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("order mismatch:\n got:  %v\n want: %v", got, want)
	}
}

func sampleObjects() []Object {
	t0 := time.Date(2024, 3, 1, 10, 30, 0, 0, time.UTC)
	return []Object{
		{name: "zeta.txt", size: 10, modTime: t0.Add(3 * time.Hour)},
		{name: "logs/", isDir: true},
		{name: "Alpha.txt", size: 300, modTime: t0.Add(1 * time.Hour)},
		{name: "data/", isDir: true},
		{name: "mid.txt", size: 50, modTime: t0.Add(2 * time.Hour)},
	}
}

func TestRenderShowsItems(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 20)
	m.SetObjects([]Object{
		{name: "data/", isDir: true},
		{name: "config"},
	})
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "data/") {
		t.Errorf("expected View to contain 'data/', got:\n%s", out)
	}
	if !strings.Contains(out, "config") {
		t.Errorf("expected View to contain 'config', got:\n%s", out)
	}
}

func TestSortNameDirsFirst(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 20)
	m.SetObjects(sampleObjects())
	mustEqual(t, visibleNames(m),
		[]string{"data/", "logs/", "Alpha.txt", "mid.txt", "zeta.txt"})
}

func TestCycleSortFieldAndDirection(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 20)
	m.SetObjects(sampleObjects())

	// o -> size ascending, dirs still first.
	m = press(m, tea.Key{Code: 'o', Text: "o"})
	mustEqual(t, visibleNames(m),
		[]string{"data/", "logs/", "zeta.txt", "mid.txt", "Alpha.txt"})
	if !strings.Contains(m.objectlist.Title, "size ↑") {
		t.Errorf("title should show 'size ↑', got %q", m.objectlist.Title)
	}

	// O -> size descending, dirs still first.
	m = press(m, tea.Key{Code: 'O', Text: "O"})
	mustEqual(t, visibleNames(m),
		[]string{"logs/", "data/", "Alpha.txt", "mid.txt", "zeta.txt"})
	if !strings.Contains(m.objectlist.Title, "size ↓") {
		t.Errorf("title should show 'size ↓', got %q", m.objectlist.Title)
	}

	// o -> time descending (direction persists across field change).
	m = press(m, tea.Key{Code: 'o', Text: "o"})
	mustEqual(t, visibleNames(m),
		[]string{"logs/", "data/", "zeta.txt", "mid.txt", "Alpha.txt"})
}

func TestSortPersistsAcrossSetObjects(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 20)
	m.SetObjects(sampleObjects())
	m = press(m, tea.Key{Code: 'o', Text: "o"}) // size asc

	// A new listing (navigating prefixes) keeps the sort mode.
	m.SetObjects(sampleObjects())
	mustEqual(t, visibleNames(m),
		[]string{"data/", "logs/", "zeta.txt", "mid.txt", "Alpha.txt"})
}

func TestFilterNarrowsAndExposesState(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 20)
	m.SetObjects(sampleObjects())

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
	if !m.FilterApplied() {
		t.Error("expected FilterApplied() true after enter")
	}
	mustEqual(t, visibleNames(m), []string{"Alpha.txt"})

	// esc clears the applied filter.
	m = press(m, tea.Key{Code: tea.KeyEscape})
	if m.FilterApplied() {
		t.Error("expected FilterApplied() false after esc")
	}
	if len(visibleNames(m)) != 5 {
		t.Errorf("expected all 5 items after clearing filter, got %v", visibleNames(m))
	}
}

func TestSubstringFilter(t *testing.T) {
	ranks := substringFilter("CONF", []string{"config", "data/", "aconfb"})
	if len(ranks) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(ranks))
	}
	if ranks[0].Index != 0 || ranks[1].Index != 2 {
		t.Errorf("unexpected match indexes: %+v", ranks)
	}
	if got := ranks[1].MatchedIndexes; got[0] != 1 || got[len(got)-1] != 4 {
		t.Errorf("unexpected matched rune positions: %v", got)
	}
}

func TestSelectedKeysDisplayOrderAndClear(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 20)
	m.SetObjects(sampleObjects())

	// Select zeta.txt (index 4) then data/ (index 0).
	m.objectlist.Select(4)
	m.ToggleSelected()
	m.objectlist.Select(0)
	m.ToggleSelected()

	mustEqual(t, m.SelectedKeys(), []string{"data/", "zeta.txt"})
	if m.SelectedCount() != 2 {
		t.Errorf("expected SelectedCount 2, got %d", m.SelectedCount())
	}
	if !strings.Contains(m.objectlist.Title, "2 selected") {
		t.Errorf("title should show selection count, got %q", m.objectlist.Title)
	}

	// Selection survives re-sorting, and follows display order.
	m = press(m, tea.Key{Code: 'o', Text: "o"}) // size asc
	mustEqual(t, m.SelectedKeys(), []string{"data/", "zeta.txt"})

	// A new listing clears the selection.
	m.SetObjects(sampleObjects())
	if m.SelectedCount() != 0 {
		t.Errorf("expected selection cleared on SetObjects, got %d", m.SelectedCount())
	}
	if m.SelectedKeys() != nil {
		t.Errorf("expected nil SelectedKeys, got %v", m.SelectedKeys())
	}
}

func TestSelectionSurvivesFiltering(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 20)
	m.SetObjects(sampleObjects())
	m.objectlist.Select(2) // Alpha.txt
	m.ToggleSelected()

	m = press(m, tea.Key{Code: '/', Text: "/"})
	m = typeString(m, "zeta")
	m = press(m, tea.Key{Code: tea.KeyEnter})
	mustEqual(t, m.SelectedKeys(), []string{"Alpha.txt"})
}

func TestCursorMemoRestore(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 20)
	parent := sampleObjects()
	m.SetObjects(parent)
	m.objectlist.Select(3)
	m.RememberPosition("bucket", "prefix/")

	// Navigate into a child prefix: cursor resets.
	m.SetObjects([]Object{{name: "child.txt"}})
	if m.objectlist.Index() != 0 {
		t.Errorf("expected cursor reset on new listing, got %d", m.objectlist.Index())
	}

	// Navigate back: pending restore applies when the listing arrives.
	m.RestorePosition("bucket", "prefix/")
	m.SetObjects(parent)
	if m.objectlist.Index() != 3 {
		t.Errorf("expected cursor restored to 3, got %d", m.objectlist.Index())
	}

	// The restore is one-shot: the next listing starts at the top.
	m.SetObjects(parent)
	if m.objectlist.Index() != 0 {
		t.Errorf("expected cursor reset after one-shot restore, got %d", m.objectlist.Index())
	}
}

func TestCursorMemoClampsAndCaps(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 20)
	m.SetObjects(sampleObjects())
	m.objectlist.Select(4)
	m.RememberPosition("b", "p")

	// Restore against a shorter listing clamps to the last item.
	m.RestorePosition("b", "p")
	m.SetObjects(sampleObjects()[:2])
	if m.objectlist.Index() != 1 {
		t.Errorf("expected clamped cursor 1, got %d", m.objectlist.Index())
	}

	// The memo never grows past its cap.
	for i := 0; i < memoCap*2; i++ {
		m.RememberPosition("b", strings.Repeat("x", i%50)+"/")
	}
	if len(m.posMemo) > memoCap {
		t.Errorf("memo exceeded cap: %d", len(m.posMemo))
	}
}

func TestViewLinesFitWidth(t *testing.T) {
	for _, width := range []int{60, 80, 100} {
		m := NewModel()
		m.SetSize(width, 20)
		m.SetTitle("s3://bucket/very/long/prefix/path/that/could/overflow/the/title/bar")
		m.SetObjects([]Object{
			{name: "dir-with-a-fairly-long-name/", isDir: true},
			{
				name:         "a-very-long-object-name-that-used-to-wrap-at-eighty-columns-in-the-real-tui.txt",
				size:         123456789,
				modTime:      time.Date(2024, 3, 1, 10, 30, 0, 0, time.UTC),
				storageClass: "STANDARD",
			},
			{
				name:         "another-long-key-with-metadata-columns-and-storage-class.log",
				size:         42,
				modTime:      time.Date(2024, 3, 2, 8, 0, 0, 0, time.UTC),
				storageClass: "GLACIER",
			},
		})
		for i, line := range strings.Split(ansi.Strip(m.View()), "\n") {
			if w := ansi.StringWidth(line); w > width {
				t.Errorf("width %d: line %d is %d cells wide:\n%q", width, i, w, line)
			}
		}
	}
}

func TestViewHeightMatchesSetSize(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 15)
	m.SetObjects(sampleObjects())
	if h := strings.Count(m.View(), "\n") + 1; h != 15 {
		t.Errorf("View() is %d lines, want exactly 15", h)
	}
}

func TestDisplayNameRelativeToPrefix(t *testing.T) {
	objs := []Object{
		{name: "syncdir/one.txt", prefix: "syncdir/", size: 10},
		{name: "syncdir/sub/", prefix: "syncdir/", isDir: true},
	}
	if got := objs[0].DisplayName(); got != "one.txt" {
		t.Errorf("DisplayName() = %q, want %q", got, "one.txt")
	}
	if got := objs[1].DisplayName(); got != "sub/" {
		t.Errorf("DisplayName() = %q, want %q", got, "sub/")
	}

	m := NewModel()
	m.SetSize(80, 20)
	m.SetObjects(objs)
	out := ansi.Strip(m.View())
	if strings.Contains(out, "syncdir/one.txt") || strings.Contains(out, "syncdir/sub/") {
		t.Errorf("rows should show prefix-relative names, got:\n%s", out)
	}
	if !strings.Contains(out, "one.txt") || !strings.Contains(out, "sub/") {
		t.Errorf("rows should show relative names, got:\n%s", out)
	}

	// Filtering matches against the relative name, and operations still
	// get the full key.
	m = press(m, tea.Key{Code: '/', Text: "/"})
	m = typeString(m, "one")
	mustEqual(t, visibleNames(m), []string{"syncdir/one.txt"})
	m = press(m, tea.Key{Code: tea.KeyEnter})
	m.ToggleSelected()
	mustEqual(t, m.SelectedKeys(), []string{"syncdir/one.txt"})
}

func TestLoadingStateInEmptyView(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 20)
	m.SetLoading(true)
	if !m.Loading() {
		t.Fatal("Loading() should be true after SetLoading(true)")
	}
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "loading") {
		t.Errorf("loading view should mention loading, got:\n%s", out)
	}

	// The result message clears the loading state.
	m, _ = m.Update(FetchObjectListResultMsg{Objects: nil})
	if m.Loading() {
		t.Error("Loading() should clear when the fetch result lands")
	}
	out = ansi.Strip(m.View())
	if strings.Contains(out, "loading") {
		t.Errorf("empty view should not mention loading after the result, got:\n%s", out)
	}
	if !strings.Contains(out, "No items") {
		t.Errorf("empty view should show 'No items', got:\n%s", out)
	}
}

func TestColumnsWide(t *testing.T) {
	m := NewModel()
	m.SetSize(100, 20)
	m.SetObjects([]Object{
		{name: "dir/", isDir: true},
		{
			name:         "report.txt",
			size:         1536,
			modTime:      time.Date(2024, 3, 1, 10, 30, 0, 0, time.UTC),
			storageClass: "STANDARD",
		},
	})
	out := ansi.Strip(m.View())
	for _, want := range []string{"1.5K", "2024-03-01 10:30", "STD", "dir/"} {
		if !strings.Contains(out, want) {
			t.Errorf("wide view should contain %q, got:\n%s", want, out)
		}
	}
}

func TestColumnsNarrowDegrade(t *testing.T) {
	m := NewModel()
	m.SetSize(30, 20)
	m.SetObjects([]Object{
		{
			name:         "a-very-long-object-name-that-overflows.txt",
			size:         1536,
			modTime:      time.Date(2024, 3, 1, 10, 30, 0, 0, time.UTC),
			storageClass: "STANDARD",
		},
	})
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "…") {
		t.Errorf("narrow view should truncate the name with ellipsis, got:\n%s", out)
	}
	if strings.Contains(out, "STD") {
		t.Errorf("narrow view should drop the storage class, got:\n%s", out)
	}
	if strings.Contains(out, "2024-03-01") {
		t.Errorf("narrow view should drop the mtime, got:\n%s", out)
	}
	if !strings.Contains(out, "1.5K") {
		t.Errorf("narrow view should keep the size column, got:\n%s", out)
	}
}

// TestRowWidthDirEqualsFile pins the delegate invariant that every row is
// emitted at the same final display width regardless of entry kind: a
// directory row (blank size/mtime/class columns) must be padded to
// exactly the width of a file row at the same list width. A shorter dir
// row makes its tail depend on the terminal renderer erasing the rest of
// the line, which is exactly where rendering artifacts hide.
func TestRowWidthDirEqualsFile(t *testing.T) {
	for _, width := range []int{25, 30, 40, 60, 80, 100, 120} {
		m := NewModel()
		m.SetSize(width, 20)
		m.SetObjects([]Object{
			{name: "hello/", isDir: true},
			{
				name:         "LICENSE",
				size:         1024,
				modTime:      time.Date(2025, 2, 25, 9, 49, 0, 0, time.UTC),
				storageClass: "STANDARD",
			},
		})

		d := newSelectDelegate(&m.selected)
		items := m.objectlist.VisibleItems()
		if len(items) != 2 {
			t.Fatalf("width %d: want 2 visible items, got %d", width, len(items))
		}
		var dir, file strings.Builder
		// Index 0 is the cursor row (dirs sort first), index 1 a normal row;
		// also compare with the cursor moved so both kinds are checked in
		// both selected and normal styling.
		d.Render(&dir, m.objectlist, 0, items[0])
		d.Render(&file, m.objectlist, 1, items[1])
		dw, fw := ansi.StringWidth(dir.String()), ansi.StringWidth(file.String())
		if dw != fw {
			t.Errorf("width %d: dir row is %d cells, file row is %d cells:\ndir:  %q\nfile: %q",
				width, dw, fw, dir.String(), file.String())
		}

		m.objectlist.Select(1)
		dir.Reset()
		file.Reset()
		d.Render(&dir, m.objectlist, 0, items[0])
		d.Render(&file, m.objectlist, 1, items[1])
		dw, fw = ansi.StringWidth(dir.String()), ansi.StringWidth(file.String())
		if dw != fw {
			t.Errorf("width %d (cursor on file): dir row is %d cells, file row is %d cells",
				width, dw, fw)
		}
	}
}
