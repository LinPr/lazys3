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
	"github.com/LinPr/lazys3/internal/tui/components/profilelist"
	"github.com/LinPr/lazys3/internal/tui/components/syncmodal"
	"github.com/LinPr/lazys3/internal/tui/components/transferpanel"
	"github.com/LinPr/lazys3/internal/tui/components/versionview"
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
// Files go through the existing bulk DeleteCmd; each directory entry
// (common prefix) becomes its own transfer row that recursively lists and
// deletes every key under the prefix (objectlist.DeletePrefixCmd). The
// confirm body calls out the recursive folder deletes by name so the
// modal never hides that whole trees are going away.
func (m *Model) promptDelete() tea.Cmd {
	var files, dirs []objectlist.Object
	for _, o := range m.objectlist.SelectedObjects() {
		if o.IsDir() {
			dirs = append(dirs, o)
		} else {
			files = append(files, o)
		}
	}
	if len(files) == 0 && len(dirs) == 0 {
		obj := m.objectlist.GetSelectedObject()
		if obj == nil {
			return nil
		}
		if obj.IsDir() {
			dirs = append(dirs, *obj)
		} else {
			files = append(files, *obj)
		}
	}
	if len(files) == 0 && len(dirs) == 0 {
		return nil
	}
	keys := make([]string, 0, len(files))
	for _, o := range files {
		keys = append(keys, o.Name())
	}
	prefixes := make([]string, 0, len(dirs))
	for _, o := range dirs {
		prefixes = append(prefixes, o.Name())
	}
	opt := m.objectListOptionFromState()
	bucket := m.selectedBucket
	var body string
	switch {
	case len(prefixes) == 0:
		body = fmt.Sprintf("Delete %d object(s) from %s?", len(keys), bucket)
	case len(keys) == 0:
		body = fmt.Sprintf("Recursively delete %d folder(s) from %s? (permanent)%s",
			len(prefixes), bucket, folderList(prefixes))
	default:
		body = fmt.Sprintf("Delete %d object(s) and recursively delete %d folder(s) from %s? (permanent)%s",
			len(keys), len(prefixes), bucket, folderList(prefixes))
	}
	m.modal.ShowConfirm(
		"Delete objects",
		body,
		func() tea.Cmd {
			cmds := make([]tea.Cmd, 0, len(prefixes)+1)
			if len(keys) > 0 {
				id := transferpanel.NewID()
				ctx, cancel := context.WithCancel(context.Background())
				// Sequence (not Batch): the row must exist before the op
				// can emit its TransferDoneMsg, or a fast-failing op leaves
				// a permanently-running row.
				cmds = append(cmds, tea.Sequence(
					addTransferCmd(transferpanel.Transfer{
						ID:     id,
						Op:     transferpanel.OpDelete,
						Label:  fmt.Sprintf("delete %d object(s) from s3://%s", len(keys), bucket),
						Status: transferpanel.StatusRunning,
						Cancel: cancel,
					}),
					objectlist.DeleteCmd(ctx, opt, keys, id),
				))
			}
			for _, prefix := range prefixes {
				id := transferpanel.NewID()
				ctx, cancel := context.WithCancel(context.Background())
				cmds = append(cmds, tea.Sequence(
					addTransferCmd(transferpanel.Transfer{
						ID:     id,
						Op:     transferpanel.OpDelete,
						Label:  fmt.Sprintf("delete dir: s3://%s/%s", bucket, prefix),
						Status: transferpanel.StatusRunning,
						Cancel: cancel,
					}),
					objectlist.DeletePrefixCmd(ctx, opt, prefix, id),
				))
			}
			return tea.Batch(cmds...)
		},
	)
	return nil
}

