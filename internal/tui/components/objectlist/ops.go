package objectlist

import (
	"context"
	"fmt"
	"path"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/LinPr/lazys3/internal/storage"
	fsstore "github.com/LinPr/lazys3/internal/storage/fs"
	s3store "github.com/LinPr/lazys3/internal/storage/s3"
	"github.com/LinPr/lazys3/internal/storage/uri"
	"github.com/LinPr/lazys3/internal/tui/components/transferpanel"
)

// s3OptionFromListOption builds the s3store.S3Option that the file-op
// commands use to construct a fresh S3Store. The fields mirror what
// FetchObjectListCmd uses, so file ops target the same endpoint/profile as
// the listing itself.
func s3OptionFromListOption(o Option) s3store.S3Option {
	return s3store.S3Option{
		UsePathStyle:   o.PathStyle,
		Region:         o.Region,
		Profile:        o.Profile,
		Endpoint:       o.EndpointURL,
		ConfigFile:     o.ConfigFile,
		CredentialFile: o.CredentialFile,
	}
}

// newStorageFromOption builds a storage.Storage (S3 + local FS) so the
// download/upload commands can use the WithProgress variants.
func newStorageFromOption(ctx context.Context, o Option) (*storage.Storage, error) {
	stOpt := storage.NewStorageOption(s3OptionFromListOption(o), fsstore.LocalOption{})
	return storage.NewStorage(ctx, *stOpt)
}

// progressFunc adapts an optional *transferpanel.Progress to the storage
// layer's callback type. The callback runs on worker goroutines; Progress
// is atomic and tracks the transferred count max-so-far.
func progressFunc(prog *transferpanel.Progress) storage.ProgressFunc {
	if prog == nil {
		return nil
	}
	return prog.Report
}

// bucketFromOption parses the bucket out of the Option's S3Uri. The S3Uri
// is the URI the objectlist was fetched with (e.g.
// "s3://my-bucket/some/prefix/"), so the bucket is the first path segment.
func bucketFromOption(o Option) (string, string, error) {
	if o.S3Uri == "" {
		return "", "", fmt.Errorf("s3uri is empty")
	}
	parsed, err := uri.ParseS3Uri(o.S3Uri)
	if err != nil {
		return "", "", err
	}
	return parsed.GetBucket(), parsed.GetKey(), nil
}

// DownloadCmd downloads a single object to a local path, reporting byte
// progress into prog (may be nil). ctx is owned by the caller: cancelling
// it aborts the transfer and the resulting TransferDoneMsg carries
// context.Canceled, which the panel renders as "canceled".
func DownloadCmd(ctx context.Context, opt Option, key, localPath, transferID string, prog *transferpanel.Progress) tea.Cmd {
	return func() tea.Msg {
		done := func(err error) tea.Msg {
			return transferpanel.TransferDoneMsg{ID: transferID, Err: err, Op: transferpanel.OpDownload, Label: labelForOp(opt, key, "->", localPath)}
		}
		bucket, _, err := bucketFromOption(opt)
		if err != nil {
			return done(err)
		}
		st, err := newStorageFromOption(ctx, opt)
		if err != nil {
			return done(err)
		}
		return done(st.DownloadFileWithProgress(ctx, bucket, key, localPath, progressFunc(prog)))
	}
}

// UploadCmd uploads a local file to the current prefix + the file's
// basename, reporting byte progress into prog (may be nil).
func UploadCmd(ctx context.Context, opt Option, localPath, transferID string, prog *transferpanel.Progress) tea.Cmd {
	return func() tea.Msg {
		bucket, prefix, err := bucketFromOption(opt)
		if err != nil {
			return transferpanel.TransferDoneMsg{ID: transferID, Err: err, Op: transferpanel.OpUpload}
		}
		base := path.Base(localPath)
		key := base
		if prefix != "" {
			// Ensure the key lands under the current prefix. Strip a
			// leading slash so we don't get "//" in the key.
			prefix = strings.TrimPrefix(prefix, "/")
			if prefix != "" && !strings.HasSuffix(prefix, "/") {
				prefix += "/"
			}
			key = prefix + base
		}
		done := func(err error) tea.Msg {
			return transferpanel.TransferDoneMsg{ID: transferID, Err: err, Op: transferpanel.OpUpload, Label: labelForOp(opt, localPath, "->", key)}
		}
		st, err := newStorageFromOption(ctx, opt)
		if err != nil {
			return done(err)
		}
		_, err = st.UploadFileWithProgress(ctx, localPath, bucket, key, progressFunc(prog))
		return done(err)
	}
}

