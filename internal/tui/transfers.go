package tui

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	s3store "github.com/LinPr/lazys3/internal/storage/s3"
	"github.com/LinPr/lazys3/internal/tui/components/objectlist"
	"github.com/LinPr/lazys3/internal/tui/components/transferpanel"
	"github.com/LinPr/lazys3/internal/tui/types"
)

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
// Directory prefixes are downloaded recursively via the sync engine.
func (m *Model) promptDownload() tea.Cmd {
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
	opt := m.objectListOptionFromState()
	bucket := m.selectedBucket

	// If any directories are selected, use the sync engine for recursive download.
	// Resolve conn before entering the modal callback — the model may be stale by then.
	if len(dirs) > 0 {
		conn := m.syncConnParams()
		title := fmt.Sprintf("Download %s to directory", transferCountPhrase(len(files), len(dirs)))
		m.modal.Show(title, ".", func(localDir string) tea.Cmd {
			syncs, skipped := remoteDirSyncs(dirs, bucket, localDir)
			if len(files) == 0 && len(syncs) == 0 {
				return errCmd(fmt.Errorf("download: %s", wildcardSkipNote(skipped)))
			}
			var cmds []tea.Cmd
			if len(skipped) > 0 {
				cmds = append(cmds, errCmd(fmt.Errorf("download: %s", wildcardSkipNote(skipped))))
			}
			if len(files) > 0 {
				cmds = append(cmds, downloadCmds(opt, bucket, files, func(o objectlist.Object) string {
					return filepath.Join(localDir, path.Base(o.Name()))
				}))
			}
			cmds = append(cmds, dirSyncCmds(syncs, conn)...)
			return tea.Batch(cmds...)
		})
		return nil
	}

	// Files-only path — unchanged behaviour.
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

			// Directory: one upload row per file, preserving sub-directory
			// structure under the destination prefix. The walk is synchronous
			// (just directory reads — no I/O) and mirrors how downloadCmds
			// builds one row per object. Each file gets its own *Progress so
			// the tick loop drives its bar independently.
			if fi, statErr := os.Stat(localPath); statErr == nil && fi.IsDir() {
				dirBase := path.Base(localPath)
				keyPrefix := dirBase + "/"
				if prefix != "" {
					p := prefix
					if !strings.HasSuffix(p, "/") {
						p += "/"
					}
					keyPrefix = p + keyPrefix
				}
				var cmds []tea.Cmd
				_ = filepath.Walk(localPath, func(filePath string, info os.FileInfo, err error) error {
					if err != nil || info.IsDir() {
						return err
					}
					rel, _ := filepath.Rel(localPath, filePath)
					fileKey := keyPrefix + filepath.ToSlash(rel)
					fid := transferpanel.NewID()
					fprog := transferpanel.NewProgress()
					fctx, fcancel := context.WithCancel(context.Background())
					cmds = append(cmds, tea.Sequence(
						addTransferCmd(transferpanel.Transfer{
							ID:       fid,
							Op:       transferpanel.OpUpload,
							Label:    fmt.Sprintf("%s -> s3://%s/%s", filePath, bucket, fileKey),
							Status:   transferpanel.StatusRunning,
							Progress: fprog,
							Cancel:   fcancel,
							// A cancelled multipart upload leaves its parts
							// behind (invisible, billable): remember where so
							// the cancel handler can abort them.
							CleanupTarget: &transferpanel.MultipartCleanup{Bucket: bucket, Prefix: fileKey},
						}),
						objectlist.UploadToKeyCmd(fctx, opt, filePath, fileKey, fid, fprog),
					))
					return nil
				})
				if len(cmds) == 0 {
					return nil
				}
				return tea.Batch(cmds...)
			}

			// Single file (or stat failed → UploadCmd surfaces the error).
			id := transferpanel.NewID()
			prog := transferpanel.NewProgress()
			ctx, cancel := context.WithCancel(context.Background())
			key := base
			if prefix != "" {
				p := prefix
				if !strings.HasSuffix(p, "/") {
					p += "/"
				}
				key = p + base
			}
			return tea.Sequence(
				addTransferCmd(transferpanel.Transfer{
					ID:       id,
					Op:       transferpanel.OpUpload,
					Label:    fmt.Sprintf("%s -> s3://%s/%s", localPath, bucket, key),
					Status:   transferpanel.StatusRunning,
					Progress: prog,
					Cancel:   cancel,
					// See the folder-upload branch above: parts of a
					// cancelled multipart upload must be aborted explicitly.
					CleanupTarget: &transferpanel.MultipartCleanup{Bucket: bucket, Prefix: key},
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
				prog := transferpanel.NewProgress()
				cmds = append(cmds, tea.Sequence(
					addTransferCmd(transferpanel.Transfer{
						ID:       id,
						Op:       transferpanel.OpDelete,
						Label:    fmt.Sprintf("delete dir: s3://%s/%s", bucket, prefix),
						Status:   transferpanel.StatusRunning,
						Cancel:   cancel,
						Progress: prog,
					}),
					objectlist.DeletePrefixCmd(ctx, opt, prefix, id, prog),
				))
			}
			return tea.Batch(cmds...)
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
		Profile:        opt.Profile,
		EndpointURL:    opt.EndpointURL,
		PathStyle:      opt.PathStyle,
		ConfigFile:     opt.ConfigFile,
		CredentialFile: opt.CredentialFile,
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
		Profile:        opt.Profile,
		EndpointURL:    opt.EndpointURL,
		PathStyle:      opt.PathStyle,
		ConfigFile:     opt.ConfigFile,
		CredentialFile: opt.CredentialFile,
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

// abortCancelledUpload returns the tea.Cmd that aborts the multipart parts
// a just-cancelled transfer may have left on the server. A cancelled
// multipart upload does not clean up after itself: the parts stay billed
// and are invisible to listings, so cancelling has to trigger the abort
// explicitly. A transfer with no CleanupTarget (a download, a copy, a sync)
// yields nil.
func (m *Model) abortCancelledUpload(cleanup *transferpanel.MultipartCleanup) tea.Cmd {
	if cleanup == nil || cleanup.Bucket == "" {
		return nil
	}
	opt := m.objectListOptionFromState()
	opt.S3Uri = fmt.Sprintf("s3://%s/%s", cleanup.Bucket, cleanup.Prefix)
	conn := m.syncConnParams()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cli, err := s3store.NewS3Client(ctx, syncS3Option(conn))
		if err != nil {
			return types.ErrMsg{Err: fmt.Errorf("cleanup multipart uploads: %w", err)}
		}
		n, err := cli.AbortStaleMultipartUploads(ctx, cleanup.Bucket, cleanup.Prefix)
		if err != nil {
			return types.ErrMsg{Err: fmt.Errorf("cleanup multipart uploads: %w", err)}
		}
		if n > 0 {
			return types.InfoMsg{Text: fmt.Sprintf("aborted %d interrupted multipart upload(s) in s3://%s/%s", n, cleanup.Bucket, cleanup.Prefix)}
		}
		return nil
	}
}
