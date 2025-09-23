package tui

import (
	"fmt"
	"log"
	"strings"

	"github.com/LinPr/lazys3/internal/tui/components/bucketlist"
	"github.com/LinPr/lazys3/internal/tui/components/objectlist"
	"github.com/LinPr/lazys3/internal/tui/components/style"
	"github.com/LinPr/lazys3/internal/tui/state"
	tea "github.com/charmbracelet/bubbletea/v2"
)

func (m *Model) setSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *Model) initComponentsSize(msg tea.WindowSizeMsg) {
	windowWidth := msg.Width / 2 * 2 // make sure the mumber can be divided by 3
	windowHeight := msg.Height / 2 * 2
	m.setSize(windowWidth, windowHeight)

	h, v := style.ProfileListStyle.GetFrameSize()
	log.Println("profilelist set size:", h, v)
	m.profileList.SetSize(windowWidth, windowHeight-v)
	h, v = style.BucketListStyle.GetFrameSize()
	m.bucketList.SetSize(windowWidth, windowHeight-v)

	h, v = style.ObjectListStyle.GetFrameSize()
	m.objectlist.SetSize(windowWidth, windowHeight-v)

	h, v = style.AppStyle.GetFrameSize()

	m.previewPannel.SetSize(windowWidth/2, windowHeight-v)

}

func (m *Model) handleProfileSelect() tea.Cmd {
	var cmd []tea.Cmd
	// 获取险种的 buckets 信息
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
		log.Println("222222 buckets:", buckets)
		m.bucketList.SetBuckets(buckets)
		m.bucketList.SetTitle(fmt.Sprintf("S3 Buckets (%s)", selectedProfile))
	}

	return tea.Batch(cmd...)
}

func (m *Model) handleBucketSelect() tea.Cmd {
	var cmds []tea.Cmd

	// 处理 bucket 选择（这里可以添加具体的业务逻辑）
	if selectedBucket := m.bucketList.GetSelectedBucket(); selectedBucket != nil {
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
			return nil
		}

		if len(objects) == 0 {
			return nil
		}

		m.objectlist.SetObjects(objects)
		m.objectlist.SetTitle(s3uri)
	}

	return tea.Batch(cmds...)
}

func (m *Model) handleObjectSelect() tea.Cmd {
	var cmds []tea.Cmd

	// 处理 bucket 选择（这里可以添加具体的业务逻辑）
	if selectedObject := m.objectlist.GetSelectedObject(); selectedObject != nil {
		selectedObject := selectedObject.Title()
		// when the selected object is not a "directory", do nothing
		if !strings.HasSuffix(selectedObject, "/") {
			return nil
		}

		m.selectedObject = selectedObject

		// 可以在这里处理选中的 bucket
		s3uri := fmt.Sprintf("s3://%s/%s", m.selectedBucket, m.selectedObject)
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
		m.objectlist.SetTitle(s3uri)

	}

	return tea.Batch(cmds...)
}

func (m *Model) handleObjectUnSelect() tea.Cmd {
	var cmds []tea.Cmd

	// 处理 bucket 选择（这里可以添加具体的业务逻辑）
	if selectedObject := m.objectlist.GetSelectedObject(); selectedObject != nil {
		selectedObject := selectedObject.Title()

		log.Println("1111111: selectedObject:", selectedObject)
		// TODO: 需要截取queue的前缀部分，返回上级object list
		var s3uri string
		// 砍掉最后一个 / 后面的部分

		parts := strings.Split(strings.TrimSuffix(selectedObject, "/"), "/")

		if len(parts) <= 1 {
			m.selectedObject = ""
			return nil
		} else if len(parts) <= 2 {
			m.selectedObject = selectedObject
			log.Printf("m.selectedBucket: %v\n", m.selectedBucket)
			s3uri = fmt.Sprintf("s3://%s", m.selectedBucket)
			log.Println("333333: m.selectedObject:", m.selectedObject)
		} else {

			m.selectedObject = strings.Join(parts[:len(parts)-2], "/")
			log.Println("222222: m.selectedObject:", m.selectedObject)
			// m.selectedObject = strings.Join(parts[:len(parts)-1], "/")
			s3uri = fmt.Sprintf("s3://%s/%s", m.selectedBucket, m.selectedObject+"/")
		}

		log.Println("----xx--- handleObjectUnSelect s3uri:", s3uri)

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
		m.objectlist.SetTitle(s3uri)
		// refresh the new selected object
		// m.selectedObject = selectedObject
	}

	return tea.Batch(cmds...)
}

func (m *Model) handleForward() (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch m.state {
	case state.ActiveProfileList:
		m.handleProfileSelect()

		// switch to bucket list if a profile is selected
		if m.profileList.GetSelectedProfile() != nil {
			m.state = state.ActiveBucketList
		}

		// close the preview pannel when switch list
		if m.previewPannel.IsVisible() {
			m.previewPannel.Toggle()
		}

	case state.ActiveBucketList:
		m.handleBucketSelect()

		// switch to object list if a bucket is selected
		if m.bucketList.GetSelectedBucket() != nil {
			m.state = state.ActiveObjectList
		}

		if m.previewPannel.IsVisible() {
			// colose the preview pannel when switch list
			m.previewPannel.Toggle()
		}

	case state.ActiveObjectList:
		m.handleObjectSelect()

		// switch to object list if a bucket is selected
		if m.objectlist.GetSelectedObject() != nil {
			m.state = state.ActiveObjectList
		}

		if m.previewPannel.IsVisible() {
			// colose the preview pannel when switch list
			m.previewPannel.Toggle()
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) handleBackward() (tea.Model, tea.Cmd) {
	switch m.state {

	case state.ActiveObjectList:
		m.handleObjectUnSelect()
		if m.selectedObject == "" {
			m.state = state.ActiveBucketList
		}

	case state.ActiveBucketList:
		// m.handleBucketSelect()
		m.state = state.ActiveProfileList

	case state.ActiveProfileList:
		// m.handleProfileSelect()
	}
	return m, nil
}

func (m *Model) handlePreviewToggle() (tea.Model, tea.Cmd) {
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

	return m, nil
}
