package tui

import (
	"context"
	"fmt"
	"path"

	tea "charm.land/bubbletea/v2"
	"github.com/LinPr/lazys3/internal/tui/components/objectlist"
	"github.com/LinPr/lazys3/internal/tui/components/transferpanel"
	"github.com/LinPr/lazys3/internal/tui/components/versionview"
	"github.com/LinPr/lazys3/internal/tui/state"
)

// handleVersionsKey dispatches the state-dependent 'v' key: the versions
// overlay in the object list, the bucket-versioning toggle flow in the
// bucket list, a no-op anywhere else (profile list).
func (m *Model) handleVersionsKey() tea.Cmd {
	switch m.state {
	case state.ActiveObjectList:
		return m.handleVersionsOpen()
	case state.ActiveBucketList:
		return m.handleVersioningToggle()
	}
	return nil
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

// handleVersioningToggle ('v' in the bucket list) kicks off the async
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
		Profile:        bopt.Profile,
		EndpointURL:    bopt.EndpointURL,
		PathStyle:      bopt.PathStyle,
		ConfigFile:     bopt.ConfigFile,
		CredentialFile: bopt.CredentialFile,
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
