package bucketlist

import (
	"strings"
	"testing"
)

// TestCursorRowHighlighted pins the cursor-row highlight: the highlighted
// bucket carries the picker highlight foreground (style.CursorHighlightFg,
// #f3ec38 → SGR 38;2;243;236;56) plus the left-border indicator, and other
// rows do not. Regression: the color was authored as 8-digit hex
// ("#f3ec38ff"), which lipgloss.Color rejects — the row silently rendered
// with no highlight at all.
func TestCursorRowHighlighted(t *testing.T) {
	const cursorSGR = "38;2;243;236;56"
	m := NewModel()
	m.SetSize(60, 20)
	m.SetBuckets([]Bucket{NewBucket("alpha"), NewBucket("beta")})
	for _, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(line, "alpha") {
			if !strings.Contains(line, cursorSGR) {
				t.Errorf("cursor row should carry the highlight fg %s:\n%q", cursorSGR, line)
			}
			if !strings.Contains(line, "│") {
				t.Errorf("cursor row should carry the left-border indicator:\n%q", line)
			}
		}
		if strings.Contains(line, "beta") && strings.Contains(line, cursorSGR) {
			t.Errorf("non-cursor row must not carry the highlight fg:\n%q", line)
		}
	}
}

// TestSetBucketsPreservesAppliedFilterAndSelection pins the round-3F
// data-safety regression at the component level: replacing the items while
// a filter is applied must keep the filter narrowing the NEW listing and
// keep the selection on the same bucket NAME. Before the fix, bubbles'
// SetItems nil'ed the filtered snapshot and SetBuckets dropped the returned
// re-filter cmd, leaving zero visible items — the next filter-accept then
// silently reset the filter with the cursor on the first bucket.
func TestSetBucketsPreservesAppliedFilterAndSelection(t *testing.T) {
	const target = "lazys3-final-smoke-0706"
	m := NewModel()
	m.SetSize(60, 20)
	m.SetBuckets([]Bucket{NewBucket("damnlin"), NewBucket(target)})

	// Apply a filter that narrows to the target (SetFilterText runs the
	// filter synchronously and leaves it in the applied state).
	m.bucketlist.SetFilterText("final-smoke")
	if b := m.GetSelectedBucket(); b == nil || b.Name() != target {
		t.Fatalf("selected after filter = %v, want %s", b, target)
	}

	// A refresh (same pattern as the delayed post-make-bucket re-fetch)
	// replaces the items. Filter text, filtered narrowing, and selection
	// must all survive.
	m.SetBuckets([]Bucket{NewBucket("aaa-first"), NewBucket("damnlin"), NewBucket(target)})
	if got := m.bucketlist.FilterValue(); got != "final-smoke" {
		t.Fatalf("filter text after refresh = %q, want final-smoke", got)
	}
	if !m.bucketlist.IsFiltered() {
		t.Fatal("applied filter state lost across SetBuckets")
	}
	if n := len(m.bucketlist.VisibleItems()); n != 1 {
		t.Fatalf("visible items after refresh = %d, want 1 (filter must keep narrowing)", n)
	}
	if b := m.GetSelectedBucket(); b == nil || b.Name() != target {
		t.Fatalf("selected after refresh = %v, want %s", b, target)
	}
}

// TestSetBucketsRestoresCursorByName pins the unfiltered half: a refresh
// that inserts rows above the cursor must keep the selection on the same
// bucket name, not the same index.
func TestSetBucketsRestoresCursorByName(t *testing.T) {
	m := NewModel()
	m.SetSize(60, 20)
	m.SetBuckets([]Bucket{NewBucket("alpha"), NewBucket("beta"), NewBucket("gamma")})
	m.bucketlist.Select(2) // gamma
	if b := m.GetSelectedBucket(); b == nil || b.Name() != "gamma" {
		t.Fatalf("precondition: selected = %v, want gamma", b)
	}

	m.SetBuckets([]Bucket{NewBucket("alpha"), NewBucket("beta"), NewBucket("beta2"), NewBucket("gamma")})
	if b := m.GetSelectedBucket(); b == nil || b.Name() != "gamma" {
		t.Fatalf("selected after refresh = %v, want gamma (restore by name, not index)", b)
	}
}
