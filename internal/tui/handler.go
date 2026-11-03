package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	s3store "github.com/LinPr/lazys3/internal/storage/s3"
	"github.com/LinPr/lazys3/internal/tui/components/bucketlist"
	"github.com/LinPr/lazys3/internal/tui/components/objectlist"
	"github.com/LinPr/lazys3/internal/tui/keybinding"
	"github.com/LinPr/lazys3/internal/tui/state"
	"github.com/LinPr/lazys3/internal/tui/types"
)

// shouldUsePathStyle returns true when endpointURL is non-empty, does
// not point at an Amazon S3 host, and is not an Aliyun OSS endpoint.
//
// Most S3-compatible services (MinIO, Tencent COS, GCS, Ceph) require
// path-style addressing. AWS S3 prefers virtual-host. Aliyun OSS is the
// notable exception: it rejects path-style on ListObjects with
// SecondLevelDomainForbidden and requires virtual-host, even though its
// endpoint URL is not an amazonaws.com host. Detect OSS by its
// `.aliyuncs.com` suffix and force virtual-host for it; everything else
// non-AWS gets path-style.
func shouldUsePathStyle(endpointURL string) bool {
	if endpointURL == "" {
		return false
	}
	host := strings.ToLower(endpointURL)
	// Strip scheme.
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	host, _, _ = strings.Cut(host, "/")
	if strings.HasSuffix(host, "amazonaws.com") {
		// AWS S3 (including transfer acceleration) — virtual-host.
		return false
	}
	if strings.HasSuffix(host, "aliyuncs.com") {
		// Aliyun OSS rejects path-style; force virtual-host.
		return false
	}
	// MinIO, Tencent COS, Huawei OBS, Ceph, GCS, etc. — path-style.
	return true
}

// statusBarHeight is the vertical budget reserved for the persistent
// status bar at the very bottom of the TUI. The bar always renders one
// line, so this is a constant 1.
const statusBarHeight = 1

// objectListOptionFromState rebuilds the objectlist.Option the active list
// would use to fetch the current prefix. It is used by file-op commands
// (download/upload/delete/copy/rename) so they can construct a fresh S3
// client targeting the same endpoint/profile/path-style as the listing.
func (m *Model) objectListOptionFromState() objectlist.Option {
	return m.objectListOption(fmt.Sprintf("s3://%s/%s", m.selectedBucket, m.selectedObject))
}

// objectListOption builds an objectlist.Option for an arbitrary s3:// URI
// on the ACTIVE profile's connection settings. Every navigation and file
// op funnels through it, so endpoint, path-style, region and the shared
// file paths can never drift between a listing and the op acting on it.
func (m *Model) objectListOption(s3uri string) objectlist.Option {
	endpointURL := m.selectedEndpoint()
	return objectlist.Option{
		S3Uri:          s3uri,
		Profile:        m.selectedProfile,
		Region:         m.selectedRegion,
		EndpointURL:    endpointURL,
		PathStyle:      shouldUsePathStyle(endpointURL),
		ConfigFile:     m.awsFiles.ConfigFile,
		CredentialFile: m.awsFiles.CredentialsFile,
		Client:         m.s3ClientOptions(),
	}
}

// s3ClientOptions is the [s3] block from config.yaml, carried into every
// client the TUI builds (listing, preview, metadata, versions, sync) so a
// service that needs ListObjectsV1 or an unverified self-signed endpoint
// works everywhere instead of only where someone remembered to pass it.
func (m *Model) s3ClientOptions() s3store.ClientOptions {
	return s3store.ClientOptions{
		MaxRetries:       m.cfg.S3.MaxRetries,
		UseListObjectsV1: m.cfg.S3.UseListObjectsV1,
		NoVerifySSL:      m.cfg.S3.InsecureSkipVerify,
		NoSignRequest:    m.cfg.S3.NoSignRequest,
	}
}

// selectedEndpoint returns the active profile's custom endpoint_url.
// A [profiles.<name>.endpoint] override wins over the shared config, so a
// profile can be pointed at an internal endpoint without touching
// ~/.aws/config.
func (m *Model) selectedEndpoint() string {
	if p, ok := m.profileStarts[m.selectedProfile]; ok && p.Endpoint != "" {
		return p.Endpoint
	}
	if p := m.profileList.GetSelectedProfile(); p != nil {
		return p.EndpointURL
	}
	return ""
}

