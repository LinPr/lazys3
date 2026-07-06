//go:build e2e

package e2e

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LinPr/lazys3/internal/storage"
)

// syncClient builds a storage.Storage against the test endpoint and a
// helper that resolves a key under the test's sync prefix.
func syncClient(t *testing.T, endpoint string) *storage.Storage {
	t.Helper()
	return clientFor(t, endpoint)
}

// syncSourceURL parses a local or s3:// path into a StorageURL, failing
// the test on parse error.
func syncSourceURL(t *testing.T, s string) *storage.StorageURL {
	t.Helper()
	u, err := storage.NewStorageURL(s)
	if err != nil {
		t.Fatalf("NewStorageURL(%q): %v", s, err)
	}
	return u
}

// TestE2E_SyncLocalToS3_NewFiles verifies that syncing a local directory
// with two files to an S3 prefix uploads both with the right content.
func TestE2E_SyncLocalToS3_NewFiles(t *testing.T) {
	endpoint := s3ServerEndpoint(t)
	client := s3Client(t, endpoint)
	bucket := s3BucketFromTestName(t)
	createBucket(t, client, bucket)

	workdir := t.TempDir()
	srcDir := filepath.Join(workdir, "src")
	writeFile(t, filepath.Join(srcDir, "a.txt"), "a-content")
	writeFile(t, filepath.Join(srcDir, "b.txt"), "b-content")

	st := syncClient(t, endpoint)
	src := syncSourceURL(t, srcDir)
	dst := syncSourceURL(t, "s3://"+bucket+"/sync/")

	res, err := st.Sync(context.Background(), src, dst, storage.SyncOptions{}, nil)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.Uploaded != 2 {
		t.Errorf("uploaded = %d, want 2 (res=%+v)", res.Uploaded, res)
	}
	if !objectExists(t, client, bucket, "sync/a.txt") {
		t.Errorf("sync/a.txt missing")
	}
	if !objectExists(t, client, bucket, "sync/b.txt") {
		t.Errorf("sync/b.txt missing")
	}
	if got := objectContent(t, client, bucket, "sync/a.txt"); got != "a-content" {
		t.Errorf("sync/a.txt content = %q, want %q", got, "a-content")
	}
}

// TestE2E_SyncLocalToS3_SizeOnly_Skip verifies that a second sync with
// --size-only on unchanged files does not re-upload.
func TestE2E_SyncLocalToS3_SizeOnly_Skip(t *testing.T) {
	endpoint := s3ServerEndpoint(t)
	client := s3Client(t, endpoint)
	bucket := s3BucketFromTestName(t)
	createBucket(t, client, bucket)

	workdir := t.TempDir()
	srcDir := filepath.Join(workdir, "src")
	writeFile(t, filepath.Join(srcDir, "a.txt"), "a-content")

	st := syncClient(t, endpoint)
	src := syncSourceURL(t, srcDir)
	dst := syncSourceURL(t, "s3://"+bucket+"/sync/")

	// First sync uploads.
	res, err := st.Sync(context.Background(), src, dst, storage.SyncOptions{}, nil)
	if err != nil {
		t.Fatalf("Sync (first): %v", err)
	}
	if res.Uploaded != 1 {
		t.Fatalf("first sync uploaded = %d, want 1", res.Uploaded)
	}

	// Second sync with SizeOnly should skip.
	res, err = st.Sync(context.Background(), src, dst, storage.SyncOptions{SizeOnly: true}, nil)
	if err != nil {
		t.Fatalf("Sync (second): %v", err)
	}
	if res.Uploaded != 0 {
		t.Errorf("second sync uploaded = %d, want 0 (size-only should skip)", res.Uploaded)
	}
	if res.Skipped != 1 {
		t.Errorf("second sync skipped = %d, want 1", res.Skipped)
	}
}

