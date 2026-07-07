package statusbar

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
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
	m.SetTransferStats(types.TransferStats{
		UpActive: 1, UpDone: 1, UpTotal: 2,
		BytesDone: 50, BytesTotal: 100,
		Failed: 1,
	})
	m.SetInfo("日本語のとても長い転送情報テキストが中間で切り詰められることを確認する")

	view := m.View()
	if strings.Contains(view, "\n") {
		t.Fatalf("View() rendered multiple lines:\n%s", view)
	}
	if w := lipgloss.Width(view); w != 80 {
		t.Fatalf("View() width = %d, want exactly 80", w)
	}
	plain := ansi.Strip(view)
	for _, want := range []string{"dev", "local", "3 selected", "[####----]", "^1/2", "x1", "? help", "…"} {
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
// transfer activity ever means no transfer segment at all.
func TestPaneAndSummaryOmittedWhenAbsent(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 1)
	m.SetProfile("dev")
	plain := ansi.Strip(m.View())
	for _, absent := range []string{"local", "remote", "selected", "[", "^", "v0", "x0"} {
		if strings.Contains(plain, absent) {
			t.Errorf("View() renders %q with nothing to show: %q", absent, plain)
		}
	}
	if !strings.Contains(plain, "? help") {
		t.Errorf("View() missing the help hint: %q", plain)
	}
}

// TestTransferSegmentRunning pins the active state: an aggregate progress
// bar over the known byte totals plus per-direction done/total batch
// counts; a direction with an empty batch is omitted; lifetime totals do
// NOT render while something runs. ASCII glyphs by default, arrows with
// nerd_font on.
func TestTransferSegmentRunning(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 1)
	m.SetTransferStats(types.TransferStats{
		UpActive: 1, UpDone: 1, UpTotal: 2,
		DownActive: 1, DownDone: 0, DownTotal: 1,
		BytesDone: 25, BytesTotal: 100,
		LifetimeUp: 9, LifetimeDown: 9,
	})

	plain := ansi.Strip(m.View())
	for _, want := range []string{"[##------]", "^1/2", "v0/1"} {
		if !strings.Contains(plain, want) {
			t.Errorf("running segment missing %q: %q", want, plain)
		}
	}
	if strings.Contains(plain, "^9") || strings.Contains(plain, "v9") {
		t.Errorf("lifetime totals rendered while transfers run: %q", plain)
	}

	style.SetNerdFont(true)
	defer style.SetNerdFont(false)
	plain = ansi.Strip(m.View())
	for _, want := range []string{"[██░░░░░░]", "↑1/2", "↓0/1"} {
		if !strings.Contains(plain, want) {
			t.Errorf("nerd-font running segment missing %q: %q", want, plain)
		}
	}

	// Upload-only batch: the download counts disappear entirely.
	m.SetTransferStats(types.TransferStats{
		UpActive: 1, UpDone: 0, UpTotal: 1,
		BytesDone: 100, BytesTotal: 100,
	})
	plain = ansi.Strip(m.View())
	if !strings.Contains(plain, "↑0/1") || strings.Contains(plain, "↓") {
		t.Errorf("upload-only segment wrong: %q", plain)
	}
}

// TestTransferSegmentIndeterminate pins the unknown-totals state: when no
// active row knows its byte total, the bar renders a bouncing block (no
// bogus percent-style full/empty fill), and the bounce follows the tick
// frame.
func TestTransferSegmentIndeterminate(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 1)
	m.SetTransferStats(types.TransferStats{UpActive: 1, UpTotal: 1, Frame: 0})
	first := ansi.Strip(m.View())
	if !strings.Contains(first, "[###-----]") {
		t.Errorf("indeterminate bar missing at frame 0: %q", first)
	}
	m.SetTransferStats(types.TransferStats{UpActive: 1, UpTotal: 1, Frame: 2})
	second := ansi.Strip(m.View())
	if !strings.Contains(second, "[--###---]") {
		t.Errorf("indeterminate bar did not bounce with the frame: %q", second)
	}
}

// TestTransferSegmentIdleLifetime pins the idle state: no bar, the
// lifetime completed totals per direction (zero segments omitted), and
// the ✗ tally for failed/canceled rows.
func TestTransferSegmentIdleLifetime(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 1)
	m.SetTransferStats(types.TransferStats{LifetimeUp: 3, LifetimeDown: 0, Failed: 2})

	plain := ansi.Strip(m.View())
	if !strings.Contains(plain, "^3") || !strings.Contains(plain, "x2") {
		t.Errorf("idle segment missing lifetime/failed tallies: %q", plain)
	}
	if strings.Contains(plain, "[") {
		t.Errorf("idle segment still renders a progress bar: %q", plain)
	}
	if strings.Contains(plain, "v0") {
		t.Errorf("zero lifetime download tally rendered: %q", plain)
	}

	style.SetNerdFont(true)
	defer style.SetNerdFont(false)
	plain = ansi.Strip(m.View())
	if !strings.Contains(plain, "↑3") || !strings.Contains(plain, "✗2") {
		t.Errorf("nerd-font idle segment missing: %q", plain)
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

	m, _ = m.Update(types.StatusUpdateMsg{Profile: "dev"})
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
// and selection from the message (the TUI's emitStatusUpdate feeds them);
// the transfer segment stays whatever SetTransferStats last pushed.
func TestStatusUpdateMsgCarriesNewFields(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 1)
	m.SetTransferStats(types.TransferStats{LifetimeUp: 4})
	m, _ = m.Update(types.StatusUpdateMsg{
		Profile:       "dev",
		Pane:          "local",
		SelectedCount: 2,
	})
	plain := ansi.Strip(m.View())
	for _, want := range []string{"local", "2 selected", "^4"} {
		if !strings.Contains(plain, want) {
			t.Errorf("View() missing %q after StatusUpdateMsg: %q", want, plain)
		}
	}
}
