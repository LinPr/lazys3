// Package help renders a full-keymap overlay for the lazys3 TUI. It lists
// every keybinding the TUI reacts to, grouped by pane focus (Global,
// Remote pane, Local pane, Selection & filter, Overlays), so users have a
// single in-app cheat sheet they can summon with `?`.
//
// The overlay is a mostly-passive component: it owns a `visible` flag, a
// `groups` slice and a scroll offset, and renders a bordered, centered
// box. The TUI's Update toggles visibility on `?` and overlays the
// rendered box on top of the main layout via lipgloss.Place. When the
// content is taller than the terminal, the box shows a window of it with
// a position footer; the TUI routes j/k/pgup/pgdown (etc.) to HandleKey.
//
// The bindings list is built once from the TUI's actual key branches (see
// DefaultBindings) so the help text stays in sync with handler.go without
// a separate keymap source of truth. Adding a keybinding to the TUI only
// requires appending an entry to DefaultBindings.
package help

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Binding is a single keybinding entry rendered as "key  description".
type Binding struct {
	Key  string
	Desc string
}

// Group is a labelled cluster of bindings (e.g. "Navigation", "File Ops").
type Group struct {
	Name     string
	Bindings []Binding
}

// Model is the help overlay state. It is a value type; toggling visibility
// is done via Toggle / Show / Hide on a pointer receiver. offset is the
// first visible content line; it resets to 0 on every open.
type Model struct {
	visible bool
	groups  []Group
	width   int
	height  int
	offset  int
}

// NewModel returns a help overlay preloaded with the default lazys3
// keybindings. The overlay starts hidden.
func NewModel() Model {
	return Model{
		visible: false,
		groups:  DefaultBindings(),
	}
}

// Init is a no-op; the overlay has no async work.
func (m Model) Init() tea.Cmd { return nil }

// Update is a no-op for the overlay; visibility is toggled by the TUI.
// Returning the model unchanged keeps the overlay compatible with the
// tea.Model contract.
func (m Model) Update(_ tea.Msg) (Model, tea.Cmd) { return m, nil }

// SetSize sets the overlay's target dimensions. The overlay uses these to
// center itself via lipgloss.Place.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// Show / Hide / Toggle control overlay visibility. Opening always starts
// at the top of the content.
func (m *Model) Show() {
	m.visible = true
	m.offset = 0
}

func (m *Model) Hide() { m.visible = false }

func (m *Model) Toggle() {
	m.visible = !m.visible
	if m.visible {
		m.offset = 0
	}
}

