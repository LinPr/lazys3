// Tests for the dual-pane directory-level recursive copies: each selected
// directory becomes one sync-engine transfer row (a one-way sync without
// --delete) while files keep the per-file upload/download rows.
package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LinPr/lazys3/internal/tui/components/locallist"
	"github.com/LinPr/lazys3/internal/tui/components/objectlist"
	"github.com/LinPr/lazys3/internal/tui/components/transferpanel"
	"github.com/LinPr/lazys3/internal/tui/state"
	"github.com/LinPr/lazys3/internal/tui/types"
)

// mkLocalSubdir creates a subdirectory in the local pane's directory and
// reloads the listing so the pane sees it.
func mkLocalSubdir(t *testing.T, m Model, dir, name string) Model {
	t.Helper()
	if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatal(err)
	}
	m = updateModel(t, m, locallist.FetchDirCmd(dir)())
	return m
}

// TestLocalDirSyncSpecs pins the sync src/dst strings a local directory
// selection produces (the narrow spec test — no modal, no cmd tree).
func TestLocalDirSyncSpecs(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	msg, ok := locallist.FetchDirCmd(dir)().(locallist.LoadedMsg)
	if !ok || msg.Err != nil {
		t.Fatalf("fetch dir: %+v", msg)
	}
	specs, skipped := localDirSyncs(msg.Entries, "bkt", "pre/")
	if len(specs) != 1 || len(skipped) != 0 {
		t.Fatalf("specs = %d (skipped %d), want 1 (0)", len(specs), len(skipped))
	}
	if want := filepath.Join(dir, "sub"); specs[0].src != want {
		t.Fatalf("src = %q, want %q", specs[0].src, want)
	}
	if want := "s3://bkt/pre/sub/"; specs[0].dst != want {
		t.Fatalf("dst = %q, want %q", specs[0].dst, want)
	}
	if want := "dir: sub/ -> s3://bkt/pre/sub/"; specs[0].label != want {
		t.Fatalf("label = %q, want %q", specs[0].label, want)
	}

	// Bucket root (no prefix) and a prefix missing its trailing slash.
	if root, _ := localDirSyncs(msg.Entries, "bkt", ""); root[0].dst != "s3://bkt/sub/" {
		t.Fatalf("root dst = %q, want s3://bkt/sub/", root[0].dst)
	}
	if noSlash, _ := localDirSyncs(msg.Entries, "bkt", "pre"); noSlash[0].dst != "s3://bkt/pre/sub/" {
		t.Fatalf("no-slash prefix dst = %q, want s3://bkt/pre/sub/", noSlash[0].dst)
	}
}

// TestRemoteDirSyncSpecs pins the sync src/dst strings a remote prefix
// selection produces.
func TestRemoteDirSyncSpecs(t *testing.T) {
	specs, skipped := remoteDirSyncs([]objectlist.Object{objectlist.NewDirObject("pre/d1/")}, "bkt", "/tmp/target")
	if len(specs) != 1 || len(skipped) != 0 {
		t.Fatalf("specs = %d (skipped %d), want 1 (0)", len(specs), len(skipped))
	}
	if want := "s3://bkt/pre/d1/"; specs[0].src != want {
		t.Fatalf("src = %q, want %q", specs[0].src, want)
	}
	if want := filepath.Join("/tmp/target", "d1"); specs[0].dst != want {
		t.Fatalf("dst = %q, want %q", specs[0].dst, want)
	}
	if want := "dir: d1/ -> /tmp/target/d1"; specs[0].label != want {
		t.Fatalf("label = %q, want %q", specs[0].label, want)
	}
}

