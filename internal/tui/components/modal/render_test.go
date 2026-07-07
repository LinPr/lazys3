package modal

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// TestConfirmModalWrapsLongURL verifies a long unbroken string (a
// 200-char presigned URL) hard-wraps inside the confirm modal instead of
// pushing the box past 80 columns.
func TestConfirmModalWrapsLongURL(t *testing.T) {
	url := "https://test-bucket.oss-cn-hangzhou.aliyuncs.com/some/deep/prefix/file.txt?" +
		strings.Repeat("X-Amz-Signature=abcdef0123456789&", 4) + "X-Amz-Expires=3600"
	if len(url) < 200 {
		url += strings.Repeat("a", 200-len(url))
	}

	m := NewModel()
	m.SetSize(120, 40)
	m.ShowConfirm("Presigned URL", "s3://test-bucket/file.txt\n\n"+url, nil)

	for i, line := range strings.Split(m.View(), "\n") {
		if w := lipgloss.Width(line); w > 120 {
			t.Fatalf("line %d is %d cols wide, want <= 120", i, w)
		}
	}

	// At exactly 80 cols the box caps at 80; no rendered line may exceed it.
	m.SetSize(80, 24)
	for i, line := range strings.Split(m.View(), "\n") {
		if w := lipgloss.Width(line); w > 80 {
			t.Fatalf("line %d is %d cols wide, want <= 80", i, w)
		}
	}

	// The wrapped body must still contain the whole URL: strip the box
	// border and padding (lipgloss pads with non-breaking spaces) from
	// each line, then re-join the fragments.
	var joined strings.Builder
	for _, line := range strings.Split(stripANSI(m.View()), "\n") {
		joined.WriteString(strings.Trim(line, "│╭╮╰╯─  "))
	}
	if !strings.Contains(joined.String(), url) {
		t.Fatal("wrapped modal body no longer contains the full URL")
	}
}

// TestInputModalNoWrappedRow verifies the textinput is sized to the floating
// box interior: an input modal must render exactly 5 rows (2 border, header,
// input, hint) with no wrapped junk row, at any terminal width.
func TestInputModalNoWrappedRow(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}, {60, 20}} {
		m := NewModel()
		m.SetSize(size[0], size[1])
		m.Show("Download to", "/tmp/some/default/path.txt", nil)

		// Long typed values must scroll inside the textinput, not wrap.
		for _, val := range []string{"", strings.Repeat("/very/long/path", 20)} {
			m.input.SetValue(val)
			view := m.View()
			if got := lipgloss.Height(view); got != 5 {
				t.Fatalf("at %dx%d (value len %d) input modal renders %d rows, want 5:\n%s",
					size[0], size[1], len(val), got, view)
			}
			box := m.boxWidth()
			for i, line := range strings.Split(view, "\n") {
				if w := lipgloss.Width(line); w > box {
					t.Fatalf("at %dx%d line %d is %d cols wide, want <= %d",
						size[0], size[1], i, w, box)
				}
			}
		}
	}
}

// stripANSI removes escape sequences so content checks see plain text.
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case inEsc:
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
		case r == '\x1b':
			inEsc = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
