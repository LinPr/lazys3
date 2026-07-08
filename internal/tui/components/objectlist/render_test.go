package objectlist

import (
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LinPr/lazys3/internal/tui/types"
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

// pressSort feeds a sort key press and returns the model plus the status
// bar note (types.InfoMsg) the key emitted, draining filter refreshes
// like press does.
func pressSort(m Model, k tea.Key) (Model, string) {
	newModel, cmd := m.Update(tea.KeyPressMsg(k))
	note := ""
	var walk func(tea.Cmd)
	walk = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			for _, sub := range msg {
				walk(sub)
			}
		case types.InfoMsg:
			note = msg.Text
		case list.FilterMatchesMsg:
			newModel, _ = newModel.Update(msg)
		}
	}
	walk(cmd)
	return newModel, note
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

	// o -> size ascending, dirs still first. The sort mode is announced
	// as a status-bar note, never in the title.
	m, note := pressSort(m, tea.Key{Code: 'o', Text: "o"})
	mustEqual(t, visibleNames(m),
		[]string{"data/", "logs/", "zeta.txt", "mid.txt", "Alpha.txt"})
	if note != "sort: size ↑" {
		t.Errorf("'o' note = %q, want 'sort: size ↑'", note)
	}
	if strings.Contains(m.objectlist.Title, "size") || strings.Contains(m.objectlist.Title, "[") {
		t.Errorf("title must not carry a sort suffix, got %q", m.objectlist.Title)
	}

	// O -> size descending, dirs still first.
	m, note = pressSort(m, tea.Key{Code: 'O', Text: "O"})
	mustEqual(t, visibleNames(m),
		[]string{"logs/", "data/", "Alpha.txt", "mid.txt", "zeta.txt"})
	if note != "sort: size ↓" {
		t.Errorf("'O' note = %q, want 'sort: size ↓'", note)
	}

	// o -> time descending (direction persists across field change).
	m, note = pressSort(m, tea.Key{Code: 'o', Text: "o"})
	mustEqual(t, visibleNames(m),
		[]string{"logs/", "data/", "zeta.txt", "mid.txt", "Alpha.txt"})
	if note != "sort: time ↓" {
		t.Errorf("second 'o' note = %q, want 'sort: time ↓'", note)
	}
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

// TestSelectedRowMarkAndHighlight pins the multi-select visuals: a toggled
// row renders "✔ " directly before the name and the whole row carries the
// mark foreground (style.SelectedMarkFg, green), while untoggled rows get a
// blank 2-cell marker and no mark color.
func TestSelectedRowMarkAndHighlight(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 20)
	m.SetObjects(sampleObjects())
	m.objectlist.Select(4) // zeta.txt
	m.ToggleSelected()
	m.objectlist.Select(0) // move the cursor off the marked row

	d := newSelectDelegate(&m.selected)
	items := m.objectlist.VisibleItems()
	var marked, normal strings.Builder
	d.Render(&marked, m.objectlist, 4, items[4])
	d.Render(&normal, m.objectlist, 3, items[3])

	if out := ansi.Strip(marked.String()); !strings.Contains(out, "✔ zeta.txt") {
		t.Errorf("marked row should render '✔ ' before the name, got %q", out)
	}
	if out := ansi.Strip(normal.String()); !strings.Contains(out, "  mid.txt") || strings.Contains(out, "✔") {
		t.Errorf("unmarked row should render a blank 2-cell marker, got %q", out)
	}
	// style.SelectedMarkFg (#04B575) as an SGR truecolor foreground.
	const markSGR = "38;2;4;181;117"
	if !strings.Contains(marked.String(), markSGR) {
		t.Errorf("marked row should carry the mark foreground %s:\n%q", markSGR, marked.String())
	}
	if strings.Contains(normal.String(), markSGR) {
		t.Errorf("unmarked row must not carry the mark foreground:\n%q", normal.String())
	}

	// The cursor row still composes with the mark: cursor styling (left
	// border indicator) plus the ✔ marker.
	m.objectlist.Select(4)
	var cursorMarked strings.Builder
	d.Render(&cursorMarked, m.objectlist, 4, items[4])
	if out := ansi.Strip(cursorMarked.String()); !strings.Contains(out, "✔ zeta.txt") || !strings.Contains(out, "│") {
		t.Errorf("cursor+marked row should keep both the ✔ and the cursor indicator, got %q", out)
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
	utc := time.Date(2024, 3, 1, 10, 30, 0, 0, time.UTC)
	m.SetObjects([]Object{
		{name: "dir/", isDir: true},
		{
			name:         "report.txt",
			size:         1536,
			modTime:      utc,
			storageClass: "STANDARD",
		},
	})
	out := ansi.Strip(m.View())
	// The mtime column renders the UTC SDK timestamp as local wall clock.
	for _, want := range []string{"1.5K", utc.Local().Format("2006-01-02 15:04"), "STD", "dir/"} {
		if !strings.Contains(out, want) {
			t.Errorf("wide view should contain %q, got:\n%s", want, out)
		}
	}
}