// TestDirSyncSpecsRefuseWildcards pins that '?'/'*' anywhere in the s3
// side of a spec refuses the entry: storage.NewStorageURL would parse it
// as a glob and truncate the prefix, silently syncing the wrong keys.
func TestDirSyncSpecsRefuseWildcards(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a*b", "c?d", "plain"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	msg := locallist.FetchDirCmd(dir)().(locallist.LoadedMsg)
	specs, skipped := localDirSyncs(msg.Entries, "bkt", "pre/")
	if len(specs) != 1 || specs[0].dst != "s3://bkt/pre/plain/" {
		t.Fatalf("specs = %+v, want only plain/", specs)
	}
	if len(skipped) != 2 {
		t.Fatalf("skipped = %v, want a*b/ and c?d/", skipped)
	}
	// A wildcard in the destination prefix refuses every entry.
	specs, skipped = localDirSyncs(msg.Entries, "bkt", "pre*/")
	if len(specs) != 0 || len(skipped) != 3 {
		t.Fatalf("wildcard prefix: specs = %d skipped = %d, want 0/3", len(specs), len(skipped))
	}

	rspecs, rskipped := remoteDirSyncs([]objectlist.Object{
		objectlist.NewDirObject("pre/d*1/"),
		objectlist.NewDirObject("pre/ok/"),
	}, "bkt", "/tmp/target")
	if len(rspecs) != 1 || rspecs[0].src != "s3://bkt/pre/ok/" {
		t.Fatalf("remote specs = %+v, want only pre/ok/", rspecs)
	}
	if len(rskipped) != 1 || rskipped[0] != "d*1/" {
		t.Fatalf("remote skipped = %v, want [d*1/]", rskipped)
	}
}

// TestDualUploadWildcardDirOnlyErrors pins the flow-level behaviour: 'u'
// on a selection whose only folder has a wildcard name surfaces an error
// instead of opening a confirm modal for zero transfers.
func TestDualUploadWildcardDirOnlyErrors(t *testing.T) {
	m, dir := dualModel(t)
	m = mkLocalSubdir(t, m, dir, "a*b")
	m.state = state.ActiveObjectList
	m.selectedBucket = "bkt"
	m.selectedObject = "pre/"
	m = updateModel(t, m, tabPress())

	nm, cmd := m.Update(keyPress('u'))
	m = nm.(Model)
	if m.modal.IsVisible() {
		t.Fatal("'u' opened a confirm modal for a wildcard-named folder")
	}
	foundErr := false
	for _, msg := range collectMsgs(cmd) {
		if em, ok := msg.(types.ErrMsg); ok && strings.Contains(em.Err.Error(), "a*b/") {
			foundErr = true
		}
	}
	if !foundErr {
		t.Fatal("wildcard-only 'u' did not surface the skip error")
	}
}

// TestLocalDirSyncResolvesSymlink pins that a symlink to a directory syncs
// its target: the sync engine walks real directories only (opening the
// symlink path would fail with EISDIR).
func TestLocalDirSyncResolvesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	msg := locallist.FetchDirCmd(dir)().(locallist.LoadedMsg)
	var link []locallist.Entry
	for _, e := range msg.Entries {
		if e.Name() == "link/" {
			link = append(link, e)
		}
	}
	if len(link) != 1 || !link[0].IsSymlink() {
		t.Fatalf("link entry not found or not a symlink: %+v", link)
	}
	specs, skipped := localDirSyncs(link, "bkt", "pre/")
	if len(specs) != 1 || len(skipped) != 0 {
		t.Fatalf("specs = %d (skipped %d), want 1 (0)", len(specs), len(skipped))
	}
	// t.TempDir may itself sit behind a symlink (macOS): compare resolved.
	want, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if specs[0].src != want {
		t.Fatalf("src = %q, want the resolved target %q", specs[0].src, want)
	}
	if want := "s3://bkt/pre/link/"; specs[0].dst != want {
		t.Fatalf("dst = %q, want %q (keyed by the link name)", specs[0].dst, want)
	}
}

// TestDualUploadDirYieldsSyncRow pins the local-focus 'u' on a highlighted
// directory: folder wording in the confirm modal and one cancellable sync
// row labelled "dir: <name>/ -> s3://bucket/prefix/<name>/".
func TestDualUploadDirYieldsSyncRow(t *testing.T) {
	m, dir := dualModel(t)
	m = mkLocalSubdir(t, m, dir, "sub")
	m.state = state.ActiveObjectList
	m.selectedBucket = "bkt"
	m.selectedObject = "pre/"
	m = updateModel(t, m, tabPress())

	m = updateModel(t, m, keyPress('u'))
	if !m.modal.IsVisible() {
		t.Fatal("'u' on a local directory did not open the confirm modal")
	}
	if want := "Upload 1 folder(s) to s3://bkt/pre/?"; !strings.Contains(m.modal.Body(), want) {
		t.Fatalf("modal body = %q, want %q", m.modal.Body(), want)
	}
	nm, cmd := m.Update(keyPress('y'))
	m = nm.(Model)
	adds := transferAdds(cmd)
	if len(adds) != 1 {
		t.Fatalf("confirm produced %d transfer rows, want 1", len(adds))
	}
	tr := adds[0].Transfer
	if tr.Op != transferpanel.OpSync {
		t.Fatalf("transfer op = %q, want sync", tr.Op)
	}
	if want := "dir: sub/ -> s3://bkt/pre/sub/"; tr.Label != want {
		t.Fatalf("transfer label = %q, want %q", tr.Label, want)
	}
	if tr.Cancel == nil {
		t.Fatal("dir sync row carries no cancellable context")
	}
	tr.Cancel()
}

