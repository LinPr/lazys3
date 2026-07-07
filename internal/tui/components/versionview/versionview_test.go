package versionview

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	s3store "github.com/LinPr/lazys3/internal/storage/s3"
	"github.com/LinPr/lazys3/internal/tui/components/objectlist"
)

func shown(bucket, key string) Model {
	m := NewModel()
	m.SetSize(80, 24)
	m.Show(objectlist.Option{}, bucket, key)
	return m
}

func loaded(m Model, versions []s3store.ObjectVersion, status string) Model {
	m, _ = m.Update(LoadedMsg{Seq: m.seq, Versions: versions, Status: status, StatusKnown: true})
	return m
}

func at(y, mo, d, h int) time.Time {
	return time.Date(y, time.Month(mo), d, h, 0, 0, 0, time.UTC)
}

func viewLines(t *testing.T, m Model) []string {
	t.Helper()
	v := m.View()
	if v == "" {
		t.Fatal("View() returned empty string for a visible overlay")
	}
	return strings.Split(v, "\n")
}

func TestViewFits80ColsWithLongAndCJKKeys(t *testing.T) {
	m := shown("存储桶", "中文前缀/非常长的中文对象名称测试用例文件名称重复重复重复/really-long-file-name-that-never-seems-to-end.tar.gz")
	m = loaded(m, []s3store.ObjectVersion{
		{Key: "k", VersionID: "CAEQmxjb3JlLWNoYWluLTAwMDAwMDAwMDAwMDAwMDA", Size: 123456789, LastModified: at(2026, 7, 6, 12), IsLatest: true},
		{Key: "k", VersionID: "3/L4kqtJlcpXroDTDmJ+rmSpXd3dIbrHY+MTRCxf3vjVBH40Nr8X8gdRQBpUMLUo", LastModified: at(2026, 7, 6, 11), IsDeleteMarker: true},
		{Key: "k", VersionID: "null", Size: 1, LastModified: at(2026, 7, 6, 10)},
	}, "Suspended")
	for i, line := range viewLines(t, m) {
		if w := ansi.StringWidth(line); w > 80 {
			t.Errorf("line %d width = %d, want <= 80: %q", i, w, line)
		}
	}
}

func TestViewNewestFirst(t *testing.T) {
	m := shown("bkt", "file.txt")
	// FetchCmd delivers newest first; the table must render in that order.
	m = loaded(m, []s3store.ObjectVersion{
		{Key: "file.txt", VersionID: "newest01", Size: 2, LastModified: at(2026, 7, 6, 12), IsLatest: true},
		{Key: "file.txt", VersionID: "oldest01", Size: 1, LastModified: at(2026, 7, 6, 11)},
	}, "Enabled")
	v := m.View()
	ni := strings.Index(v, "newest01")
	oi := strings.Index(v, "oldest01")
	if ni < 0 || oi < 0 {
		t.Fatalf("version ids missing from view:\n%s", v)
	}
	if ni > oi {
		t.Errorf("newest version rendered after oldest (newest at %d, oldest at %d)", ni, oi)
	}
}

func TestViewMarkersRender(t *testing.T) {
	m := shown("bkt", "file.txt")
	m = loaded(m, []s3store.ObjectVersion{
		{Key: "file.txt", VersionID: "marker01", LastModified: at(2026, 7, 6, 12), IsLatest: true, IsDeleteMarker: true},
		{Key: "file.txt", VersionID: "stored01", Size: 5, LastModified: at(2026, 7, 6, 11)},
	}, "Enabled")
	v := m.View()
	if !strings.Contains(v, "LATEST") {
		t.Errorf("view missing LATEST marker:\n%s", v)
	}
	if !strings.Contains(v, "DELETE-MARKER") {
		t.Errorf("view missing DELETE-MARKER marker:\n%s", v)
	}
}

// TestViewTimesRenderLocal pins the timezone fix: version LastModified (UTC
// from the SDK) renders as the local wall clock. The expectation is built
// with .Local() so the test is deterministic on any machine.
func TestViewTimesRenderLocal(t *testing.T) {
	utc := at(2026, 7, 6, 12)
	m := shown("bkt", "file.txt")
	m = loaded(m, []s3store.ObjectVersion{
		{Key: "file.txt", VersionID: "v1", Size: 2, LastModified: utc, IsLatest: true},
	}, "Enabled")
	v := ansi.Strip(m.View())
	if want := utc.Local().Format("2006-01-02 15:04"); !strings.Contains(v, want) {
		t.Errorf("view missing local-rendered time %q:\n%s", want, v)
	}
	if _, off := utc.Local().Zone(); off != 0 {
		if raw := utc.Format("2006-01-02 15:04"); strings.Contains(v, raw) {
			t.Errorf("view still renders raw UTC %q:\n%s", raw, v)
		}
	}
}

func TestViewLoadingState(t *testing.T) {
	m := shown("bkt", "file.txt")
	if v := m.View(); !strings.Contains(v, "loading versions") {
		t.Errorf("loading view missing loading line:\n%s", v)
	}
}

func TestViewErrorStateSurfacesInBody(t *testing.T) {
	m := shown("bkt", "file.txt")
	m, _ = m.Update(LoadedMsg{Seq: m.seq, Err: errors.New("api error NotImplemented: A header you provided implies functionality that is not implemented")})
	v := m.View()
	if !strings.Contains(v, "versions unavailable") || !strings.Contains(v, "NotImplemented") {
		t.Errorf("error view missing error body:\n%s", v)
	}
	if m.SelectedVersion() != nil {
		t.Error("SelectedVersion must be nil in the error state")
	}
}

