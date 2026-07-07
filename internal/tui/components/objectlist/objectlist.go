// Package objectlist renders the S3 object/prefix browser and dispatches
// file operations (download/upload/delete/copy/rename/sync).
package objectlist

import (
	"fmt"
	"log"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/LinPr/lazys3/internal/tui/components/style"
	"github.com/LinPr/lazys3/internal/tui/keybinding"
	"github.com/LinPr/lazys3/internal/tui/types"
)

// memoCap bounds the per-prefix cursor memo; oldest entries are evicted
// first.
const memoCap = 128

// Model wraps a bubbles list.Model with multi-select, sort and
// cursor-memo state. The selected map is keyed by the Object's Title()
// (the S3 key, including any trailing slash for directories) so the
// marker survives re-sorting and filtering.
//
// The map is a pointer-typed field on a value-receiver Model: the TUI's
// Update loop takes a Model by value, mutates the map through the
// pointer, and the custom delegate reads the same map live when
// rendering. This avoids the bubble-tea footgun of value-receiver
// methods losing mutations across Update calls.
type Model struct {
	Title      string
	objectlist list.Model
	selected   map[string]bool

	// Master listing for the current bucket/prefix, kept in display
	// (sorted) order. Re-sorting reorders this slice and pushes it back
	// into the list.
	objects []Object

	baseTitle string
	sortBy    sortField
	sortDesc  bool

	// Outer dimensions (including the border frame) from SetSize.
	width   int
	height  int
	focused bool
	loading bool

	// Per-prefix cursor memo (see RememberPosition/RestorePosition).
	posMemo        map[string]int
	memoKeys       []string
	pendingRestore string
}

// NewModel constructs an empty object list with the custom select-aware
// delegate. The delegate is wired to the Model's selection map so it
// reflects the current selection on every render.
//
// The wrapped list keeps its built-in `/` filtering but with a
// case-insensitive substring matcher, and its quit/page keybindings are
// narrowed so global hotkeys (q, d, u, left, right, ...) are not consumed
// by the list as pagination/quit.
func NewModel() Model {
	items := make([]list.Item, 0)
	selected := make(map[string]bool)
	delegate := newSelectDelegate(&selected)
	objectlist := list.New(items, delegate, 0, 0)
	objectlist.Filter = substringFilter
	objectlist.DisableQuitKeybindings()
	objectlist.KeyMap.PrevPage = key.NewBinding(
		key.WithKeys("pgup"), key.WithHelp("pgup", "prev page"))
	objectlist.KeyMap.NextPage = key.NewBinding(
		key.WithKeys("pgdown"), key.WithHelp("pgdn", "next page"))
	m := Model{
		objectlist: objectlist,
		selected:   selected,
		focused:    true,
		posMemo:    make(map[string]int),
	}
	m.refreshTitle()
	return m
}

func (m Model) Init() tea.Cmd {
	return nil
}

// Update forwards messages to the underlying list. Sort keys (o/O) are
// intercepted here when the filter input is not focused; everything else
// (including `/`, filter typing, esc, enter-to-apply) is handled by the
// bubbles list itself.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case FetchObjectListResultMsg:
		log.Println("objectlist got FetchObjectListResultMsg, err=", msg.Err, "count=", len(msg.Objects))
		m.SetLoading(false)
		objects, err := msg.Objects, msg.Err
		if err != nil {
			// Clear the listing (the fetched prefix could not be read) and
			// surface the failure on the status bar so an errored fetch is
			// distinguishable from a genuinely empty prefix.
			m.SetObjects([]Object{})
			log.Println("Error fetching object list:", err)
			return m, func() tea.Msg {
				return types.ErrMsg{Err: fmt.Errorf("list objects: %w", err)}
			}
		}
		m.SetObjects(objects)

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

	newObjectListModel, cmd := m.objectlist.Update(msg)
	m.objectlist = newObjectListModel
	return m, cmd
}

// View renders the bordered list at exactly the outer size from SetSize.
// lipgloss v2's Width/Height include the border frame, so the style gets
// the outer dimensions while the wrapped list was sized to the inner ones.
func (m Model) View() string {
	border := style.FocusedBorderColor
	if !m.focused {
		border = style.UnfocusedBorderColor
	}
	return style.ObjectListStyle.
		BorderForeground(border).
		Width(m.width).
		Height(m.height).
		Render(m.objectlist.View())
}

// SetFocused marks the pane as owning list-navigation keys (dual-pane
// mode); View picks the border color from it. Constructors default to
// focused so single-pane rendering is unchanged.
func (m *Model) SetFocused(v bool) { m.focused = v }

// Focused reports whether the pane is focused.
func (m Model) Focused() bool { return m.focused }

