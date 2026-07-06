package tui

import (
	"context"
	"fmt"
	"log"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/LinPr/lazys3/internal/storage"
	fsstore "github.com/LinPr/lazys3/internal/storage/fs"
	s3store "github.com/LinPr/lazys3/internal/storage/s3"
	"github.com/LinPr/lazys3/internal/tui/components/bucketlist"
	"github.com/LinPr/lazys3/internal/tui/components/objectlist"
	"github.com/LinPr/lazys3/internal/tui/components/syncmodal"
	"github.com/LinPr/lazys3/internal/tui/components/transferpanel"
	"github.com/LinPr/lazys3/internal/tui/keybinding"
	"github.com/LinPr/lazys3/internal/tui/state"
	"github.com/LinPr/lazys3/internal/tui/types"
	tea "github.com/charmbracelet/bubbletea/v2"
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

// transferPanelHeight is the vertical budget reserved for the transfer
// panel at the bottom of the TUI. The panel's own View() collapses to ""
// when there are no transfers, so this only kicks in once an op is queued.
const transferPanelHeight = 6

// statusBarHeight is the vertical budget reserved for the persistent
// status bar at the very bottom of the TUI. The bar always renders one
// line, so this is a constant 1.
const statusBarHeight = 1

// objectListOptionFromState rebuilds the objectlist.Option the active list
// would use to fetch the current prefix. It is used by file-op commands
// (download/upload/delete/copy/rename) so they can construct a fresh S3
// client targeting the same endpoint/profile/path-style as the listing.
func (m *Model) objectListOptionFromState() objectlist.Option {
	var endpointURL string
	var pathStyle bool
	var region string
	if p := m.profileList.GetSelectedProfile(); p != nil {
		endpointURL = p.EndpointURL
		pathStyle = shouldUsePathStyle(endpointURL)
		// region is left empty here; the objectlist.Option carries it
		// from the bucketlist flow, but we don't currently thread it.
		// NewS3Client falls back to us-east-1 when empty.
		region = ""
	}
	s3uri := fmt.Sprintf("s3://%s/%s", m.selectedBucket, m.selectedObject)
	return objectlist.Option{
		S3Uri:       s3uri,
		Profile:     m.selectedProfile,
		EndpointURL: endpointURL,
		PathStyle:   pathStyle,
		Region:      region,
	}
}

// bucketListOptionFromState rebuilds the bucketlist.Option the active list
// would use. Used by make-bucket/delete-bucket ops.
func (m *Model) bucketListOptionFromState() bucketlist.Option {
	var endpointURL string
	var pathStyle bool
	if p := m.profileList.GetSelectedProfile(); p != nil {
		endpointURL = p.EndpointURL
		pathStyle = shouldUsePathStyle(endpointURL)
	}
	return bucketlist.Option{
		Profile:     m.selectedProfile,
		EndpointURL: endpointURL,
		PathStyle:   pathStyle,
	}
}

// handleFileOp dispatches the file-op keys (d/u/D/r/c/B/s) to the
// appropriate modal flow. The modal's onConfirm callback returns the
// tea.Cmd that starts the actual operation. Returns nil when the key
// doesn't apply to the current state (e.g. 'd' has no meaning in
// ActiveProfileList).
//
// Key bindings (Track D's help overlay should list these):
//   - d: download selected object (ActiveObjectList, file selected)
//   - u: upload local file to current prefix (ActiveObjectList)
//   - D: delete selected object(s) / empty bucket (ActiveObjectList /
//     ActiveBucketList)
//   - r: rename selected object (copy+delete) (ActiveObjectList, file)
//   - c: copy selected object to s3://bucket/key (ActiveObjectList, file)
//   - B: make bucket (ActiveBucketList)
//   - s: sync directory (ActiveObjectList / ActiveBucketList)
//   - y: presigned share URL for selected object (ActiveObjectList, file)
//   - t: toggle transfer panel visibility (handled in tui.go)
func (m *Model) handleFileOp(key string) tea.Cmd {
	switch m.state {
	case state.ActiveBucketList:
		switch key {
		case "D":
			return m.promptDeleteBucket()
		case "B":
			return m.promptMakeBucket()
		case "s":
			return m.promptSync()
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
		case "s":
			return m.promptSync()
		case keybinding.PresignYank:
			return m.promptPresign()
		}
	}
	return nil
}

// promptPresign opens a modal asking for the presigned-URL expiry, then
// generates a shareable GET URL for the highlighted object. Directories
// have no object to sign, so they surface a status-bar error instead. The
// result arrives as objectlist.PresignDoneMsg (handled in tui.go), which
// shows the URL in a confirm modal and copies it to the clipboard.
func (m *Model) promptPresign() tea.Cmd {
	obj := m.objectlist.GetSelectedObject()
	if obj == nil {
		return nil
	}
	if obj.IsDir() {
		return func() tea.Msg {
			return types.ErrMsg{Err: fmt.Errorf("presign: directories are not supported; select an object file")}
		}
	}
	key := obj.Name()
	opt := m.objectListOptionFromState()
	m.modal.Show(
		fmt.Sprintf("Presign URL expiry for %s (1s..168h)", path.Base(key)),
		"1h",
		func(expiryStr string) tea.Cmd {
			return objectlist.PresignCmd(opt, key, expiryStr)
		},
	)
	return nil
}

// addTransferCmd returns a tea.Cmd that queues a transfer row via
// TransferAddMsg. Modal onConfirm callbacks run against a stale copy of
// the model (the closure captures the Update pass that opened the modal),
// so rows must be created message-style on the live model — never by
// mutating the captured m. It also means a cancelled modal (esc) creates
// no row at all.
func addTransferCmd(t transferpanel.Transfer) tea.Cmd {
	return func() tea.Msg {
		return transferpanel.TransferAddMsg{Transfer: t}
	}
}

// downloadCmds builds one transfer row + DownloadCmd pair per object. Each
// transfer owns a cancellable context (stored on the row for the 'x' key)
// and a shared Progress counter the panel's tick loop renders.
func downloadCmds(opt objectlist.Option, bucket string, objs []objectlist.Object, dest func(objectlist.Object) string) tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(objs))
	for _, obj := range objs {
		id := transferpanel.NewID()
		dst := dest(obj)
		prog := transferpanel.NewProgress()
		ctx, cancel := context.WithCancel(context.Background())
		// Sequence each add+op pair so the row exists before the op can
		// emit its TransferDoneMsg (a fast-failing op would otherwise
		// leave a permanently-running row).
		cmds = append(cmds, tea.Sequence(
			addTransferCmd(transferpanel.Transfer{
				ID:       id,
				Op:       transferpanel.OpDownload,
				Label:    fmt.Sprintf("s3://%s/%s -> %s", bucket, obj.Name(), dst),
				Status:   transferpanel.StatusRunning,
				Progress: prog,
				Cancel:   cancel,
			}),
			objectlist.DownloadCmd(ctx, opt, obj.Name(), dst, id, prog),
		))
	}
	return tea.Batch(cmds...)
}