// folderList renders up to three folder names (one per line) for the
// delete confirm body, then "+K more" for the rest.
func folderList(prefixes []string) string {
	shown := prefixes
	extra := 0
	if len(shown) > 3 {
		extra = len(shown) - 3
		shown = shown[:3]
	}
	s := "\n" + strings.Join(shown, "\n")
	if extra > 0 {
		s += fmt.Sprintf("\n+%d more", extra)
	}
	return s
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

// errCmd wraps an error into a status-bar ErrMsg Cmd.
func errCmd(err error) tea.Cmd {
	return func() tea.Msg {
		return types.ErrMsg{Err: err}
	}
}

// handleVersionsOpen opens the object-versions overlay ('v') for the
// highlighted file. A directory or nil selection surfaces a status-bar
// error instead (prefixes have no version history of their own).
func (m *Model) handleVersionsOpen() tea.Cmd {
	if m.state != state.ActiveObjectList {
		return nil
	}
	obj := m.objectlist.GetSelectedObject()
	if obj == nil || obj.IsDir() {
		return errCmd(fmt.Errorf("versions: select an object file (directories have no version history)"))
	}
	opt := m.objectListOptionFromState()
	return m.versionView.Show(opt, m.selectedBucket, obj.Name())
}

// handleVersioningToggle ('V' in the bucket list) kicks off the async
// GetBucketVersioning probe; the confirm modal opens when BucketStatusMsg
// arrives (see handleBucketStatus) so the current status is shown in it.
func (m *Model) handleVersioningToggle() tea.Cmd {
	if m.state != state.ActiveBucketList {
		return nil
	}
	b := m.bucketList.GetSelectedBucket()
	if b == nil {
		return nil
	}
	bopt := m.bucketListOptionFromState()
	opt := objectlist.Option{
		Profile:     bopt.Profile,
		EndpointURL: bopt.EndpointURL,
		PathStyle:   bopt.PathStyle,
	}
	return versionview.BucketStatusCmd(opt, b.Title())
}

// handleBucketStatus opens the versioning toggle confirm modal once the
// bucket's current status is known. Probe failures (e.g. endpoints without
// a versioning API) surface on the status bar. Mirroring the presign guard,
// the modal is never opened behind another modal or a full-screen overlay;
// the status falls back to a status-bar note instead.
func (m *Model) handleBucketStatus(msg versionview.BucketStatusMsg) tea.Cmd {
	if msg.Err != nil {
		return errCmd(fmt.Errorf("bucket versioning: %w", msg.Err))
	}
	if m.overlayActive() {
		m.statusBar.SetInfo(fmt.Sprintf("bucket %s versioning: %s", msg.Bucket, versionview.StatusLabel(msg.Status)))
		return nil
	}
	enable := msg.Status != "Enabled"
	target := "Suspended"
	if enable {
		target = "Enabled"
	}
	body := fmt.Sprintf("Bucket %s versioning is currently %s.\nSet it to %s?",
		msg.Bucket, versionview.StatusLabel(msg.Status), target)
	if enable && msg.Status == "" {
		body += "\n\nnote: once versioned, a bucket can only be suspended, never unversioned"
	}
	opt := msg.Opt
	bucket := msg.Bucket
	m.modal.ShowConfirm(
		"Bucket versioning",
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
					Op:     transferpanel.OpVersioning,
					Label:  fmt.Sprintf("versioning s3://%s -> %s", bucket, target),
					Status: transferpanel.StatusRunning,
					Cancel: cancel,
				}),
				versionview.PutVersioningCmd(ctx, opt, bucket, enable, id),
			)
		},
	)
	return nil
}

// handleVersionAction routes an overlay row action (d/R/D on a version) to
// its modal flow. The modal opens over the overlay (the overlay stays open
// behind it; tui.go's View gives the modal render precedence).
func (m *Model) handleVersionAction(msg versionview.ActionMsg) tea.Cmd {
	// ActionMsg is delivered async, so a second d/R/D can land before the
	// first modal was answered. Mirroring the presign guard, never clobber
	// the open modal (a half-typed download path would be silently lost).
	if m.modal.IsVisible() {
		m.statusBar.SetInfo("finish the open dialog first")
		return nil
	}
	switch msg.Kind {
	case versionview.ActionDownload:
		return m.promptVersionDownload(msg)
	case versionview.ActionRestore:
		return m.promptVersionRestore(msg)
	case versionview.ActionDelete:
		return m.promptVersionDelete(msg)
	}
	return nil
}

// promptVersionDownload opens the destination modal for downloading one
// specific version. Delete markers carry no content, so they error out on
// the status bar instead.
func (m *Model) promptVersionDownload(msg versionview.ActionMsg) tea.Cmd {
	short := versionview.ShortID(msg.Version.VersionID)
	if msg.Version.IsDeleteMarker {
		return errCmd(fmt.Errorf("download: %s@%s is a delete marker, not content", path.Base(msg.Key), short))
	}
	defaultPath := path.Base(msg.Key)
	if defaultPath == "" || defaultPath == "." {
		defaultPath = "./download"
	}
	opt, bucket, key, versionID := msg.Opt, msg.Bucket, msg.Key, msg.Version.VersionID
	m.modal.Show(
		fmt.Sprintf("Download %s@%s to", path.Base(key), short),
		defaultPath,
		func(localPath string) tea.Cmd {
			id := transferpanel.NewID()
			prog := transferpanel.NewProgress()
			ctx, cancel := context.WithCancel(context.Background())
			// Sequence (not Batch): the row must exist before the op can
			// emit its TransferDoneMsg, or a fast-failing op leaves a
			// permanently-running row.
			return tea.Sequence(
				addTransferCmd(transferpanel.Transfer{
					ID:       id,
					Op:       transferpanel.OpDownload,
					Label:    fmt.Sprintf("s3://%s/%s@%s -> %s", bucket, key, short, localPath),
					Status:   transferpanel.StatusRunning,
					Progress: prog,
					Cancel:   cancel,
				}),
				versionview.DownloadVersionCmd(ctx, opt, bucket, key, versionID, localPath, id, prog),
			)
		},
	)
	return nil
}

