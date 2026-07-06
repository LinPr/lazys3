// Package statusbar renders a single-line bar at the bottom of the lazys3
// TUI that surfaces the current navigation context (profile, s3 URI,
// selected count) and the most recent error.
//
// The bar is driven by two messages delivered by the TUI's Update loop:
//
//   - types.StatusUpdateMsg — refreshes the profile/bucket/prefix and the
//     selection count. The TUI emits this after dispatching to the active
//     list so the bar always reflects the current state.
//   - types.ErrMsg — sets lastError so failures from file-op Cmds surface
//     visibly. The error is dismissable (DismissError).
//
// The bar is rendered as one line when the terminal is wide enough, and
// gracefully truncates the s3 URI when narrow. It is always visible.
package statusbar

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LinPr/lazys3/internal/tui/types"
)

// Model is the status bar state.
type Model struct {
	profile        string
	bucket         string
	prefix         string
	selectedCount  int
	lastError      string
	width          int
	height         int
	dismissedError string // snapshot of the dismissed error so we don't reshow
}

// NewModel returns a fresh, empty status bar.
func NewModel() Model {
	return Model{}
}

// Init is a no-op; the bar has no async work of its own.
func (m Model) Init() tea.Cmd { return nil }

// SetSize sets the bar's allocated dimensions. The bar always renders as a
// single line, so height is informational (used by the layout to reserve
// rows).
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// SetError displays an error message on the bar. Empty messages clear the
// error. Reissuing the same error string that the user just dismissed is
// ignored so the bar doesn't flicker the same error back.
func (m *Model) SetError(msg string) {
	if msg == "" {
		m.lastError = ""
		m.dismissedError = ""
		return
	}
	if msg == m.dismissedError {
		return
	}
	m.lastError = msg
}

// DismissError clears the current error and remembers it so a reissued
// SetError with the same string does not resurface it.
func (m *Model) DismissError() {
	if m.lastError != "" {
		m.dismissedError = m.lastError
	}
	m.lastError = ""
}

// SetProfile / SetBucket / SetPrefix / SetSelectedCount are imperative
// setters used by the TUI to keep the bar in sync with the active state
// without round-tripping through a message.
func (m *Model) SetProfile(p string)    { m.profile = p }
func (m *Model) SetBucket(b string)     { m.bucket = b }
func (m *Model) SetPrefix(p string)     { m.prefix = p }
func (m *Model) SetSelectedCount(n int) { m.selectedCount = n }

// Profile / Bucket / Prefix / SelectedCount / LastError are read accessors
// used by the TUI and by tests.
func (m Model) Profile() string    { return m.profile }
func (m Model) Bucket() string     { return m.bucket }
func (m Model) Prefix() string     { return m.prefix }
func (m Model) SelectedCount() int { return m.selectedCount }
func (m Model) LastError() string  { return m.lastError }

// S3URI returns the s3://bucket/prefix string the bar displays. When no
// bucket is selected it returns "" so the bar can render a placeholder.
func (m Model) S3URI() string {
	if m.bucket == "" {
		return ""
	}
	if m.prefix == "" {
		return fmt.Sprintf("s3://%s", m.bucket)
	}
	return fmt.Sprintf("s3://%s/%s", m.bucket, m.prefix)
}

// Update handles types.StatusUpdateMsg and types.ErrMsg. All other
// messages are ignored; the bar is passive.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch tmsg := msg.(type) {
	case types.StatusUpdateMsg:
		m.profile = tmsg.Profile
		m.bucket = tmsg.Bucket
		m.prefix = tmsg.Prefix
		m.selectedCount = tmsg.SelectedCount
	case types.ErrMsg:
		m.SetError(tmsg.Err.Error())
	}
	return m, nil
}

// View renders the bar as a single styled line. The layout is:
//
//	[profile]  s3://bucket/prefix   [N selected]   [last error]
//
// When width is tight the s3 URI is truncated from the middle; the error
// is dropped before the URI is dropped. The bar always occupies exactly
// one terminal line.
func (m Model) View() string {
	if m.width <= 0 {
		return ""
	}

	profileStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#e39f00ff")).
		Background(lipgloss.Color("#444745ff")).
		Padding(0, 1)
	pathStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#dddddd"))
	selStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#3b82f6"))
	errStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#ffffff")).
		Background(lipgloss.Color("#cc0000")).
		Padding(0, 1)

	profileBlock := profileStyle.Render(profileLabel(m.profile))
	uri := m.S3URI()
	if uri == "" {
		uri = "—"
	}

	selBlock := ""
	if m.selectedCount > 0 {
		selBlock = selStyle.Render(fmt.Sprintf("%d selected", m.selectedCount))
	}
	errBlock := ""
	if m.lastError != "" {
		errBlock = errStyle.Render("err: " + truncateMiddle(m.lastError, 30))
	}

	sep := "  "
	used := lipgloss.Width(profileBlock) + lipgloss.Width(sep)
	used += lipgloss.Width(selBlock)
	if selBlock != "" {
		used += lipgloss.Width(sep)
	}
	used += lipgloss.Width(errBlock)
	if errBlock != "" {
		used += lipgloss.Width(sep)
	}
	uriW := m.width - used
	if uriW < 8 {
		uriW = 8
	}
	uriBlock := pathStyle.Render(truncateMiddle(uri, uriW))

	parts := []string{profileBlock, uriBlock}
	if selBlock != "" {
		parts = append(parts, selBlock)
	}
	if errBlock != "" {
		parts = append(parts, errBlock)
	}

	bar := lipgloss.NewStyle().
		Background(lipgloss.Color("#222222")).
		Width(m.width).
		MaxHeight(1)
	return bar.Render(strings.Join(parts, sep))
}

// profileLabel returns the profile name or a placeholder when none is set.
func profileLabel(p string) string {
	if p == "" {
		return "no profile"
	}
	return p
}

// truncateMiddle shortens s to fit within width cells, keeping the head and
// tail and replacing the middle with "…" when truncation is needed. When
// width is large enough the original string is returned unchanged.
func truncateMiddle(s string, width int) string {
	if width <= 0 {
		return ""
	}
	w := lipgloss.Width(s)
	if w <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	keep := width - 1
	head := keep / 2
	tail := keep - head
	// TruncateLeft keeps a wide grapheme straddling the cut, which can
	// overshoot the tail budget; advance the cut until the tail fits.
	cut := w - tail
	right := ansi.TruncateLeft(s, cut, "")
	for lipgloss.Width(right) > tail && cut < w {
		cut++
		right = ansi.TruncateLeft(s, cut, "")
	}
	return ansi.Truncate(s, head, "") + "…" + right
}
