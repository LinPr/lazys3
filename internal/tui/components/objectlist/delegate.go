package objectlist

import (
	"fmt"
	"io"
	"strings"

	"github.com/LinPr/lazys3/internal/strutil"
	"github.com/charmbracelet/bubbles/v2/list"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// selectionSet is the shared state used by the custom delegate to mark
// selected items visually. It is a *map[string]bool so the delegate (a
// value type) sees live mutations performed by Model.ToggleSelected /
// Model.ClearSelection.
type selectionSet = map[string]bool

// Column layout: marker | name (flex) | size | mtime | class. The
// right-hand columns are dropped as the list narrows — storage class
// first, then mtime, then size — so the name always keeps at least
// minNameWidth cells.
const (
	markerWidth  = 4  // "[x] "
	sizeWidth    = 8  // "123.4M" right-aligned
	mtimeWidth   = 16 // "2006-01-02 15:04"
	classWidth   = 3  // "STD"
	colGap       = 2
	minNameWidth = 12
)

// selectDelegate is a list.ItemDelegate that renders one aligned row per
// Object: a selection marker, the (truncated) key, and metadata columns.
// It mirrors bubbles' DefaultDelegate styling (including filter-match
// highlighting on the name) so the list look is preserved.
//
// The delegate is constructed with a pointer to the Model's selection map
// so it always reflects the latest state without the delegate being
// mutable itself.
type selectDelegate struct {
	styles   list.DefaultItemStyles
	selected *selectionSet
	height   int
	spacing  int
}

func newSelectDelegate(selected *selectionSet) selectDelegate {
	return selectDelegate{
		styles:   list.NewDefaultItemStyles(true),
		selected: selected,
		height:   1,
		spacing:  0,
	}
}

func (d selectDelegate) Height() int                             { return d.height }
func (d selectDelegate) Spacing() int                            { return d.spacing }
func (d selectDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

// markerFor returns the selection prefix for the given item title. The
// prefix is a fixed-width 4-cell column so the rest of the row aligns
// regardless of selection state.
func (d selectDelegate) markerFor(title string) string {
	if d.selected != nil && (*d.selected)[title] {
		return "[x] "
	}
	return "[ ] "
}

// shortStorageClass abbreviates an S3 storage class for the narrow class
// column.
func shortStorageClass(sc string) string {
	switch sc {
	case "":
		return ""
	case "STANDARD":
		return "STD"
	case "STANDARD_IA":
		return "IA"
	case "ONEZONE_IA":
		return "1Z"
	case "INTELLIGENT_TIERING":
		return "INT"
	case "GLACIER":
		return "GLA"
	case "GLACIER_IR":
		return "GIR"
	case "DEEP_ARCHIVE":
		return "DAR"
	case "REDUCED_REDUNDANCY":
		return "RR"
	default:
		return ansi.Truncate(sc, classWidth, "")
	}
}

// metaColumns renders the right-hand metadata columns for the object,
// given which columns fit. Directories show blank metadata.
func metaColumns(o Object, showSize, showMtime, showClass bool) string {
	var sb strings.Builder
	pad := func(s string, w int, right bool) {
		sb.WriteString(strings.Repeat(" ", colGap))
		if gap := w - lipgloss.Width(s); gap > 0 {
			if right {
				sb.WriteString(strings.Repeat(" ", gap))
				sb.WriteString(s)
				return
			}
			sb.WriteString(s)
			sb.WriteString(strings.Repeat(" ", gap))
			return
		}
		sb.WriteString(s)
	}
	if showSize {
		size := ""
		if !o.isDir {
			size = strutil.HumanizeBytes(o.size)
		}
		pad(size, sizeWidth, true)
	}
	if showMtime {
		mtime := ""
		if !o.isDir && !o.modTime.IsZero() {
			mtime = o.modTime.Format("2006-01-02 15:04")
		}
		pad(mtime, mtimeWidth, false)
	}
	if showClass {
		class := ""
		if !o.isDir {
			class = shortStorageClass(o.storageClass)
		}
		pad(class, classWidth, false)
	}
	return sb.String()
}

// Render prints one aligned row for the Object. Filter-match highlighting
// is applied to the name column only (offset past the selection marker);
// the selected/normal/dimmed row styling mirrors
// list.DefaultDelegate.Render.
func (d selectDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	obj, ok := item.(Object)
	if !ok {
		return
	}
	if m.Width() <= 0 {
		return
	}
	s := &d.styles
	textwidth := m.Width() - s.NormalTitle.GetPaddingLeft() - s.NormalTitle.GetPaddingRight()

	// Degrade gracefully at narrow widths: drop class, then mtime, then
	// size, always preserving minNameWidth for the name.
	avail := textwidth - markerWidth
	showSize := avail >= minNameWidth+colGap+sizeWidth
	showMtime := showSize && avail >= minNameWidth+2*colGap+sizeWidth+mtimeWidth
	showClass := showMtime && avail >= minNameWidth+3*colGap+sizeWidth+mtimeWidth+classWidth

	meta := metaColumns(obj, showSize, showMtime, showClass)
	nameWidth := textwidth - markerWidth - lipgloss.Width(meta)
	// The name column shows the key relative to the current prefix; the
	// filter matches (rune indexes into FilterValue == DisplayName) stay
	// aligned with it.
	name := ansi.Truncate(obj.DisplayName(), nameWidth, "…")
	namePad := ""
	if gap := nameWidth - lipgloss.Width(name); gap > 0 {
		namePad = strings.Repeat(" ", gap)
	}
	row := d.markerFor(obj.name) + name + namePad + meta

	var (
		isSelected  = index == m.Index()
		emptyFilter = m.FilterState() == list.Filtering && m.FilterValue() == ""
		isFiltered  = m.FilterState() == list.Filtering || m.FilterState() == list.FilterApplied
	)

	// Filter matches are rune indexes into the name; shift them past the
	// marker column and drop any beyond the truncated name.
	var matchedRunes []int
	if isFiltered && index < len(m.VisibleItems()) {
		nameRunes := len([]rune(name))
		for _, r := range m.MatchesForItem(index) {
			if r < nameRunes {
				matchedRunes = append(matchedRunes, r+markerWidth)
			}
		}
	}

	switch {
	case emptyFilter:
		row = s.DimmedTitle.Render(row)
	case isSelected && m.FilterState() != list.Filtering:
		if isFiltered {
			unmatched := s.SelectedTitle.Inline(true)
			matched := unmatched.Inherit(s.FilterMatch)
			row = lipgloss.StyleRunes(row, matchedRunes, matched, unmatched)
		}
		row = s.SelectedTitle.Render(row)
	default:
		if isFiltered {
			unmatched := s.NormalTitle.Inline(true)
			matched := unmatched.Inherit(s.FilterMatch)
			row = lipgloss.StyleRunes(row, matchedRunes, matched, unmatched)
		}
		row = s.NormalTitle.Render(row)
	}

	fmt.Fprintf(w, "%s", row) //nolint:errcheck
}