func TestViewHintWhenVersioningNotEnabled(t *testing.T) {
	m := shown("bkt", "file.txt")
	m = loaded(m, []s3store.ObjectVersion{
		{Key: "file.txt", VersionID: "null", Size: 1, LastModified: at(2026, 7, 6, 12), IsLatest: true},
	}, "")
	if v := m.View(); !strings.Contains(v, "bucket versioning is not enabled") {
		t.Errorf("view missing disabled-versioning hint:\n%s", v)
	}

	// Enabled buckets get no hint; an unknown status (probe failed) gets
	// no guessed hint either.
	m2 := shown("bkt", "file.txt")
	m2 = loaded(m2, nil, "Enabled")
	if v := m2.View(); strings.Contains(v, "do not create versions") {
		t.Errorf("enabled bucket must not render the hint:\n%s", v)
	}
	m3 := shown("bkt", "file.txt")
	m3, _ = m3.Update(LoadedMsg{Seq: m3.seq, Versions: nil, Status: "", StatusKnown: false})
	if v := m3.View(); strings.Contains(v, "bucket versioning is") {
		t.Errorf("unknown status must not render a guessed hint:\n%s", v)
	}
}

func TestHandleKeyCursorClampsAndActions(t *testing.T) {
	m := shown("bkt", "file.txt")
	versions := []s3store.ObjectVersion{
		{Key: "file.txt", VersionID: "ver00001", Size: 3, LastModified: at(2026, 7, 6, 12), IsLatest: true},
		{Key: "file.txt", VersionID: "ver00002", Size: 2, LastModified: at(2026, 7, 6, 11)},
		{Key: "file.txt", VersionID: "ver00003", Size: 1, LastModified: at(2026, 7, 6, 10)},
	}
	m = loaded(m, versions, "Enabled")

	m.HandleKey("k")
	if got := m.SelectedVersion(); got == nil || got.VersionID != "ver00001" {
		t.Fatalf("cursor moved above the first row: %+v", got)
	}
	m.HandleKey("j")
	m.HandleKey("j")
	m.HandleKey("j")
	m.HandleKey("j")
	if got := m.SelectedVersion(); got == nil || got.VersionID != "ver00003" {
		t.Fatalf("cursor did not clamp to the last row: %+v", got)
	}

	cmd := m.HandleKey("R")
	if cmd == nil {
		t.Fatal("HandleKey(R) returned nil cmd on a valid row")
	}
	am, ok := cmd().(ActionMsg)
	if !ok {
		t.Fatalf("HandleKey(R) cmd produced %T, want ActionMsg", cmd())
	}
	if am.Kind != ActionRestore || am.Version.VersionID != "ver00003" || am.Bucket != "bkt" || am.Key != "file.txt" {
		t.Errorf("ActionMsg = %+v, want restore of ver00003 on bkt/file.txt", am)
	}

	// Unknown keys are swallowed; no cmd, cursor unchanged.
	if cmd := m.HandleKey("q"); cmd != nil {
		t.Error("HandleKey(q) must be swallowed (nil cmd)")
	}
}

func TestHandleKeyActionOnEmptyListIsNil(t *testing.T) {
	m := shown("bkt", "file.txt")
	m = loaded(m, nil, "Enabled")
	if cmd := m.HandleKey("d"); cmd != nil {
		t.Error("HandleKey(d) on an empty listing must return nil")
	}
}

// TestStaleLoadedMsgIsDropped pins the fetch-race guard: a listing from a
// superseded fetch (previous Show or Refresh) must never overwrite the
// current one, or d/R/D would pair the shown key with another key's rows.
func TestStaleLoadedMsgIsDropped(t *testing.T) {
	m := shown("bkt", "a.txt")
	staleSeq := m.seq
	m.Show(objectlist.Option{}, "bkt", "b.txt")

	m, _ = m.Update(LoadedMsg{Seq: staleSeq, StatusKnown: true, Status: "Enabled", Versions: []s3store.ObjectVersion{
		{Key: "a.txt", VersionID: "fromA001", Size: 1, LastModified: at(2026, 7, 6, 12), IsLatest: true},
	}})
	if !m.loading || len(m.versions) != 0 {
		t.Fatalf("stale LoadedMsg was applied: loading=%v versions=%+v", m.loading, m.versions)
	}

	m, _ = m.Update(LoadedMsg{Seq: m.seq, StatusKnown: true, Status: "Enabled", Versions: []s3store.ObjectVersion{
		{Key: "b.txt", VersionID: "fromB001", Size: 2, LastModified: at(2026, 7, 6, 13), IsLatest: true},
	}})
	if v := m.SelectedVersion(); v == nil || v.VersionID != "fromB001" {
		t.Fatalf("current LoadedMsg not applied: %+v", v)
	}

	// A stale reply arriving after Refresh (same key, older fetch) is
	// dropped too.
	m.Refresh()
	m, _ = m.Update(LoadedMsg{Seq: m.seq - 1, StatusKnown: true, Status: "Enabled"})
	if !m.loading {
		t.Fatal("stale LoadedMsg cleared the loading state after Refresh")
	}
}

func TestShortID(t *testing.T) {
	if got := ShortID(""); got != "null" {
		t.Errorf("ShortID(\"\") = %q, want null", got)
	}
	if got := ShortID("null"); got != "null" {
		t.Errorf("ShortID(null) = %q, want null", got)
	}
	if got := ShortID("abcd1234"); got != "abcd1234" {
		t.Errorf("ShortID(abcd1234) = %q, want unchanged", got)
	}
	if got := ShortID("abcd12345"); got != "abcd1234…" {
		t.Errorf("ShortID(abcd12345) = %q, want abcd1234…", got)
	}
}
