//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestE2E_MultipartLargeObject verifies that a large upload goes through
// the s3 manager's multipart path and the resulting object matches the
// input bytes, with byte progress ending at the total size.
//
// The test runs in gofakes3 mode only: real-service multipart behaviour
// (checksum trailers, part-size limits) varies between providers and is
// validated separately by the orchestrator's real-OSS run when wired.
func TestE2E_MultipartLargeObject(t *testing.T) {
	if useReal {
		t.Skip("multipart e2e is gofakes3-only; skip against real services")
	}

	// Body larger than the part size so the manager splits it into
	// multiple parts (5 MiB parts, 12 MiB body -> 3 parts).
	const partSize = 5 * 1024 * 1024
	body := bytes.Repeat([]byte("multipart-body!!"), 12*1024*1024/16) // 12 MiB

	endpoint := s3ServerEndpoint(t)
	client := s3Client(t, endpoint)
	bucket := s3BucketFromTestName(t)
	createBucket(t, client, bucket)

	workdir := t.TempDir()
	src := filepath.Join(workdir, "large.bin")
	if err := os.WriteFile(src, body, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	st := clientFor(t, endpoint)
	rec := &progressRecorder{}
	out, err := st.UploadFileMultipart(context.Background(), src, bucket, "large.bin", partSize, rec.record)
	if err != nil {
		t.Fatalf("UploadFileMultipart: %v", err)
	}
	if out == nil || out.Location == "" {
		t.Errorf("UploadOutput = %+v, want non-empty Location", out)
	}

	transferred, totals := rec.snapshot()
	assertProgressSequence(t, transferred, totals, int64(len(body)))

	if !objectExists(t, client, bucket, "large.bin") {
		t.Fatal("large.bin missing after multipart upload")
	}
	if got := objectContent(t, client, bucket, "large.bin"); got != string(body) {
		t.Errorf("content mismatch: got len=%d, want len=%d", len(got), len(body))
	}
}
