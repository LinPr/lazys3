//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"

	s3store "github.com/LinPr/lazys3/internal/storage/s3"
)

// TestE2E_ListBuckets verifies that S3Store.ListBuckets returns the
// buckets created on the test endpoint.
func TestE2E_ListBuckets(t *testing.T) {

	endpoint := s3ServerEndpoint(t)
	client := s3Client(t, endpoint)

	bucketA := s3BucketFromTestName(t) + "-a"
	bucketB := s3BucketFromTestName(t) + "-b"
	createBucket(t, client, bucketA)
	createBucket(t, client, bucketB)

	store, err := s3store.NewS3Client(context.Background(), s3StoreOptionFor(t, endpoint))
	if err != nil {
		t.Fatalf("NewS3Client: %v", err)
	}

	buckets, err := store.ListBuckets(context.Background())
	if err != nil {
		t.Fatalf("ListBuckets: %v", err)
	}

	names := make(map[string]bool, len(buckets))
	for _, b := range buckets {
		names[*b.Name] = true
	}
	if !names[bucketA] {
		t.Errorf("ListBuckets: %q missing (got %v)", bucketA, names)
	}
	if !names[bucketB] {
		t.Errorf("ListBuckets: %q missing (got %v)", bucketB, names)
	}
}

// TestE2E_ListObjectsWithPagination verifies that
// S3Store.ListObjectsWithPagination returns the expected common prefixes
// and objects under a prefix with "/" delimiter.
func TestE2E_ListObjectsWithPagination(t *testing.T) {

	endpoint := s3ServerEndpoint(t)
	client := s3Client(t, endpoint)
	bucket := s3BucketFromTestName(t)
	createBucket(t, client, bucket)

	putObject(t, client, bucket, "logs/a.log", "a")
	putObject(t, client, bucket, "logs/b.log", "b")
	putObject(t, client, bucket, "other.txt", "c")

	store, err := s3store.NewS3Client(context.Background(), s3StoreOptionFor(t, endpoint))
	if err != nil {
		t.Fatalf("NewS3Client: %v", err)
	}

	prefixes, objects, err := store.ListObjectsWithPagination(context.Background(), bucket, "")
	if err != nil {
		t.Fatalf("ListObjectsWithPagination: %v", err)
	}

	// "logs/" should appear as a common prefix.
	var sawLogsPrefix bool
	for _, p := range prefixes {
		if strings.HasSuffix(*p.Prefix, "logs/") {
			sawLogsPrefix = true
			break
		}
	}
	if !sawLogsPrefix {
		t.Errorf("expected common prefix logs/, got %+v", prefixes)
	}

	// "other.txt" should appear as an object key.
	var sawOther bool
	for _, o := range objects {
		if *o.Key == "other.txt" {
			sawOther = true
			break
		}
	}
	if !sawOther {
		t.Errorf("expected object other.txt, got %+v", objects)
	}
}