// SetLoading marks a fetch as in flight. While loading, the empty state
// renders "No items (loading…)" instead of "No items" so an in-flight
// listing is distinguishable from a genuinely empty prefix.
func (m *Model) SetLoading(v bool) {
	m.loading = v
	if v {
		m.objectlist.SetStatusBarItemName("item (loading…)", "items (loading…)")
	} else {
		m.objectlist.SetStatusBarItemName("item", "items")
	}
}

// Loading reports whether a fetch is in flight.
func (m Model) Loading() bool { return m.loading }

// SetTitle sets the base list title (the s3:// URI). The rendered title
// also carries the active sort mode and the selection count.
func (m *Model) SetTitle(title string) {
	m.baseTitle = title
	m.refreshTitle()
}

// refreshTitle recomposes the visible title from the base title, the
// active sort mode and the selection count. The result is middle-truncated
// to the list's inner width so the title bar never word-wraps onto a
// second line (which would push every row down and misalign the panes in
// dual-pane mode).
func (m *Model) refreshTitle() {
	base := m.baseTitle
	if base == "" {
		base = "objects"
	}
	title := fmt.Sprintf("%s  [%s]", base, m.SortStatus())
	if n := len(m.selected); n > 0 {
		title = fmt.Sprintf("%s  %d selected", title, n)
	}
	m.objectlist.Title = style.FitListTitle(title, m.objectlist.Width())
}

// SetObjects replaces the listing. This means the content changed (new
// prefix or refresh), so the selection is cleared, any applied filter is
// reset, the current sort mode is applied, and the cursor goes back to
// the top — unless a RestorePosition is pending, in which case the
// memoised cursor index is restored.
func (m *Model) SetObjects(items []Object) {
	m.objects = append([]Object(nil), items...)
	m.ClearSelection()
	m.objectlist.ResetFilter()
	sortObjects(m.objects, m.sortBy, m.sortDesc)
	m.objectlist.SetItems(m.listItems())
	// bubbles' SetItems recomputes the page size against the pagination
	// line's PREVIOUS height (updatePagination subtracts it before
	// SetTotalPages runs), so a listing that crosses the one-page boundary
	// renders one row too many — the pane draws a line taller than its box
	// until the next SetSize. Re-apply the size to converge (through the
	// component SetSize, which also restores the narrowed help budget).
	m.SetSize(m.width, m.height)
	m.objectlist.ResetSelected()
	if m.pendingRestore != "" && len(m.objects) > 0 {
		if idx, ok := m.posMemo[m.pendingRestore]; ok {
			if idx >= len(m.objects) {
				idx = len(m.objects) - 1
			}
			if idx > 0 {
				m.objectlist.Select(idx)
			}
		}
		m.pendingRestore = ""
	}
	m.refreshTitle()
}

func (m Model) listItems() []list.Item {
	listItems := make([]list.Item, 0, len(m.objects))
	for _, o := range m.objects {
		listItems = append(listItems, o)
	}
	return listItems
}

func (m *Model) GetSelectedObject() *Object {
	if item, ok := m.objectlist.SelectedItem().(Object); ok {
		return &item
	}
	return nil
}

// SetSize sets the component's outer dimensions. The wrapped list gets the
// inner size (outer minus the border frame) so rows never overflow the box.
// The title is re-fit to the new width (it is truncated to fit one line).
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	fh, fv := style.ObjectListStyle.GetFrameSize()
	m.objectlist.SetSize(max(width-fh, 0), max(height-fv, 0))
	// bubbles sizes its help to the full list width but renders it inside
	// HelpStyle's 2-col left padding, so a footer of exactly that width
	// wraps onto a second line at narrow pane widths. Shrink the help
	// budget by the style's frame so the footer truncates ("…") instead.
	m.objectlist.Help.SetWidth(max(m.objectlist.Width()-m.objectlist.Styles.HelpStyle.GetHorizontalFrameSize(), 0))
	m.refreshTitle()
}

// GetSize returns the outer dimensions from SetSize.
func (m Model) GetSize() (width, height int) {
	return m.width, m.height
}

// Filtering reports whether the list's filter input is focused (the user
// is typing a filter). The parent Update MUST skip its global hotkey
// handling while this is true, otherwise typing "data" would trigger the
// download modal on 'd', invert the selection on 'a', and so on.
func (m Model) Filtering() bool {
	return m.objectlist.SettingFilter()
}

// FilterApplied reports whether a confirmed filter is narrowing the list.
func (m Model) FilterApplied() bool {
	return m.objectlist.IsFiltered()
}

