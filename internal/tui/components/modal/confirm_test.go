package modal

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// showConfirmWithFlags opens a confirm modal recording which callback ran.
func showConfirmWithFlags(m *Model, confirmed, cancelled *bool) {
	m.ShowConfirmWithCancel("Delete objects", "Delete 2 object(s) from bkt?",
		func() tea.Cmd { *confirmed = true; return nil },
		func() tea.Cmd { *cancelled = true; return nil },
	)
}

// TestConfirmDefaultIsYesAndEnterConfirms pins the fast-path default
// (explicit user preference): a freshly opened confirm modal has Yes
// focused, so a bare enter confirms. esc/n remain one-key cancels.
func TestConfirmDefaultIsYesAndEnterConfirms(t *testing.T) {
	m := NewModel()
	var confirmed, cancelled bool
	showConfirmWithFlags(&m, &confirmed, &cancelled)

	if !m.focusYes {
		t.Fatal("confirm modal did not open with Yes highlighted")
	}
	m, _ = press(t, m, tea.Key{Code: tea.KeyEnter})
	if !confirmed || cancelled {
		t.Fatalf("enter on default highlight: confirmed=%v cancelled=%v, want confirm only", confirmed, cancelled)
	}
	if m.IsVisible() {
		t.Fatal("modal still visible after enter")
	}
}

// TestConfirmTabTogglesAndEnterExecutesNo pins the tab toggle: tab moves
// the highlight to No and enter then cancels (onCancel runs, onConfirm
// does not).
func TestConfirmTabTogglesAndEnterExecutesNo(t *testing.T) {
	m := NewModel()
	var confirmed, cancelled bool
	showConfirmWithFlags(&m, &confirmed, &cancelled)

	m, _ = press(t, m, tea.Key{Code: tea.KeyTab})
	if m.focusYes {
		t.Fatal("tab did not move the highlight to No")
	}
	m, _ = press(t, m, tea.Key{Code: tea.KeyEnter})
	if confirmed || !cancelled {
		t.Fatalf("enter on No: confirmed=%v cancelled=%v, want cancel only", confirmed, cancelled)
	}
	if m.IsVisible() {
		t.Fatal("modal still visible after enter on No")
	}
}

// TestConfirmArrowsToggleHighlight pins left/right as highlight toggles.
func TestConfirmArrowsToggleHighlight(t *testing.T) {
	m := NewModel()
	m.ShowConfirm("t", "b", nil)

	m, _ = press(t, m, tea.Key{Code: tea.KeyRight})
	if m.focusYes {
		t.Fatal("right arrow did not move the highlight to No")
	}
	m, _ = press(t, m, tea.Key{Code: tea.KeyLeft})
	if !m.focusYes {
		t.Fatal("left arrow did not move the highlight back to Yes")
	}
}

// TestConfirmShortcutsUnchanged pins the y/n/esc shortcuts: they act
// regardless of the current highlight.
func TestConfirmShortcutsUnchanged(t *testing.T) {
	// 'y' confirms even with No highlighted.
	m := NewModel()
	var confirmed, cancelled bool
	showConfirmWithFlags(&m, &confirmed, &cancelled)
	m, _ = press(t, m, tea.Key{Code: tea.KeyTab})
	m, _ = press(t, m, tea.Key{Code: 'y', Text: "y"})
	if !confirmed || cancelled {
		t.Fatalf("'y': confirmed=%v cancelled=%v, want confirm only", confirmed, cancelled)
	}

	// 'n' cancels even with Yes highlighted (the default).
	m = NewModel()
	confirmed, cancelled = false, false
	showConfirmWithFlags(&m, &confirmed, &cancelled)
	m, _ = press(t, m, tea.Key{Code: 'n', Text: "n"})
	if confirmed || !cancelled {
		t.Fatalf("'n': confirmed=%v cancelled=%v, want cancel only", confirmed, cancelled)
	}

	// esc cancels.
	m = NewModel()
	confirmed, cancelled = false, false
	showConfirmWithFlags(&m, &confirmed, &cancelled)
	m, _ = press(t, m, tea.Key{Code: tea.KeyEscape})
	if confirmed || !cancelled {
		t.Fatalf("esc: confirmed=%v cancelled=%v, want cancel only", confirmed, cancelled)
	}
	if m.IsVisible() {
		t.Fatal("modal still visible after esc")
	}
}

