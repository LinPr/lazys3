package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea/v2"

	"github.com/LinPr/lazys3/internal/tui/components/objectlist"
	"github.com/LinPr/lazys3/internal/tui/state"
)

// pump feeds msg into the real Update, executes every returned tea.Cmd,
// and feeds the resulting messages back into Update until the model goes
// quiet — a mini bubbletea event loop. This is what surfaces bubbles'
// ASYNC filtering: typing in the filter input only returns a tea.Cmd
// whose list.FilterMatchesMsg must be routed back into the SAME list by
// the top-level Update before VisibleItems narrows. Unit tests that drain
// the cmd straight into the component (locallist's own tests) cannot
// catch a top-level routing bug; this pump goes through Model.Update for
// every message, exactly like the real program.
//
// Cursor-blink messages are dropped: cursor.BlinkCmd blocks on the blink
// timer (~0.5s) and its BlinkMsg re-arms another BlinkCmd, so feeding
// them back would spin the pump forever without touching list state.
func pump(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	queue := []tea.Msg{msg}
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 1000 {
			t.Fatal("event pump did not settle after 1000 steps")
		}
		cur := queue[0]
		queue = queue[1:]
		if strings.Contains(fmt.Sprintf("%T", cur), "cursor.") {
			continue
		}
		nm, cmd := m.Update(cur)
		m = nm.(Model)
		queue = append(queue, collectMsgs(cmd)...)
	}
	return m
}

func localVisibleNames(m Model) []string {
	var names []string
	for _, e := range m.localList.VisibleEntries() {
		names = append(names, e.Name())
	}
	return names
}

// TestLocalPaneFilterNarrowsViaEventPump pins defect: with the local pane
// focused in dual mode, typing '/' + "tm" must narrow the local listing
// while typing (bubbles' async FilterMatchesMsg routed back to the local
// list), enter must keep it narrowed, and esc must restore the full
// listing.
func TestLocalPaneFilterNarrowsViaEventPump(t *testing.T) {
	m, _ := dualModel(t, "tmp.txt", "alpha.txt", "beta.txt")
	m = pump(t, m, tabPress())
	if !m.localFocused() {
		t.Fatal("tab did not focus the local pane")
	}

	m = pump(t, m, keyPress('/'))
	if !m.localList.Filtering() {
		t.Fatal("'/' did not focus the local filter input")
	}
	for _, r := range "tm" {
		m = pump(t, m, keyPress(r))
	}
	// The listing must narrow LIVE, while the pattern is still being typed.
	if got := localVisibleNames(m); len(got) != 1 || got[0] != "tmp.txt" {
		t.Fatalf("visible after typing %q = %v, want [tmp.txt]", "tm", got)
	}

	// enter applies the filter; the narrowing must survive.
	m = pump(t, m, tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if m.localList.Filtering() {
		t.Fatal("enter did not apply the filter")
	}
	if got := localVisibleNames(m); len(got) != 1 || got[0] != "tmp.txt" {
		t.Fatalf("visible after enter = %v, want [tmp.txt]", got)
	}

	// esc clears the filter: the full listing returns.
	m = pump(t, m, tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if got := localVisibleNames(m); len(got) != 3 {
		t.Fatalf("visible after esc = %v, want all 3 entries", got)
	}
}

// TestRemoteFilterStillNarrowsViaEventPump guards the other side of the
// FilterMatchesMsg routing: with the REMOTE pane focused in dual mode the
// message must keep flowing to the remote list (the localFocused branch
// must not swallow it).
func TestRemoteFilterStillNarrowsViaEventPump(t *testing.T) {
	m, _ := dualModel(t, "a.txt")
	// Remote pane starts focused; make the object list the active remote
	// list with a listing to narrow.
	m.state = state.ActiveObjectList
	m.selectedBucket = "bkt"
	m.objectlist.SetObjects([]objectlist.Object{
		objectlist.NewDirObject("tmp/"),
		objectlist.NewDirObject("docs/"),
	})

	m = pump(t, m, keyPress('/'))
	if !m.objectlist.Filtering() {
		t.Fatal("'/' did not focus the remote filter input")
	}
	for _, r := range "tm" {
		m = pump(t, m, keyPress(r))
	}
	var names []string
	for _, o := range m.objectlist.VisibleObjects() {
		names = append(names, o.Name())
	}
	if len(names) != 1 || names[0] != "tmp/" {
		t.Fatalf("remote visible after typing = %v, want [tmp/]", names)
	}
}