// bucketListOptionFromState rebuilds the bucketlist.Option the active list
// would use. Used by make-bucket/delete-bucket ops.
func (m *Model) bucketListOptionFromState() bucketlist.Option {
	endpointURL := m.selectedEndpoint()
	return bucketlist.Option{
		Profile:        m.selectedProfile,
		Region:         m.selectedRegion,
		EndpointURL:    endpointURL,
		PathStyle:      shouldUsePathStyle(endpointURL),
		ConfigFile:     m.awsFiles.ConfigFile,
		CredentialFile: m.awsFiles.CredentialsFile,
		Client:         m.s3ClientOptions(),
	}
}

// handleFileOp dispatches the file-op keys (d/u/D/r/c/B/s/y/Y) to the
// appropriate modal flow. The modal's onConfirm callback returns the
// tea.Cmd that starts the actual operation. Returns nil when the key
// doesn't apply to the current state (e.g. 'd' has no meaning in
// ActiveProfileList).
//
// Key bindings (the help overlay should list these):
//   - d: download selected object (ActiveObjectList, file selected)
//   - u: upload local file to current prefix (ActiveObjectList)
//   - D: delete selected object(s) / empty bucket (ActiveObjectList /
//     ActiveBucketList)
//   - r: rename selected object (copy+delete) (ActiveObjectList, file)
//   - c: copy selected object to s3://bucket/key (ActiveObjectList, file)
//   - B: make bucket (ActiveBucketList; in the object list it only hints —
//     remote "directories" are prefixes that exist through their objects)
//   - s: sync directory (ActiveObjectList / ActiveBucketList)
//   - y: yank the highlighted item's s3:// URI to the clipboard
//     (ActiveBucketList / ActiveObjectList)
//   - Y: presigned share URL for selected object (ActiveObjectList, file)
//   - t: toggle the transfers overlay (handled in tui.go)
func (m *Model) handleFileOp(key string) tea.Cmd {
	switch m.state {
	case state.ActiveBucketList:
		switch key {
		case "D":
			return m.promptDeleteBucket()
		case "B":
			return m.promptMakeBucket()
		case "s":
			return m.promptSync(m.remoteLocation(), "")
		case keybinding.YankURI:
			return m.yankRemoteURI()
		}
	case state.ActiveObjectList:
		switch key {
		case "d":
			return m.promptDownload()
		case "u":
			return m.promptUpload()
		case "D":
			return m.promptDelete()
		case "r":
			return m.promptRename()
		case "c":
			return m.promptCopy()
		case "B":
			m.statusBar.SetInfo("B makes buckets (bucket list) / directories (local pane)")
			return nil
		case "s":
			return m.promptSync(m.remoteLocation(), "")
		case keybinding.YankURI:
			return m.yankRemoteURI()
		case keybinding.Presign:
			return m.promptPresign()
		case keybinding.PresignUpload:
			return m.promptPresignUpload()
		case keybinding.MultipartCleanup:
			return m.promptMultipartCleanup()
		case keybinding.Search:
			return m.promptSearch()
		}
	}
	return nil
}

// yankRemoteURI ('y' on a remote pane) copies the highlighted item's s3://
// URI to the system clipboard via OSC52: s3://bucket in the bucket list,
// s3://bucket/key in the object list (a directory row yields its prefix
// URI). The dual-pane local mirror is localYankPath.
func (m *Model) yankRemoteURI() tea.Cmd {
	var uri string
	switch m.state {
	case state.ActiveBucketList:
		b := m.bucketList.GetSelectedBucket()
		if b == nil {
			return nil
		}
		uri = "s3://" + b.Title()
	case state.ActiveObjectList:
		obj := m.objectlist.GetSelectedObject()
		if obj == nil {
			return nil
		}
		uri = fmt.Sprintf("s3://%s/%s", m.selectedBucket, obj.Name())
	default:
		return nil
	}
	m.statusBar.SetInfo("uri copied: " + uri)
	return tea.SetClipboard(uri)
}

