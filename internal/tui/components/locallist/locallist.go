package locallist

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/LinPr/lazys3/internal/tui/components/filter"
	"github.com/LinPr/lazys3/internal/tui/components/style"
	"github.com/LinPr/lazys3/internal/tui/keybinding"
	"github.com/LinPr/lazys3/internal/tui/types"
	"github.com/charmbracelet/bubbles/v2/key"
	"github.com/charmbracelet/bubbles/v2/list"
	tea "github.com/charmbracelet/bubbletea/v2"
)

// memoCap bounds the per-directory cursor memo; oldest entries are
// evicted first.
const memoCap = 128

// Model wraps a bubbles list.Model with multi-select, sort and
// cursor-memo state for a local directory listing. The selected map is
// keyed by the Entry's Name() (base name, trailing slash for
// directories) so the marker survives re-sorting and filtering; names
// are unique within one directory and the selection clears on every
// directory change.
//
// Unlike objectlist, the cursor memo is driven internally (Enter/Up call
// remember/restore themselves) because the parent has no per-dir
// navigation logic to hook.
type Model struct {
	list     list.Model
	selected map[string]bool

	// Master listing for the committed directory, kept in display
	// (sorted) order.
	entries []Entry
	dir     string

	sortBy   sortField
	sortDesc bool

	// Outer dimensions (including the border frame) from SetSize.
	width   int
	height  int
	focused bool
	loading bool

	// Per-directory cursor memo.
	posMemo        map[string]int
	memoKeys       []string
	pendingRestore string
}

// NewModel constructs an empty local list with the custom select-aware
// delegate, mirroring objectlist.NewModel: case-insensitive substring
// filtering, quit keys disabled, paging narrowed to pgup/pgdown so
// global hotkeys are not consumed by the list.
func NewModel() Model {
	items := make([]list.Item, 0)
	selected := make(map[string]bool)
	delegate := newSelectDelegate(&selected)
	l := list.New(items, delegate, 0, 0)
	l.Filter = filter.Substring
	l.DisableQuitKeybindings()
	l.KeyMap.PrevPage = key.NewBinding(
		key.WithKeys("pgup"), key.WithHelp("pgup", "prev page"))
	l.KeyMap.NextPage = key.NewBinding(
		key.WithKeys("pgdown"), key.WithHelp("pgdn", "next page"))
	m := Model{
		list:     l,
		selected: selected,
		posMemo:  make(map[string]int),
	}
	m.refreshTitle()
	return m
}

func (m Model) Init() tea.Cmd {
	return nil
}

// Update handles LoadedMsg (committing the navigation only on success)
// and the sort keys (o/O) when the filter input is not focused;
// everything else (including `/`, filter typing, esc, enter-to-apply) is
// handled by the wrapped bubbles list.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case LoadedMsg:
		m.SetLoading(false)
		if msg.Err != nil {
			// Keep the previous directory and listing; surface the failure
			// on the status bar so the pane never gets stuck in an
			// unreadable directory.
			m.pendingRestore = ""
			return m, func() tea.Msg {
				return types.ErrMsg{Err: fmt.Errorf("read dir %s: %w", msg.Dir, msg.Err)}
			}
		}
		m.dir = msg.Dir
		m.setEntries(msg.Entries)
		return m, nil

	case tea.KeyPressMsg:
		if !m.Filtering() {
			switch keybinding.KeyString(msg.String()) {
			case keybinding.ObjectSortCycle:
				return m, m.CycleSortField()
			case keybinding.ObjectSortReverse:
				return m, m.ToggleSortDirection()
			}
		}
	}

	newList, cmd := m.list.Update(msg)
	m.list = newList
	return m, cmd
}

// View renders the bordered list at exactly the outer size from SetSize.
// The border color reflects the pane focus.
func (m Model) View() string {
	border := style.FocusedBorderColor
	if !m.focused {
		border = style.UnfocusedBorderColor
	}
	return style.LocalListStyle.
		BorderForeground(border).
		Width(m.width).
		Height(m.height).
		Render(m.list.View())
}

// SetSize sets the component's outer dimensions. The wrapped list gets the
// inner size (outer minus the border frame) so rows never overflow the box.
// The title is re-fit to the new width (it is truncated to fit one line).
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	fh, fv := style.LocalListStyle.GetFrameSize()
	m.list.SetSize(max(width-fh, 0), max(height-fv, 0))
	// bubbles sizes its help to the full list width but renders it inside
	// HelpStyle's 2-col left padding, so a footer of exactly that width
	// wraps onto a second line at narrow pane widths. Shrink the help
	// budget by the style's frame so the footer truncates ("…") instead.
	m.list.Help.Width = max(m.list.Width()-m.list.Styles.HelpStyle.GetHorizontalFrameSize(), 0)
	m.refreshTitle()
}