// DeleteCmd deletes the given keys from the bucket. The keys slice carries
// the whole multi-selection in display order.
func DeleteCmd(ctx context.Context, opt Option, keys []string, transferID string) tea.Cmd {
	return func() tea.Msg {
		done := func(err error) tea.Msg {
			return transferpanel.TransferDoneMsg{ID: transferID, Err: err, Op: transferpanel.OpDelete, Label: labelForOp(opt, strings.Join(keys, ","), "delete", "")}
		}
		bucket, _, err := bucketFromOption(opt)
		if err != nil {
			return done(err)
		}
		cli, err := s3store.NewS3Client(ctx, s3OptionFromListOption(opt))
		if err != nil {
			return done(err)
		}
		return done(cli.DeleteObjects(ctx, bucket, keys))
	}
}

// DeletePrefixCmd recursively deletes every object under a directory
// prefix: a no-delimiter listing (paginated, ctx checked between pages)
// followed by DeleteObjects batches of up to 1000 keys (ctx checked
// between batches). The done message carries a final "N object(s)
// deleted" note for the transfer row — an empty prefix reports
// "0 object(s) deleted"; per-batch note updates are a later iteration.
func DeletePrefixCmd(ctx context.Context, opt Option, prefix, transferID string) tea.Cmd {
	return func() tea.Msg {
		done := func(n int, err error) tea.Msg {
			return transferpanel.TransferDoneMsg{
				ID:    transferID,
				Err:   err,
				Op:    transferpanel.OpDelete,
				Label: labelForOp(opt, prefix, "delete dir", ""),
				Note:  fmt.Sprintf("%d object(s) deleted", n),
			}
		}
		bucket, _, err := bucketFromOption(opt)
		if err != nil {
			return done(0, err)
		}
		cli, err := s3store.NewS3Client(ctx, s3OptionFromListOption(opt))
		if err != nil {
			return done(0, err)
		}
		n, err := cli.DeletePrefix(ctx, bucket, prefix)
		return done(n, err)
	}
}

// CopyCmd copies a single object to a destination bucket/key. The dstS3Uri
// is parsed with uri.ParseS3Uri. Copying an object onto itself is rejected
// (the S3 API refuses an in-place copy without a metadata directive).
func CopyCmd(ctx context.Context, opt Option, srcKey, dstS3Uri, transferID string) tea.Cmd {
	return func() tea.Msg {
		done := func(err error) tea.Msg {
			return transferpanel.TransferDoneMsg{ID: transferID, Err: err, Op: transferpanel.OpCopy, Label: labelForOp(opt, srcKey, "->", dstS3Uri)}
		}
		srcBucket, _, err := bucketFromOption(opt)
		if err != nil {
			return done(err)
		}
		dstBucket, dstKey, err := parseDst(dstS3Uri)
		if err != nil {
			return done(err)
		}
		if dstBucket == srcBucket && dstKey == srcKey {
			return done(fmt.Errorf("copy: source and destination are identical (s3://%s/%s)", srcBucket, srcKey))
		}
		cli, err := s3store.NewS3Client(ctx, s3OptionFromListOption(opt))
		if err != nil {
			return done(err)
		}
		return done(cli.CopyObject(ctx, srcBucket, srcKey, dstBucket, dstKey))
	}
}