// TestMtimeRendersInLocalTimezone pins the timezone fix: an S3 UTC
// timestamp renders as its LOCAL equivalent, never raw UTC. Run under a
// fixed non-UTC TZ so the assertion bites on any machine.
func TestMtimeRendersInLocalTimezone(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	prev := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = prev })

	m := NewModel()
	m.SetSize(100, 20)
	m.SetObjects([]Object{{
		name:    "report.txt",
		size:    1,
		modTime: time.Date(2024, 3, 1, 22, 30, 0, 0, time.UTC),
	}})
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "2024-03-02 06:30") {
		t.Errorf("mtime should render UTC 22:30 as +08:00 next-day 06:30, got:\n%s", out)
	}
	if strings.Contains(out, "2024-03-01 22:30") {
		t.Errorf("mtime column still renders raw UTC:\n%s", out)
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

// TestTitleFitsNarrowPane pins the dual-pane title guard: at a 40-col
// pane with a ~60-char base title, the title must be middle-truncated
// rather than word-wrapped by lipgloss — no rendered line may exceed the
// width AND the view must keep exactly SetSize's height (a wrapped title
// bar adds lines, pushing every row down and misaligning the two panes).
func TestTitleFitsNarrowPane(t *testing.T) {
	longBase := "s3://" + strings.Repeat("x", 55) // 60-char base title
	assertFits := func(t *testing.T, m Model, width, height int) {
		t.Helper()
		lines := strings.Split(m.View(), "\n")
		if len(lines) != height {
			t.Errorf("view has %d lines, want exactly %d (title bar wrapped?)", len(lines), height)
		}
		for i, line := range lines {
			if w := ansi.StringWidth(line); w > width {
				t.Errorf("line %d is %d cells wide, exceeds %d:\n%q", i, w, width, ansi.Strip(line))
			}
		}
	}

	m := NewModel()
	m.SetSize(40, 20)
	m.SetTitle(longBase)
	m.SetObjects(sampleObjects())
	assertFits(t, m, 40, 20)
	// Middle truncation keeps the head of the URI readable and never
	// smuggles a sort suffix back in.
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "s3://") || !strings.Contains(out, "…") {
		t.Errorf("truncated title lost its head or its ellipsis:\n%s", out)
	}
	if strings.Contains(out, "↑]") || strings.Contains(out, "↓]") {
		t.Errorf("title carries a sort suffix again:\n%s", out)
	}

	// A title set wide and then shrunk must be re-fit by SetSize.
	m2 := NewModel()
	m2.SetSize(100, 20)
	m2.SetTitle(longBase)
	m2.SetObjects(sampleObjects())
	m2.SetSize(40, 20)
	assertFits(t, m2, 40, 20)

	// And growing back re-fits from the untruncated base title.
	m2.SetSize(100, 20)
	if !strings.Contains(ansi.Strip(m2.View()), longBase) {
		t.Error("growing the pane did not restore the full title")
	}
	assertFits(t, m2, 100, 20)
}

// TestStaleFetchResultDropped pins the generation guard: a listing from a
// superseded Fetch is dropped (and does not clear the newer fetch's
// loading state), while the current generation's result applies. Unstamped
// results (Gen 0, a bare FetchObjectListCmd) always apply.
func TestStaleFetchResultDropped(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 20)
	_ = m.Fetch(Option{S3Uri: "s3://old/"}) // gen 1
	_ = m.Fetch(Option{S3Uri: "s3://new/"}) // gen 2 supersedes gen 1

	m, _ = m.Update(FetchObjectListResultMsg{Gen: 1, Objects: []Object{{name: "stale.txt"}}})
	if names := visibleNames(m); len(names) != 0 {
		t.Fatalf("stale gen-1 result applied: %v", names)
	}
	if !m.Loading() {
		t.Fatal("stale result cleared the current fetch's loading state")
	}

	m, _ = m.Update(FetchObjectListResultMsg{Gen: 2, Objects: []Object{{name: "fresh.txt"}}})
	mustEqual(t, visibleNames(m), []string{"fresh.txt"})
	if m.Loading() {
		t.Fatal("current-generation result did not clear loading")
	}

	m, _ = m.Update(FetchObjectListResultMsg{Objects: []Object{{name: "plain.txt"}}})
	mustEqual(t, visibleNames(m), []string{"plain.txt"})
}
