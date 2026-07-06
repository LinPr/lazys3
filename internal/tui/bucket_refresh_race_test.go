package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea/v2"

	"github.com/LinPr/lazys3/internal/tui/components/bucketlist"
	"github.com/LinPr/lazys3/internal/tui/components/locallist"
	"github.com/LinPr/lazys3/internal/tui/components/transferpanel"
	"github.com/LinPr/lazys3/internal/tui/state"
)

// bucketListResult builds the message a (possibly delayed) bucket-list
// fetch delivers, without any network.
func bucketListResult(names ...string) bucketlist.FetchBucketListResultMsg {
	bs := make([]bucketlist.Bucket, 0, len(names))
	for _, n := range names {
		bs = append(bs, bucketlist.NewBucket(n))
	}
	return bucketlist.FetchBucketListResultMsg{Buckets: bs}
}

// TestDelayedBucketRefreshMidFilterKeepsSelection reproduces the round-3F
// live failure (dual-pane upload landing in the wrong bucket): the delayed
// post-make-bucket FetchBucketListResultMsg lands in the middle of the
// user's '/'-filter interaction, between the last pattern keystroke and
// the accept-enter. Before the fix, bucketlist.SetBuckets dropped bubbles'
// re-filter cmd, so the accept-enter saw zero visible items and silently
// RESET the filter with the cursor on the first bucket ("damnlin"); the
// select-enter then wrote m.selectedBucket = "damnlin" and the dual-pane
// upload captured it as the destination. The filter, the selection, and
// the captured upload destination must all survive the refresh.
func TestDelayedBucketRefreshMidFilterKeepsSelection(t *testing.T) {
	const target = "lazys3-final-smoke-0706"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "final.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewLazyS3Model()
	m = pump(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.state = state.ActiveBucketList
	m.selectedProfile = "oss"
	m = pump(t, m, bucketListResult("damnlin", target))

	// '/' + pattern narrows the bucket list to the target.
	m = pump(t, m, keyPress('/'))
	if !m.bucketList.Filtering() {
		t.Fatal("'/' did not focus the bucket filter input")
	}
	for _, r := range "final-smoke" {
		m = pump(t, m, keyPress(r))
	}

	// The delayed make-bucket refresh lands NOW, between the last filter
	// keystroke and the accept-enter (the exact live-session window).
	m = pump(t, m, bucketListResult("damnlin", target))

	m = pump(t, m, enterPress()) // accept the filter
	m = pump(t, m, enterPress()) // select the highlighted bucket

	if m.selectedBucket != target {
		t.Fatalf("selectedBucket after refresh-race select = %q, want %q", m.selectedBucket, target)
	}
	if m.state != state.ActiveObjectList {
		t.Fatalf("state after select = %v, want ActiveObjectList", m.state)
	}

	// Dual-pane upload path: 'l' opens dual mode with the local pane
	// focused; commit the temp dir as its listing, then 'u' prompts the
	// upload. The confirm body and the actual transfer rows must target
	// the bucket the user filtered to — never "damnlin".
	m = pump(t, m, keyPress('l'))
	if !m.dualPane || !m.localFocused() {
		t.Fatal("'l' did not enter dual-pane mode with local focus")
	}
	m = pump(t, m, locallist.FetchDirCmd(dir)())

	nm, _ := m.Update(keyPress('u'))
	m = nm.(Model)
	if !m.modal.IsVisible() {
		t.Fatal("'u' did not open the upload confirm modal")
	}
	if !strings.Contains(m.modal.Body(), "s3://"+target+"/") {
		t.Fatalf("upload confirm body = %q, want destination s3://%s/", m.modal.Body(), target)
	}

	// Confirm ('y') and inspect the captured destination on the actual
	// transfer rows (transferAdds never runs the network op).
	nm2, cmd := m.Update(keyPress('y'))
	m = nm2.(Model)
	adds := transferAdds(cmd)
	if len(adds) != 1 {
		t.Fatalf("confirm produced %d transfer rows, want 1", len(adds))
	}
	if got := adds[0].Transfer.Label; !strings.Contains(got, "s3://"+target+"/final.txt") {
		t.Fatalf("upload row label = %q, want destination s3://%s/final.txt", got, target)
	}
}

// TestBucketSelectRefusedWhileRefreshInFlight pins the other timeline of
// the same live failure: the user filters for the just-created bucket
// while the post-make-bucket re-fetch is still in flight, so the pattern
// matches nothing in the STALE listing; bubbles' accept-enter silently
// clears the filter (cursor on "damnlin") and the select-enter would open
// — and route uploads into — a bucket the user never picked. The select
// must be refused until the listing is current, and work normally after
// the refresh lands.
func TestBucketSelectRefusedWhileRefreshInFlight(t *testing.T) {
	const target = "lazys3-final-smoke-0706"
	m := NewLazyS3Model()
	m = pump(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.state = state.ActiveBucketList
	m.selectedProfile = "oss"
	// Pre-make-bucket listing: the target does not exist yet.
	m = pump(t, m, bucketListResult("damnlin"))

	// make-bucket completed: refreshAfterOp marks the listing loading and
	// dispatches the slow re-fetch (deliberately not executed — in flight).
	_ = m.refreshAfterOp(transferpanel.TransferDoneMsg{Op: transferpanel.OpMakeBucket})
	if !m.bucketList.Loading() {
		t.Fatal("refreshAfterOp(OpMakeBucket) did not mark the bucket list loading")
	}

	m = pump(t, m, keyPress('/'))
	for _, r := range "final-smoke" {
		m = pump(t, m, keyPress(r))
	}
	m = pump(t, m, enterPress()) // accept: zero matches -> bubbles resets the filter
	m = pump(t, m, enterPress()) // select: must be refused mid-refresh

	if m.selectedBucket != "" {
		t.Fatalf("selectedBucket = %q, want select refused while the listing refreshes", m.selectedBucket)
	}
	if m.state != state.ActiveBucketList {
		t.Fatalf("state = %v, want still ActiveBucketList while the listing refreshes", m.state)
	}

	// The delayed refresh lands: filtering and selecting now picks the
	// target as usual.
	m = pump(t, m, bucketListResult("damnlin", target))
	if m.bucketList.Loading() {
		t.Fatal("refresh result did not clear the loading state")
	}
	m = pump(t, m, keyPress('/'))
	for _, r := range "final-smoke" {
		m = pump(t, m, keyPress(r))
	}
	m = pump(t, m, enterPress())
	m = pump(t, m, enterPress())
	if m.selectedBucket != target {
		t.Fatalf("selectedBucket after refresh = %q, want %q", m.selectedBucket, target)
	}
}

// TestBucketRefreshResultRoutedWhileObjectListActive pins the routing half
// of the fix: a delayed FetchBucketListResultMsg arriving after the user
// already entered a bucket (state == ActiveObjectList) must still reach
// the bucket list. Before the fix, the state dispatch fed it to the object
// list, silently dropping it — the bucket list stayed stale and marked
// loading forever, and the freshly created bucket never appeared.
func TestBucketRefreshResultRoutedWhileObjectListActive(t *testing.T) {
	const target = "lazys3-final-smoke-0706"
	m := NewLazyS3Model()
	m = pump(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.state = state.ActiveObjectList
	m.selectedBucket = target
	m.bucketList.SetLoading(true)

	m = pump(t, m, bucketListResult("damnlin", target))

	if m.bucketList.Loading() {
		t.Fatal("FetchBucketListResultMsg dropped while the object list is active: bucket list stuck loading")
	}
	if m.selectedBucket != target {
		t.Fatalf("selectedBucket = %q, want %q (refresh must not touch it)", m.selectedBucket, target)
	}
}
