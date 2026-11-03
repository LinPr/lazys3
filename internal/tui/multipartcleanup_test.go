// Tests for the multipart-cleanup and presigned-upload flows: cancelled
// multipart parts are invisible to listings but billed, so 'P' must find and
// remove them, and 'U' must produce a URL a third party can PUT to.
package tui

import (
	"strings"
	"testing"

	s3store "github.com/LinPr/lazys3/internal/storage/s3"
	"github.com/LinPr/lazys3/internal/tui/components/transferpanel"
)

// TestAbortCancelledUploadNilTarget pins that non-upload transfers (a
// download, a copy, a sync) produce no cleanup work: only uploads can leave
// parts behind.
func TestAbortCancelledUploadNilTarget(t *testing.T) {
	m := searchModel(t)
	if cmd := m.abortCancelledUpload(nil); cmd != nil {
		t.Fatal("nil cleanup target should produce no cmd")
	}
	if cmd := m.abortCancelledUpload(&transferpanel.MultipartCleanup{}); cmd != nil {
		t.Fatal("cleanup target without a bucket should produce no cmd")
	}
}

// TestMultipartListEmptyPrefixHints pins the no-op path: with nothing in
// flight the user gets a clear note, not an empty confirm modal.
func TestMultipartListEmptyPrefixHints(t *testing.T) {
	m := searchModel(t)
	nm, cmd := m.Update(multipartListMsg{Bucket: "bkt", Prefix: "dir/"})
	m = nm.(Model)
	if m.modal.IsVisible() {
		t.Fatal("no uploads should not open a confirm modal")
	}
	if !strings.Contains(m.statusBar.Info(), "no interrupted multipart uploads") {
		t.Fatalf("status info = %q, want a no-uploads note", m.statusBar.Info())
	}
	if cmd != nil {
		if msgs := collectMsgs(cmd); len(msgs) > 1 {
			t.Fatalf("unexpected messages: %d", len(msgs))
		}
	}
}

// TestMultipartListOpensConfirm pins that found uploads are shown with their
// keys before anything is aborted: cleanup is destructive, so it must be
// confirmed, and the body must name what goes away.
func TestMultipartListOpensConfirm(t *testing.T) {
	m := searchModel(t)
	nm, _ := m.Update(multipartListMsg{
		Bucket: "bkt",
		Prefix: "dir/",
		Uploads: []s3store.MultipartUploadInfo{
			{Key: "dir/big.bin", UploadID: "u1", Initiated: "2026-01-01 00:00:00"},
			{Key: "dir/other.bin", UploadID: "u2", Initiated: "2026-01-02 00:00:00"},
		},
	})
	m = nm.(Model)
	if !m.modal.IsVisible() {
		t.Fatal("multipart list did not open a confirm modal")
	}
	body := m.modal.Body()
	for _, want := range []string{"2 interrupted", "dir/big.bin", "dir/other.bin", "billed as storage"} {
		if !strings.Contains(body, want) {
			t.Errorf("confirm body missing %q:\n%s", want, body)
		}
	}
}

// TestMultipartListTruncatesLongLists pins the display cap so a bucket with
// hundreds of stale uploads cannot produce an unusable modal.
func TestMultipartListTruncatesLongLists(t *testing.T) {
	m := searchModel(t)
	ups := make([]s3store.MultipartUploadInfo, 20)
	for i := range ups {
		ups[i] = s3store.MultipartUploadInfo{Key: "dir/" + strings.Repeat("x", i) + ".bin", UploadID: "u"}
	}
	nm, _ := m.Update(multipartListMsg{Bucket: "bkt", Prefix: "dir/", Uploads: ups})
	m = nm.(Model)
	if !strings.Contains(m.modal.Body(), "... and 12 more") {
		t.Errorf("long list should be truncated at 8 entries, body:\n%s", m.modal.Body())
	}
}

// TestPresignPutCmdLabelsMethod pins that an upload link is distinguishable
// from a download link in the result modal: handing someone a PUT URL when
// they expect a GET (or the reverse) silently breaks their use case.
func TestPresignPutCmdLabelsMethod(t *testing.T) {
	// The label is what the modal and status bar show; the message shape is
	// asserted by the objectlist package's own tests (it needs a live client
	// to sign).
	if presignMethodLabel("PUT") != "upload" {
		t.Errorf("label for PUT = %q, want upload", presignMethodLabel("PUT"))
	}
	if presignMethodLabel("") != "download" {
		t.Errorf("empty method should label as download")
	}
	if presignMethodLabel("GET") != "download" {
		t.Errorf("GET should label as download")
	}
}