// promptDownload opens a modal for downloading the current selection. A
// single file prompts for a destination path; a multi-selection prompts
// for a destination directory and spawns one transfer row per object.
func (m *Model) promptDownload() tea.Cmd {
	var files []objectlist.Object
	for _, o := range m.objectlist.SelectedObjects() {
		if !o.IsDir() {
			files = append(files, o)
		}
	}
	if len(files) == 0 {
		obj := m.objectlist.GetSelectedObject()
		if obj == nil || obj.IsDir() {
			return nil
		}
		files = append(files, *obj)
	}
	opt := m.objectListOptionFromState()
	bucket := m.selectedBucket

	if len(files) == 1 {
		obj := files[0]
		defaultPath := path.Base(obj.Name())
		if defaultPath == "" || defaultPath == "." {
			defaultPath = "./download"
		}
		m.modal.Show(
			"Download to",
			defaultPath,
			func(localPath string) tea.Cmd {
				return downloadCmds(opt, bucket, []objectlist.Object{obj},
					func(objectlist.Object) string { return localPath })
			},
		)
		return nil
	}

	m.modal.Show(
		fmt.Sprintf("Download %d objects to directory", len(files)),
		".",
		func(dir string) tea.Cmd {
			return downloadCmds(opt, bucket, files, func(o objectlist.Object) string {
				return filepath.Join(dir, path.Base(o.Name()))
			})
		},
	)
	return nil
}