// GetSize returns the outer dimensions from SetSize.
func (m Model) GetSize() (width, height int) {
	return m.width, m.height
}

// SetFocused marks the pane as owning list-navigation keys; View picks
// the border color from it.
func (m *Model) SetFocused(v bool) { m.focused = v }

// Focused reports whether the pane is focused.
func (m Model) Focused() bool { return m.focused }

// Dir returns the committed current directory ("" before the first
// successful load).
func (m Model) Dir() string { return m.dir }

// EnsureLoaded returns the first-ever directory fetch (the user's home,
// falling back to "/"), or nil once a directory has been loaded. Dir()
// persists across dual-pane toggles, so re-entering dual mode keeps the
// last visited directory.
func (m *Model) EnsureLoaded() tea.Cmd {
	if m.dir != "" {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "/"
	}
	m.SetLoading(true)
	return FetchDirCmd(home)
}

// Enter navigates into the highlighted directory: the current cursor is
// memoised, a restore is armed for the child, and its fetch is returned.
// The navigation commits only when the LoadedMsg arrives with Err==nil.
// No-op (nil) for files or an empty listing.
func (m *Model) Enter() tea.Cmd {
	e := m.GetSelectedEntry()
	if e == nil || !e.isDir {
		return nil
	}
	m.rememberPosition(m.dir)
	m.pendingRestore = e.path
	m.SetLoading(true)
	return FetchDirCmd(e.path)
}

// Up navigates to the parent directory, memoising like Enter. No-op at
// the filesystem root or before the first load.
func (m *Model) Up() tea.Cmd {
	if m.dir == "" {
		return nil
	}
	parent := filepath.Dir(m.dir)
	if parent == m.dir {
		return nil
	}
	m.rememberPosition(m.dir)
	m.pendingRestore = parent
	m.SetLoading(true)
	return FetchDirCmd(parent)
}

// Refresh re-fetches the current directory, keeping the cursor position
// via the pending restore.
func (m *Model) Refresh() tea.Cmd {
	if m.dir == "" {
		return nil
	}
	m.rememberPosition(m.dir)
	m.pendingRestore = m.dir
	m.SetLoading(true)
	return FetchDirCmd(m.dir)
}

// GetSelectedEntry returns the currently highlighted entry, or nil.
func (m *Model) GetSelectedEntry() *Entry {
	if item, ok := m.list.SelectedItem().(Entry); ok {
		return &item
	}
	return nil
}

// SetLoading marks a fetch as in flight, adjusting the empty-state text
// like objectlist does.
func (m *Model) SetLoading(v bool) {
	m.loading = v
	if v {
		m.list.SetStatusBarItemName("item (loading…)", "items (loading…)")
	} else {
		m.list.SetStatusBarItemName("item", "items")
	}
}

// Loading reports whether a fetch is in flight.
func (m Model) Loading() bool { return m.loading }

// Filtering reports whether the list's filter input is focused (the user
// is typing a filter). The parent Update MUST skip its global hotkey
// handling while this is true.
func (m Model) Filtering() bool {
	return m.list.SettingFilter()
}

// refreshTitle recomposes the visible title from the current directory,
// the active sort mode and the selection count. The result is middle-
// truncated to the list's inner width so a deep path never word-wraps the
// title bar onto a second line (which would push every row down and
// misalign the panes in dual-pane mode).
func (m *Model) refreshTitle() {
	base := m.dir
	if base == "" {
		base = "local"
	}
	title := fmt.Sprintf("%s  [%s]", base, m.SortStatus())
	if n := len(m.selected); n > 0 {
		title = fmt.Sprintf("%s  %d selected", title, n)
	}
	m.list.Title = style.FitListTitle(title, m.list.Width())
}

// setEntries replaces the listing after a successful load: the selection
// is cleared, any applied filter is reset, the current sort mode is
// applied, and the cursor goes back to the top — unless a restore is
// pending for the loaded directory.
func (m *Model) setEntries(items []Entry) {
	m.entries = append([]Entry(nil), items...)
	m.ClearSelection()
	m.list.ResetFilter()
	sortEntries(m.entries, m.sortBy, m.sortDesc)
	m.list.SetItems(m.listItems())
	m.list.ResetSelected()
	if m.pendingRestore != "" && len(m.entries) > 0 {
		if idx, ok := m.posMemo[m.pendingRestore]; ok {
			if idx >= len(m.entries) {
				idx = len(m.entries) - 1
			}
			if idx > 0 {
				m.list.Select(idx)
			}
		}
	}
	m.pendingRestore = ""
	m.refreshTitle()
}

