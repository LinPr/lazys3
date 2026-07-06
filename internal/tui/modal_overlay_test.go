package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"

	"github.com/LinPr/lazys3/internal/tui/components/objectlist"
)

// TestConfirmModalFloatsOverLayout pins ISSUE 6: a confirm modal composited
// over an 80x24 layout is a centered floating box — the layout's first and
// last rows (pane top border, status bar) stay visible and byte-identical
// around it, and closing the modal reveals the untouched layout.
func TestConfirmModalFloatsOverLayout(t *testing.T) {
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	base := m.View()
	baseLines := strings.Split(base, "\n")

	m.modal.ShowConfirm("Delete object", "delete s3://bkt/dir/file.txt ?", nil)
	out := m.View()
	outLines := strings.Split(out, "\n")

	if len(outLines) != len(baseLines) {
		t.Fatalf("modal view has %d lines, layout has %d", len(outLines), len(baseLines))
	}
	if w := lipgloss.Width(out); w != 80 {
		t.Fatalf("modal view width = %d, want 80", w)
	}
	if !strings.Contains(out, "Delete object") || !strings.Contains(out, "file.txt ?") {
		t.Fatal("floating modal content missing from the composited view")
	}
	// Every bg row above and below the centered box is untouched — in
	// particular the first (pane top border) and last (status bar) lines.
	boxH := lipgloss.Height(m.modal.View())
	boxTop := (24 - boxH) / 2
	for i := range outLines {
		if i >= boxTop && i < boxTop+boxH {
			continue
		}
		if outLines[i] != baseLines[i] {
			t.Fatalf("bg line %d changed under the floating modal:\n got %q\nwant %q",
				i, outLines[i], baseLines[i])
		}
	}
	// The box's border row differs from the bg but keeps the 80-col width.
	changed := false
	for i, l := range outLines {
		if w := lipgloss.Width(l); w != 80 {
			t.Fatalf("line %d width = %d, want 80", i, w)
		}
		if l != baseLines[i] {
			changed = true
		}
	}
	if !changed {
		t.Fatal("no line changed: the modal was not composited at all")
	}

	// Confirming removes the box and reveals the identical layout.
	m = updateModel(t, m, tea.KeyPressMsg(tea.Key{Code: 'y', Text: "y"}))
	if m.modal.IsVisible() {
		t.Fatal("modal still visible after y")
	}
	if got := m.View(); got != base {
		t.Fatal("layout after closing the modal differs from the pre-modal layout")
	}
}

// TestInputModalFloatsOverVersionOverlay pins the overlay precedence: with
// the versions overlay open, a modal floats over the OVERLAY's canvas (not
// the base layout), so the overlay stays visible around the box.
func TestInputModalFloatsOverVersionOverlay(t *testing.T) {
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	_ = m.versionView.Show(objectlist.Option{}, "bkt", "dir/file.txt") // Cmd not executed: no fetch runs
	overlay := m.View()

	m.modal.Show("Download version to", "/tmp/file.txt", nil)
	out := m.View()
	outLines := strings.Split(out, "\n")
	overlayLines := strings.Split(overlay, "\n")
	if len(outLines) != len(overlayLines) {
		t.Fatalf("modal-over-overlay view has %d lines, overlay has %d", len(outLines), len(overlayLines))
	}
	if outLines[0] != overlayLines[0] || outLines[len(outLines)-1] != overlayLines[len(overlayLines)-1] {
		t.Fatal("versions overlay rows not visible around the floating modal")
	}
	if !strings.Contains(out, "Download version to") {
		t.Fatal("modal title missing over the versions overlay")
	}
}
