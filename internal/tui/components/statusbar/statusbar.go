// Package statusbar renders a single-line bar at the bottom of the lazys3
// TUI that surfaces the current context (profile, focused pane, selection
// count, transfer tallies) and the most recent info note or error.
//
// The bar is driven by two messages delivered by the TUI's Update loop:
//
//   - types.StatusUpdateMsg — refreshes the profile, the focused-pane
//     indicator and the selection count. The TUI emits this after
//     dispatching to the active list so the bar always reflects the
//     current state.
//   - types.ErrMsg — sets lastError so failures from file-op Cmds surface
//     visibly. The error is cleared by the user's next key press (tui.go
//     calls SetError("") at the top of its KeyMsg handling).
//
// The transfer segment is NOT message-driven: tui.go pushes a live
// types.TransferStats snapshot via SetTransferStats on the render path
// (composeView), so the progress bar follows the transfer panel's 200ms
// tick without routing byte counters through the deduped StatusUpdateMsg.
//
// Layout (one line, always):
//
//	[profile] [pane] [N selected] [transfers] [info] [error] … [? help]
//
// The transfer segment shows, while any upload/download is active, an
// aggregate progress bar with per-direction batch counts ("[███░░░░░]
// ↑1/2 ↓0/1"); when nothing runs it shows the lifetime completed totals
// ("↑N ↓M"). Failed/canceled rows keep the ✗ tally in both states. The
// pane indicator appears only in dual-pane mode and the selection count
// only when >0. The transient info and the error share the remaining
// middle width (ansi-aware middle truncation); "? help" is right-aligned.
package statusbar

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LinPr/lazys3/internal/tui/components/style"
	"github.com/LinPr/lazys3/internal/tui/types"
)

// Model is the status bar state.
type Model struct {
	profile       string
	bucket        string
	prefix        string
	selectedCount int
	pane          string
	stats         types.TransferStats
	lastError     string
	info          string
	width         int
	height        int
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
// error (tui.go does this on every key press, so a failure never outstays
// the user's next action).
func (m *Model) SetError(msg string) {
	m.lastError = msg
}

// SetInfo displays a transient informational note (e.g. "presigned URL
// copied to clipboard" when the result modal could not be shown). It is
// cleared by the next StatusUpdateMsg, i.e. the next navigation or
// selection change.
func (m *Model) SetInfo(msg string) { m.info = msg }

// Info returns the current informational note.
func (m Model) Info() string { return m.info }

// SetProfile / SetBucket / SetPrefix / SetSelectedCount / SetPane /
// SetTransferStats are imperative setters used by the TUI (and tests) to
// keep the bar in sync without round-tripping through a message.
// SetTransferStats is called on the render path with a live
// transferpanel.Stats() snapshot.
func (m *Model) SetProfile(p string)                    { m.profile = p }
func (m *Model) SetBucket(b string)                     { m.bucket = b }
func (m *Model) SetPrefix(p string)                     { m.prefix = p }
func (m *Model) SetSelectedCount(n int)                 { m.selectedCount = n }
func (m *Model) SetPane(p string)                       { m.pane = p }
func (m *Model) SetTransferStats(s types.TransferStats) { m.stats = s }

// Profile / Bucket / Prefix / SelectedCount / Pane / LastError are read
// accessors used by the TUI and by tests.
func (m Model) Profile() string    { return m.profile }
func (m Model) Bucket() string     { return m.bucket }
func (m Model) Prefix() string     { return m.prefix }
func (m Model) SelectedCount() int { return m.selectedCount }
func (m Model) Pane() string       { return m.pane }
func (m Model) LastError() string  { return m.lastError }

// Update handles types.StatusUpdateMsg and types.ErrMsg. All other
// messages are ignored; the bar is passive.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch tmsg := msg.(type) {
	case types.StatusUpdateMsg:
		m.profile = tmsg.Profile
		m.bucket = tmsg.Bucket
		m.prefix = tmsg.Prefix
		m.selectedCount = tmsg.SelectedCount
		m.pane = tmsg.Pane
		// The info note is dismissed only on navigation-ish changes
		// (ClearInfo); transfer-tally-only refreshes leave it readable.
		if tmsg.ClearInfo {
			m.info = ""
		}
	case types.ErrMsg:
		m.SetError(tmsg.Err.Error())
	}
	return m, nil
}