// promptUpload opens a modal asking for a local file path. The destination
// key is currentPrefix + basename. On confirm, queue + start UploadCmd.
func (m *Model) promptUpload() tea.Cmd {
	opt := m.objectListOptionFromState()
	bucket := m.selectedBucket
	prefix := strings.TrimPrefix(m.selectedObject, "/")
	m.modal.Show(
		"Upload from",
		"./file.txt",
		func(localPath string) tea.Cmd {
			base := path.Base(localPath)
			key := base
			if prefix != "" {
				p := prefix
				if !strings.HasSuffix(p, "/") {
					p += "/"
				}
				key = p + base
			}
			id := transferpanel.NewID()
			prog := transferpanel.NewProgress()
			ctx, cancel := context.WithCancel(context.Background())
			// Sequence (not Batch): the row must exist before the op can
			// emit its TransferDoneMsg, or a fast-failing op leaves a
			// permanently-running row.
			return tea.Sequence(
				addTransferCmd(transferpanel.Transfer{
					ID:       id,
					Op:       transferpanel.OpUpload,
					Label:    fmt.Sprintf("%s -> s3://%s/%s", localPath, bucket, key),
					Status:   transferpanel.StatusRunning,
					Progress: prog,
					Cancel:   cancel,
				}),
				objectlist.UploadCmd(ctx, opt, localPath, id, prog),
			)
		},
	)
	return nil
}

// promptDelete opens a confirm modal for deleting the selected object(s):
// the multi-selection in display order, or the single highlighted item.
// Directory entries (common prefixes) are excluded — DeleteObjects on a
// bare prefix key would silently leave every child object in place — and
// the skip is surfaced in the confirm body (or as an error when nothing
// deletable remains).
func (m *Model) promptDelete() tea.Cmd {
	var objs []objectlist.Object
	skippedDirs := 0
	for _, o := range m.objectlist.SelectedObjects() {
		if o.IsDir() {
			skippedDirs++
			continue
		}
		objs = append(objs, o)
	}
	if len(objs) == 0 && skippedDirs == 0 {
		obj := m.objectlist.GetSelectedObject()
		if obj == nil {
			return nil
		}
		if obj.IsDir() {
			skippedDirs++
		} else {
			objs = append(objs, *obj)
		}
	}
	if len(objs) == 0 {
		if skippedDirs > 0 {
			return func() tea.Msg {
				return types.ErrMsg{Err: fmt.Errorf("delete: directories are not supported; select object files")}
			}
		}
		return nil
	}
	keys := make([]string, 0, len(objs))
	for _, o := range objs {
		keys = append(keys, o.Name())
	}
	opt := m.objectListOptionFromState()
	bucket := m.selectedBucket
	body := fmt.Sprintf("Delete %d object(s) from %s?", len(keys), bucket)
	if skippedDirs > 0 {
		body += fmt.Sprintf(" (%d director(y/ies) skipped)", skippedDirs)
	}
	m.modal.ShowConfirm(
		"Delete objects",
		body,
		func() tea.Cmd {
			id := transferpanel.NewID()
			ctx, cancel := context.WithCancel(context.Background())
			// Sequence (not Batch): the row must exist before the op can
			// emit its TransferDoneMsg, or a fast-failing op leaves a
			// permanently-running row.
			return tea.Sequence(
				addTransferCmd(transferpanel.Transfer{
					ID:     id,
					Op:     transferpanel.OpDelete,
					Label:  fmt.Sprintf("delete %d object(s) from s3://%s", len(keys), bucket),
					Status: transferpanel.StatusRunning,
					Cancel: cancel,
				}),
				objectlist.DeleteCmd(ctx, opt, keys, id),
			)
		},
	)
	return nil
}

// promptRename opens a modal asking for the new destination s3:// URI.
// Rename is copy+delete.
func (m *Model) promptRename() tea.Cmd {
	obj := m.objectlist.GetSelectedObject()
	if obj == nil || obj.IsDir() {
		return nil
	}
	srcKey := obj.Name()
	opt := m.objectListOptionFromState()
	defaultDst := fmt.Sprintf("s3://%s/%s", m.selectedBucket, srcKey)
	m.modal.Show(
		"Rename to",
		defaultDst,
		func(dstS3Uri string) tea.Cmd {
			id := transferpanel.NewID()
			ctx, cancel := context.WithCancel(context.Background())
			// Sequence (not Batch): the row must exist before the op can
			// emit its TransferDoneMsg, or a fast-failing op leaves a
			// permanently-running row.
			return tea.Sequence(
				addTransferCmd(transferpanel.Transfer{
					ID:     id,
					Op:     transferpanel.OpRename,
					Label:  fmt.Sprintf("rename %s -> %s", srcKey, dstS3Uri),
					Status: transferpanel.StatusRunning,
					Cancel: cancel,
				}),
				objectlist.RenameCmd(ctx, opt, srcKey, dstS3Uri, id),
			)
		},
	)
	return nil
}