// TestDualDownloadDirYieldsSyncRow pins the remote-focus 'd' on a
// highlighted directory prefix: folder wording and one sync row into
// <localDir>/<name>.
func TestDualDownloadDirYieldsSyncRow(t *testing.T) {
	m, dir := dualModel(t, "existing.txt")
	m.state = state.ActiveObjectList
	m.selectedBucket = "bkt"
	m.selectedObject = "pre/"
	m.objectlist.SetObjects([]objectlist.Object{objectlist.NewDirObject("pre/d1/")})

	m = updateModel(t, m, keyPress('d'))
	if !m.modal.IsVisible() {
		t.Fatal("'d' on a remote directory did not open the confirm modal")
	}
	if want := "Download 1 folder(s) to " + dir + "?"; !strings.Contains(m.modal.Body(), want) {
		t.Fatalf("modal body = %q, want %q", m.modal.Body(), want)
	}
	nm, cmd := m.Update(keyPress('y'))
	m = nm.(Model)
	adds := transferAdds(cmd)
	if len(adds) != 1 {
		t.Fatalf("confirm produced %d transfer rows, want 1", len(adds))
	}
	tr := adds[0].Transfer
	if tr.Op != transferpanel.OpSync {
		t.Fatalf("transfer op = %q, want sync", tr.Op)
	}
	if want := "dir: d1/ -> " + filepath.Join(dir, "d1"); tr.Label != want {
		t.Fatalf("transfer label = %q, want %q", tr.Label, want)
	}
	tr.Cancel()
}

// TestDualUploadMixedSelection pins a files+folders selection with local
// focus: the confirm body counts both kinds and confirming enqueues one
// upload row per file plus one sync row per directory.
func TestDualUploadMixedSelection(t *testing.T) {
	m, dir := dualModel(t, "a.txt")
	m = mkLocalSubdir(t, m, dir, "sub")
	m.state = state.ActiveObjectList
	m.selectedBucket = "bkt"
	m.selectedObject = "pre/"
	m = updateModel(t, m, tabPress())
	m.localList.InvertSelection() // select sub/ and a.txt

	m = updateModel(t, m, keyPress('c'))
	if !m.modal.IsVisible() {
		t.Fatal("'c' on a mixed selection did not open the confirm modal")
	}
	if want := "Upload 1 file(s) and 1 folder(s) to s3://bkt/pre/?"; !strings.Contains(m.modal.Body(), want) {
		t.Fatalf("modal body = %q, want %q", m.modal.Body(), want)
	}
	nm, cmd := m.Update(keyPress('y'))
	m = nm.(Model)
	adds := transferAdds(cmd)
	if len(adds) != 2 {
		t.Fatalf("confirm produced %d transfer rows, want 2", len(adds))
	}
	byOp := map[transferpanel.Op]string{}
	for _, a := range adds {
		byOp[a.Transfer.Op] = a.Transfer.Label
		if a.Transfer.Cancel == nil {
			t.Fatalf("%q row carries no cancellable context", a.Transfer.Op)
		}
		a.Transfer.Cancel()
	}
	if want := filepath.Join(dir, "a.txt") + " -> s3://bkt/pre/a.txt"; byOp[transferpanel.OpUpload] != want {
		t.Fatalf("upload label = %q, want %q", byOp[transferpanel.OpUpload], want)
	}
	if want := "dir: sub/ -> s3://bkt/pre/sub/"; byOp[transferpanel.OpSync] != want {
		t.Fatalf("sync label = %q, want %q", byOp[transferpanel.OpSync], want)
	}
}

