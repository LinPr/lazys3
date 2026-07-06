package historyview

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/LinPr/lazys3/internal/history"
)

func loaded(m Model, recs []history.Record) Model {
	m.Show()
	m, _ = m.Update(LoadedMsg{Records: recs})
	return m
}

func viewLines(t *testing.T, m Model) []string {
	t.Helper()
	v := m.View()
	if v == "" {
		t.Fatal("View() returned empty string for a visible overlay")
	}
	return strings.Split(v, "\n")
}

func TestViewFits80ColsWithLongAndCJKLabels(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 24)
	m = loaded(m, []history.Record{
		{
			Time:   "2026-07-06T12:34:56Z",
			Op:     "download",
			Status: "done",
			Bytes:  123456789,
			Label:  "s3://bucket/very/long/prefix/path/that/never/seems/to/end/really-long-file-name.tar.gz -> ./downloads/really-long-file-name.tar.gz",
			Note:   "3 file(s) done",
		},
		{
			Time:   "2026-07-06T12:00:00Z",
			Op:     "upload",
			Status: "failed",
			Bytes:  -1,
			Label:  "本地文件夹/非常长的中文对象名称测试用例文件名称重复重复重复.txt -> s3://存储桶/中文前缀/",
			Error:  "connection reset by peer while uploading part 3 of 12 to the endpoint",
		},
	})
	for i, line := range viewLines(t, m) {
		if w := ansi.StringWidth(line); w > 80 {
			t.Errorf("line %d width = %d, want <= 80: %q", i, w, line)
		}
	}
}

func TestViewEmptyHistoryShowsFriendlyLine(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 24)
	m = loaded(m, nil)
	if v := m.View(); !strings.Contains(v, "no transfers recorded yet") {
		t.Errorf("empty history view missing friendly line:\n%s", v)
	}
}

func TestViewNewestFirst(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 24)
	// Load delivers newest first; the table must render in that order.
	m = loaded(m, []history.Record{
		{Time: "2026-07-06T12:00:00Z", Op: "upload", Status: "done", Bytes: 1, Label: "newest-item"},
		{Time: "2026-07-06T11:00:00Z", Op: "download", Status: "done", Bytes: 1, Label: "oldest-item"},
	})
	v := m.View()
	ni := strings.Index(v, "newest-item")
	oi := strings.Index(v, "oldest-item")
	if ni < 0 || oi < 0 {
		t.Fatalf("labels missing from view:\n%s", v)
	}
	if ni > oi {
		t.Errorf("newest record rendered after oldest (newest at %d, oldest at %d)", ni, oi)
	}
}

func TestViewLoadingState(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 24)
	m.Show()
	if v := m.View(); !strings.Contains(v, "loading history") {
		t.Errorf("loading view missing loading line:\n%s", v)
	}
}

func TestHandleKeyScrollClamps(t *testing.T) {
	m := NewModel()
	m.SetSize(80, 10) // page = 10 - 2 (frame) - 3 = 5
	recs := make([]history.Record, 12)
	for i := range recs {
		recs[i] = history.Record{Time: "2026-07-06T12:00:00Z", Op: "download", Status: "done", Label: "x"}
	}
	m = loaded(m, recs)
	m.HandleKey("k")
	if m.offset != 0 {
		t.Errorf("offset after k at top = %d, want 0", m.offset)
	}
	for i := 0; i < 100; i++ {
		m.HandleKey("j")
	}
	if want := len(recs) - m.pageSize(); m.offset != want {
		t.Errorf("offset after 100 j = %d, want %d", m.offset, want)
	}
	m.HandleKey("pgup")
	m.HandleKey("pgup")
	m.HandleKey("pgup")
	if m.offset != 0 {
		t.Errorf("offset after pgups = %d, want 0", m.offset)
	}
}