// promptCopy opens a modal asking for the destination s3://bucket/key URI.
func (m *Model) promptCopy() tea.Cmd {
	obj := m.objectlist.GetSelectedObject()
	if obj == nil || obj.IsDir() {
		return nil
	}
	srcKey := obj.Name()
	opt := m.objectListOptionFromState()
	defaultDst := fmt.Sprintf("s3://%s/%s.copy", m.selectedBucket, srcKey)
	m.modal.Show(
		"Copy to",
		defaultDst,
		func(dstS3Uri string) tea.Cmd {
			id := transferpanel.NewID()
			ctx, cancel := context.WithCancel(context.Background())
			// Sequence (not Batch): the row must exist before the op can
			// emit its TransferDoneMsg, or a fast-failing op leaves a
			// permanently-running row.
			return tea.Sequence(
				addTransferCmd(transferpanel.Transfer{
					ID:     id,
					Op:     transferpanel.OpCopy,
					Label:  fmt.Sprintf("copy %s -> %s", srcKey, dstS3Uri),
					Status: transferpanel.StatusRunning,
					Cancel: cancel,
				}),
				objectlist.CopyCmd(ctx, opt, srcKey, dstS3Uri, id),
			)
		},
	)
	return nil
}

// promptMakeBucket opens a modal asking for a new bucket name. Region
// defaults to the profile region (or us-east-1).
func (m *Model) promptMakeBucket() tea.Cmd {
	opt := m.bucketListOptionFromState()
	listOpt := objectlist.Option{
		Profile:     opt.Profile,
		EndpointURL: opt.EndpointURL,
		PathStyle:   opt.PathStyle,
	}
	m.modal.Show(
		"Make bucket",
		"my-new-bucket",
		func(name string) tea.Cmd {
			id := transferpanel.NewID()
			ctx, cancel := context.WithCancel(context.Background())
			// Sequence (not Batch): the row must exist before the op can
			// emit its TransferDoneMsg, or a fast-failing op leaves a
			// permanently-running row.
			return tea.Sequence(
				addTransferCmd(transferpanel.Transfer{
					ID:     id,
					Op:     transferpanel.OpMakeBucket,
					Label:  fmt.Sprintf("mb s3://%s", name),
					Status: transferpanel.StatusRunning,
					Cancel: cancel,
				}),
				objectlist.MakeBucketCmd(ctx, listOpt, name, listOpt.Region, id),
			)
		},
	)
	return nil
}

// promptDeleteBucket opens a confirm modal for deleting the selected empty
// bucket.
func (m *Model) promptDeleteBucket() tea.Cmd {
	b := m.bucketList.GetSelectedBucket()
	if b == nil {
		return nil
	}
	name := b.Title()
	opt := m.bucketListOptionFromState()
	listOpt := objectlist.Option{
		Profile:     opt.Profile,
		EndpointURL: opt.EndpointURL,
		PathStyle:   opt.PathStyle,
	}
	body := fmt.Sprintf("Delete empty bucket %s?", name)
	m.modal.ShowConfirm(
		"Delete bucket",
		body,
		func() tea.Cmd {
			id := transferpanel.NewID()
			ctx, cancel := context.WithCancel(context.Background())
			// Sequence (not Batch): the row must exist before the op can
			// emit its TransferDoneMsg, or a fast-failing op leaves a
			// permanently-running row.
			return tea.Sequence(
				addTransferCmd(transferpanel.Transfer{
					ID:     id,
					Op:     transferpanel.OpDeleteBucket,
					Label:  fmt.Sprintf("rb s3://%s", name),
					Status: transferpanel.StatusRunning,
					Cancel: cancel,
				}),
				objectlist.DeleteBucketCmd(ctx, listOpt, name, id),
			)
		},
	)
	return nil
}