func (m *Model) initComponentsSize(msg tea.WindowSizeMsg) {
	m.setSize(msg.Width, msg.Height)

	// A terminal too narrow for two readable panes drops dual mode
	// entirely (re-enter with 'l' after widening). Width 0 is an unknown
	// size, not a narrow one (same semantics as enterDualPane).
	if m.dualPane && m.width > 0 && m.width < minDualPaneWidth {
		m.exitDualPane()
		m.statusBar.SetInfo(fmt.Sprintf("dual-pane closed: terminal too narrow (needs ≥%d cols)", minDualPaneWidth))
	}

	// All component sizes are outer dimensions — borders included — so
	// lists + status bar stack to exactly the terminal height with no
	// overflow.
	m.resizeLists()

	// The status bar gets the remaining 1 row at the very bottom.
	m.statusBar.SetSize(m.width, statusBarHeight)
	m.modal.SetSize(m.width, m.height)
	// The full-screen overlays (help, transfers, versions) and the
	// floating p/m overlays get the whole canvas so they can lay
	// themselves out over the screen.
	m.help.SetSize(m.width, m.height)
	m.transferView.SetSize(m.width, m.height)
	m.versionView.SetSize(m.width, m.height)
	m.contentView.SetSize(m.width, m.height)
	m.metaView.SetSize(m.width, m.height)
}

// handleProfileSelect selects the highlighted profile and starts its
// navigation. The returned bool reports whether the in-memory start
// location (config or CLI) applied, in which case the model is already at
// ActiveObjectList and the caller must not switch to the bucket list.
func (m *Model) handleProfileSelect() (tea.Cmd, bool) {
	selectedItem := m.profileList.GetSelectedProfile()
	if selectedItem == nil {
		return nil, false
	}
	selectedProfile := selectedItem.Title()
	m.selectedProfile = selectedProfile
	// The shared config is the baseline; a [profiles.<name>] override in
	// lazys3's own config wins, so the same AWS profile can be used against
	// a different region/endpoint without editing ~/.aws/config.
	m.selectedRegion = selectedItem.Region()
	if p, ok := m.profileStarts[selectedProfile]; ok {
		if p.Region != "" {
			m.selectedRegion = p.Region
		}
	}
	// Plumb the profile's endpoint_url into the bucket list Option so
	// non-AWS services (Aliyun OSS, Huawei OBS, ...) actually connect
	// through the right endpoint. Path-style is forced for any
	// non-Amazonaws endpoint because OSS/OBS/COS/MinIO require it.
	opt := m.bucketListOptionFromState()
	m.bucketList.SetOption(&opt)

	// A configured (or CLI) start location opens the object list directly:
	// no ListBuckets call is made, so bucket-scoped credentials work. The
	// bucket list is fetched lazily if the user backs out to it.
	if loc, ok := m.startLocationFor(selectedProfile); ok {
		m.openStartLocation(loc)
		return m.objectlist.Fetch(m.objectListOptionFromState()), true
	}

	m.bucketList.SetLoading(true)
	return bucketlist.FetchBucketListCmd(&opt), false
}

func (m *Model) handleBucketSelect() tea.Cmd {
	var cmds []tea.Cmd

	// 处理 bucket 选择（这里可以添加具体的业务逻辑）
	if selectedBucket := m.bucketList.GetSelectedBucket(); selectedBucket != nil {
		selectedBucket := selectedBucket.Title()
		m.selectedBucket = selectedBucket
		// Entering a bucket always lands at its root prefix.
		m.selectedObject = ""
		// Restore the cursor position from a previous visit to this
		// bucket's root, if any (applied by the next non-empty SetObjects).
		m.objectlist.RestorePosition(selectedBucket, "")

		s3uri := fmt.Sprintf("s3://%s", selectedBucket)
		opt := m.objectListOption(s3uri)

		m.objectlist.SetTitle(s3uri)
		cmds = append(cmds, m.objectlist.Fetch(opt))
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

		// Memoise the cursor in the prefix we're leaving, and arm a
		// restore for the prefix we're entering (in case it was visited
		// before).
		m.objectlist.RememberPosition(m.selectedBucket, m.selectedObject)
		m.selectedObject = selectedObject
		m.objectlist.RestorePosition(m.selectedBucket, selectedObject)

		s3uri := fmt.Sprintf("s3://%s/%s", m.selectedBucket, m.selectedObject)
		opt := m.objectListOption(s3uri)

		m.objectlist.SetTitle(s3uri)
		cmds = append(cmds, m.objectlist.Fetch(opt))
	}

	return tea.Batch(cmds...)
}

