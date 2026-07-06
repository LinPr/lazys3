//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	s3store "github.com/LinPr/lazys3/internal/storage/s3"
)

// TestE2E_MakeBucket verifies that S3Store.CreateBucket creates a bucket
// that subsequent HeadBucket calls can see.
func TestE2E_MakeBucket(t *testing.T) {

	endpoint := s3ServerEndpoint(t)
	client := s3Client(t, endpoint)

	bucket := s3BucketFromTestName(t)
	store, err := s3store.NewS3Client(context.Background(), s3StoreOptionFor(t, endpoint))
	if err != nil {
		t.Fatalf("NewS3Client: %v", err)
	}

	if err := store.CreateBucket(context.Background(), bucket, createBucketRegion(t)); err != nil {
		t.Fatalf("CreateBucket: %v", err)
	}
	exists, err := store.BucketExists(context.Background(), bucket)
	if err != nil {
		t.Fatalf("BucketExists: %v", err)
	}
	if !exists {
		t.Errorf("bucket %q should exist after CreateBucket", bucket)
	}
	t.Cleanup(func() {
		// Best-effort cleanup; the bucket is empty after the test.
		_, _ = client.DeleteBucket(context.Background(), &s3.DeleteBucketInput{
			Bucket: aws.String(bucket),
		})
	})
}
