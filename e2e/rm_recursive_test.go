//go:build e2e

package e2e

import (
	"context"
	"testing"

	s3store "github.com/LinPr/lazys3/internal/storage/s3"
)

// TestE2E_RecursiveDeletePrefix verifies S3Store.DeletePrefix (the 'D' on a
// directory row): every key under the prefix goes, including nested ones,
// while keys under a sibling prefix sharing the same leading characters
// ("a2/" vs "a/") survive — the prefix-scoping trap a non-"/"-terminated
// prefix would fall into.
func TestE2E_RecursiveDeletePrefix(t *testing.T) {

	endpoint := endpointFor(t)
	client := s3Client(t, endpoint)
	bucket := s3BucketFromTestName(t)
	createBucket(t, client, bucket)

	putObject(t, client, bucket, "a/1", "one")
	putObject(t, client, bucket, "a/b/2", "two")
	putObject(t, client, bucket, "a2/3", "three")

	store, err := s3store.NewS3Client(context.Background(), s3StoreOptionFor(t, endpoint))
	if err != nil {
		t.Fatalf("NewS3Client: %v", err)
	}

	n, err := store.DeletePrefix(context.Background(), bucket, "a/")
	if err != nil {
		t.Fatalf("DeletePrefix(a/): %v", err)
	}
	if n != 2 {
		t.Errorf("DeletePrefix(a/) deleted %d keys, want 2", n)
	}
	if objectExists(t, client, bucket, "a/1") {
		t.Errorf("a/1 should have been deleted")
	}
	if objectExists(t, client, bucket, "a/b/2") {
		t.Errorf("a/b/2 should have been deleted (nested key)")
	}
	if !objectExists(t, client, bucket, "a2/3") {
		t.Errorf("a2/3 should have survived: prefix a/ must not match a2/")
	}

	// A prefix with no keys is a no-op.
	n, err = store.DeletePrefix(context.Background(), bucket, "nothing/")
	if err != nil {
		t.Fatalf("DeletePrefix(nothing/): %v", err)
	}
	if n != 0 {
		t.Errorf("DeletePrefix(nothing/) deleted %d keys, want 0", n)
	}

	// A non-"/"-terminated prefix is rejected up front (the scoping guard).
	if _, err := store.DeletePrefix(context.Background(), bucket, "a2"); err == nil {
		t.Errorf("DeletePrefix(a2) succeeded, want an error for a prefix without a trailing slash")
	}
	if !objectExists(t, client, bucket, "a2/3") {
		t.Errorf("a2/3 must still exist after the rejected DeletePrefix(a2)")
	}
}
