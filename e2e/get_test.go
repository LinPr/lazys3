//go:build e2e

package e2e

import (
	"context"
	"path/filepath"
	"testing"
)

// TestE2E_GetDownloadsObject verifies that storage.DownloadFile downloads
// an object to a local file with matching content.
func TestE2E_GetDownloadsObject(t *testing.T) {

	endpoint := s3ServerEndpoint(t)
	client := s3Client(t, endpoint)
	bucket := s3BucketFromTestName(t)
	createBucket(t, client, bucket)

	content := "from-s3-content"
	putObject(t, client, bucket, "a.txt", content)

	workdir := t.TempDir()
	dst := filepath.Join(workdir, "out.txt")

	st := clientFor(t, endpoint)
	if err := st.DownloadFile(context.Background(), bucket, "a.txt", dst); err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	if got := fileContent(t, dst); got != content {
		t.Errorf("local file content = %q, want %q", got, content)
	}
}

// A directory destination means "download into it" — the object's base
// name is appended instead of failing the temp-file rename against the
// existing directory.
func TestE2E_GetDownloadsIntoDirectory(t *testing.T) {

	endpoint := s3ServerEndpoint(t)
	client := s3Client(t, endpoint)
	bucket := s3BucketFromTestName(t)
	createBucket(t, client, bucket)

	content := "dir-dest-content"
	putObject(t, client, bucket, "nested/dl.txt", content)

	workdir := t.TempDir()

	st := clientFor(t, endpoint)
	if err := st.DownloadFile(context.Background(), bucket, "nested/dl.txt", workdir); err != nil {
		t.Fatalf("DownloadFile into dir: %v", err)
	}
	if got := fileContent(t, filepath.Join(workdir, "dl.txt")); got != content {
		t.Errorf("downloaded content = %q, want %q", got, content)
	}
}
