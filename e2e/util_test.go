//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// defaultAccessKeyID/Secret are the static credentials we feed to the S3
// client via env vars. gofakes3 does not validate them, but the AWS SDK
// requires them to be non-empty.
const (
	defaultAccessKeyID     = "lazys3-test"
	defaultSecretAccessKey = "lazys3-test-secret"
	defaultRegion          = "us-east-1"
)

// s3Client returns an S3 client configured against the given gofakes3
// endpoint, using static credentials and path-style addressing.
//
// This client is used by the test harness itself (createBucket, putObject,
// objectExists, objectContent) to set up and assert on S3 state. The
// storage-layer tests use clientFor/storage.NewStorage instead.
//
// In real-OSS mode (useReal == true), the client is built from the shared
// config profile named by realProfile so it uses the same credentials,
// region, and endpoint as the storage layer under test. The profile's
// endpoint_url is picked up by the SDK as the base endpoint automatically.
// OSS requires virtual-host-style addressing (path-style is rejected with
// SecondLevelDomainForbidden), so UsePathStyle is left false in real mode.
func s3Client(t *testing.T, endpoint string) *s3.Client {
	t.Helper()
	if useReal {
		ctx := context.Background()
		cfg, err := config.LoadDefaultConfig(ctx, config.WithSharedConfigProfile(realProfile))
		if err != nil {
			t.Fatalf("LoadDefaultConfig(profile=%q): %v", realProfile, err)
		}
		return s3.NewFromConfig(cfg)
	}
	return s3.New(s3.Options{
		BaseEndpoint: aws.String(endpoint),
		Region:       defaultRegion,
		Credentials:  credentials.NewStaticCredentialsProvider(defaultAccessKeyID, defaultSecretAccessKey, ""),
		UsePathStyle: true,
	})
}

// createBucket creates a bucket and registers a cleanup that deletes it
// (and all objects in it) at the end of the test.
//
// In real-OSS mode, OSS's ListBuckets is eventually consistent: a bucket
// created milliseconds ago may not appear in the listing returned to the
// store under test even though it exists. To avoid spurious "missing
// bucket" failures, we wait for the bucket to be reachable via HeadBucket
// before returning.
func createBucket(t *testing.T, client *s3.Client, bucket string) {
	t.Helper()
	_, err := client.CreateBucket(t.Context(), &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		t.Fatalf("CreateBucket(%q): %v", bucket, err)
	}
	if !waitForBucketExists(t.Context(), client, bucket) {
		t.Fatalf("CreateBucket(%q): bucket did not become visible to HeadBucket in time", bucket)
	}
	t.Cleanup(func() {
		// IMPORTANT: t.Context() is canceled just before Cleanup functions
		// run (Go 1.24+), so every SDK call made with it here would fail
		// instantly with "context canceled" and silently leak the bucket on
		// real OSS. Use a fresh, bounded context instead.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := destroyBucket(ctx, client, bucket); err != nil {
			t.Errorf("cleanup: bucket %q may have leaked: %v", bucket, err)
		}
	})
}

// destroyBucket empties the bucket (per-object fallback for OSS's bulk
// DeleteObjects Content-MD5 quirk) and then deletes it, retrying the
// list-then-delete cycle and the final DeleteBucket until the bucket is
// gone or ctx expires. OSS listings are eventually consistent, so a single
// list pass can miss just-written objects and DeleteBucket can transiently
// report BucketNotEmpty right after the last object was removed.
func destroyBucket(ctx context.Context, client *s3.Client, bucket string) error {
	var lastErr error
	for ctx.Err() == nil {
		// List everything currently visible and delete it.
		var objects []types.ObjectIdentifier
		p := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
			Bucket: aws.String(bucket),
		})
		listFailed := false
		for p.HasMorePages() {
			out, err := p.NextPage(ctx)
			if err != nil {
				// NoSuchBucket etc. means nothing to clean up.
				var nsb *types.NoSuchBucket
				if errors.As(err, &nsb) {
					return nil
				}
				lastErr = fmt.Errorf("list objects: %w", err)
				listFailed = true
				break
			}
			for _, o := range out.Contents {
				objects = append(objects, types.ObjectIdentifier{Key: o.Key})
			}
		}
		if listFailed {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		deleteObjectList(ctx, client, bucket, objects)

		_, err := client.DeleteBucket(ctx, &s3.DeleteBucketInput{
			Bucket: aws.String(bucket),
		})
		if err == nil {
			return nil
		}
		var nsb *types.NoSuchBucket
		if errors.As(err, &nsb) {
			return nil
		}
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			switch apiErr.ErrorCode() {
			case "NoSuchBucket", "NotFound":
				return nil
			case "BucketNotEmpty":
				// A versioned bucket can be "empty" in the plain listing yet
				// still hold noncurrent versions and delete markers, which
				// DeleteBucket refuses to remove. Purge the version history
				// and retry. Unversioned buckets never reach this branch
				// with leftovers the plain purge above cannot handle.
				if perr := purgeObjectVersions(ctx, client, bucket); perr != nil {
					lastErr = fmt.Errorf("purge versions: %w", perr)
					time.Sleep(500 * time.Millisecond)
					continue
				}
			}
		}
		lastErr = fmt.Errorf("delete bucket: %w", err)
		time.Sleep(500 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = ctx.Err()
	}
	return lastErr
}