func (m *Model) handleForward(_ tea.Msg) tea.Cmd {
	var cmds []tea.Cmd

	switch m.state {
	case state.ActiveProfileList:
		cmd, jumped := m.handleProfileSelect()
		cmds = append(cmds, cmd)

		// Normally a profile opens the bucket list; a configured/CLI start
		// location already switched the model to the object list.
		if !jumped && m.profileList.GetSelectedProfile() != nil {
			m.state = state.ActiveBucketList
		}

	case state.ActiveBucketList:
		// DATA-SAFETY guard: while a bucket-list refresh is in flight the
		// visible listing is stale — a filter typed against it (e.g. for a
		// just-created bucket the stale listing doesn't contain) matches
		// nothing, and bubbles' accept-enter then silently clears the
		// filter, parking the cursor on the first bucket. Entering it now
		// would open — and route queued uploads into — a bucket the user
		// never picked. Refuse the select until the listing is current.
		if m.bucketList.Loading() {
			m.statusBar.SetInfo("bucket list is refreshing — try again in a moment")
			break
		}
		cmd := m.handleBucketSelect()
		cmds = append(cmds, cmd)

		// switch to object list if a bucket is selected
		if m.bucketList.GetSelectedBucket() != nil {
			m.state = state.ActiveObjectList
		}

	case state.ActiveObjectList:
		cmd := m.handleObjectSelect()
		cmds = append(cmds, cmd)

		// switch to object list if a bucket is selected
		if m.objectlist.GetSelectedObject() != nil {
			m.state = state.ActiveObjectList
		}
	}

	return tea.Batch(cmds...)
}

func (m *Model) handleBackward() tea.Cmd {
	var cmds []tea.Cmd

	switch m.state {
	case state.ActiveObjectList:
		cmd := m.handleObjectUnSelect()
		// nil means we were already at the bucket root: back out to the
		// bucket list, memoising the root cursor so re-entering the bucket
		// restores it. Otherwise the cmd fetches the parent prefix and we
		// blank the list while the fetch is in flight (the pending cursor
		// restore survives the empty SetObjects).
		if cmd == nil {
			m.objectlist.RememberPosition(m.selectedBucket, "")
			m.state = state.ActiveBucketList
			// A profile with a configured start location never listed
			// buckets. Fetch them now that the user actually opened the
			// bucket list; a restricted account then sees the (expected)
			// access-denied error only here, not at every startup.
			if lazy := m.ensureBucketListLoaded(); lazy != nil {
				cmds = append(cmds, lazy)
			}
		} else {
			cmds = append(cmds, cmd)
		}
		m.objectlist.SetObjects([]objectlist.Object{})
	case state.ActiveBucketList:
		// m.handleBucketSelect()
		m.state = state.ActiveProfileList

	case state.ActiveProfileList:
		// m.handleProfileSelect()
	}

	return tea.Batch(cmds...)
}

// handleObjectUnSelect navigates one prefix level up. It works off the
// model's current prefix (m.selectedObject) rather than the highlighted
// item, so it also works on empty listings. Returns nil when already at
// the bucket root — the caller then switches back to the bucket list.
func (m *Model) handleObjectUnSelect() tea.Cmd {
	cur := m.selectedObject
	if cur == "" {
		return nil
	}

	// Memoise the cursor in the prefix we're leaving.
	m.objectlist.RememberPosition(m.selectedBucket, cur)

	parts := strings.Split(strings.TrimSuffix(cur, "/"), "/")
	parent := ""
	if len(parts) > 1 {
		parent = strings.Join(parts[:len(parts)-1], "/") + "/"
	}
	m.selectedObject = parent

	s3uri := fmt.Sprintf("s3://%s", m.selectedBucket)
	if parent != "" {
		s3uri = fmt.Sprintf("s3://%s/%s", m.selectedBucket, parent)
	}

	m.objectlist.SetTitle(s3uri)
	// Restore the cursor position saved when we descended into cur (the
	// bucket root uses prefix "").
	m.objectlist.RestorePosition(m.selectedBucket, parent)

	return m.objectlist.Fetch(m.objectListOptionFromState())
}

