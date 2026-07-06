//go:build e2e

package e2e

import (
	"context"
	"testing"

	s3store "github.com/LinPr/lazys3/internal/storage/s3"
)

// TestE2E_CopyS3ToS3 verifies that S3Store.CopyToBucket performs a
// server-side copy between two buckets.
func TestE2E_CopyS3ToS3(t *testing.T) {

	endpoint := s3ServerEndpoint(t)
	client := s3Client(t, endpoint)

	srcBucket := s3BucketFromTestName(t) + "-src"
	dstBucket := s3BucketFromTestName(t) + "-dst"
	createBucket(t, client, srcBucket)
	createBucket(t, client, dstBucket)

	content := "server-side-copy"
	putObject(t, client, srcBucket, "a.txt", content)

	store, err := s3store.NewS3Client(context.Background(), s3StoreOptionFor(t, endpoint))
	if err != nil {
		t.Fatalf("NewS3Client: %v", err)
	}

	if err := store.CopyToBucket(context.Background(), srcBucket, dstBucket, "a.txt"); err != nil {
		t.Fatalf("CopyToBucket: %v", err)
	}
	if !objectExists(t, client, dstBucket, "a.txt") {
		t.Fatalf("destination object not found")
	}
	if got := objectContent(t, client, dstBucket, "a.txt"); got != content {
		t.Errorf("dst content = %q, want %q", got, content)
	}
}

// TestE2E_CopyWithinBucket verifies that S3Store.CopyToFolder copies an
// object to a subfolder within the same bucket.
func TestE2E_CopyWithinBucket(t *testing.T) {

	endpoint := s3ServerEndpoint(t)
	client := s3Client(t, endpoint)
	bucket := s3BucketFromTestName(t)
	createBucket(t, client, bucket)

	content := "within-bucket"
	putObject(t, client, bucket, "a.txt", content)

	store, err := s3store.NewS3Client(context.Background(), s3StoreOptionFor(t, endpoint))
	if err != nil {
		t.Fatalf("NewS3Client: %v", err)
	}

	if err := store.CopyToFolder(context.Background(), bucket, "a.txt", "archive"); err != nil {
		t.Fatalf("CopyToFolder: %v", err)
	}
	if got := objectContent(t, client, bucket, "archive/a.txt"); got != content {
		t.Errorf("archive/a.txt = %q, want %q", got, content)
	}
}
