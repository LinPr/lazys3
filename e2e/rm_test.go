//go:build e2e

package e2e

import (
	"context"
	"testing"

	s3store "github.com/LinPr/lazys3/internal/storage/s3"
)

// TestE2E_RmDeletesObject verifies that S3Store.DeleteObjects removes the
// specified object.
func TestE2E_RmDeletesObject(t *testing.T) {

	endpoint := s3ServerEndpoint(t)
	client := s3Client(t, endpoint)
	bucket := s3BucketFromTestName(t)
	createBucket(t, client, bucket)

	putObject(t, client, bucket, "file.txt", "content")
	if !objectExists(t, client, bucket, "file.txt") {
		t.Fatalf("setup: object should exist")
	}

	store, err := s3store.NewS3Client(context.Background(), s3StoreOptionFor(t, endpoint))
	if err != nil {
		t.Fatalf("NewS3Client: %v", err)
	}

	if err := store.DeleteObjects(context.Background(), bucket, []string{"file.txt"}); err != nil {
		t.Fatalf("DeleteObjects: %v", err)
	}
	if objectExists(t, client, bucket, "file.txt") {
		t.Errorf("object should have been deleted")
	}
}

// TestE2E_RmDeletesMultiple verifies that DeleteObjects handles a batch.
func TestE2E_RmDeletesMultiple(t *testing.T) {

	endpoint := s3ServerEndpoint(t)
	client := s3Client(t, endpoint)
	bucket := s3BucketFromTestName(t)
	createBucket(t, client, bucket)

	putObject(t, client, bucket, "a.log", "1")
	putObject(t, client, bucket, "b.log", "2")
	putObject(t, client, bucket, "c.txt", "3")

	store, err := s3store.NewS3Client(context.Background(), s3StoreOptionFor(t, endpoint))
	if err != nil {
		t.Fatalf("NewS3Client: %v", err)
	}

	if err := store.DeleteObjects(context.Background(), bucket, []string{"a.log", "b.log"}); err != nil {
		t.Fatalf("DeleteObjects: %v", err)
	}
	if objectExists(t, client, bucket, "a.log") {
		t.Errorf("a.log should have been deleted")
	}
	if objectExists(t, client, bucket, "b.log") {
		t.Errorf("b.log should have been deleted")
	}
	if !objectExists(t, client, bucket, "c.txt") {
		t.Errorf("c.txt should still exist")
	}
}
