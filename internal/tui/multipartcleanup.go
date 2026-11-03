package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	s3store "github.com/LinPr/lazys3/internal/storage/s3"

	tea "charm.land/bubbletea/v2"
	"github.com/LinPr/lazys3/internal/tui/components/objectlist"
	"github.com/LinPr/lazys3/internal/tui/types"
)

// promptPresignUpload opens the expiry modal for a presigned PUT URL: the
// counterpart of 'Y' for the "let someone upload into this prefix" case.
// Unlike a download URL it names a NEW object, so the modal asks for both
// the key and the expiry.
func (m *Model) promptPresignUpload() tea.Cmd {
	if m.selectedBucket == "" {
		return errCmd(fmt.Errorf("presign upload: open a bucket first"))
	}
	prefix := m.selectedObject
	m.modal.Show(
		fmt.Sprintf("Presigned PUT: key under s3://%s/%s (then expiry, e.g. 1h)", m.selectedBucket, prefix),
		prefix+"upload.bin",
		func(key string) tea.Cmd {
			key = strings.TrimSpace(key)
			if key == "" {
				return errCmd(fmt.Errorf("presign upload: empty key"))
			}
			return showInputModalCmd("Expiry (1s..168h)", "1h", func(expiryStr string) tea.Cmd {
				opt := m.objectListOptionFromState()
				return objectlist.PresignPutCmd(opt, key, expiryStr)
			})
		},
	)
	return nil
}

// promptMultipartCleanup lists the interrupted multipart uploads under the
// current prefix and asks before aborting them. Their parts never appear in
// a listing but are billed as storage until aborted, so this is the only
// place a user can see and reclaim them.
func (m *Model) promptMultipartCleanup() tea.Cmd {
	if m.selectedBucket == "" {
		return errCmd(fmt.Errorf("multipart cleanup: open a bucket first"))
	}
	bucket, prefix := m.selectedBucket, m.selectedObject
	conn := m.syncConnParams()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cli, err := s3store.NewS3Client(ctx, syncS3Option(conn))
		if err != nil {
			return types.ErrMsg{Err: err}
		}
		uploads, err := cli.ListMultipartUploads(ctx, bucket, prefix)
		if err != nil {
			return types.ErrMsg{Err: fmt.Errorf("list multipart uploads: %w", err)}
		}
		return multipartListMsg{Bucket: bucket, Prefix: prefix, Uploads: uploads}
	}
}

// multipartListMsg carries the in-progress uploads back to the live model,
// which opens the confirm modal (a modal cannot be opened from inside a
// Cmd, which runs off the Update goroutine).
type multipartListMsg struct {
	Bucket  string
	Prefix  string
	Uploads []s3store.MultipartUploadInfo
}

// handleMultipartList opens the cleanup confirm for the listed uploads.
func (m *Model) handleMultipartList(msg multipartListMsg) tea.Cmd {
	if len(msg.Uploads) == 0 {
		m.statusBar.SetInfo(fmt.Sprintf("no interrupted multipart uploads under s3://%s/%s", msg.Bucket, msg.Prefix))
		return nil
	}
	var body strings.Builder
	fmt.Fprintf(&body, "%d interrupted multipart upload(s) under s3://%s/%s\n\n", len(msg.Uploads), msg.Bucket, msg.Prefix)
	for i, u := range msg.Uploads {
		if i == 8 {
			fmt.Fprintf(&body, "... and %d more\n", len(msg.Uploads)-8)
			break
		}
		fmt.Fprintf(&body, "  %s  (started %s)\n", u.Key, u.Initiated)
	}
	body.WriteString("\nTheir parts are invisible to listings but billed as storage. Abort them?")
	conn := m.syncConnParams()
	bucket, prefix := msg.Bucket, msg.Prefix
	m.modal.ShowConfirm("Abort interrupted uploads", body.String(), func() tea.Cmd {
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			cli, err := s3store.NewS3Client(ctx, syncS3Option(conn))
			if err != nil {
				return types.ErrMsg{Err: err}
			}
			n, err := cli.AbortStaleMultipartUploads(ctx, bucket, prefix)
			if err != nil {
				return types.ErrMsg{Err: fmt.Errorf("aborted %d, then: %w", n, err)}
			}
			return types.InfoMsg{Text: fmt.Sprintf("aborted %d interrupted multipart upload(s)", n)}
		}
	})
	return nil
}