// transferSegment renders the transfer block from the live stats snapshot.
//
// While any upload/download is active: "[███░░░░░] ↑1/2 ↓0/1" — an 8-cell
// aggregate progress bar over the batch's rows with known byte totals
// (indeterminate bounce when none is known), plus done/total batch counts
// per direction (a direction with an empty batch is omitted). When
// nothing is active: the lifetime completed totals "↑N ↓M" (never reset;
// zero segments omitted). Failed/canceled rows append the ✗ tally in both
// states. ASCII fallbacks ("[###-----] ^1/2 v0/1", "xF") when nerd_font
// is off. Other op kinds (delete/mb/sync/...) only surface via ✗.
func (m Model) transferSegment() string {
	st := m.stats
	up, down, fail := "↑", "↓", "✗"
	if !style.NerdFontEnabled() {
		up, down, fail = "^", "v", "x"
	}
	runStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#3b82f6"))
	okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#22c55e"))
	failStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444"))
	var parts []string
	if st.UpActive+st.DownActive > 0 {
		parts = append(parts, runStyle.Render("["+progressBar(st)+"]"))
		if st.UpTotal > 0 {
			parts = append(parts, runStyle.Render(fmt.Sprintf("%s%d/%d", up, st.UpDone, st.UpTotal)))
		}
		if st.DownTotal > 0 {
			parts = append(parts, runStyle.Render(fmt.Sprintf("%s%d/%d", down, st.DownDone, st.DownTotal)))
		}
	} else {
		if st.LifetimeUp > 0 {
			parts = append(parts, okStyle.Render(fmt.Sprintf("%s%d", up, st.LifetimeUp)))
		}
		if st.LifetimeDown > 0 {
			parts = append(parts, okStyle.Render(fmt.Sprintf("%s%d", down, st.LifetimeDown)))
		}
	}
	if st.Failed > 0 {
		parts = append(parts, failStyle.Render(fmt.Sprintf("%s%d", fail, st.Failed)))
	}
	return strings.Join(parts, " ")
}

// progressBar renders the segment's 8-cell bar: a determinate fill from
// the aggregate byte counters when any active total is known, otherwise a
// 3-cell block bouncing with the panel's tick frame (never a bogus
// percentage).
func progressBar(st types.TransferStats) string {
	const width = 8
	full, empty := "█", "░"
	if !style.NerdFontEnabled() {
		full, empty = "#", "-"
	}
	if st.BytesTotal <= 0 {
		const block = 3
		span := width - block
		pos := st.Frame % (2 * span)
		if pos > span {
			pos = 2*span - pos
		}
		return strings.Repeat(empty, pos) + strings.Repeat(full, block) +
			strings.Repeat(empty, width-block-pos)
	}
	pct := float64(st.BytesDone) / float64(st.BytesTotal)
	if pct < 0 {
		pct = 0
	} else if pct > 1 {
		pct = 1
	}
	filled := min(int(pct*float64(width)), width)
	return strings.Repeat(full, filled) + strings.Repeat(empty, width-filled)
}

// View renders the bar as a single styled line:
//
//	[profile] [pane] [N selected] [transfer segment] [info] [error] … [? help]
//
// The info/error middle section is ansi-truncated (from the middle) to
// whatever width the fixed segments leave over; "? help" is right-aligned.
// The bar always occupies exactly one terminal line.
func (m Model) View() string {
	if m.width <= 0 {
		return ""
	}

	// The profile chip and the error chip pick up theme overrides via the
	// shared style vars (title_fg/title_bg, status_error_fg).
	profileStyle := style.TitleStyle
	paneStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#a78bfa"))
	selStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#3b82f6"))
	infoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#22c55e"))
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#777777"))
	errStyle := style.StatusErrorStyle

	const sep = "  "
	segs := []string{profileStyle.Render(profileLabel(m.profile))}
	if m.pane != "" {
		segs = append(segs, paneStyle.Render(m.pane))
	}
	if m.selectedCount > 0 {
		segs = append(segs, selStyle.Render(fmt.Sprintf("%d selected", m.selectedCount)))
	}
	if ts := m.transferSegment(); ts != "" {
		segs = append(segs, ts)
	}
	left := strings.Join(segs, sep)
	helpBlock := helpStyle.Render("? help")

	// The middle (info + error) gets whatever the fixed segments leave
	// over. The error is budgeted first so it is never squeezed out by a
	// long info note; both are middle-truncated (CJK-safe).
	avail := m.width - lipgloss.Width(left) - lipgloss.Width(helpBlock) - 2*len(sep)
	var middle []string
	errW := 0
	if m.lastError != "" && avail > 0 {
		errText := truncateMiddle("err: "+m.lastError, avail)
		errW = lipgloss.Width(errText) + len(sep)
		middle = append(middle, errStyle.Render(errText))
	}
	if m.info != "" && avail-errW > 0 {
		infoBlock := infoStyle.Render(truncateMiddle(m.info, avail-errW))
		middle = append([]string{infoBlock}, middle...)
	}

	line := left
	if len(middle) > 0 {
		line += sep + strings.Join(middle, sep)
	}
	if pad := m.width - lipgloss.Width(line) - lipgloss.Width(helpBlock); pad > 0 {
		line += strings.Repeat(" ", pad) + helpBlock
	}
	// Hard-clip as a last resort so a too-narrow bar truncates instead of
	// wrapping (MaxHeight would otherwise clip the wrapped tail invisibly).
	line = ansi.Truncate(line, m.width, "")

	bar := lipgloss.NewStyle().
		Background(lipgloss.Color("#222222")).
		Width(m.width).
		MaxHeight(1)
	return bar.Render(line)
}

// profileLabel returns the profile name or a placeholder when none is set.
func profileLabel(p string) string {
	if p == "" {
		return "no profile"
	}
	return p
}

// truncateMiddle shortens s to fit within width cells, keeping the head and
// tail and replacing the middle with "…" when truncation is needed. It is
// shared with the list titles as style.TruncateMiddle.
func truncateMiddle(s string, width int) string {
	return style.TruncateMiddle(s, width)
}