// TestConfirmReopenResetsHighlightToYes pins that every ShowConfirm resets
// the highlight, even after the previous modal was left on No.
func TestConfirmReopenResetsHighlightToYes(t *testing.T) {
	m := NewModel()
	m.ShowConfirm("t", "b", nil)
	m, _ = press(t, m, tea.Key{Code: tea.KeyTab})
	m, _ = press(t, m, tea.Key{Code: tea.KeyEscape})

	m.ShowConfirm("t2", "b2", nil)
	if !m.focusYes {
		t.Fatal("reopened confirm modal did not reset the highlight to Yes")
	}
}

// TestConfirmButtonsRenderAndHighlightMoves pins the footer buttons: both
// render, the highlight styling follows the focus, both states occupy the
// same width (no footer jitter on tab), and the box still fits 80 columns.
func TestConfirmButtonsRenderAndHighlightMoves(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 24)
	m.ShowConfirm("Delete objects", "Delete 2 object(s) from bkt?", nil)

	viewYes := m.View()
	plain := stripANSI(viewYes)
	if !strings.Contains(plain, "[ Yes ]") || !strings.Contains(plain, "[ No ]") {
		t.Fatalf("confirm footer missing the buttons:\n%s", plain)
	}
	if !strings.Contains(viewYes, buttonFocusStyle().Render("[ Yes ]")) {
		t.Fatal("Yes button not rendered with the focus style on open")
	}

	m, _ = press(t, m, tea.Key{Code: tea.KeyTab})
	viewNo := m.View()
	if viewNo == viewYes {
		t.Fatal("view unchanged after tab: the highlight did not move")
	}
	if !strings.Contains(viewNo, buttonFocusStyle().Render("[ No ]")) {
		t.Fatal("No button not rendered with the focus style after tab")
	}
	if strings.Contains(viewNo, buttonFocusStyle().Render("[ Yes ]")) {
		t.Fatal("Yes button still carries the focus style after tab")
	}

	// Equal widths in both states: the focus chip must not add columns,
	// or the footer jitters every time the highlight moves.
	if wYes, wNo := lipgloss.Width(viewYes), lipgloss.Width(viewNo); wYes != wNo {
		t.Fatalf("footer width changed with the highlight: %d vs %d cols", wYes, wNo)
	}
	if fw, bw := lipgloss.Width(buttonFocusStyle().Render("[ Yes ]")), lipgloss.Width(buttonBlurStyle.Render("[ Yes ]")); fw != bw {
		t.Fatalf("focused button is %d cols, blurred %d — states must match", fw, bw)
	}

	// 80-col fit at the default box width.
	for i, line := range strings.Split(viewYes, "\n") {
		if w := lipgloss.Width(line); w > 80 {
			t.Fatalf("line %d is %d cols wide, want <= 80", i, w)
		}
	}
}

// TestInfoModalSingleOKButton pins ShowInfo: an informational modal (e.g.
// the presign result) renders a single [ OK ] button — no Yes/No question
// — and enter dismisses it without running any callback.
func TestInfoModalSingleOKButton(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 24)
	m.ShowInfo("Presigned URL", "https://example.com/x — copied to clipboard")

	plain := stripANSI(m.View())
	if !strings.Contains(plain, "[ OK ]") {
		t.Fatalf("info modal missing the OK button:\n%s", plain)
	}
	if strings.Contains(plain, "[ Yes ]") || strings.Contains(plain, "[ No ]") {
		t.Fatalf("info modal renders Yes/No buttons:\n%s", plain)
	}

	// tab has nothing to toggle; enter dismisses.
	m, _ = press(t, m, tea.Key{Code: tea.KeyTab})
	m, _ = press(t, m, tea.Key{Code: tea.KeyEnter})
	if m.IsVisible() {
		t.Fatal("info modal still visible after enter")
	}

	// esc dismisses too.
	m.ShowInfo("t", "b")
	m, _ = press(t, m, tea.Key{Code: tea.KeyEscape})
	if m.IsVisible() {
		t.Fatal("info modal still visible after esc")
	}
}
