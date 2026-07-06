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

type Model struct {
	Title      string
	Option     *Option
	bucketlist list.Model

	// Outer dimensions (including the border frame) from SetSize.
	width   int
	height  int
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
	bucketlist.Title = BucketListTitle
	bucketlist.DisableQuitKeybindings()
	// Narrow paging to pgup/pgdown so the default bindings (right/l/d/f,
	// left/h/b/u) don't shadow the global navigation and file-op keys.
	bucketlist.KeyMap.PrevPage = key.NewBinding(
		key.WithKeys("pgup"), key.WithHelp("pgup", "prev page"))
	bucketlist.KeyMap.NextPage = key.NewBinding(
		key.WithKeys("pgdown"), key.WithHelp("pgdn", "next page"))
	return Model{
		bucketlist: bucketlist,
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
	return style.BucketListStyle.
		Width(m.width).
		Height(m.height).
		Render(m.bucketlist.View())
}

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
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	fh, fv := style.BucketListStyle.GetFrameSize()
	m.bucketlist.SetSize(max(width-fh, 0), max(height-fv, 0))
}

// GetSize returns the outer dimensions from SetSize.
func (m *Model) GetSize() (width, height int) {
	return m.width, m.height
}
