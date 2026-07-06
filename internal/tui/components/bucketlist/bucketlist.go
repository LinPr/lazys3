// Package bucketlist renders the S3 bucket picker and fetches bucket lists.
package bucketlist

import (
	"fmt"
	"log"

	"github.com/LinPr/lazys3/internal/tui/components/filter"
	"github.com/LinPr/lazys3/internal/tui/components/style"
	"github.com/LinPr/lazys3/internal/tui/types"
	"github.com/charmbracelet/bubbles/v2/key"
	"github.com/charmbracelet/bubbles/v2/list"
	tea "github.com/charmbracelet/bubbletea/v2"
)

const BucketListTitle = "S3 Buckets"

// listTitle composes the list title, prefixed with the Nerd Font bucket
// glyph when icons are enabled.
func listTitle() string {
	if g, _ := style.BucketIcon(); g != "" {
		return g + " " + BucketListTitle
	}
	return BucketListTitle
}

type Model struct {
	Title      string
	Option     *Option
	bucketlist list.Model

	// Outer dimensions (including the border frame) from SetSize.
	width   int
	height  int
	focused bool
	loading bool
}

func NewModel() Model {

	items := make([]list.Item, 0)

	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.Styles = NewCustomItemStyles(true)

	bucketlist := list.New(items, delegate, 0, 0)
	bucketlist.Styles = CustomStyle(true)
	bucketlist.Filter = filter.Substring
	bucketlist.Title = listTitle()
	bucketlist.DisableQuitKeybindings()
	// Narrow paging to pgup/pgdown so the default bindings (right/l/d/f,
	// left/h/b/u) don't shadow the global navigation and file-op keys.
	bucketlist.KeyMap.PrevPage = key.NewBinding(
		key.WithKeys("pgup"), key.WithHelp("pgup", "prev page"))
	bucketlist.KeyMap.NextPage = key.NewBinding(
		key.WithKeys("pgdown"), key.WithHelp("pgdn", "next page"))
	return Model{
		bucketlist: bucketlist,
		focused:    true,
	}
}

func (m *Model) Init() tea.Cmd {

	return nil
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case FetchBucketListResultMsg:
		m.SetLoading(false)
		buckets, err := msg.Buckets, msg.Err
		if err != nil {
			// Clear the listing and surface the failure on the status bar so
			// an errored fetch is distinguishable from an empty account.
			m.SetBuckets(nil)
			log.Println("Error fetching bucket list:", err)
			return m, func() tea.Msg {
				return types.ErrMsg{Err: fmt.Errorf("list buckets: %w", err)}
			}
		}
		m.SetBuckets(buckets)
	}

	var cmd tea.Cmd
	m.bucketlist, cmd = m.bucketlist.Update(msg)
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
	return style.BucketListStyle.
		BorderForeground(border).
		Width(m.width).
		Height(m.height).
		Render(m.bucketlist.View())
}

// SetFocused marks the pane as owning list-navigation keys (dual-pane
// mode); View picks the border color from it. Constructors default to
// focused so single-pane rendering is unchanged.
func (m *Model) SetFocused(v bool) { m.focused = v }

// Focused reports whether the pane is focused.
func (m Model) Focused() bool { return m.focused }

// SetLoading marks a fetch as in flight. While loading, the empty state
// renders "No items (loading…)" instead of "No items" so an in-flight
// listing is distinguishable from an account with no buckets.
func (m *Model) SetLoading(v bool) {
	m.loading = v
	if v {
		m.bucketlist.SetStatusBarItemName("item (loading…)", "items (loading…)")
	} else {
		m.bucketlist.SetStatusBarItemName("item", "items")
	}
}

// Loading reports whether a fetch is in flight.
func (m Model) Loading() bool { return m.loading }

func (m *Model) SetTitle(title string) {
	m.Title = title
}

func (m *Model) SetOption(opt *Option) {
	m.Option = opt
}

func (m *Model) SetBuckets(items []Bucket) {
	listItems := make([]list.Item, 0)
	for _, b := range items {
		listItems = append(listItems, b)
	}
	m.bucketlist.SetItems(listItems)
}

// Filtering reports whether the list's filter input is focused. The parent
// Update must skip global hotkey handling while this is true.
func (m Model) Filtering() bool {
	return m.bucketlist.SettingFilter()
}

func (m *Model) GetSelectedBucket() *Bucket {
	if item, ok := m.bucketlist.SelectedItem().(Bucket); ok {
		return &item
	}
	return nil
}

// SetSize sets the component's outer dimensions. The wrapped list gets the
// inner size (outer minus the border frame) so rows never overflow the box.
// The title is re-fit to the new width so it never wraps at narrow widths.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	fh, fv := style.BucketListStyle.GetFrameSize()
	m.bucketlist.SetSize(max(width-fh, 0), max(height-fv, 0))
	// bubbles sizes its help to the full list width but renders it inside
	// HelpStyle's 2-col left padding, so a footer of exactly that width
	// wraps onto a second line at narrow pane widths. Shrink the help
	// budget by the style's frame so the footer truncates ("…") instead.
	m.bucketlist.Help.Width = max(m.bucketlist.Width()-m.bucketlist.Styles.HelpStyle.GetHorizontalFrameSize(), 0)
	m.bucketlist.Title = style.FitListTitle(listTitle(), m.bucketlist.Width())
}

// GetSize returns the outer dimensions from SetSize.
func (m *Model) GetSize() (width, height int) {
	return m.width, m.height
}