// HandleKey scrolls the overlay (j/k, arrows, pgup/pgdown, g/G). Keys are
// no-ops while the content fits the box; unrecognised keys are swallowed
// by design (the TUI forwards nothing else while the overlay is visible).
func (m *Model) HandleKey(key string) {
	total := m.lineCount()
	avail := m.contentHeight()
	if avail <= 0 || total <= avail {
		m.offset = 0
		return
	}
	page := avail - 1 // one row is reserved for the scroll footer
	if page < 1 {
		page = 1
	}
	switch key {
	case "j", "down":
		m.offset++
	case "k", "up":
		m.offset--
	case "pgdown":
		m.offset += page
	case "pgup":
		m.offset -= page
	case "g", "home":
		m.offset = 0
	case "G", "end":
		m.offset = total // clamped below
	}
	if max := total - page; m.offset > max {
		m.offset = max
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// IsVisible reports whether the overlay is currently shown.
func (m Model) IsVisible() bool { return m.visible }

// SetGroups replaces the binding groups. Used by callers that want to
// customise the keymap (e.g. to add profile-list-only keys when the active
// list changes). The default groups cover the global keybindings; callers
// can append state-specific groups.
func (m *Model) SetGroups(groups []Group) { m.groups = groups }

// DefaultBindings returns the canonical lazys3 keymap, grouped by pane
// focus. This is the single source of truth the help overlay renders;
// handler.go's key branches should match these entries.
//
// Groups:
//   - Global:             keys that work regardless of pane focus
//   - Remote pane (S3):   file ops with the remote (bucket/object) list focused
//   - Local pane:         the same op keys with the dual-pane local list focused
//   - Selection & filter: marking, filtering and sorting the focused list
//   - Overlays:           shared scroll and close keys of the ?/t/v/p/m overlays
//
// Every key is documented in exactly one group, except the file-op keys
// whose behaviour depends on pane focus (d/u/D/r/c/B/s/y/g): those appear
// once per pane group with the focus-specific description — and 'g', which
// is additionally a scroll key inside the overlays (see
// TestHelpDocumentsKeysExactlyOnce).
func DefaultBindings() []Group {
	return []Group{
		{
			Name: "Global",
			Bindings: []Binding{
				{Key: "q", Desc: "quit lazys3"},
				{Key: "ctrl+c", Desc: "force quit"},
				{Key: "?", Desc: "toggle this help overlay"},
				{Key: "t", Desc: "toggle the live transfers overlay (newest first; enter: per-file sync detail, ←/→ scroll wide tables)"},
				{Key: "x", Desc: "cancel the most recent running transfer (transfers overlay: the highlighted one)"},
				{Key: "l", Desc: "toggle dual-pane layout (local ⇄ remote, needs ≥80 cols)"},
				{Key: "tab", Desc: "switch focus between remote and local panes (dual-pane)"},
				{Key: "p", Desc: "preview file content (floating overlay, first 256 KiB)"},
				{Key: "m", Desc: "object/file metadata (floating overlay; buckets and profiles too)"},
				{Key: "enter / →", Desc: "open selected (profile → buckets → objects)"},
				{Key: "backspace / ←", Desc: "go back one level"},
				{Key: "↑ / k, ↓ / j", Desc: "move the list cursor (also scrolls the ?/t/v/p/m overlays)"},
			},
		},
		{
			Name: "Remote pane (S3)",
			Bindings: []Binding{
				{Key: "d", Desc: "download selected object(s); dual-pane: folders too, into the local directory"},
				{Key: "u", Desc: "upload a local file to the current prefix (single-pane only; dual-pane hints to press tab — uploads run with local focus)"},
				{Key: "D", Desc: "delete selected object(s); folders recursively (permanent) / empty bucket (bucket list)"},
				{Key: "r", Desc: "rename selected object (copy + delete)"},
				{Key: "c", Desc: "copy selected object to s3://bucket/key (dual-pane: to the local pane)"},
				{Key: "B", Desc: "make bucket (bucket list; the object list only hints)"},
				{Key: "s", Desc: "sync directory (local ⇄ s3, s3 ⇄ s3; dual-pane prefills both sides)"},
				{Key: "y", Desc: "yank the highlighted bucket/object s3:// URI to the clipboard"},
				{Key: "Y", Desc: "generate presigned share URL (object files only)"},
				{Key: "v", Desc: "object versions (object list) / toggle bucket versioning Enabled ⇄ Suspended (bucket list)"},
				{Key: "g", Desc: "go to path: s3://bucket/prefix/ switches bucket, /path from the bucket root, rel/path from here"},
			},
		},
		{
			Name: "Local pane",
			Bindings: []Binding{
				{Key: "u", Desc: "upload selection to the remote bucket/prefix (folders sync recursively)"},
				{Key: "c", Desc: "copy selection to the remote pane (same as u)"},
				{Key: "d", Desc: "hints to press tab (downloads run with remote focus)"},
				{Key: "D", Desc: "delete selection (permanent, no trash; directories recursive)"},
				{Key: "r", Desc: "rename the highlighted entry (same directory)"},
				{Key: "B", Desc: "create a directory"},
				{Key: "s", Desc: "sync directory: local pane → remote pane (prefilled, editable)"},
				{Key: "y", Desc: "yank the highlighted entry's absolute path to the clipboard"},
				{Key: "g", Desc: "go to directory (absolute, ~ or relative to the current one)"},
			},
		},
		{
			Name: "Selection & filter",
			Bindings: []Binding{
				{Key: "space", Desc: "toggle selection on the highlighted item (✔ mark)"},
				{Key: "a", Desc: "invert selection (select all ↔ none)"},
				{Key: "/", Desc: "filter the focused list (enter applies, esc clears)"},
				{Key: "o", Desc: "cycle sort field (name → size → time)"},
				{Key: "O", Desc: "reverse sort direction"},
			},
		},
		{
			Name: "Overlays",
			Bindings: []Binding{
				{Key: "pgup / pgdn", Desc: "scroll one page"},
				{Key: "g / G", Desc: "jump to top / bottom (help, transfers, preview, metadata)"},
				{Key: "esc", Desc: "close the overlay (lists: clear filter; modal: cancel)"},
			},
		},
	}
}

var boxStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color("#3b82f6")).
	Padding(1, 2)

var dimStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#aaaaaa"))

// lineCount is how many content lines the overlay renders in total: the
// title plus, per group, a blank separator, the group name and its
// bindings. It mirrors renderLines without building the strings.
func (m Model) lineCount() int {
	n := 1
	for _, g := range m.groups {
		n += 2 + len(g.Bindings)
	}
	return n
}

// contentHeight is how many content lines fit inside the box (terminal
// height minus the border+padding frame). <= 0 means the size is unknown
// (render everything, matching the pre-size fallback).
func (m Model) contentHeight() int {
	if m.height <= 0 {
		return 0
	}
	return m.height - boxStyle.GetVerticalFrameSize()
}

// renderLines builds the styled content lines (title, group headers,
// binding rows), each truncated to the width the box can spend on it so
// the overlay never overflows a narrow terminal horizontally.
func (m Model) renderLines() []string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#e39f00")).
		Background(lipgloss.Color("#444745")).
		Padding(0, 1)
	groupStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#3b82f6"))
	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#dddddd"))
	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#aaaaaa"))

	lines := []string{titleStyle.Render("lazys3 — keybindings")}
	for _, g := range m.groups {
		lines = append(lines, "", groupStyle.Render(g.Name))
		// Compute a uniform key column width within the group so the
		// descriptions align.
		keyW := 0
		for _, b := range g.Bindings {
			if w := lipgloss.Width(b.Key); w > keyW {
				keyW = w
			}
		}
		keyW += 2 // gutter
		for _, b := range g.Bindings {
			lines = append(lines,
				keyStyle.Render(padRight(b.Key, keyW))+descStyle.Render(b.Desc))
		}
	}
	if maxW := m.width - boxStyle.GetHorizontalFrameSize(); m.width > 0 && maxW > 0 {
		for i, l := range lines {
			lines[i] = ansi.Truncate(l, maxW, "…")
		}
	}
	return lines
}