// refreshAfterOp returns the tea.Cmd that re-fetches the list touched by
// the completed op. Downloads/uploads don't need a refresh of the remote
// list, but deletes/copies/renames/mb/rb/sync do.
func (m *Model) refreshAfterOp(done transferpanel.TransferDoneMsg) tea.Cmd {
	switch done.Op {
	case transferpanel.OpDelete, transferpanel.OpCopy, transferpanel.OpRename,
		transferpanel.OpUpload:
		if m.state == state.ActiveObjectList {
			opt := m.objectListOptionFromState()
			m.objectlist.SetLoading(true)
			return objectlist.FetchObjectListCmd(opt)
		}
	case transferpanel.OpMakeBucket, transferpanel.OpDeleteBucket:
		if m.state == state.ActiveBucketList {
			opt := m.bucketListOptionFromState()
			m.bucketList.SetLoading(true)
			return bucketlist.FetchBucketListCmd(&opt)
		}
	case transferpanel.OpSync:
		// A sync may add/delete objects in the currently viewed listing.
		switch m.state {
		case state.ActiveObjectList:
			opt := m.objectListOptionFromState()
			m.objectlist.SetLoading(true)
			return objectlist.FetchObjectListCmd(opt)
		case state.ActiveBucketList:
			opt := m.bucketListOptionFromState()
			m.bucketList.SetLoading(true)
			return bucketlist.FetchBucketListCmd(&opt)
		}
	}
	return nil
}

// promptSync starts the chained-modal sync flow. It prompts for source,
// then destination, then a flags string, and on the final confirm runs
// the sync via the syncmodal package.
//
// The chained-modal approach reuses Track B's modal as-is: each Show
// call's onConfirm callback opens the next modal. The final callback
// builds a storage.Storage from the active profile's endpoint/path-style
// and dispatches the sync via syncmodal.NewCmd + tea.Every for the
// polling loop.
//
// The default source is the current s3://bucket/prefix (so the user can
// sync the current prefix to a local dir by typing the destination and
// hitting enter). The default destination is empty (the user must type
// it). The default flags string is empty (a default size-and-mtime
// sync).
func (m *Model) promptSync() tea.Cmd {
	// Default source: the current s3 URI (bucket + prefix), or "" if
	// we're at the bucket list level.
	defaultSrc := ""
	if m.state == state.ActiveObjectList {
		if m.selectedBucket != "" {
			if m.selectedObject != "" {
				defaultSrc = fmt.Sprintf("s3://%s/%s", m.selectedBucket, m.selectedObject)
			} else {
				defaultSrc = fmt.Sprintf("s3://%s", m.selectedBucket)
			}
		}
	}

	// Capture the active profile's endpoint/path-style so the final
	// callback can build a storage.Storage. We resolve these up front
	// (rather than inside the callback) because the user could navigate
	// away while the modal is open.
	var endpointURL string
	var pathStyle bool
	var profile string
	if p := m.profileList.GetSelectedProfile(); p != nil {
		endpointURL = p.EndpointURL
		pathStyle = shouldUsePathStyle(endpointURL)
		profile = m.selectedProfile
	}

	// The chained modals are reopened via ShowInputModalMsg rather than by
	// calling m.modal.Show inside the callbacks: the callbacks run against
	// a stale captured model, so the next modal must be opened message-style
	// on the live model.
	m.modal.Show(
		"Sync source (s3://bucket/prefix or local dir)",
		defaultSrc,
		func(src string) tea.Cmd {
			// Default destination: empty for the user to type. A
			// local path for s3→local, or s3://bucket/prefix/ for
			// local→s3 / s3→s3.
			return showInputModalCmd("Sync destination", "", func(dst string) tea.Cmd {
				return showInputModalCmd(
					"Sync flags (--delete --size-only --dry-run --exclude=*.log)",
					"",
					func(flagsStr string) tea.Cmd {
						return startSyncCmd(src, dst, flagsStr, endpointURL, pathStyle, profile)
					},
				)
			})
		},
	)
	return nil
}

// showInputModalCmd asks the root Update to open the input modal on the
// live model (see types.ShowInputModalMsg).
func showInputModalCmd(title, placeholder string, onConfirm func(string) tea.Cmd) tea.Cmd {
	return func() tea.Msg {
		return types.ShowInputModalMsg{
			Title:       title,
			Placeholder: placeholder,
			OnConfirm:   onConfirm,
		}
	}
}