// purgeObjectVersions deletes every object version and delete marker in
// the bucket, paginating ListObjectVersions with the
// KeyMarker/VersionIdMarker cursor. Needed to empty a versioned bucket:
// the plain object listing hides noncurrent versions and delete markers.
func purgeObjectVersions(ctx context.Context, client *s3.Client, bucket string) error {
	input := &s3.ListObjectVersionsInput{Bucket: aws.String(bucket)}
	for {
		out, err := client.ListObjectVersions(ctx, input)
		if err != nil {
			return err
		}
		var objects []types.ObjectIdentifier
		for _, v := range out.Versions {
			objects = append(objects, types.ObjectIdentifier{Key: v.Key, VersionId: v.VersionId})
		}
		for _, m := range out.DeleteMarkers {
			objects = append(objects, types.ObjectIdentifier{Key: m.Key, VersionId: m.VersionId})
		}
		deleteObjectList(ctx, client, bucket, objects)
		if out.IsTruncated == nil || !*out.IsTruncated {
			return nil
		}
		input.KeyMarker = out.NextKeyMarker
		input.VersionIdMarker = out.NextVersionIdMarker
	}
}

// deleteObjectList deletes the given objects, working around OSS's
// MissingArgument rejection of the bulk DeleteObjects API by falling back
// to per-object DeleteObject calls when the bulk request fails.
func deleteObjectList(ctx context.Context, client *s3.Client, bucket string, objects []types.ObjectIdentifier) {
	if len(objects) == 0 {
		return
	}
	_, err := client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(bucket),
		Delete: &types.Delete{Objects: objects},
	})
	if err == nil {
		return
	}
	// Fall back to per-object deletes, keeping the version pin when the
	// identifier targets a specific version or delete marker.
	for _, o := range objects {
		_, _ = client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket:    aws.String(bucket),
			Key:       o.Key,
			VersionId: o.VersionId,
		})
	}
}

// waitForBucketExists polls HeadBucket until it succeeds or the context
// times out. Used to fence on OSS's eventually-consistent bucket creation
// so the test does not race ahead of the bucket becoming visible.
func waitForBucketExists(ctx context.Context, client *s3.Client, bucket string) bool {
	deadline, ok := ctx.Deadline()
	if !ok {
		// Fall back to a bounded wait when the test context has no
		// deadline (e.g. tests that pass context.Background()). Five
		// seconds is plenty for OSS's eventual consistency.
		deadline = time.Now().Add(5 * time.Second)
	}
	for {
		_, err := client.HeadBucket(ctx, &s3.HeadBucketInput{
			Bucket: aws.String(bucket),
		})
		if err == nil {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// putObject uploads content to the given bucket/key.
func putObject(t *testing.T, client *s3.Client, bucket, key, content string) {
	t.Helper()
	_, err := client.PutObject(t.Context(), &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   strings.NewReader(content),
	})
	if err != nil {
		t.Fatalf("PutObject(%q, %q): %v", bucket, key, err)
	}
}

// objectExists reports whether the object exists in the bucket.
func objectExists(t *testing.T, client *s3.Client, bucket, key string) bool {
	t.Helper()
	_, err := client.HeadObject(t.Context(), &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err == nil {
		return true
	}
	// S3 returns 404 wrapped in NotFound; treat any error as "not found"
	// for test purposes.
	return false
}

// objectContent downloads the object and returns its body as a string.
func objectContent(t *testing.T, client *s3.Client, bucket, key string) string {
	t.Helper()
	out, err := client.GetObject(t.Context(), &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		t.Fatalf("GetObject(%q, %q): %v", bucket, key, err)
	}
	defer out.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(out.Body); err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	return buf.String()
}

// fileContent reads a local file, failing the test on error.
func fileContent(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", path, err)
	}
	return string(b)
}

// writeFile writes content to a local file, creating parent dirs.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

// sprintf is a tiny helper retained for parity with s6cmd's e2e util; tests
// format s3 URIs with it.
func sprintf(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}