// RenameCmd implements rename as copy+delete. It emits a single
// TransferDoneMsg covering both steps; the copy must succeed before the
// delete runs.
func RenameCmd(ctx context.Context, opt Option, srcKey, dstS3Uri, transferID string) tea.Cmd {
	return func() tea.Msg {
		done := func(err error) tea.Msg {
			return transferpanel.TransferDoneMsg{ID: transferID, Err: err, Op: transferpanel.OpRename, Label: labelForOp(opt, srcKey, "->", dstS3Uri)}
		}
		srcBucket, _, err := bucketFromOption(opt)
		if err != nil {
			return done(err)
		}
		dstBucket, dstKey, err := parseDst(dstS3Uri)
		if err != nil {
			return done(err)
		}
		if dstBucket == srcBucket && dstKey == srcKey {
			return done(fmt.Errorf("rename: source and destination are identical (s3://%s/%s)", srcBucket, srcKey))
		}
		cli, err := s3store.NewS3Client(ctx, s3OptionFromListOption(opt))
		if err != nil {
			return done(err)
		}
		if err := cli.CopyObject(ctx, srcBucket, srcKey, dstBucket, dstKey); err != nil {
			return done(fmt.Errorf("rename copy step: %w", err))
		}
		if err := cli.DeleteObjects(ctx, srcBucket, []string{srcKey}); err != nil {
			return done(fmt.Errorf("rename delete step: %w", err))
		}
		return done(nil)
	}
}

// MakeBucketCmd creates a new bucket. The region defaults to the option's
// region (which itself defaults to us-east-1 inside NewS3Client).
func MakeBucketCmd(ctx context.Context, opt Option, bucket, region, transferID string) tea.Cmd {
	return func() tea.Msg {
		done := func(err error) tea.Msg {
			return transferpanel.TransferDoneMsg{ID: transferID, Err: err, Op: transferpanel.OpMakeBucket, Label: "mb s3://" + bucket}
		}
		cli, err := s3store.NewS3Client(ctx, s3OptionFromListOption(opt))
		if err != nil {
			return done(err)
		}
		if region == "" {
			region = opt.Region
		}
		return done(cli.CreateBucket(ctx, bucket, region))
	}
}

// DeleteBucketCmd removes an empty bucket. The S3 API rejects non-empty
// buckets; the error is surfaced to the user via the transfer panel.
func DeleteBucketCmd(ctx context.Context, opt Option, bucket, transferID string) tea.Cmd {
	return func() tea.Msg {
		done := func(err error) tea.Msg {
			return transferpanel.TransferDoneMsg{ID: transferID, Err: err, Op: transferpanel.OpDeleteBucket, Label: "rb s3://" + bucket}
		}
		cli, err := s3store.NewS3Client(ctx, s3OptionFromListOption(opt))
		if err != nil {
			return done(err)
		}
		return done(cli.DeleteBucket(ctx, bucket))
	}
}

// parseDst parses a destination s3://bucket/key URI and validates the
// bucket is non-empty.
func parseDst(dstS3Uri string) (bucket, key string, err error) {
	parsed, err := uri.ParseS3Uri(dstS3Uri)
	if err != nil {
		return "", "", err
	}
	if parsed.GetBucket() == "" {
		return "", "", fmt.Errorf("destination bucket is empty in %q", dstS3Uri)
	}
	return parsed.GetBucket(), parsed.GetKey(), nil
}

// labelForOp builds a human-readable label for a transfer row.
func labelForOp(_ Option, a, sep, b string) string {
	if b == "" {
		return fmt.Sprintf("%s %s", a, sep)
	}
	return fmt.Sprintf("%s %s %s", a, sep, b)
}

// CurrentSelectedKeys returns the keys the delete/copy/rename ops should
// target: the multi-selection in display order when non-empty, otherwise
// the single highlighted item.
func CurrentSelectedKeys(m Model) []string {
	if keys := m.SelectedKeys(); len(keys) > 0 {
		return keys
	}
	if obj := m.GetSelectedObject(); obj != nil {
		return []string{obj.Name()}
	}
	return nil
}
