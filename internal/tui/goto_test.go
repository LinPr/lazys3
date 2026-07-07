// Tests for the 'g' go-to-path flow (goto.go): the remote grammar, the
// bucket-switch navigation, the local pane resolution, and the routing
// precedence (overlays keep g=scroll, filter inputs keep typing).
package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/LinPr/lazys3/internal/tui/components/objectlist"
	"github.com/LinPr/lazys3/internal/tui/components/transferpanel"
	"github.com/LinPr/lazys3/internal/tui/state"
	"github.com/LinPr/lazys3/internal/tui/types"
)

// TestParseGotoTarget pins the remote grammar: s3:// URIs switch bucket,
// a leading '/' resolves from the bucket root, bare paths are relative to
// the current prefix, '..' clamps at the root, and every non-empty result
// is normalized to a '/'-terminated prefix.
func TestParseGotoTarget(t *testing.T) {
	cases := []struct {
		name, input, curBucket, curPrefix string
		bucket, prefix                    string
		wantErr                           bool
	}{
		{name: "uri bucket root", input: "s3://other", curBucket: "cur", bucket: "other", prefix: ""},
		{name: "uri with prefix", input: "s3://other/a/b", curBucket: "cur", bucket: "other", prefix: "a/b/"},
		{name: "uri trailing slash", input: "s3://other/a/b/", curBucket: "cur", bucket: "other", prefix: "a/b/"},
		{name: "uri dotdot", input: "s3://other/a/b/../c", curBucket: "cur", bucket: "other", prefix: "a/c/"},
		{name: "uri empty bucket", input: "s3://", curBucket: "cur", wantErr: true},
		{name: "root relative", input: "/x/y", curBucket: "cur", curPrefix: "a/b/", bucket: "cur", prefix: "x/y/"},
		{name: "root itself", input: "/", curBucket: "cur", curPrefix: "a/b/", bucket: "cur", prefix: ""},
		{name: "relative", input: "sub/dir", curBucket: "cur", curPrefix: "a/", bucket: "cur", prefix: "a/sub/dir/"},
		{name: "relative from root", input: "sub", curBucket: "cur", curPrefix: "", bucket: "cur", prefix: "sub/"},
		{name: "dotdot within prefix", input: "../c", curBucket: "cur", curPrefix: "a/b/", bucket: "cur", prefix: "a/c/"},
		{name: "dotdot clamped at root", input: "../../../up", curBucket: "cur", curPrefix: "a/", bucket: "cur", prefix: "up/"},
		{name: "dot and doubled slashes", input: "./x//y/.", curBucket: "cur", curPrefix: "", bucket: "cur", prefix: "x/y/"},
		{name: "spaces trimmed", input: "  s3://other/p  ", curBucket: "cur", bucket: "other", prefix: "p/"},
		{name: "bare path without bucket", input: "x/y", curBucket: "", wantErr: true},
		{name: "rooted path without bucket", input: "/x", curBucket: "", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bucket, prefix, err := parseGotoTarget(tc.input, tc.curBucket, tc.curPrefix)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseGotoTarget(%q) = %q/%q, want error", tc.input, bucket, prefix)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseGotoTarget(%q) error: %v", tc.input, err)
			}
			if bucket != tc.bucket || prefix != tc.prefix {
				t.Fatalf("parseGotoTarget(%q) = %q/%q, want %q/%q", tc.input, bucket, prefix, tc.bucket, tc.prefix)
			}
		})
	}
}

// gotoObjectListModel builds a model parked in the object list at
// s3://bkt/docs/ so the goto flow has a current location to resolve from.
func gotoObjectListModel(t *testing.T) Model {
	t.Helper()
	m := objectListModel(t)
	m.selectedBucket = "bkt"
	m.selectedObject = "docs/"
	return m
}