// View renders the overlay. When hidden it returns "". When visible it
// renders a bordered, centered box listing the bindings grouped by
// category. Content taller than the box scrolls: a window of lines is
// shown with a position footer ("12-24 of 53 ↑↓ · j/k scroll · ?/esc
// close"); content that fits renders in full, exactly as before. A box
// too short for a footer row drops the footer so the overlay never
// exceeds the terminal height.
func (m Model) View() string {
	if !m.visible {
		return ""
	}

	lines := m.renderLines()
	// The box width is fixed from the longest line of the FULL content
	// (already clamped to the terminal width by renderLines), so the
	// border never changes size while the user scrolls a window of it.
	boxW := 0
	for _, l := range lines {
		if w := lipgloss.Width(l); w > boxW {
			boxW = w
		}
	}
	if avail := m.contentHeight(); avail > 0 && len(lines) > avail {
		// Reserve room for the widest footer the pager can produce so the
		// arrows appearing/disappearing never widen the box mid-scroll.
		total := len(lines)
		maxFooter := fmt.Sprintf("%d-%d of %d ↑ ↓ · j/k scroll · ?/esc close", total, total, total)
		if maxW := m.width - boxStyle.GetHorizontalFrameSize(); m.width > 0 && maxW > 0 {
			maxFooter = ansi.Truncate(maxFooter, maxW, "…")
		}
		if w := lipgloss.Width(maxFooter); w > boxW {
			boxW = w
		}
		page := avail - 1 // the footer takes the last row
		showFooter := true
		if page < 1 {
			// Too short for a footer row: spend the whole box on content
			// so the overlay never exceeds the terminal height.
			page = avail
			showFooter = false
		}
		offset := m.offset
		if max := len(lines) - page; offset > max {
			offset = max
		}
		if offset < 0 {
			offset = 0
		}
		end := offset + page
		if end > len(lines) {
			end = len(lines)
		}
		window := append([]string{}, lines[offset:end]...)
		if showFooter {
			footer := fmt.Sprintf("%d-%d of %d", offset+1, end, len(lines))
			if offset > 0 {
				footer += " ↑"
			}
			if end < len(lines) {
				footer += " ↓"
			}
			footer += " · j/k scroll · ?/esc close"
			if maxW := m.width - boxStyle.GetHorizontalFrameSize(); m.width > 0 && maxW > 0 {
				footer = ansi.Truncate(footer, maxW, "…")
			}
			window = append(window, dimStyle.Render(footer))
		}
		lines = window
	}

	// Pad every visible line to the fixed content width so the rendered
	// box tracks the full content's widest line, not the visible window's.
	for i, l := range lines {
		lines[i] = padRight(l, boxW)
	}

	box := boxStyle.Render(strings.Join(lines, "\n"))

	// Center on the screen over a dimmed full-canvas background so the
	// overlay visually replaces the underlying layout (matching the
	// modal's overlay behaviour in modal.go).
	w, h := m.width, m.height
	if w <= 0 || h <= 0 {
		return box
	}
	dimBg := lipgloss.NewStyle().Background(lipgloss.Color("#1a1a1a"))
	rendered := lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceStyle(dimBg),
	)
	return rendered
}

// padRight pads s with spaces on the right so its visible width is at
// least w. It uses lipgloss.Width so non-ASCII glyphs (arrows, ⇄) align.
func padRight(s string, w int) string {
	sw := lipgloss.Width(s)
	if sw >= w {
		return s
	}
	return s + strings.Repeat(" ", w-sw)
}