// SetSortMode sets the initial sort field ("name" | "size" | "time") and
// direction from the loaded config. Unknown/empty fields keep the default
// (name). Called at construction, before any listing arrives.
func (m *Model) SetSortMode(field string, desc bool) {
	switch field {
	case "size":
		m.sortBy = sortBySize
	case "time":
		m.sortBy = sortByTime
	default:
		m.sortBy = sortByName
	}
	m.sortDesc = desc
	m.refreshTitle()
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

// applySort re-sorts the master listing with the current mode, keeping
// the cursor on the same object when possible.
func (m *Model) applySort() tea.Cmd {
	var current string
	if o := m.GetSelectedObject(); o != nil {
		current = o.name
	}
	sortObjects(m.objects, m.sortBy, m.sortDesc)
	cmd := m.objectlist.SetItems(m.listItems())
	if current != "" && !m.objectlist.IsFiltered() {
		for i, o := range m.objects {
			if o.name == current {
				m.objectlist.Select(i)
				break
			}
		}
	}
	m.refreshTitle()
	return cmd
}

// memoKey builds the per-listing key for the cursor memo.
func memoKey(bucket, prefix string) string {
	return bucket + "\x00" + prefix
}

// RememberPosition saves the current cursor index for the given
// bucket/prefix. The parent calls this right before navigating away
// (into a child prefix or back to the parent). The memo is capped;
// oldest entries are evicted.
func (m *Model) RememberPosition(bucket, prefix string) {
	k := memoKey(bucket, prefix)
	if _, exists := m.posMemo[k]; !exists {
		m.memoKeys = append(m.memoKeys, k)
		if len(m.memoKeys) > memoCap {
			delete(m.posMemo, m.memoKeys[0])
			m.memoKeys = m.memoKeys[1:]
		}
	}
	m.posMemo[k] = m.objectlist.GlobalIndex()
}

// RestorePosition arms a cursor restore for the given bucket/prefix. The
// parent calls this when navigating to a listing that may have been
// visited before; the memoised index is applied by the next non-empty
// SetObjects (i.e. when the fetched listing arrives).
func (m *Model) RestorePosition(bucket, prefix string) {
	m.pendingRestore = memoKey(bucket, prefix)
}

// ToggleSelected toggles the selection state of the currently highlighted
// object. No-op when no item is selected.
func (m *Model) ToggleSelected() {
	obj := m.GetSelectedObject()
	if obj == nil {
		return
	}
	if m.selected == nil {
		m.selected = make(map[string]bool)
	}
	m.selected[obj.Name()] = !m.selected[obj.Name()]
	if !m.selected[obj.Name()] {
		delete(m.selected, obj.Name())
	}
	m.refreshTitle()
}

// VisibleObjects returns the objects currently visible in the list —
// i.e. after any filter narrowing — in display order.
func (m Model) VisibleObjects() []Object {
	items := m.objectlist.VisibleItems()
	out := make([]Object, 0, len(items))
	for _, it := range items {
		if o, ok := it.(Object); ok {
			out = append(out, o)
		}
	}
	return out
}

// SelectedKeys returns the names of all selected objects in display
// (sorted) order. ops.go's CurrentSelectedKeys helper picks this up so
// delete/copy/rename operate on the whole selection when non-empty.
func (m Model) SelectedKeys() []string {
	if len(m.selected) == 0 {
		return nil
	}
	out := make([]string, 0, len(m.selected))
	for _, o := range m.objects {
		if m.selected[o.name] {
			out = append(out, o.name)
		}
	}
	return out
}

// SelectedObjects returns the selected objects in display order.
func (m Model) SelectedObjects() []Object {
	if len(m.selected) == 0 {
		return nil
	}
	out := make([]Object, 0, len(m.selected))
	for _, o := range m.objects {
		if m.selected[o.name] {
			out = append(out, o)
		}
	}
	return out
}

// SelectedCount returns the number of selected objects.
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

// InvertSelection flips the selection state of every visible item. Used
// by the `a` keybinding: pressing `a` once selects all visible items
// (or inverts if some are already selected), giving the user a quick
// "select all" gesture.
func (m *Model) InvertSelection() {
	if m.selected == nil {
		m.selected = make(map[string]bool)
	}
	for _, it := range m.objectlist.VisibleItems() {
		obj, ok := it.(Object)
		if !ok {
			continue
		}
		name := obj.Name()
		if m.selected[name] {
			delete(m.selected, name)
		} else {
			m.selected[name] = true
		}
	}
	m.refreshTitle()
}

// SelectAll marks every visible item as selected. This is a hard select
// (not a toggle) so the user can re-select all after a partial clear.
func (m *Model) SelectAll() {
	if m.selected == nil {
		m.selected = make(map[string]bool)
	}
	for _, it := range m.objectlist.VisibleItems() {
		obj, ok := it.(Object)
		if !ok {
			continue
		}
		m.selected[obj.Name()] = true
	}
	m.refreshTitle()
}