// resizeLists sizes all three remote lists (and the dual-pane local list)
// from the current window size: a 50/50 split in dual mode, full width
// otherwise. Sizing every list (not just the active one) keeps a list
// switch from rendering a stale-width list. The list area always reserves
// room for the persistent status bar at the bottom (transfers live in the
// 't' overlay, so the lists own every other row). All sizes are outer
// dimensions; each component subtracts its own border frame.
func (m *Model) resizeLists() {
	listHeight := m.height - statusBarHeight
	if listHeight < 4 {
		listHeight = 4
	}
	if m.dualPane {
		// Dual layout: remote pane left, local pane right.
		lw := m.width / 2
		rw := m.width - lw
		m.profileList.SetSize(lw, listHeight)
		m.bucketList.SetSize(lw, listHeight)
		m.objectlist.SetSize(lw, listHeight)
		m.localList.SetSize(rw, listHeight)
		return
	}
	m.profileList.SetSize(m.width, listHeight)
	m.bucketList.SetSize(m.width, listHeight)
	m.objectlist.SetSize(m.width, listHeight)
}

// previewHint is the status-bar nudge shown when 'p' is pressed on
// something that has no byte content to preview.
const previewHint = "preview works on files"

func (m *Model) setSize(width, height int) {
	m.width = width
	m.height = height
}

// errCmd wraps an error into a status-bar ErrMsg Cmd.
func errCmd(err error) tea.Cmd {
	return func() tea.Msg {
		return types.ErrMsg{Err: err}
	}
}

// handleContentPreview ('p') opens the floating content-preview overlay
// for the focused pane's highlighted file: a bounded local read with local
// focus, a ranged GetObject on the object list otherwise. Directories,
// empty listings and the profile/bucket lists surface a status-bar hint.
func (m *Model) handleContentPreview() tea.Cmd {
	if m.localFocused() {
		e := m.localList.GetSelectedEntry()
		if e == nil || e.IsDir() {
			m.statusBar.SetInfo(previewHint)
			return nil
		}
		return m.contentView.ShowLocal(e.Path())
	}
	if m.state != state.ActiveObjectList {
		m.statusBar.SetInfo(previewHint + " — open a bucket first")
		return nil
	}
	obj := m.objectlist.GetSelectedObject()
	if obj == nil || obj.IsDir() {
		m.statusBar.SetInfo(previewHint)
		return nil
	}
	return m.contentView.ShowRemote(obj.GetPreviewRequest())
}

// handleMetadataOpen ('m') opens the floating metadata overlay for the
// focused pane's highlighted item; every list state has a metadata source
// (object/prefix, bucket, local entry, profile). Only an empty listing
// surfaces a hint.
func (m *Model) handleMetadataOpen() tea.Cmd {
	if m.localFocused() {
		e := m.localList.GetSelectedEntry()
		if e == nil {
			m.statusBar.SetInfo("nothing selected")
			return nil
		}
		return m.metaView.ShowLocal(e.Path())
	}
	switch m.state {
	case state.ActiveProfileList:
		p := m.profileList.GetSelectedProfile()
		if p == nil {
			m.statusBar.SetInfo("nothing selected")
			return nil
		}
		var files []string
		for _, f := range []string{m.awsFiles.ConfigFile, m.awsFiles.CredentialsFile} {
			if f != "" {
				files = append(files, f)
			}
		}
		m.metaView.ShowProfile(p.Title(), p.EndpointURL, p.Region(), files)
		return nil
	case state.ActiveBucketList:
		b := m.bucketList.GetSelectedBucket()
		if b == nil {
			m.statusBar.SetInfo("nothing selected")
			return nil
		}
		return m.metaView.ShowBucket(b.GetPreviewRequest())
	case state.ActiveObjectList:
		obj := m.objectlist.GetSelectedObject()
		if obj == nil {
			m.statusBar.SetInfo("nothing selected")
			return nil
		}
		return m.metaView.ShowObject(obj.GetPreviewRequest(), obj.IsDir())
	}
	return nil
}
