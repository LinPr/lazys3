// Package help renders a full-keymap overlay for the lazys3 TUI. It lists
// every keybinding the TUI reacts to, grouped by category (Navigation,
// File Ops, Selection, Search, Panels, Quit), so users have a single
// in-app cheat sheet they can summon with `?`.
//
// The overlay is a passive component: it owns a `visible` flag and a
// `groups` slice, and renders a bordered, centered box. The TUI's Update
// toggles visibility on `?` and overlays the rendered box on top of the
// main layout via lipgloss.Place.
//
// The bindings list is built once from the TUI's actual key branches (see
// DefaultBindings) so the help text stays in sync with handler.go without
// a separate keymap source of truth. Adding a keybinding to the TUI only
// requires appending an entry to DefaultBindings.
package help

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
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
// is done via Toggle / Show / Hide on a pointer receiver.
type Model struct {
	visible bool
	groups  []Group
	width   int
	height  int
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

// Show / Hide / Toggle control overlay visibility.
func (m *Model) Show()   { m.visible = true }
func (m *Model) Hide()   { m.visible = false }
func (m *Model) Toggle() { m.visible = !m.visible }

// IsVisible reports whether the overlay is currently shown.
func (m Model) IsVisible() bool { return m.visible }

// SetGroups replaces the binding groups. Used by callers that want to
// customise the keymap (e.g. to add profile-list-only keys when the active
// list changes). The default groups cover the global keybindings; callers
// can append state-specific groups.
func (m *Model) SetGroups(groups []Group) { m.groups = groups }

// DefaultBindings returns the canonical lazys3 keymap, grouped by
// category. This is the single source of truth the help overlay renders;
// handler.go's key branches should match these entries.
//
// Categories:
//   - Navigation: forward/back/preview toggle
//   - File Ops:   download/upload/delete/rename/copy/make-bucket/rb/sync
//   - Selection:  toggle/select-all/clear
//   - Search:     filter / clear-filter
//   - Panels:     transfer-panel toggle / help toggle
//   - Quit:       quit / force-quit
func DefaultBindings() []Group {
	return []Group{
		{
			Name: "Navigation",
			Bindings: []Binding{
				{Key: "enter / →", Desc: "open selected (profile → buckets → objects)"},
				{Key: "backspace / ←", Desc: "go back one level"},
				{Key: "p", Desc: "toggle preview panel"},
				{Key: "↑ / k, ↓ / j", Desc: "move cursor (bubbles list defaults)"},
			},
		},
		{
			Name: "File Ops",
			Bindings: []Binding{
				{Key: "d", Desc: "download selected object(s) (multi-select → one transfer per object)"},
				{Key: "u", Desc: "upload local file to current prefix"},
				{Key: "D", Desc: "delete selected object(s) / empty bucket"},
				{Key: "r", Desc: "rename selected object (copy + delete)"},
				{Key: "c", Desc: "copy selected object to s3://bucket/key"},
				{Key: "B", Desc: "make bucket (in bucket list)"},
				{Key: "s", Desc: "sync directory (local ⇄ s3, s3 ⇄ s3)"},
				{Key: "y", Desc: "generate presigned share URL"},
			},
		},
		{
			Name: "Selection",
			Bindings: []Binding{
				{Key: "space", Desc: "toggle selection on current object"},
				{Key: "a", Desc: "invert selection (select all ↔ none)"},
			},
		},
		{
			Name: "Search & Sort",
			Bindings: []Binding{
				{Key: "/", Desc: "start filter on the object list (keys go to the filter while typing)"},
				{Key: "enter", Desc: "apply filter"},
				{Key: "esc", Desc: "clear filter / cancel filtering"},
				{Key: "o", Desc: "cycle sort field (name → size → time)"},
				{Key: "O", Desc: "reverse sort direction"},
			},
		},
		{
			Name: "Panels & Transfers",
			Bindings: []Binding{
				{Key: "t", Desc: "toggle transfer panel visibility"},
				{Key: "T", Desc: "transfer history (persistent, across sessions)"},
				{Key: "x", Desc: "cancel the most recent running transfer"},
				{Key: "?", Desc: "toggle this help overlay"},
			},
		},
		{
			Name: "Quit",
			Bindings: []Binding{
				{Key: "q", Desc: "quit lazys3"},
				{Key: "ctrl+c", Desc: "force quit"},
			},
		},
	}
}

// View renders the overlay. When hidden it returns "". When visible it
// renders a bordered, centered box listing all bindings grouped by
// category.
func (m Model) View() string {
	if !m.visible {
		return ""
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#e39f00ff")).
		Background(lipgloss.Color("#444745ff")).
		Padding(0, 1)
	groupStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#3b82f6"))
	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#dddddd"))
	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#aaaaaa"))

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("lazys3 — keybindings"))
	sb.WriteString("\n")

	for _, g := range m.groups {
		sb.WriteString("\n")
		sb.WriteString(groupStyle.Render(g.Name))
		sb.WriteString("\n")
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
			line := keyStyle.Render(padRight(b.Key, keyW)) +
				descStyle.Render(b.Desc)
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#3b82f6")).
		Padding(1, 2).
		Render(strings.TrimRight(sb.String(), "\n"))

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
