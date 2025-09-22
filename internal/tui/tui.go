package tui

import (
	"fmt"
	"log"

	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"

	"github.com/LinPr/lazys3/internal/tui/components/bucketlist"
	"github.com/LinPr/lazys3/internal/tui/components/objectlist"
	"github.com/LinPr/lazys3/internal/tui/components/preview"
	"github.com/LinPr/lazys3/internal/tui/components/profilelist"
	"github.com/LinPr/lazys3/internal/tui/components/style"
	"github.com/LinPr/lazys3/internal/tui/state"
)

type size struct {
	width  int
	height int
}

type Model struct {
	state           state.State
	profileList     profilelist.Model
	bucketList      bucketlist.Model
	objectlist      objectlist.Model
	previewPannel   preview.PreviewModel
	selectedProfile string
	selectedBucket  string
	size
}

func NewLazyS3Model() Model {
	return Model{
		state:         state.ActiveProfileList,
		bucketList:    bucketlist.NewModel(),
		profileList:   profilelist.NewModel(),
		objectlist:    objectlist.NewModel(),
		previewPannel: preview.NewPreviewModel(),
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	// m.state = state.ActiveProfileList
	m.state = state.Unknow
	return m.profileList.Init()
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:

		windowWidth := msg.Width / 2 * 2 // make sure the mumber can be divided by 3
		windowHeight := msg.Height / 2 * 2

		m.SetSize(windowWidth, windowHeight)

		h, v := style.ProfileListStyle.GetFrameSize()
		log.Println("profilelist set size:", h, v)
		m.profileList.SetSize(windowWidth, windowHeight-v)
		h, v = style.BucketListStyle.GetFrameSize()
		m.bucketList.SetSize(windowWidth, windowHeight-v)

		h, v = style.ObjectListStyle.GetFrameSize()
		m.objectlist.SetSize(windowWidth, windowHeight-v)

		h, v = style.AppStyle.GetFrameSize()

		m.previewPannel.SetSize(windowWidth/2, windowHeight-v)

		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "p":
			switch m.state {
			case state.ActiveProfileList:
				m.previewPannel.Toggle()
				_, v := style.ProfileListStyle.GetFrameSize()
				if m.previewPannel.IsVisible() {
					m.profileList.SetSize(m.width/2, m.height-v)
				} else {
					m.profileList.SetSize(m.width, m.height-v)
				}
			case state.ActiveBucketList:
				m.previewPannel.Toggle()
				_, v := style.BucketListStyle.GetFrameSize()
				if m.previewPannel.IsVisible() {
					m.bucketList.SetSize(m.width/2, m.height-v)
				} else {
					m.bucketList.SetSize(m.width, m.height-v)
				}
			}
		case "left":
			log.Println("left pressed")
			switch m.state {
			case state.ActiveBucketList:
				// m.handleBucketSelect()
				m.state = state.ActiveProfileList
			case state.ActiveObjectList:
				// m.handleObjectSelect()
				m.state = state.ActiveBucketList
			case state.ActiveProfileList:
				// m.handleProfileSelect()
			}
			// if m.state != state.ActiveProfileList {
			// 	m.state = state.ActiveProfileList
			// 	if m.previewPannel.IsVisible() {
			// 		// colose the preview pannel when switch list
			// 		m.previewPannel.Toggle()
			// 	}
			// }

		case "enter", "right":
			switch m.state {
			case state.ActiveProfileList:
				m.handleProfileSelect()
				if m.profileList.GetSelectedProfile() != nil {
					m.state = state.ActiveBucketList
				}
				if m.previewPannel.IsVisible() {
					// colose the preview pannel when switch list
					m.previewPannel.Toggle()
				}

			case state.ActiveBucketList:
				m.handleBucketSelect()

				if m.bucketList.GetSelectedBucket() != nil {
					m.state = state.ActiveObjectList
				}
				if m.previewPannel.IsVisible() {
					// colose the preview pannel when switch list
					m.previewPannel.Toggle()
				}
				// case state.ActiveObjectList:
				// 	m.handleBucketSelect()
				// 	if m.bucketList.GetSelectedBucket() != nil {
				// 		m.state = state.ActiveObjectList
				// 	}
			}

		default:
			log.Println("key string:", msg.String())
		}

	}

	// dispatch message to the active component

	var cmd tea.Cmd
	var updated tea.Model
	switch m.state {
	case state.ActiveProfileList:

		updated, cmd = m.profileList.Update(msg)
		if pl, ok := updated.(profilelist.Model); ok {
			m.profileList = pl
		}
		log.Println("1111111111111111111111111")
		m.previewPannel.SetContent(m.profileList.GetSelectedProfile())
		log.Println("after profilelist update")
	case state.ActiveBucketList:

		updated, cmd = m.bucketList.Update(msg)
		if bl, ok := updated.(bucketlist.Model); ok {
			m.bucketList = bl
		}
		m.previewPannel.SetContent(m.bucketList.GetSelectedBucket())

	case state.ActiveObjectList:

		updated, cmd = m.objectlist.Update(msg)
		if ol, ok := updated.(objectlist.Model); ok {
			m.objectlist = ol
		}
		m.previewPannel.SetContent(m.objectlist.GetSelectedObject())
	}

	return m, cmd
}