// TestE2E_SyncLocalToS3_SizeOnly_Differs verifies that when the source
// file's content (but not size) changes, the default strategy re-uploads
// but --size-only does not.
func TestE2E_SyncLocalToS3_SizeOnly_Differs(t *testing.T) {
	endpoint := s3ServerEndpoint(t)
	client := s3Client(t, endpoint)
	bucket := s3BucketFromTestName(t)
	createBucket(t, client, bucket)

	workdir := t.TempDir()
	srcDir := filepath.Join(workdir, "src")
	srcPath := filepath.Join(srcDir, "a.txt")
	writeFile(t, srcPath, "aaaa-content") // 11 bytes

	st := syncClient(t, endpoint)
	src := syncSourceURL(t, srcDir)
	dst := syncSourceURL(t, "s3://"+bucket+"/sync/")

	if _, err := st.Sync(context.Background(), src, dst, storage.SyncOptions{}, nil); err != nil {
		t.Fatalf("Sync (first): %v", err)
	}

	// Wait long enough that the second writeFile's mtime is strictly
	// newer than the first upload's by more than the idempotence window
	// (2s). Without this the second sync may see the new mtime fall
	// inside the window and skip (sizes match, mtime within 2s).
	time.Sleep(2100 * time.Millisecond)
	// Same size, different content.
	writeFile(t, srcPath, "bbbb-content") // still 11 bytes
	if got := objectContent(t, client, bucket, "sync/a.txt"); got != "aaaa-content" {
		t.Fatalf("pre-sync content = %q, want %q", got, "aaaa-content")
	}

	// Size-only should skip and leave the old content.
	res, err := st.Sync(context.Background(), src, dst, storage.SyncOptions{SizeOnly: true}, nil)
	if err != nil {
		t.Fatalf("Sync (size-only): %v", err)
	}
	if res.Uploaded != 0 {
		t.Errorf("size-only uploaded = %d, want 0", res.Uploaded)
	}
	if got := objectContent(t, client, bucket, "sync/a.txt"); got != "aaaa-content" {
		t.Errorf("after size-only sync, content = %q, want %q (should not have re-uploaded)", got, "aaaa-content")
	}

	// Default strategy (size + mtime) should re-upload because mtime
	// is newer.
	res, err = st.Sync(context.Background(), src, dst, storage.SyncOptions{}, nil)
	if err != nil {
		t.Fatalf("Sync (default): %v", err)
	}
	if res.Uploaded != 1 {
		t.Errorf("default uploaded = %d, want 1", res.Uploaded)
	}
	if got := objectContent(t, client, bucket, "sync/a.txt"); got != "bbbb-content" {
		t.Errorf("after default sync, content = %q, want %q", got, "bbbb-content")
	}
}

// TestE2E_SyncS3ToLocal verifies that syncing a remote prefix to a local
// directory writes the files.
func TestE2E_SyncS3ToLocal(t *testing.T) {
	endpoint := s3ServerEndpoint(t)
	client := s3Client(t, endpoint)
	bucket := s3BucketFromTestName(t)
	createBucket(t, client, bucket)

	putObject(t, client, bucket, "sync/a.txt", "a-content")
	putObject(t, client, bucket, "sync/b.txt", "b-content")

	workdir := t.TempDir()
	dstDir := filepath.Join(workdir, "dst")

	st := syncClient(t, endpoint)
	src := syncSourceURL(t, "s3://"+bucket+"/sync/")
	dst := syncSourceURL(t, dstDir)

	res, err := st.Sync(context.Background(), src, dst, storage.SyncOptions{}, nil)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.Downloaded != 2 {
		t.Errorf("downloaded = %d, want 2 (res=%+v)", res.Downloaded, res)
	}
	if got := fileContent(t, filepath.Join(dstDir, "a.txt")); got != "a-content" {
		t.Errorf("a.txt = %q, want %q", got, "a-content")
	}
	if got := fileContent(t, filepath.Join(dstDir, "b.txt")); got != "b-content" {
		t.Errorf("b.txt = %q, want %q", got, "b-content")
	}
}