// startSyncCmd dispatches the sync via syncmodal.NewCmd, plus the first
// tea.Every poll tick. The SyncPollMsg handler in tui.go re-arms the ticker
// while the sync is registered, so per-file progress keeps flowing to the
// panel. The row carries the sync context's CancelFunc so 'x' aborts it.
//
// The storage.Storage is built lazily inside the sync Cmd (StorageFn):
// NewStorage resolves credentials and can block on network I/O, so it must
// not run on the Update goroutine.
func startSyncCmd(src, dst, flagsStr, endpointURL string, pathStyle bool, profile string) tea.Cmd {
	flags := syncmodal.ParseFlags(flagsStr)
	id := transferpanel.NewID()
	label := fmt.Sprintf("sync %s -> %s", src, dst)

	// Use the S3Option that the objectlist flow already uses, so the sync
	// talks to the same endpoint as the listing.
	s3opt := s3store.S3Option{
		UsePathStyle: pathStyle,
		Profile:      profile,
		Endpoint:     endpointURL,
	}
	storageOpt := storage.NewStorageOption(s3opt, fsstore.LocalOption{})
	ctx, cancel := context.WithCancel(context.Background())

	add := addTransferCmd(transferpanel.Transfer{
		ID:     id,
		Op:     transferpanel.OpSync,
		Label:  label,
		Status: transferpanel.StatusRunning,
		Cancel: cancel,
	})

	syncCmd := syncmodal.NewCmd(syncmodal.CmdDeps{
		Ctx: ctx,
		StorageFn: func(ctx context.Context) (*storage.Storage, error) {
			return storage.NewStorage(ctx, *storageOpt)
		},
		Src:        src,
		Dst:        dst,
		Flags:      flags,
		TransferID: id,
	})

	poll := tea.Every(200*time.Millisecond, syncmodal.PollCmd(id))

	// Sequence the add before the sync so the row exists when the sync's
	// TransferDoneMsg arrives; the poll ticker runs alongside.
	return tea.Batch(tea.Sequence(add, syncCmd), poll)
}

func (m *Model) setSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *Model) initComponentsSize(msg tea.WindowSizeMsg) {
	m.setSize(msg.Width, msg.Height)

	// Lists and the preview panel are sized from the preview panel's
	// visibility (half width when it is shown). All component sizes are
	// outer dimensions — borders included — so lists + transfer panel +
	// status bar stack to exactly the terminal height with no overflow.
	m.resizeLists()

	// Transfer panel gets its reserved slice; status bar gets the
	// remaining 1 row at the very bottom.
	m.transferPanel.SetSize(m.width, transferPanelHeight)
	m.statusBar.SetSize(m.width, statusBarHeight)
	m.modal.SetSize(m.width, m.height)
	// Help overlay uses the full canvas so it can center itself over
	// the whole screen.
	m.help.SetSize(m.width, m.height)
}

func (m *Model) handleProfileSelect() tea.Cmd {
	if selectedItem := m.profileList.GetSelectedProfile(); selectedItem != nil {
		selectedProfile := selectedItem.Title()
		m.selectedProfile = selectedProfile
		// Plumb the profile's endpoint_url into the bucket list Option so
		// non-AWS services (Aliyun OSS, Huawei OBS, ...) actually connect
		// through the right endpoint. Path-style is forced for any
		// non-Amazonaws endpoint because OSS/OBS/COS/MinIO require it.
		endpointURL := selectedItem.EndpointURL
		pathStyle := shouldUsePathStyle(endpointURL)
		// 获取对应的 buckets
		opt := &bucketlist.Option{
			Profile:     selectedProfile,
			EndpointURL: endpointURL,
			PathStyle:   pathStyle,
		}
		m.bucketList.SetOption(opt)
		m.bucketList.SetLoading(true)
		return bucketlist.FetchBucketListCmd(opt)
	}

	return nil
}