func (m Model) listItems() []list.Item {
	listItems := make([]list.Item, 0, len(m.entries))
	for _, e := range m.entries {
		listItems = append(listItems, e)
	}
	return listItems
}

// CycleSortField advances name -> size -> time -> name and re-sorts. The
// returned cmd (a filter re-run when a filter is applied) must be
// dispatched.
func (m *Model) CycleSortField() tea.Cmd {
	m.sortBy = (m.sortBy + 1) % sortFieldCount
	return m.applySort()
}

// ToggleSortDirection flips ascending/descending and re-sorts.
func (m *Model) ToggleSortDirection() tea.Cmd {
	m.sortDesc = !m.sortDesc
	return m.applySort()
}

// SortStatus renders the active sort mode, e.g. "name ↑" or "size ↓".
func (m Model) SortStatus() string {
	arrow := "↑"
	if m.sortDesc {
		arrow = "↓"
	}
	return m.sortBy.String() + " " + arrow
}

// applySort re-sorts the listing with the current mode, keeping the
// cursor on the same entry when possible.
func (m *Model) applySort() tea.Cmd {
	var current string
	if e := m.GetSelectedEntry(); e != nil {
		current = e.name
	}
	sortEntries(m.entries, m.sortBy, m.sortDesc)
	cmd := m.list.SetItems(m.listItems())
	if current != "" && !m.list.IsFiltered() {
		for i, e := range m.entries {
			if e.name == current {
				m.list.Select(i)
				break
			}
		}
	}
	m.refreshTitle()
	return cmd
}

// rememberPosition saves the current cursor index for the given
// directory. The memo is capped; oldest entries are evicted.
func (m *Model) rememberPosition(dir string) {
	if dir == "" {
		return
	}
	if _, exists := m.posMemo[dir]; !exists {
		m.memoKeys = append(m.memoKeys, dir)
		if len(m.memoKeys) > memoCap {
			delete(m.posMemo, m.memoKeys[0])
			m.memoKeys = m.memoKeys[1:]
		}
	}
	m.posMemo[dir] = m.list.GlobalIndex()
}

// ToggleSelected toggles the selection state of the currently highlighted
// entry. No-op when the listing is empty.
func (m *Model) ToggleSelected() {
	e := m.GetSelectedEntry()
	if e == nil {
		return
	}
	if m.selected == nil {
		m.selected = make(map[string]bool)
	}
	m.selected[e.Name()] = !m.selected[e.Name()]
	if !m.selected[e.Name()] {
		delete(m.selected, e.Name())
	}
	m.refreshTitle()
}

// InvertSelection flips the selection state of every visible item, giving
// the `a` keybinding a quick "select all" gesture.
func (m *Model) InvertSelection() {
	if m.selected == nil {
		m.selected = make(map[string]bool)
	}
	for _, it := range m.list.VisibleItems() {
		e, ok := it.(Entry)
		if !ok {
			continue
		}
		name := e.Name()
		if m.selected[name] {
			delete(m.selected, name)
		} else {
			m.selected[name] = true
		}
	}
	m.refreshTitle()
}

// VisibleEntries returns the entries currently visible in the list —
// i.e. after any filter narrowing — in display order.
func (m Model) VisibleEntries() []Entry {
	items := m.list.VisibleItems()
	out := make([]Entry, 0, len(items))
	for _, it := range items {
		if e, ok := it.(Entry); ok {
			out = append(out, e)
		}
	}
	return out
}

// SelectedEntries returns the selected entries in display (sorted) order.
func (m Model) SelectedEntries() []Entry {
	if len(m.selected) == 0 {
		return nil
	}
	out := make([]Entry, 0, len(m.selected))
	for _, e := range m.entries {
		if m.selected[e.name] {
			out = append(out, e)
		}
	}
	return out
}

// SelectedPaths returns the absolute paths of the selected entries in
// display order.
func (m Model) SelectedPaths() []string {
	entries := m.SelectedEntries()
	if len(entries) == 0 {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.path)
	}
	return out
}

// SelectedCount returns the number of selected entries.
func (m Model) SelectedCount() int {
	return len(m.selected)
}

// ClearSelection empties the selection set.
func (m *Model) ClearSelection() {
	for k := range m.selected {
		delete(m.selected, k)
	}
	m.refreshTitle()
}
