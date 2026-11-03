package s3store

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

// MultipartUploadInfo describes one in-progress multipart upload. The
// parts of such an upload are invisible to ListObjects but are billed as
// storage until the upload is completed or aborted.
type MultipartUploadInfo struct {
	Key      string
	UploadID string
	// Initiated is the human-readable start time ("" when the service did
	// not report one).
	Initiated string
}

// ListMultipartUploads returns the in-progress multipart uploads under a
// prefix. Interrupted uploads (cancelled from the TUI, killed process,
// dropped connection) leave their parts on the server: they are invisible
// to ListObjects but are billed as storage, so the UI needs a way to see
// and remove them.
func (s3store *S3Store) ListMultipartUploads(ctx context.Context, bucket, prefix string) ([]MultipartUploadInfo, error) {
	var out []MultipartUploadInfo
	paginator := s3.NewListMultipartUploadsPaginator(s3store.client, &s3.ListMultipartUploadsInput{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			// Some S3-compatible implementations (gofakes3 among them)
			// answer an empty ListMultipartUploads with NoSuchUpload
			// instead of an empty list. For a "what is in flight here"
			// query that means the same thing, so treat it as none —
			// otherwise a clean prefix looks like a failure. NoSuchBucket
			// and every other error still propagate.
			var apiErr smithy.APIError
			if errors.As(err, &apiErr) && apiErr.ErrorCode() == "NoSuchUpload" {
				return out, nil
			}
			return nil, err
		}
		for _, u := range page.Uploads {
			info := MultipartUploadInfo{}
			if u.Key != nil {
				info.Key = *u.Key
			}
			if u.UploadId != nil {
				info.UploadID = *u.UploadId
			}
			if u.Initiated != nil {
				info.Initiated = u.Initiated.Format("2006-01-02 15:04:05")
			}
			out = append(out, info)
		}
	}
	return out, nil
}

// AbortMultipartUpload discards an in-progress upload and frees its parts.
func (s3store *S3Store) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	_, err := s3store.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	})
	return err
}

// AbortStaleMultipartUploads aborts every in-progress upload under prefix,
// returning how many were aborted. This is the cleanup hook for cancelled
// transfers: the TUI records the (bucket, key) it cancelled and calls this
// so no billable parts are left behind.
func (s3store *S3Store) AbortStaleMultipartUploads(ctx context.Context, bucket, prefix string) (int, error) {
	uploads, err := s3store.ListMultipartUploads(ctx, bucket, prefix)
	if err != nil {
		return 0, err
	}
	aborted := 0
	var firstErr error
	for _, u := range uploads {
		if err := s3store.AbortMultipartUpload(ctx, bucket, u.Key, u.UploadID); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		aborted++
	}
	return aborted, firstErr
}
