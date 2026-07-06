package versionview

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea/v2"

	"github.com/LinPr/lazys3/internal/storage"
	fsstore "github.com/LinPr/lazys3/internal/storage/fs"
	s3store "github.com/LinPr/lazys3/internal/storage/s3"
	"github.com/LinPr/lazys3/internal/tui/components/objectlist"
	"github.com/LinPr/lazys3/internal/tui/components/transferpanel"
)

// fetchTimeout bounds the overlay's listing fetch and the bucket-status
// probe so a hung endpoint cannot leave the overlay loading forever.
const fetchTimeout = 30 * time.Second

// s3Option mirrors objectlist's ops wiring: the overlay's ops target the
// same endpoint/profile/path-style as the listing that opened it.
func s3Option(o objectlist.Option) s3store.S3Option {
	return s3store.S3Option{
		UsePathStyle: o.PathStyle,
		Region:       o.Region,
		Profile:      o.Profile,
		Endpoint:     o.EndpointURL,
	}
}

// LoadedMsg carries the version listing fetched by fetchCmd (newest first,
// delete markers included) plus the bucket's versioning status for the
// disabled-versioning hint line. StatusKnown is false when the status probe
// itself failed (the hint is then suppressed rather than guessed). Seq
// echoes the fetch request's sequence number so Update can drop listings
// from a superseded fetch.
type LoadedMsg struct {
	Seq         int
	Versions    []s3store.ObjectVersion
	Status      string
	StatusKnown bool
	Err         error
}

// fetchCmd lists the version history of bucket/key off the Update
// goroutine. The bucket-status probe is best-effort: its failure only
// drops the hint line, never the listing.
func fetchCmd(opt objectlist.Option, bucket, key string, seq int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		cli, err := s3store.NewS3Client(ctx, s3Option(opt))
		if err != nil {
			return LoadedMsg{Seq: seq, Err: err}
		}
		status, serr := cli.GetBucketVersioning(ctx, bucket)
		versions, err := cli.ListObjectVersions(ctx, bucket, key)
		if err != nil {
			return LoadedMsg{Seq: seq, Status: status, StatusKnown: serr == nil, Err: err}
		}
		return LoadedMsg{Seq: seq, Versions: versions, Status: status, StatusKnown: serr == nil}
	}
}

// DownloadVersionCmd downloads one specific version of bucket/key to
// localPath (a directory destination stores basename(key) inside it, "-"
// streams to stdout — same semantics as the plain download path), reporting
// byte progress into prog (may be nil). ctx is owned by the caller:
// cancelling it aborts the transfer.
func DownloadVersionCmd(ctx context.Context, opt objectlist.Option, bucket, key, versionID, localPath, transferID string, prog *transferpanel.Progress) tea.Cmd {
	return func() tea.Msg {
		label := fmt.Sprintf("s3://%s/%s@%s -> %s", bucket, key, ShortID(versionID), localPath)
		done := func(err error) tea.Msg {
			return transferpanel.TransferDoneMsg{ID: transferID, Err: err, Op: transferpanel.OpDownload, Label: label}
		}
		stOpt := storage.NewStorageOption(s3Option(opt), fsstore.LocalOption{})
		st, err := storage.NewStorage(ctx, *stOpt)
		if err != nil {
			return done(err)
		}
		var pf storage.ProgressFunc
		if prog != nil {
			pf = prog.Report
		}
		return done(st.DownloadFileVersionWithProgress(ctx, bucket, key, versionID, localPath, pf))
	}
}

// RestoreVersionCmd makes versionID the latest version of bucket/key via a
// server-side copy in the version's own storage class. Under Enabled
// versioning the old version stays in the history; under Suspended it
// overwrites the null version (the confirm modal warns about this).
func RestoreVersionCmd(ctx context.Context, opt objectlist.Option, bucket, key, versionID, storageClass, transferID string) tea.Cmd {
	return func() tea.Msg {
		label := fmt.Sprintf("restore s3://%s/%s@%s", bucket, key, ShortID(versionID))
		done := func(err error) tea.Msg {
			return transferpanel.TransferDoneMsg{ID: transferID, Err: err, Op: transferpanel.OpCopy, Label: label}
		}
		cli, err := s3store.NewS3Client(ctx, s3Option(opt))
		if err != nil {
			return done(err)
		}
		return done(cli.RestoreObjectVersion(ctx, bucket, key, versionID, storageClass))
	}
}

// DeleteVersionCmd permanently removes one version (or delete marker) of
// bucket/key. Removing the current delete marker undeletes the object.
func DeleteVersionCmd(ctx context.Context, opt objectlist.Option, bucket, key, versionID, transferID string) tea.Cmd {
	return func() tea.Msg {
		label := fmt.Sprintf("delete version s3://%s/%s@%s", bucket, key, ShortID(versionID))
		done := func(err error) tea.Msg {
			return transferpanel.TransferDoneMsg{ID: transferID, Err: err, Op: transferpanel.OpDelete, Label: label}
		}
		cli, err := s3store.NewS3Client(ctx, s3Option(opt))
		if err != nil {
			return done(err)
		}
		return done(cli.DeleteObjectVersion(ctx, bucket, key, versionID))
	}
}

// BucketStatusMsg carries the result of BucketStatusCmd: the bucket's
// current versioning status, echoed with the Option so the confirm-modal
// flow can build the toggle Cmd.
type BucketStatusMsg struct {
	Opt    objectlist.Option
	Bucket string
	Status string
	Err    error
}

// BucketStatusCmd fetches the bucket's versioning status off the Update
// goroutine. Errors (e.g. NotImplemented endpoints) surface on the status
// bar via the TUI's handler.
func BucketStatusCmd(opt objectlist.Option, bucket string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		cli, err := s3store.NewS3Client(ctx, s3Option(opt))
		if err != nil {
			return BucketStatusMsg{Opt: opt, Bucket: bucket, Err: err}
		}
		status, err := cli.GetBucketVersioning(ctx, bucket)
		return BucketStatusMsg{Opt: opt, Bucket: bucket, Status: status, Err: err}
	}
}

// PutVersioningCmd sets the bucket's versioning status to Enabled (true)
// or Suspended (false).
func PutVersioningCmd(ctx context.Context, opt objectlist.Option, bucket string, enable bool, transferID string) tea.Cmd {
	return func() tea.Msg {
		target := "Suspended"
		if enable {
			target = "Enabled"
		}
		label := fmt.Sprintf("versioning s3://%s -> %s", bucket, target)
		done := func(err error) tea.Msg {
			return transferpanel.TransferDoneMsg{ID: transferID, Err: err, Op: transferpanel.OpVersioning, Label: label}
		}
		cli, err := s3store.NewS3Client(ctx, s3Option(opt))
		if err != nil {
			return done(err)
		}
		return done(cli.PutBucketVersioning(ctx, bucket, enable))
	}
}