func (m Model) View() string {
	log.Printf("m.state: %v\n", m.state)
	switch m.state {
	case state.ActiveProfileList:
		log.Println("2222222222222222222")
		profileView := m.profileList.View()
		previewView := m.previewPannel.View()
		return lipgloss.JoinHorizontal(lipgloss.Top, profileView, previewView)

	case state.ActiveBucketList:
		bucketView := m.bucketList.View()
		previewView := m.previewPannel.View()

		return lipgloss.JoinHorizontal(lipgloss.Top, bucketView, previewView)
	case state.ActiveObjectList:
		objectView := m.objectlist.View()
		previewView := m.previewPannel.View()
		return lipgloss.JoinHorizontal(lipgloss.Top, objectView, previewView)
	default:
		return style.ErrorStyle.Render("Unknown component")
	}
}

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *Model) handleProfileSelect() (tea.Model, tea.Cmd) {
	// 获取选中的 profile
	if selectedItem := m.profileList.GetSelectedProfile(); selectedItem != nil {
		selectedProfile := selectedItem.Title()
		m.selectedProfile = selectedProfile

		// 获取对应的 buckets
		opt := bucketlist.Option{
			Profile: selectedProfile,
		}
		buckets, err := bucketlist.FetchBucketList(opt)
		if err != nil {
			// 处理错误，例如显示错误消息
			log.Println("Error fetching bucket list:", err)
		}
		m.bucketList.SetBuckets(buckets)
		m.bucketList.SetTitle(fmt.Sprintf("S3 Buckets (%s)", selectedProfile))

	}
	return m, nil
}

func (m *Model) handleBucketSelect() (tea.Model, tea.Cmd) {
	// 处理 bucket 选择（这里可以添加具体的业务逻辑）
	if selectedBucket := m.bucketList.GetSelectedBucket(); selectedBucket != nil {
		log.Println("aaaaaaaaaaaaaaaaaaa")
		selectedBucket := selectedBucket.Title()
		m.selectedBucket = selectedBucket

		// 可以在这里处理选中的 bucket

		s3uri := fmt.Sprintf("s3://%s", selectedBucket)

		opt := objectlist.Option{
			S3Uri:   s3uri,
			Profile: m.selectedProfile,
		}
		objects, err := objectlist.FetchObjectList(opt)
		if err != nil {
			// 处理错误，例如显示错误消息
			log.Println("Error fetching object list:", err)
		}

		m.objectlist.SetObjects(objects)
		m.objectlist.SetTitle(fmt.Sprintf("S3 Objects (%s)", selectedBucket))
	}

	return m, nil

}

// func (m *Model) handleObjectSelect() (tea.Model, tea.Cmd) {
// 	return nil, nil
// }