// TestE2E_SyncS3ToS3 verifies that syncing between two buckets/prefixes
// copies objects server-side.
func TestE2E_SyncS3ToS3(t *testing.T) {
	endpoint := s3ServerEndpoint(t)
	client := s3Client(t, endpoint)
	srcBucket := s3BucketFromTestName(t) + "-src"
	dstBucket := s3BucketFromTestName(t) + "-dst"
	createBucket(t, client, srcBucket)
	createBucket(t, client, dstBucket)

	putObject(t, client, srcBucket, "sync/a.txt", "a-content")
	putObject(t, client, srcBucket, "sync/b.txt", "b-content")

	st := syncClient(t, endpoint)
	src := syncSourceURL(t, "s3://"+srcBucket+"/sync/")
	dst := syncSourceURL(t, "s3://"+dstBucket+"/sync/")

	res, err := st.Sync(context.Background(), src, dst, storage.SyncOptions{}, nil)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.Copied != 2 {
		t.Errorf("copied = %d, want 2 (res=%+v)", res.Copied, res)
	}
	if !objectExists(t, client, dstBucket, "sync/a.txt") {
		t.Errorf("dst sync/a.txt missing")
	}
	if !objectExists(t, client, dstBucket, "sync/b.txt") {
		t.Errorf("dst sync/b.txt missing")
	}
	if got := objectContent(t, client, dstBucket, "sync/a.txt"); got != "a-content" {
		t.Errorf("dst a.txt content = %q, want %q", got, "a-content")
	}
}

// TestE2E_Sync_Delete verifies that --delete removes destination objects
// not present in the source.
func TestE2E_Sync_Delete(t *testing.T) {
	endpoint := s3ServerEndpoint(t)
	client := s3Client(t, endpoint)
	bucket := s3BucketFromTestName(t)
	createBucket(t, client, bucket)

	// Source has a.txt only; destination has a.txt AND extra.txt.
	putObject(t, client, bucket, "sync/a.txt", "a-content")
	putObject(t, client, bucket, "sync/extra.txt", "extra-content")

	// Build a local source dir with only a.txt.
	workdir := t.TempDir()
	srcDir := filepath.Join(workdir, "src")
	writeFile(t, filepath.Join(srcDir, "a.txt"), "a-content")

	st := syncClient(t, endpoint)
	src := syncSourceURL(t, srcDir)
	dst := syncSourceURL(t, "s3://"+bucket+"/sync/")

	res, err := st.Sync(context.Background(), src, dst, storage.SyncOptions{Delete: true}, nil)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.Deleted != 1 {
		t.Errorf("deleted = %d, want 1 (res=%+v)", res.Deleted, res)
	}
	if objectExists(t, client, bucket, "sync/extra.txt") {
		t.Errorf("sync/extra.txt should have been deleted")
	}
	if !objectExists(t, client, bucket, "sync/a.txt") {
		t.Errorf("sync/a.txt should still exist")
	}
}

// TestE2E_Sync_DryRun verifies that --dry-run makes no changes.
func TestE2E_Sync_DryRun(t *testing.T) {
	endpoint := s3ServerEndpoint(t)
	client := s3Client(t, endpoint)
	bucket := s3BucketFromTestName(t)
	createBucket(t, client, bucket)

	workdir := t.TempDir()
	srcDir := filepath.Join(workdir, "src")
	writeFile(t, filepath.Join(srcDir, "a.txt"), "a-content")
	writeFile(t, filepath.Join(srcDir, "b.txt"), "b-content")

	st := syncClient(t, endpoint)
	src := syncSourceURL(t, srcDir)
	dst := syncSourceURL(t, "s3://"+bucket+"/sync/")

	res, err := st.Sync(context.Background(), src, dst, storage.SyncOptions{DryRun: true}, nil)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.Uploaded != 2 {
		t.Errorf("dry-run uploaded = %d, want 2 (counts should reflect what would happen)", res.Uploaded)
	}
	if objectExists(t, client, bucket, "sync/a.txt") {
		t.Errorf("dry-run should not have uploaded sync/a.txt")
	}
	if objectExists(t, client, bucket, "sync/b.txt") {
		t.Errorf("dry-run should not have uploaded sync/b.txt")
	}
}