// TestDualDownloadMixedSelection is the remote mirror: one download row
// per file plus one sync row per directory prefix.
func TestDualDownloadMixedSelection(t *testing.T) {
	m, dir := dualModel(t, "existing.txt")
	m.state = state.ActiveObjectList
	m.selectedBucket = "bkt"
	m.objectlist.SetObjects([]objectlist.Object{
		objectlist.NewDirObject("d1/"),
		objectlist.NewFileObject("f.txt"),
	})
	m.objectlist.InvertSelection()

	m = updateModel(t, m, keyPress('d'))
	if want := "Download 1 file(s) and 1 folder(s) to " + dir + "?"; !strings.Contains(m.modal.Body(), want) {
		t.Fatalf("modal body = %q, want %q", m.modal.Body(), want)
	}
	nm, cmd := m.Update(keyPress('y'))
	m = nm.(Model)
	adds := transferAdds(cmd)
	if len(adds) != 2 {
		t.Fatalf("confirm produced %d transfer rows, want 2", len(adds))
	}
	byOp := map[transferpanel.Op]string{}
	for _, a := range adds {
		byOp[a.Transfer.Op] = a.Transfer.Label
		a.Transfer.Cancel()
	}
	if want := "s3://bkt/f.txt -> " + filepath.Join(dir, "f.txt"); byOp[transferpanel.OpDownload] != want {
		t.Fatalf("download label = %q, want %q", byOp[transferpanel.OpDownload], want)
	}
	if want := "dir: d1/ -> " + filepath.Join(dir, "d1"); byOp[transferpanel.OpSync] != want {
		t.Fatalf("sync label = %q, want %q", byOp[transferpanel.OpSync], want)
	}
}

// TestNonSuccessSyncStillRefreshesPanes pins that a partially-failed or
// cancelled sync refreshes the listings anyway: the engine keeps going
// past per-file errors and writes completed files atomically, so both
// panes may have changed even when the row ends failed/canceled.
func TestNonSuccessSyncStillRefreshesPanes(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"partial failure", errors.New("upload x: boom")},
		{"user cancel", context.Canceled},
	} {
		m, _ := dualModel(t)
		m.state = state.ActiveObjectList
		m.selectedBucket = "bkt"
		m = updateModel(t, m, transferpanel.TransferDoneMsg{
			ID:    "sync-refresh-test",
			Op:    transferpanel.OpSync,
			Label: "dir: sub/ -> s3://bkt/pre/sub/",
			Err:   tc.err,
		})
		if !m.objectlist.Loading() {
			t.Fatalf("%s: remote listing not re-fetched after a non-success sync", tc.name)
		}
	}
}

// TestDualUploadFilesOnlyNoSyncRows pins that a files-only selection keeps
// the plain wording and produces no sync rows.
func TestDualUploadFilesOnlyNoSyncRows(t *testing.T) {
	m, dir := dualModel(t, "a.txt")
	m.state = state.ActiveObjectList
	m.selectedBucket = "bkt"
	m.selectedObject = "pre/"
	m = updateModel(t, m, tabPress())

	m = updateModel(t, m, keyPress('u'))
	if want := "Upload 1 file(s) to s3://bkt/pre/?"; !strings.Contains(m.modal.Body(), want) {
		t.Fatalf("modal body = %q, want %q", m.modal.Body(), want)
	}
	if strings.Contains(m.modal.Body(), "folder") {
		t.Fatalf("files-only body mentions folders: %q", m.modal.Body())
	}
	nm, cmd := m.Update(keyPress('y'))
	m = nm.(Model)
	adds := transferAdds(cmd)
	if len(adds) != 1 {
		t.Fatalf("confirm produced %d transfer rows, want 1", len(adds))
	}
	if op := adds[0].Transfer.Op; op != transferpanel.OpUpload {
		t.Fatalf("transfer op = %q, want upload (no sync rows for files)", op)
	}
	if want := filepath.Join(dir, "a.txt") + " -> s3://bkt/pre/a.txt"; adds[0].Transfer.Label != want {
		t.Fatalf("upload label = %q, want %q", adds[0].Transfer.Label, want)
	}
	adds[0].Transfer.Cancel()
}
