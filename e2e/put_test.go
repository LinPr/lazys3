//go:build e2e

package e2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	s3store "github.com/LinPr/lazys3/internal/storage/s3"
)

// TestE2E_PutUploadsObject verifies that S3Store.PutObject uploads a
// local file to the bucket and the object appears in listings.
//
// We exercise S3Store.PutObject directly rather than storage.UploadFile
// because storage.UploadFile historically went through FileStore.Create
// which truncated the source file before passing it to PutObject. The
// storage.UploadFile path is exercised separately in
// TestE2E_PutUploadsObjectViaStorage below.
func TestE2E_PutUploadsObject(t *testing.T) {

	endpoint := s3ServerEndpoint(t)
	client := s3Client(t, endpoint)
	bucket := s3BucketFromTestName(t)
	createBucket(t, client, bucket)

	workdir := t.TempDir()
	src := filepath.Join(workdir, "hello.txt")
	content := "put-content"
	writeFile(t, src, content)

	store, err := s3store.NewS3Client(context.Background(), s3StoreOptionFor(t, endpoint))
	if err != nil {
		t.Fatalf("NewS3Client: %v", err)
	}

	f, err := os.Open(src)
	if err != nil {
		t.Fatalf("os.Open: %v", err)
	}
	defer f.Close()

	if _, err := store.PutObject(context.Background(), f, bucket, "hello.txt"); err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	if !objectExists(t, client, bucket, "hello.txt") {
		t.Errorf("object not found after upload")
	}
	if got := objectContent(t, client, bucket, "hello.txt"); got != content {
		t.Errorf("content = %q, want %q", got, content)
	}
}

// TestE2E_PutUploadsObjectViaStorage verifies that storage.UploadFile
// uploads a local file without truncating the source. This is a regression
// test for the bug where UploadFile used FileStore.Create (O_TRUNC) on the
// source path, which zeroed the local file and produced an empty object
// body that gofakes3 rejected with MissingContentLength.
//
// The fix in internal/storage/storage.go uses os.Open (read-only) instead.
// This test exercises that path end-to-end: it writes a non-empty local
// file, uploads it via storage.UploadFile, then asserts (a) the object
// exists with matching content, and (b) the local source file still has
// its original content (i.e. was not truncated).
func TestE2E_PutUploadsObjectViaStorage(t *testing.T) {

	endpoint := s3ServerEndpoint(t)
	client := s3Client(t, endpoint)
	bucket := s3BucketFromTestName(t)
	createBucket(t, client, bucket)

	workdir := t.TempDir()
	src := filepath.Join(workdir, "src.txt")
	content := "storage-upload-content"
	writeFile(t, src, content)

	st := clientFor(t, endpoint)

	if _, err := st.UploadFile(context.Background(), src, bucket, "via-storage.txt"); err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	// Remote object should exist with the uploaded content.
	if !objectExists(t, client, bucket, "via-storage.txt") {
		t.Errorf("object not found after UploadFile")
	}
	if got := objectContent(t, client, bucket, "via-storage.txt"); got != content {
		t.Errorf("remote content = %q, want %q", got, content)
	}

	// Local source file must still contain its original content (i.e.
	// UploadFile did not truncate it).
	if got := fileContent(t, src); got != content {
		t.Errorf("local source file content = %q, want %q (source was truncated)", got, content)
	}
}