// TestE2E_Sync_Exclude verifies that --exclude=*.log skips log files.
func TestE2E_Sync_Exclude(t *testing.T) {
	endpoint := s3ServerEndpoint(t)
	client := s3Client(t, endpoint)
	bucket := s3BucketFromTestName(t)
	createBucket(t, client, bucket)

	workdir := t.TempDir()
	srcDir := filepath.Join(workdir, "src")
	writeFile(t, filepath.Join(srcDir, "a.txt"), "a-content")
	writeFile(t, filepath.Join(srcDir, "b.log"), "log-content")
	writeFile(t, filepath.Join(srcDir, "c.txt"), "c-content")

	st := syncClient(t, endpoint)
	src := syncSourceURL(t, srcDir)
	dst := syncSourceURL(t, "s3://"+bucket+"/sync/")

	res, err := st.Sync(context.Background(), src, dst, storage.SyncOptions{Exclude: []string{"*.log"}}, nil)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.Uploaded != 2 {
		t.Errorf("uploaded = %d, want 2 (res=%+v)", res.Uploaded, res)
	}
	if !objectExists(t, client, bucket, "sync/a.txt") {
		t.Errorf("sync/a.txt missing")
	}
	if !objectExists(t, client, bucket, "sync/c.txt") {
		t.Errorf("sync/c.txt missing")
	}
	if objectExists(t, client, bucket, "sync/b.log") {
		t.Errorf("sync/b.log should have been excluded")
	}
}

// TestE2E_SyncS3ToLocal_NestedPaths verifies that nested relative paths
// are preserved end-to-end (subdir/file.txt -> dst/subdir/file.txt).
func TestE2E_SyncS3ToLocal_NestedPaths(t *testing.T) {
	endpoint := s3ServerEndpoint(t)
	client := s3Client(t, endpoint)
	bucket := s3BucketFromTestName(t)
	createBucket(t, client, bucket)

	putObject(t, client, bucket, "sync/sub/a.txt", "a-content")
	putObject(t, client, bucket, "sync/sub/b.txt", "b-content")

	workdir := t.TempDir()
	dstDir := filepath.Join(workdir, "dst")

	st := syncClient(t, endpoint)
	src := syncSourceURL(t, "s3://"+bucket+"/sync/")
	dst := syncSourceURL(t, dstDir)

	if _, err := st.Sync(context.Background(), src, dst, storage.SyncOptions{}, nil); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if got := fileContent(t, filepath.Join(dstDir, "sub", "a.txt")); got != "a-content" {
		t.Errorf("sub/a.txt = %q, want %q", got, "a-content")
	}
	if got := fileContent(t, filepath.Join(dstDir, "sub", "b.txt")); got != "b-content" {
		t.Errorf("sub/b.txt = %q, want %q", got, "b-content")
	}
}

// TestE2E_SyncLocalToS3_Idempotent verifies that running the same sync
// twice (default strategy) uploads the first time and skips the second
// time.
func TestE2E_SyncLocalToS3_Idempotent(t *testing.T) {
	endpoint := s3ServerEndpoint(t)
	client := s3Client(t, endpoint)
	bucket := s3BucketFromTestName(t)
	createBucket(t, client, bucket)

	workdir := t.TempDir()
	srcDir := filepath.Join(workdir, "src")
	writeFile(t, filepath.Join(srcDir, "a.txt"), "a-content")

	st := syncClient(t, endpoint)
	src := syncSourceURL(t, srcDir)
	dst := syncSourceURL(t, "s3://"+bucket+"/sync/")

	res, err := st.Sync(context.Background(), src, dst, storage.SyncOptions{}, nil)
	if err != nil {
		t.Fatalf("Sync (first): %v", err)
	}
	if res.Uploaded != 1 {
		t.Fatalf("first sync uploaded = %d, want 1", res.Uploaded)
	}

	res, err = st.Sync(context.Background(), src, dst, storage.SyncOptions{}, nil)
	if err != nil {
		t.Fatalf("Sync (second): %v", err)
	}
	if res.Uploaded != 0 {
		t.Errorf("second sync uploaded = %d, want 0 (idempotent)", res.Uploaded)
	}
	if res.Skipped != 1 {
		t.Errorf("second sync skipped = %d, want 1", res.Skipped)
	}
}

// Ensure unused imports don't trip the build when only a subset of tests
// reference them.
var _ = strings.HasPrefix
var _ = os.Stat