// promptVersionRestore opens the confirm modal for restoring a version as
// latest (server-side copy onto the same key). Delete markers cannot be
// copy-sourced; removing them is the D action instead.
func (m *Model) promptVersionRestore(msg versionview.ActionMsg) tea.Cmd {
	short := versionview.ShortID(msg.Version.VersionID)
	if msg.Version.IsDeleteMarker {
		return errCmd(fmt.Errorf("restore: %s@%s is a delete marker; use D to remove it (undelete)", path.Base(msg.Key), short))
	}
	opt, bucket, key, versionID := msg.Opt, msg.Bucket, msg.Key, msg.Version.VersionID
	storageClass := msg.Version.StorageClass
	// The copy only stacks a new version when versioning is Enabled; when
	// Suspended (or never enabled) it is written as the "null" version,
	// destroying any existing null version of the key — say so instead of
	// promising a preserved history.
	body := fmt.Sprintf("Restore %s@%s as the latest version?\n", key, short)
	switch {
	case msg.StatusKnown && msg.Status == "Enabled":
		body += "A server-side copy is stacked on top; the history keeps this version."
	case msg.StatusKnown:
		body += fmt.Sprintf("warning: bucket versioning is %s — the copy is written as the \"null\" version, permanently overwriting the current null version of this key.", versionview.StatusLabel(msg.Status))
	default:
		body += "warning: the bucket's versioning status is unknown — unless it is Enabled, the copy permanently overwrites the current \"null\" version of this key."
	}
	m.modal.ShowConfirm(
		"Restore version",
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
					Op:     transferpanel.OpCopy,
					Label:  fmt.Sprintf("restore s3://%s/%s@%s", bucket, key, short),
					Status: transferpanel.StatusRunning,
					Cancel: cancel,
				}),
				versionview.RestoreVersionCmd(ctx, opt, bucket, key, versionID, storageClass, id),
			)
		},
	)
	return nil
}

// promptVersionDelete opens the confirm modal for permanently deleting one
// version. On a delete-marker row the action is relabelled "remove delete
// marker (undelete)" — removing the current marker brings the previous
// version back as latest.
func (m *Model) promptVersionDelete(msg versionview.ActionMsg) tea.Cmd {
	short := versionview.ShortID(msg.Version.VersionID)
	opt, bucket, key, versionID := msg.Opt, msg.Bucket, msg.Key, msg.Version.VersionID
	title := "Delete version"
	body := fmt.Sprintf("Permanently delete %s@%s?\nThis bypasses versioning and cannot be undone.", key, short)
	label := fmt.Sprintf("delete version s3://%s/%s@%s", bucket, key, short)
	if msg.Version.IsDeleteMarker {
		title = "Remove delete marker (undelete)"
		body = fmt.Sprintf("Remove the delete marker %s@%s?\nThe previous version becomes latest again (undelete).", key, short)
		label = fmt.Sprintf("undelete s3://%s/%s (remove marker @%s)", bucket, key, short)
	}
	m.modal.ShowConfirm(
		title,
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
					Label:  label,
					Status: transferpanel.StatusRunning,
					Cancel: cancel,
				}),
				versionview.DeleteVersionCmd(ctx, opt, bucket, key, versionID, id),
			)
		},
	)
	return nil
}