// TestGotoRemoteBucketSwitch pins the full remote flow: 'g' opens the modal
// with the current location prefilled, and confirming an s3:// URI switches
// bucket + prefix and rebuilds the fetch option for the new location.
func TestGotoRemoteBucketSwitch(t *testing.T) {
	m := gotoObjectListModel(t)

	m = updateModel(t, m, keyPress('g'))
	if !m.modal.IsVisible() || !strings.Contains(m.modal.Title(), "Go to") {
		t.Fatalf("'g' did not open the goto modal (title=%q)", m.modal.Title())
	}
	nm, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = nm.(Model)
	// Enter without typing submits the placeholder: the current location.
	var got *gotoRemoteMsg
	for _, msg := range collectMsgs(cmd) {
		if gm, ok := msg.(gotoRemoteMsg); ok {
			got = &gm
		}
	}
	if got == nil {
		t.Fatal("goto confirm emitted no gotoRemoteMsg")
	}
	if got.input != "s3://bkt/docs/" {
		t.Fatalf("placeholder submit = %q, want the current location", got.input)
	}

	// Dispatch a bucket-switching goto on the live model (the fetch cmd is
	// inspected via the option, never executed).
	nm, _ = m.Update(gotoRemoteMsg{input: "s3://other/pre/fix"})
	m = nm.(Model)
	if m.selectedBucket != "other" || m.selectedObject != "pre/fix/" {
		t.Fatalf("goto landed at %q/%q, want other/pre/fix/", m.selectedBucket, m.selectedObject)
	}
	if m.state != state.ActiveObjectList {
		t.Fatalf("state = %v, want ActiveObjectList", m.state)
	}
	if opt := m.objectListOptionFromState(); opt.S3Uri != "s3://other/pre/fix/" {
		t.Fatalf("fetch option S3Uri = %q, want s3://other/pre/fix/", opt.S3Uri)
	}
}

// TestGotoRemoteRelativeAndRoot pins the in-bucket resolutions on the live
// model: relative descends from the current prefix, '/' jumps to the root.
func TestGotoRemoteRelativeAndRoot(t *testing.T) {
	m := gotoObjectListModel(t)
	nm, _ := m.Update(gotoRemoteMsg{input: "sub/../deep/"})
	m = nm.(Model)
	if m.selectedBucket != "bkt" || m.selectedObject != "docs/deep/" {
		t.Fatalf("relative goto landed at %q/%q, want bkt/docs/deep/", m.selectedBucket, m.selectedObject)
	}
	nm, _ = m.Update(gotoRemoteMsg{input: "/"})
	m = nm.(Model)
	if m.selectedObject != "" {
		t.Fatalf("'/' goto landed at prefix %q, want bucket root", m.selectedObject)
	}
	if opt := m.objectListOptionFromState(); opt.S3Uri != "s3://bkt/" {
		t.Fatalf("fetch option S3Uri = %q, want s3://bkt/", opt.S3Uri)
	}
}

// TestGotoFromBucketListNeedsURI pins the bucket-list behaviour: a full
// s3:// URI jumps straight into the object list; a bare or rooted path only
// surfaces a status-bar hint — even when a previously-visited bucket is
// still parked in selectedBucket (handleBackward keeps it), so the input is
// never silently resolved against that stale bucket.
func TestGotoFromBucketListNeedsURI(t *testing.T) {
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.state = state.ActiveBucketList
	// Simulate having entered a bucket and backed out to the bucket list.
	m.selectedBucket = "stale-bucket"
	m.selectedObject = "old/prefix/"

	m = updateModel(t, m, keyPress('g'))
	if !m.modal.IsVisible() {
		t.Fatal("'g' in the bucket list did not open the goto modal")
	}
	if strings.Contains(m.modal.Title(), "relative") {
		t.Fatalf("bucket-list goto title advertises relative paths: %q", m.modal.Title())
	}
	m.modal.Hide()

	// Bare and rooted paths: an error lands on the status bar, nothing
	// navigates (in particular not into the stale bucket).
	for _, input := range []string{"some/prefix", "/some/prefix"} {
		nm, cmd := m.Update(gotoRemoteMsg{input: input})
		m = nm.(Model)
		foundErr := false
		for _, msg := range collectMsgs(cmd) {
			if em, ok := msg.(types.ErrMsg); ok && strings.Contains(em.Err.Error(), "s3://") {
				foundErr = true
			}
		}
		if !foundErr {
			t.Fatalf("goto %q from the bucket list produced no s3:// hint error", input)
		}
		if m.state != state.ActiveBucketList {
			t.Fatalf("goto %q: state = %v, want ActiveBucketList unchanged", input, m.state)
		}
		if m.selectedBucket != "stale-bucket" || m.selectedObject != "old/prefix/" {
			t.Fatalf("goto %q navigated to %q/%q", input, m.selectedBucket, m.selectedObject)
		}
	}

	// Full URI: jumps straight into the object list.
	nm, _ := m.Update(gotoRemoteMsg{input: "s3://jump/pre/"})
	m = nm.(Model)
	if m.state != state.ActiveObjectList || m.selectedBucket != "jump" || m.selectedObject != "pre/" {
		t.Fatalf("URI goto = state %v %q/%q, want object list jump/pre/", m.state, m.selectedBucket, m.selectedObject)
	}
}

