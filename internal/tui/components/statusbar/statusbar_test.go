package statusbar

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss/v2"

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

func TestViewStaysSingleLineWithCJKPrefix(t *testing.T) {
	m := NewModel()
	m.SetSize(40, 1)
	m.SetProfile("dev")
	m.SetBucket("バケット")
	m.SetPrefix("日本語のとても長いプレフィックス/深い/階層/")
	m.SetSelectedCount(3)
	m.SetError("boom")

	view := m.View()
	if strings.Contains(view, "\n") {
		t.Fatalf("View() rendered multiple lines:\n%s", view)
	}
	if !strings.Contains(view, "3 selected") {
		t.Errorf("View() dropped the selection block: %q", view)
	}
	if !strings.Contains(view, "boom") {
		t.Errorf("View() dropped the error block: %q", view)
	}
}

func TestInfoRendersAndClearsOnStatusUpdate(t *testing.T) {
	m := NewModel()
	m.SetSize(120, 1)
	m.SetInfo("presigned URL copied to clipboard")

	if view := m.View(); !strings.Contains(view, "copied to clipboard") {
		t.Fatalf("View() dropped the info block: %q", view)
	}

	m, _ = m.Update(types.StatusUpdateMsg{Profile: "dev"})
	if m.Info() != "" {
		t.Fatalf("Info() = %q after StatusUpdateMsg, want cleared", m.Info())
	}
	if strings.Contains(m.View(), "copied to clipboard") {
		t.Fatal("View() still shows the info block after StatusUpdateMsg")
	}
}
