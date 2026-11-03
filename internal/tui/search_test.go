// Tests for the recursive bucket search ('F'): the term is a case-insensitive
// substring of the key, hits jump to the object's prefix, and the jump note
// survives the navigation that follows it.
package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/LinPr/lazys3/internal/tui/state"
	"github.com/LinPr/lazys3/internal/tui/types"
)

func directoryOfTest(t *testing.T, key, want string) {
	t.Helper()
	if got := directoryOf(key); got != want {
		t.Errorf("directoryOf(%q) = %q, want %q", key, got, want)
	}
}

// TestDirectoryOf pins the key→prefix mapping a search hit uses to jump:
// the result must be a "/"-terminated prefix, and a key at the bucket root
// must give an empty prefix (the root itself).
func TestDirectoryOf(t *testing.T) {
	directoryOfTest(t, "a/b/c.txt", "a/b/")
	directoryOfTest(t, "c.txt", "")
	directoryOfTest(t, "dir/", "dir/")
	directoryOfTest(t, "", "")
}

// searchModel returns a model in the object list on bucket "bkt".
func searchModel(t *testing.T) Model {
	t.Helper()
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	m.selectedProfile = "demo"
	m.selectedBucket = "bkt"
	m.selectedObject = "current/"
	m.state = state.ActiveObjectList
	return m
}

// TestSearchJumpsToFirstHit pins the happy path: a hit under another prefix
// moves the listing there rather than merely filtering what is on screen.
func TestSearchJumpsToFirstHit(t *testing.T) {
	m := searchModel(t)
	nm, cmd := m.Update(searchResultsMsg{
		Term:   "report",
		Bucket: "bkt",
		Hits:   []string{"reports/2026/q1-report.pdf", "other/q2-report.pdf"},
	})
	m = nm.(Model)

	if m.selectedBucket != "bkt" || m.selectedObject != "reports/2026/" {
		t.Fatalf("landed at %q/%q, want bkt/reports/2026/", m.selectedBucket, m.selectedObject)
	}
	if m.state != state.ActiveObjectList {
		t.Fatalf("state = %v, want ActiveObjectList", m.state)
	}
	if !m.objectlist.Loading() {
		t.Fatal("search jump did not arm a listing fetch")
	}
	// The note must be delivered as a message, not set directly: the
	// navigation above clears direct status-bar notes.
	var noted bool
	for _, msg := range collectMsgs(cmd) {
		if im, ok := msg.(types.InfoMsg); ok && strings.Contains(im.Text, "report") {
			noted = true
		}
	}
	if !noted {
		t.Fatal("search jump did not emit a status note")
	}
}

// TestSearchNoMatchKeepsLocation pins that an empty result explains itself
// and never moves the user: a search that silently relocates the listing is
// worse than one that finds nothing.
func TestSearchNoMatchKeepsLocation(t *testing.T) {
	m := searchModel(t)
	nm, cmd := m.Update(searchResultsMsg{Term: "nope", Bucket: "bkt"})
	m = nm.(Model)

	if m.selectedObject != "current/" {
		t.Fatalf("location moved to %q on an empty result", m.selectedObject)
	}
	if m.objectlist.Loading() {
		t.Fatal("empty result must not start a fetch")
	}
	// The status-update message is expected; a fetch or navigation is not.
	if cmd != nil {
		for _, msg := range collectMsgs(cmd) {
			switch v := msg.(type) {
			case types.StatusUpdateMsg:
			default:
				t.Fatalf("empty result emitted %T, want only a status update", v)
			}
		}
	}
	if !strings.Contains(m.statusBar.Info(), "no match") {
		t.Fatalf("status info = %q, want a no-match note", m.statusBar.Info())
	}
}

// TestSearchErrorSurfaces pins that a listing failure reaches the status bar
// instead of being swallowed.
func TestSearchErrorSurfaces(t *testing.T) {
	m := searchModel(t)
	_, cmd := m.Update(searchResultsMsg{Term: "x", Bucket: "bkt", Err: errors.New("listing failed")})
	var reported bool
	for _, msg := range collectMsgs(cmd) {
		if em, ok := msg.(types.ErrMsg); ok && strings.Contains(em.Err.Error(), "search") {
			reported = true
		}
	}
	if !reported {
		t.Fatal("search error was not surfaced")
	}
}

// TestSearchRequiresBucket pins the guard: 'F' outside a bucket only hints.
func TestSearchRequiresBucket(t *testing.T) {
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	nm, cmd := m.Update(keyPress('F'))
	m = nm.(Model)
	if m.modal.IsVisible() {
		t.Fatal("search modal opened without a bucket")
	}
	if cmd != nil {
		if msgs := collectMsgs(cmd); len(msgs) > 1 {
			t.Fatalf("guard emitted %d messages, want at most the status update", len(msgs))
		}
	}
}