func (m *Model) handleBucketSelect() tea.Cmd {
	log.Println("handleBucketSelect called, state:", m.state)
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

		// Plumb endpoint/path-style from the active profile into the
		// object list Option so object operations target the same
		// endpoint as bucket listing.
		var endpointURL string
		var pathStyle bool
		if p := m.profileList.GetSelectedProfile(); p != nil {
			endpointURL = p.EndpointURL
			pathStyle = shouldUsePathStyle(endpointURL)
		}
		opt := objectlist.Option{
			S3Uri:       s3uri,
			Profile:     m.selectedProfile,
			EndpointURL: endpointURL,
			PathStyle:   pathStyle,
		}

		m.objectlist.SetTitle(s3uri)
		m.objectlist.SetLoading(true)
		cmd := objectlist.FetchObjectListCmd(opt)
		cmds = append(cmds, cmd)
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

		// 可以在这里处理选中的 bucket
		s3uri := fmt.Sprintf("s3://%s/%s", m.selectedBucket, m.selectedObject)
		var endpointURL string
		var pathStyle bool
		if p := m.profileList.GetSelectedProfile(); p != nil {
			endpointURL = p.EndpointURL
			pathStyle = shouldUsePathStyle(endpointURL)
		}
		opt := objectlist.Option{
			S3Uri:       s3uri,
			Profile:     m.selectedProfile,
			EndpointURL: endpointURL,
			PathStyle:   pathStyle,
		}

		m.objectlist.SetTitle(s3uri)
		m.objectlist.SetLoading(true)
		log.Println("----xx--- handleObjectSelect s3uri:", s3uri)
		cmd := objectlist.FetchObjectListCmd(opt)
		cmds = append(cmds, cmd)
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
	log.Println("handleObjectUnSelect s3uri:", s3uri)

	m.objectlist.SetTitle(s3uri)
	// Restore the cursor position saved when we descended into cur (the
	// bucket root uses prefix "").
	m.objectlist.RestorePosition(m.selectedBucket, parent)
	m.objectlist.SetLoading(true)

	opt := m.objectListOptionFromState()
	return objectlist.FetchObjectListCmd(opt)
}

func (m *Model) handleForward(_ tea.Msg) tea.Cmd {
	var cmds []tea.Cmd

	switch m.state {
	case state.ActiveProfileList:
		cmd := m.handleProfileSelect()
		cmds = append(cmds, cmd)

		// switch to bucket list if a profile is selected
		if m.profileList.GetSelectedProfile() != nil {
			m.state = state.ActiveBucketList
		}

		// close the preview panel (restoring list widths) on list switch
		m.closePreview()

	case state.ActiveBucketList:
		cmd := m.handleBucketSelect()
		cmds = append(cmds, cmd)

		// switch to object list if a bucket is selected
		if m.bucketList.GetSelectedBucket() != nil {
			m.state = state.ActiveObjectList
		}

		m.closePreview()

	case state.ActiveObjectList:
		cmd := m.handleObjectSelect()
		cmds = append(cmds, cmd)

		// switch to object list if a bucket is selected
		if m.objectlist.GetSelectedObject() != nil {
			m.state = state.ActiveObjectList
		}

		m.closePreview()
	}

	return tea.Batch(cmds...)
}

func (m *Model) handleBackward() tea.Cmd {
	var cmds []tea.Cmd

	// A backward navigation switches (or reloads) the visible list; close
	// the preview so a stale item preview never sits next to the new list.
	m.closePreview()

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

func (m *Model) handlePreviewToggle() {
	m.previewPanel.Toggle()
	m.resizeLists()
}

// resizeLists sizes all three lists from the current window size and the
// preview panel's visibility: half width next to a visible preview, full
// width otherwise. Sizing every list (not just the active one) keeps a
// list switch from rendering a list whose width was set for the other
// preview state. The list area always reserves room for the transfer
// panel and the persistent status bar at the bottom. All sizes are outer
// dimensions; each component subtracts its own border frame.
func (m *Model) resizeLists() {
	listHeight := m.height - transferPanelHeight - statusBarHeight
	if listHeight < 4 {
		listHeight = 4
	}
	w := m.width
	if m.previewPanel.IsVisible() {
		w = m.width / 2
	}
	m.profileList.SetSize(w, listHeight)
	m.bucketList.SetSize(w, listHeight)
	m.objectlist.SetSize(w, listHeight)
	// The preview panel takes the columns left over next to a half-width
	// list; its viewport clips content to this size.
	m.previewPanel.SetSize(m.width-w, listHeight)
}

// closePreview hides the preview panel (if visible) and restores the lists
// to full width. Used on list switches so navigation never leaves a stale
// preview next to a full-width list, or a half-width list next to a hidden
// preview.
func (m *Model) closePreview() {
	if !m.previewPanel.IsVisible() {
		return
	}
	m.previewPanel.Hide()
	m.resizeLists()
}
