package statusbar

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LinPr/lazys3/internal/tui/components/style"
	"github.com/LinPr/lazys3/internal/tui/types"
)

func TestTruncateMiddle(t *testing.T) {
	tests := []struct {
		name  string
		s     string
		width int
	}{
		{"ascii short", "s3://bucket", 20},
		{"ascii truncated", "s3://bucket/some/long/prefix/key.txt", 16},
		{"cjk truncated", "s3://バケット/日本語のプレフィックス/ファイル.txt", 16},
		{"cjk narrow", "日本語日本語日本語日本語", 7},
		{"mixed", "s3://bucket/中文路径/文件名.txt", 12},
		{"width one", "日本語", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateMiddle(tt.s, tt.width)
			if w := lipgloss.Width(got); w > tt.width {
				t.Errorf("truncateMiddle(%q, %d) = %q, width %d exceeds budget", tt.s, tt.width, got, w)
			}
			if lipgloss.Width(tt.s) > tt.width && !strings.Contains(got, "…") {
				t.Errorf("truncateMiddle(%q, %d) = %q, expected ellipsis", tt.s, tt.width, got)
			}
		})
	}

	if got := truncateMiddle("anything", 0); got != "" {
		t.Errorf("truncateMiddle with width 0 = %q, want empty", got)
	}
	if got := truncateMiddle("short", 10); got != "short" {
		t.Errorf("truncateMiddle without overflow = %q, want unchanged", got)
	}
}

// TestStatusBarSegmentsAt80Cols pins the reworked layout at a standard
// width: every segment present, no s3 URI, no overflow, "? help" right-
// aligned, and a long CJK info note middle-truncated instead of wrapping.
func TestStatusBarSegmentsAt80Cols(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 1)
	m.SetProfile("dev")
	m.SetBucket("bkt")
	m.SetPrefix("pre/")
	m.SetPane("local")
	m.SetSelectedCount(3)
	m.SetTransferCounts(2, 5, 1)
	m.SetInfo("日本語のとても長い転送情報テキストが中間で切り詰められることを確認する")

	view := m.View()
	if strings.Contains(view, "\n") {
		t.Fatalf("View() rendered multiple lines:\n%s", view)
	}
	if w := lipgloss.Width(view); w != 80 {
		t.Fatalf("View() width = %d, want exactly 80", w)
	}
	plain := ansi.Strip(view)
	for _, want := range []string{"dev", "local", "3 selected", ">2", "ok5", "x1", "? help", "…"} {
		if !strings.Contains(plain, want) {
			t.Errorf("View() missing %q: %q", want, plain)
		}
	}
	if strings.Contains(plain, "s3://") {
		t.Errorf("View() still renders the s3 URI: %q", plain)
	}
	if !strings.HasSuffix(strings.TrimRight(plain, " "), "? help") {
		t.Errorf("'? help' is not right-aligned: %q", plain)
	}
}

// TestPaneAndSummaryOmittedWhenAbsent pins the conditional segments:
// single-pane mode has no pane indicator, zero selection no count, and no
// transfer rows no summary.
func TestPaneAndSummaryOmittedWhenAbsent(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 1)
	m.SetProfile("dev")
	plain := ansi.Strip(m.View())
	for _, absent := range []string{"local", "remote", "selected", ">", "ok", "x"} {
		if strings.Contains(plain, absent) {
			t.Errorf("View() renders %q with nothing to show: %q", absent, plain)
		}
	}
	if !strings.Contains(plain, "? help") {
		t.Errorf("View() missing the help hint: %q", plain)
	}
}

// TestTransferSummaryGlyphs pins the glyph sets: ASCII by default, the
// icon glyphs with nerd_font on; zero tallies are omitted.
func TestTransferSummaryGlyphs(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 1)
	m.SetTransferCounts(1, 2, 0)

	plain := ansi.Strip(m.View())
	if !strings.Contains(plain, ">1") || !strings.Contains(plain, "ok2") {
		t.Errorf("ASCII summary missing: %q", plain)
	}
	if strings.Contains(plain, "x0") {
		t.Errorf("zero failed tally rendered: %q", plain)
	}

	style.SetNerdFont(true)
	defer style.SetNerdFont(false)
	plain = ansi.Strip(m.View())
	if !strings.Contains(plain, "▶1") || !strings.Contains(plain, "✓2") {
		t.Errorf("nerd-font summary missing: %q", plain)
	}
}

// TestViewStaysSingleLineWhenNarrow pins the narrow-terminal behavior: the
// bar truncates instead of wrapping, keeping the selection count and the
// error visible.
func TestViewStaysSingleLineWhenNarrow(t *testing.T) {
	m := NewModel()
	m.SetSize(40, 1)
	m.SetProfile("dev")
	m.SetSelectedCount(3)
	m.SetError("boom")

	view := m.View()
	if strings.Contains(view, "\n") {
		t.Fatalf("View() rendered multiple lines:\n%s", view)
	}
	if w := lipgloss.Width(view); w > 40 {
		t.Fatalf("View() width = %d, exceeds 40", w)
	}
	plain := ansi.Strip(view)
	if !strings.Contains(plain, "3 selected") {
		t.Errorf("View() dropped the selection block: %q", plain)
	}
	if !strings.Contains(plain, "boom") {
		t.Errorf("View() dropped the error block: %q", plain)
	}
}

// TestInfoClearedOnlyWhenAsked pins the ClearInfo contract: a transfer-
// tally-only refresh (ClearInfo unset) keeps the note readable; a
// navigation-driven update (ClearInfo set by emitStatusUpdate) dismisses it.
func TestInfoClearedOnlyWhenAsked(t *testing.T) {
	m := NewModel()
	m.SetSize(120, 1)
	m.SetInfo("presigned URL copied to clipboard")

	if view := m.View(); !strings.Contains(view, "copied to clipboard") {
		t.Fatalf("View() dropped the info block: %q", view)
	}

	m, _ = m.Update(types.StatusUpdateMsg{Profile: "dev", TransfersDone: 1})
	if m.Info() == "" {
		t.Fatal("Info() cleared by a StatusUpdateMsg without ClearInfo")
	}

	m, _ = m.Update(types.StatusUpdateMsg{Profile: "dev", ClearInfo: true})
	if m.Info() != "" {
		t.Fatalf("Info() = %q after ClearInfo StatusUpdateMsg, want cleared", m.Info())
	}
	if strings.Contains(m.View(), "copied to clipboard") {
		t.Fatal("View() still shows the info block after ClearInfo StatusUpdateMsg")
	}
}

// TestStatusUpdateMsgCarriesNewFields pins that Update applies the pane
// and tallies from the message (the TUI's emitStatusUpdate feeds them).
func TestStatusUpdateMsgCarriesNewFields(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 1)
	m, _ = m.Update(types.StatusUpdateMsg{
		Profile:          "dev",
		Pane:             "local",
		SelectedCount:    2,
		TransfersRunning: 1,
		TransfersDone:    4,
		TransfersFailed:  3,
	})
	plain := ansi.Strip(m.View())
	for _, want := range []string{"local", "2 selected", ">1", "ok4", "x3"} {
		if !strings.Contains(plain, want) {
			t.Errorf("View() missing %q after StatusUpdateMsg: %q", want, plain)
		}
	}
}
