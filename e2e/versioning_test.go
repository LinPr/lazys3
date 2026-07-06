//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/LinPr/lazys3/internal/storage"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// TestE2E_Versioning exercises the whole object-versioning surface of the
// storage layer against one bucket: enable versioning, build a two-version
// history, read and restore an old version, delete a specific version, and
// round-trip a delete marker.
//
// gofakes3 does not support bucket versioning, so the test probes
// PutBucketVersioning/ListObjectVersions once and skips in fake mode; it
// runs for real with LAZYS3_E2E_REAL=oss.
func TestE2E_Versioning(t *testing.T) {

	endpoint := endpointFor(t)
	client := s3Client(t, endpoint)
	bucket := s3BucketFromTestName(t)
	createBucket(t, client, bucket)

	ctx := context.Background()
	st := clientFor(t, endpoint)
	const key = "versioned.txt"

	// Probe once in fake mode and skip if the backend cannot do full
	// versioning. gofakes3's s3mem accepts PutBucketVersioning and lists
	// versions, but its CopySource parser has no versionId subresource
	// support, so restore can never work there; real OSS is unaffected.
	if !useReal {
		if reason := versioningProbe(ctx, st, client, t, bucket); reason != "" {
			t.Skipf("backend does not fully support object versioning: %s", reason)
		}
	}

	if err := st.PutBucketVersioning(ctx, bucket, true); err != nil {
		t.Fatalf("PutBucketVersioning: %v", err)
	}

	putObject(t, client, bucket, key, "v1")
	putObject(t, client, bucket, key, "v2")

	versions, err := st.ListObjectVersions(ctx, bucket, key)
	if err != nil {
		t.Fatalf("ListObjectVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("ListObjectVersions returned %d entries, want 2: %+v", len(versions), versions)
	}
	if !versions[0].IsLatest || versions[0].IsDeleteMarker {
		t.Errorf("versions[0] = %+v, want the latest non-marker version first", versions[0])
	}
	if versions[1].IsLatest {
		t.Errorf("versions[1].IsLatest = true, want the older version second")
	}
	older := versions[1]

	// Download the older version and check it holds the v1 content.
	dst := filepath.Join(t.TempDir(), "old.txt")
	if err := st.DownloadFileVersionWithProgress(ctx, bucket, key, older.VersionID, dst, nil); err != nil {
		t.Fatalf("DownloadFileVersionWithProgress: %v", err)
	}
	if got := fileContent(t, dst); got != "v1" {
		t.Errorf("old version content = %q, want %q", got, "v1")
	}

	// Restore the older version: a copy of v1 becomes the newest version.
	if err := st.RestoreObjectVersion(ctx, bucket, key, older.VersionID, older.StorageClass); err != nil {
		t.Fatalf("RestoreObjectVersion: %v", err)
	}
	if got := objectContent(t, client, bucket, key); got != "v1" {
		t.Errorf("content after restore = %q, want %q", got, "v1")
	}
	versions, err = st.ListObjectVersions(ctx, bucket, key)
	if err != nil {
		t.Fatalf("ListObjectVersions after restore: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("after restore: %d versions, want 3: %+v", len(versions), versions)
	}

	// Permanently delete the middle ("v2") version; the history shrinks.
	if err := st.DeleteObjectVersion(ctx, bucket, key, versions[1].VersionID); err != nil {
		t.Fatalf("DeleteObjectVersion(%q): %v", versions[1].VersionID, err)
	}
	versions, err = st.ListObjectVersions(ctx, bucket, key)
	if err != nil {
		t.Fatalf("ListObjectVersions after version delete: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("after version delete: %d versions, want 2: %+v", len(versions), versions)
	}

	// A plain (unversioned) delete creates a delete marker on top.
	if _, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
	versions, err = st.ListObjectVersions(ctx, bucket, key)
	if err != nil {
		t.Fatalf("ListObjectVersions after delete: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("after delete: %d entries, want 3 (2 versions + marker): %+v", len(versions), versions)
	}
	marker := versions[0]
	if !marker.IsDeleteMarker || !marker.IsLatest {
		t.Fatalf("versions[0] = %+v, want the delete marker as latest entry", marker)
	}
	if objectExists(t, client, bucket, key) {
		t.Errorf("object still visible after delete marker was created")
	}

	// Deleting the delete marker undeletes the object.
	if err := st.DeleteObjectVersion(ctx, bucket, key, marker.VersionID); err != nil {
		t.Fatalf("DeleteObjectVersion(marker %q): %v", marker.VersionID, err)
	}
	if got := objectContent(t, client, bucket, key); got != "v1" {
		t.Errorf("content after undelete = %q, want %q", got, "v1")
	}
	versions, err = st.ListObjectVersions(ctx, bucket, key)
	if err != nil {
		t.Fatalf("ListObjectVersions after undelete: %v", err)
	}
	if len(versions) != 2 {
		t.Errorf("after undelete: %d versions, want 2: %+v", len(versions), versions)
	}
	for _, v := range versions {
		if v.IsDeleteMarker {
			t.Errorf("delete marker still present after undelete: %+v", v)
		}
	}
}

// versioningProbe exercises the versioning operations the test depends on
// against a throwaway key and returns a non-empty skip reason on the first
// one the backend rejects or silently ignores. It is only called in fake
// mode; destroyBucket cleans up the probe versions with the bucket.
func versioningProbe(ctx context.Context, st *storage.Storage, client *s3.Client, t *testing.T, bucket string) string {
	const key = ".versioning-probe"
	if err := st.PutBucketVersioning(ctx, bucket, true); err != nil {
		return fmt.Sprintf("PutBucketVersioning: %v", err)
	}
	putObject(t, client, bucket, key, "p1")
	putObject(t, client, bucket, key, "p2")
	versions, err := st.ListObjectVersions(ctx, bucket, key)
	if err != nil {
		return fmt.Sprintf("ListObjectVersions: %v", err)
	}
	if len(versions) != 2 {
		return fmt.Sprintf("ListObjectVersions returned %d entries for two puts, want 2 (versioning no-op)", len(versions))
	}
	if err := st.RestoreObjectVersion(ctx, bucket, key, versions[1].VersionID, versions[1].StorageClass); err != nil {
		return fmt.Sprintf("RestoreObjectVersion: %v", err)
	}
	return ""
}