// TestGotoProfileListHints pins that 'g' on the profile list only hints —
// there is nothing to jump within before a profile is opened.
func TestGotoProfileListHints(t *testing.T) {
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updateModel(t, m, keyPress('g'))
	if m.modal.IsVisible() {
		t.Fatal("'g' on the profile list opened a modal")
	}
	if !strings.Contains(m.statusBar.Info(), "open a bucket first") {
		t.Fatalf("status info = %q, want the open-a-bucket hint", m.statusBar.Info())
	}
}

// TestGotoLocalSuccessAndFailures pins the local flow: relative and ~ paths
// resolve and commit, while a nonexistent target or a file keeps the pane
// on its current directory with a status-bar error.
func TestGotoLocalSuccessAndFailures(t *testing.T) {
	m, dir := dualModel(t, "f.txt")
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	m = updateModel(t, m, tabPress()) // focus the local pane

	// Relative path: 'g', type "sub", enter — the pane commits to it.
	m = updateModel(t, m, keyPress('g'))
	if !m.modal.IsVisible() || m.modal.Title() != "Go to directory" {
		t.Fatalf("'g' with local focus modal title = %q", m.modal.Title())
	}
	m = typeInModal(t, m, "sub")
	m = pump(t, m, tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if m.localList.Dir() != sub {
		t.Fatalf("local dir after relative goto = %q, want %q", m.localList.Dir(), sub)
	}

	// Nonexistent target: error, pane unchanged.
	m = pump(t, m, gotoLocalMsg{path: filepath.Join(dir, "missing")})
	if m.localList.Dir() != sub {
		t.Fatalf("local dir after failed goto = %q, want unchanged %q", m.localList.Dir(), sub)
	}
	if !strings.Contains(m.statusBar.LastError(), "missing") {
		t.Fatalf("status error = %q, want the failed path", m.statusBar.LastError())
	}

	// A file is not a directory: error, pane unchanged.
	m = pump(t, m, gotoLocalMsg{path: filepath.Join(dir, "f.txt")})
	if m.localList.Dir() != sub {
		t.Fatalf("local dir after file goto = %q, want unchanged %q", m.localList.Dir(), sub)
	}

	// ~ expands to the (test-scoped) home directory.
	home := t.TempDir()
	t.Setenv("HOME", home)
	m = pump(t, m, gotoLocalMsg{path: "~"})
	if got := m.localList.Dir(); got != home {
		t.Fatalf("local dir after ~ goto = %q, want %q", got, home)
	}
}

// TestGotoKeyInsideOverlayStillScrolls pins the routing precedence: with
// the transfers overlay open, 'g'/'G' move its cursor and never open the
// goto modal.
func TestGotoKeyInsideOverlayStillScrolls(t *testing.T) {
	m := NewLazyS3Model()
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	for i := 0; i < 3; i++ {
		m = updateModel(t, m, transferpanel.TransferAddMsg{Transfer: transferpanel.Transfer{
			Op: transferpanel.OpDownload, Label: "s3://b/k", Status: transferpanel.StatusRunning,
		}})
	}
	m = updateModel(t, m, keyPress('t'))
	m = updateModel(t, m, tea.KeyPressMsg(tea.Key{Code: 'g', Mod: tea.ModShift}))
	if m.transferView.Cursor() != 2 {
		t.Fatalf("G inside the overlay moved the cursor to %d, want 2", m.transferView.Cursor())
	}
	m = updateModel(t, m, keyPress('g'))
	if m.transferView.Cursor() != 0 {
		t.Fatalf("g inside the overlay moved the cursor to %d, want 0", m.transferView.Cursor())
	}
	if m.modal.IsVisible() {
		t.Fatal("'g' inside the transfers overlay opened the goto modal")
	}
}

// TestGotoKeyWhileFilteringTypes pins that 'g' typed into a live filter
// input narrows the list instead of opening the goto modal.
func TestGotoKeyWhileFilteringTypes(t *testing.T) {
	m := gotoObjectListModel(t)
	m.objectlist.SetObjects([]objectlist.Object{
		objectlist.NewFileObject("gamma.txt"),
		objectlist.NewFileObject("alpha.txt"),
	})
	m = pump(t, m, keyPress('/'))
	if !m.objectlist.Filtering() {
		t.Fatal("'/' did not start filtering")
	}
	m = pump(t, m, keyPress('g'))
	if m.modal.IsVisible() {
		t.Fatal("'g' while filtering opened the goto modal")
	}
	if !m.objectlist.Filtering() {
		t.Fatal("'g' while filtering closed the filter input")
	}
	var names []string
	for _, o := range m.objectlist.VisibleObjects() {
		names = append(names, o.Name())
	}
	if strings.Join(names, ",") != "gamma.txt" {
		t.Fatalf("filter 'g' narrowed to %v, want [gamma.txt]", names)
	}
}