// refreshAfterOp returns the tea.Cmd that re-fetches the list touched by
// the completed op. Downloads/uploads don't need a refresh of the remote
// list, but deletes/copies/renames/mb/rb/sync do.
func (m *Model) refreshAfterOp(done transferpanel.TransferDoneMsg) tea.Cmd {
	// A local-filesystem op (dual-pane local delete) never changes the
	// remote listing: refresh the local pane only.
	if done.Local {
		return m.localList.Refresh()
	}
	var cmds []tea.Cmd
	switch done.Op {
	case transferpanel.OpDelete, transferpanel.OpCopy, transferpanel.OpRename,
		transferpanel.OpUpload:
		if m.state == state.ActiveObjectList {
			opt := m.objectListOptionFromState()
			m.objectlist.SetLoading(true)
			cmds = append(cmds, objectlist.FetchObjectListCmd(opt))
		}
	case transferpanel.OpMakeBucket, transferpanel.OpDeleteBucket:
		if m.state == state.ActiveBucketList {
			opt := m.bucketListOptionFromState()
			m.bucketList.SetLoading(true)
			cmds = append(cmds, bucketlist.FetchBucketListCmd(&opt))
		}
	case transferpanel.OpSync:
		// A sync may add/delete objects in the currently viewed listing.
		switch m.state {
		case state.ActiveObjectList:
			opt := m.objectListOptionFromState()
			m.objectlist.SetLoading(true)
			cmds = append(cmds, objectlist.FetchObjectListCmd(opt))
		case state.ActiveBucketList:
			opt := m.bucketListOptionFromState()
			m.bucketList.SetLoading(true)
			cmds = append(cmds, bucketlist.FetchBucketListCmd(&opt))
		}
	}
	// Downloads and syncs may have created files in the local pane's
	// directory; refresh it (cursor kept) while dual mode is showing it.
	if m.dualPane && (done.Op == transferpanel.OpDownload || done.Op == transferpanel.OpSync) {
		if cmd := m.localList.Refresh(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
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
// The single-pane flow passes the current s3://bucket/prefix as the
// default source (so the user can sync the current prefix to a local dir
// by typing the destination and hitting enter) and an empty destination.
// Dual-pane mode prefills src with the focused pane's location and dst
// with the other pane's (see promptDualSync); both stay editable. The
// default flags string is always empty (a default size-and-mtime sync).
func (m *Model) promptSync(defaultSrc, defaultDst string) tea.Cmd {
	// Capture the active profile's endpoint/path-style so the final
	// callback can build a storage.Storage. We resolve these up front
	// (rather than inside the callback) because the user could navigate
	// away while the modal is open.
	endpointURL, pathStyle, profile := m.syncConnParams()

	// The chained modals are reopened via ShowInputModalMsg rather than by
	// calling m.modal.Show inside the callbacks: the callbacks run against
	// a stale captured model, so the next modal must be opened message-style
	// on the live model.
	m.modal.Show(
		"Sync source (s3://bucket/prefix or local dir)",
		defaultSrc,
		func(src string) tea.Cmd {
			// Destination: empty for the user to type in single-pane (a
			// local path for s3→local, or s3://bucket/prefix/ for
			// local→s3 / s3→s3); the other pane's location in dual mode.
			return showInputModalCmd("Sync destination", defaultDst, func(dst string) tea.Cmd {
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

// syncConnParams resolves the active profile's endpoint/path-style and
// name for building a storage.Storage inside a sync Cmd. Resolved up front
// by the prompt flows (never inside a modal callback, which runs against a
// stale model).
func (m *Model) syncConnParams() (endpointURL string, pathStyle bool, profile string) {
	if p := m.profileList.GetSelectedProfile(); p != nil {
		endpointURL = p.EndpointURL
		pathStyle = shouldUsePathStyle(endpointURL)
		profile = m.selectedProfile
	}
	return endpointURL, pathStyle, profile
}

// startSyncCmd dispatches the user-typed sync flow ('s' key) with the
// default "sync src -> dst" row label.
func startSyncCmd(src, dst, flagsStr, endpointURL string, pathStyle bool, profile string) tea.Cmd {
	label := fmt.Sprintf("sync %s -> %s", src, dst)
	return syncTransferCmd(src, dst, label, syncmodal.ParseFlags(flagsStr), endpointURL, pathStyle, profile)
}

// syncTransferCmd dispatches one sync via syncmodal.NewCmd, plus the first
// tea.Every poll tick. The SyncPollMsg handler in tui.go re-arms the ticker
// while the sync is registered, so per-file progress keeps flowing to the
// panel. The row carries the sync context's CancelFunc so 'x' aborts it.
// Besides the 's' flow it backs the dual-pane directory copies, which pass
// zero Flags (a plain recursive one-way copy) and a "dir: ..." label.
//
// The storage.Storage is built lazily inside the sync Cmd (StorageFn):
// NewStorage resolves credentials and can block on network I/O, so it must
// not run on the Update goroutine.
func syncTransferCmd(src, dst, label string, flags syncmodal.Flags, endpointURL string, pathStyle bool, profile string) tea.Cmd {
	id := transferpanel.NewID()

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
		Label:      label,
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

	// A terminal too narrow for two readable panes drops dual mode
	// entirely (re-enter with 'l' after widening).
	if m.dualPane && m.width < minDualPaneWidth {
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
	// The full-screen overlays (help, history, transfers, versions) and
	// the floating p/m overlays get the whole canvas so they can lay
	// themselves out over the screen.
	m.help.SetSize(m.width, m.height)
	m.historyView.SetSize(m.width, m.height)
	m.transferView.SetSize(m.width, m.height)
	m.versionView.SetSize(m.width, m.height)
	m.contentView.SetSize(m.width, m.height)
	m.metaView.SetSize(m.width, m.height)
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
		files := append([]string{}, profilelist.DefaultSharedConfigFiles...)
		files = append(files, profilelist.DefaultSharedCredentialsFiles...)
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
