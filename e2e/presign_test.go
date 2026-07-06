//go:build e2e

package e2e

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// httpGetURL fetches url with a plain HTTP client (no SDK signing) and
// returns the status code and body.
func httpGetURL(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("http.Get(%q): %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, string(body)
}

// TestE2E_PresignGetObject verifies that Storage.PresignGetObject returns a
// URL that a plain, unauthenticated HTTP GET can fetch the object from.
func TestE2E_PresignGetObject(t *testing.T) {
	endpoint := endpointFor(t)
	client := s3Client(t, endpoint)
	bucket := s3BucketFromTestName(t)
	createBucket(t, client, bucket)

	content := "presigned-get-content"
	putObject(t, client, bucket, "a.txt", content)

	st := clientFor(t, endpoint)
	url, err := st.PresignGetObject(context.Background(), bucket, "a.txt", time.Hour)
	if err != nil {
		t.Fatalf("PresignGetObject: %v", err)
	}

	status, body := httpGetURL(t, url)
	if status != http.StatusOK {
		t.Fatalf("GET %s: status = %d, want %d (body: %q)", url, status, http.StatusOK, body)
	}
	if body != content {
		t.Errorf("GET body = %q, want %q", body, content)
	}
}

// TestE2E_PresignGetObjectExpired verifies that a presigned GET URL is
// rejected with 403 once its expiry has passed.
func TestE2E_PresignGetObjectExpired(t *testing.T) {
	if !useReal {
		// gofakes3 does not validate SigV4 signatures at all, so an expired
		// presigned URL is still served with 200. The expiry behaviour is
		// only observable against a real service (LAZYS3_E2E_REAL=oss).
		t.Skipf("gofakes3 does not enforce presigned-URL expiry; run with LAZYS3_E2E_REAL to cover this")
	}
	endpoint := endpointFor(t)
	client := s3Client(t, endpoint)
	bucket := s3BucketFromTestName(t)
	createBucket(t, client, bucket)

	putObject(t, client, bucket, "a.txt", "expiring-content")

	st := clientFor(t, endpoint)
	url, err := st.PresignGetObject(context.Background(), bucket, "a.txt", time.Second)
	if err != nil {
		t.Fatalf("PresignGetObject: %v", err)
	}

	time.Sleep(2 * time.Second)
	status, body := httpGetURL(t, url)
	if status != http.StatusForbidden {
		t.Errorf("GET expired URL: status = %d, want %d (body: %q)", status, http.StatusForbidden, body)
	}
}

// TestE2E_PresignPutObject verifies that Storage.PresignPutObject returns a
// URL that a plain HTTP PUT can upload through, and that the object lands
// with the uploaded content.
func TestE2E_PresignPutObject(t *testing.T) {
	endpoint := endpointFor(t)
	client := s3Client(t, endpoint)
	bucket := s3BucketFromTestName(t)
	createBucket(t, client, bucket)

	content := "presigned-put-content"

	st := clientFor(t, endpoint)
	url, err := st.PresignPutObject(context.Background(), bucket, "up.txt", time.Hour)
	if err != nil {
		t.Fatalf("PresignPutObject: %v", err)
	}

	req, err := http.NewRequest(http.MethodPut, url, strings.NewReader(content))
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", url, err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT %s: status = %d, want %d (body: %q)", url, resp.StatusCode, http.StatusOK, body)
	}

	if got := objectContent(t, client, bucket, "up.txt"); got != content {
		t.Errorf("object content = %q, want %q", got, content)
	}
}

// TestE2E_PresignExpiryValidation verifies the expiry range checks: below
// 1s and above 7 days are rejected client-side, before any request is made.
func TestE2E_PresignExpiryValidation(t *testing.T) {
	endpoint := endpointFor(t)
	st := clientFor(t, endpoint)
	ctx := context.Background()

	if _, err := st.PresignGetObject(ctx, "bucket", "key", 100*time.Millisecond); err == nil {
		t.Errorf("PresignGetObject(100ms): want error, got nil")
	}
	if _, err := st.PresignGetObject(ctx, "bucket", "key", 8*24*time.Hour); err == nil {
		t.Errorf("PresignGetObject(8 days): want error, got nil")
	}
	if _, err := st.PresignPutObject(ctx, "bucket", "key", -time.Second); err == nil {
		t.Errorf("PresignPutObject(-1s): want error, got nil")
	}
}
