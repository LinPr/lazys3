//go:build e2e

package e2e

import (
	"context"
	"testing"

	s3store "github.com/LinPr/lazys3/internal/storage/s3"
)

// TestE2E_ListAndAbortMultipartUploads covers the cleanup path for
// interrupted uploads: their parts never appear in a listing but are billed
// as storage, so the UI has to be able to find and abort them.
func TestE2E_ListAndAbortMultipartUploads(t *testing.T) {
	endpoint := endpointFor(t)
	bucket := s3BucketFromTestName(t)
	client := s3Client(t, endpoint)
	createBucket(t, client, bucket)

	store, err := s3store.NewS3Client(context.Background(), s3StoreOptionFor(t, endpoint))
	if err != nil {
		t.Fatalf("NewS3Client: %v", err)
	}

	// No uploads to begin with.
	uploads, err := store.ListMultipartUploads(context.Background(), bucket, "prefix/")
	if err != nil {
		t.Fatalf("ListMultipartUploads: %v", err)
	}
	if len(uploads) != 0 {
		t.Fatalf("initial uploads = %d, want 0", len(uploads))
	}

	// Aborting a prefix with nothing in flight is a no-op, not an error.
	n, err := store.AbortStaleMultipartUploads(context.Background(), bucket, "prefix/")
	if err != nil {
		t.Fatalf("AbortStaleMultipartUploads on empty prefix: %v", err)
	}
	if n != 0 {
		t.Errorf("aborted = %d, want 0", n)
	}
}

// TestE2E_AbortMultipartUploadRemovesIt pins the round trip required by the
// 'x'-during-upload path: start an upload through the SDK, confirm it is
// visible, abort it, confirm it is gone.
func TestE2E_AbortMultipartUploadRemovesIt(t *testing.T) {
	endpoint := endpointFor(t)
	bucket := s3BucketFromTestName(t)
	client := s3Client(t, endpoint)
	createBucket(t, client, bucket)

	ctx := context.Background()
	store, err := s3store.NewS3Client(ctx, s3StoreOptionFor(t, endpoint))
	if err != nil {
		t.Fatalf("NewS3Client: %v", err)
	}

	uploadID := createMultipartUpload(t, client, bucket, "dir/big.bin")
	uploads, err := store.ListMultipartUploads(ctx, bucket, "dir/")
	if err != nil {
		t.Fatalf("ListMultipartUploads: %v", err)
	}
	if len(uploads) != 1 || uploads[0].Key != "dir/big.bin" || uploads[0].UploadID != uploadID {
		t.Fatalf("uploads = %+v, want the one we started (%s)", uploads, uploadID)
	}

	if err := store.AbortMultipartUpload(ctx, bucket, "dir/big.bin", uploadID); err != nil {
		t.Fatalf("AbortMultipartUpload: %v", err)
	}

	uploads, err = store.ListMultipartUploads(ctx, bucket, "dir/")
	if err != nil {
		t.Fatalf("ListMultipartUploads after abort: %v", err)
	}
	if len(uploads) != 0 {
		t.Fatalf("uploads after abort = %+v, want none", uploads)
	}
}

// TestE2E_AbortStaleMultipartUploadsScopesByPrefix pins that the bulk abort
// only touches the requested prefix: an in-flight upload under a sibling
// prefix must survive, or a cleanup triggered from one directory would
// destroy an unrelated transfer.
func TestE2E_AbortStaleMultipartUploadsScopesByPrefix(t *testing.T) {
	endpoint := endpointFor(t)
	bucket := s3BucketFromTestName(t)
	client := s3Client(t, endpoint)
	createBucket(t, client, bucket)

	ctx := context.Background()
	store, err := s3store.NewS3Client(ctx, s3StoreOptionFor(t, endpoint))
	if err != nil {
		t.Fatalf("NewS3Client: %v", err)
	}

	createMultipartUpload(t, client, bucket, "keep/a.bin")
	createMultipartUpload(t, client, bucket, "drop/b.bin")
	createMultipartUpload(t, client, bucket, "drop/c.bin")

	n, err := store.AbortStaleMultipartUploads(ctx, bucket, "drop/")
	if err != nil {
		t.Fatalf("AbortStaleMultipartUploads: %v", err)
	}
	if n != 2 {
		t.Errorf("aborted = %d, want 2", n)
	}

	remaining, err := store.ListMultipartUploads(ctx, bucket, "")
	if err != nil {
		t.Fatalf("ListMultipartUploads: %v", err)
	}
	if len(remaining) != 1 || remaining[0].Key != "keep/a.bin" {
		t.Fatalf("remaining = %+v, want only keep/a.bin", remaining)
	}
}
