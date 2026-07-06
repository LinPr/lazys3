//go:build e2e

package e2e

import (
	"context"
	"testing"

	s3store "github.com/LinPr/lazys3/internal/storage/s3"
)

// TestE2E_RemoveBucketEmpty verifies that S3Store.DeleteBucket removes an
// empty bucket.
func TestE2E_RemoveBucketEmpty(t *testing.T) {

	endpoint := s3ServerEndpoint(t)
	client := s3Client(t, endpoint)
	bucket := s3BucketFromTestName(t)
	createBucket(t, client, bucket)

	store, err := s3store.NewS3Client(context.Background(), s3StoreOptionFor(t, endpoint))
	if err != nil {
		t.Fatalf("NewS3Client: %v", err)
	}

	if err := store.DeleteBucket(context.Background(), bucket); err != nil {
		t.Fatalf("DeleteBucket: %v", err)
	}

	// The bucket should be gone. BucketExists should return false.
	exists, _ := store.BucketExists(context.Background(), bucket)
	if exists {
		t.Errorf("bucket %q should have been removed", bucket)
	}
}

// TestE2E_RemoveBucketNonEmpty verifies that DeleteBucket on a non-empty
// bucket fails (BucketNotEmpty).
func TestE2E_RemoveBucketNonEmpty(t *testing.T) {

	endpoint := s3ServerEndpoint(t)
	client := s3Client(t, endpoint)
	bucket := s3BucketFromTestName(t)
	createBucket(t, client, bucket)

	putObject(t, client, bucket, "a.txt", "a")

	store, err := s3store.NewS3Client(context.Background(), s3StoreOptionFor(t, endpoint))
	if err != nil {
		t.Fatalf("NewS3Client: %v", err)
	}

	// Regardless of the outcome, the bucket (and its object) is removed by
	// createBucket's cleanup, which empties the bucket with per-object
	// deletes and retries DeleteBucket until it is gone.
	if err := store.DeleteBucket(context.Background(), bucket); err == nil {
		t.Errorf("DeleteBucket on non-empty bucket should have failed")
	}
}
