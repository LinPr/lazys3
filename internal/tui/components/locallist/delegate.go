package locallist

import (
	"fmt"
	"io"
	"strings"

	"github.com/LinPr/lazys3/internal/strutil"
	"github.com/LinPr/lazys3/internal/tui/components/style"
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

// Column layout: marker | name (flex) | size | mtime. The right-hand
// columns are dropped as the list narrows — mtime first, then size — so
// the name always keeps at least minNameWidth cells. (Adapted from
// objectlist's delegate, minus the storage-class column.)
const (
	markerWidth  = 4  // "[x] "
	sizeWidth    = 8  // "123.4M" right-aligned
	mtimeWidth   = 16 // "2006-01-02 15:04"
	colGap       = 2
	minNameWidth = 12
)

// selectDelegate is a list.ItemDelegate that renders one aligned row per
// Entry: a selection marker, the (truncated) name, and metadata columns.
// It mirrors bubbles' DefaultDelegate styling (including filter-match
// highlighting on the name) so the list look is preserved.
type selectDelegate struct {
	styles   list.DefaultItemStyles
	selected *selectionSet
	height   int
	spacing  int
}

func newSelectDelegate(selected *selectionSet) selectDelegate {
	styles := list.NewDefaultItemStyles(true)
	// Theme override for the highlighted row (style.Apply runs before the
	// components are constructed).
	if c := style.SelectedItemFg; c != nil {
		styles.SelectedTitle = styles.SelectedTitle.Foreground(c).BorderForeground(c)
	}
	return selectDelegate{
		styles:   styles,
		selected: selected,
		height:   1,
		spacing:  0,
	}
}

// iconRuneIndexes returns the row-relative rune indexes of the icon glyph
// (which sits right after the 4-rune marker), for lipgloss.StyleRunes.
func iconRuneIndexes(icon string) []int {
	idx := make([]int, 0, len([]rune(icon)))
	for i := range []rune(icon) {
		idx = append(idx, markerWidth+i)
	}
	return idx
}

func (d selectDelegate) Height() int                             { return d.height }
func (d selectDelegate) Spacing() int                            { return d.spacing }
func (d selectDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

// markerFor returns the selection prefix for the given item name. The
// prefix is a fixed-width 4-cell column so the rest of the row aligns
// regardless of selection state.
func (d selectDelegate) markerFor(name string) string {
	if d.selected != nil && (*d.selected)[name] {
		return "[x] "
	}
	return "[ ] "
}

// metaColumns renders the right-hand metadata columns for the entry,
// given which columns fit. Directories show blank metadata.
func metaColumns(e Entry, showSize, showMtime bool) string {
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
		if !e.isDir {
			size = strutil.HumanizeBytes(e.size)
		}
		pad(size, sizeWidth, true)
	}
	if showMtime {
		mtime := ""
		if !e.isDir && !e.modTime.IsZero() {
			mtime = e.modTime.Format("2006-01-02 15:04")
		}
		pad(mtime, mtimeWidth, false)
	}
	return sb.String()
}

// Render prints one aligned row for the Entry. Filter-match highlighting
// is applied to the name column only (offset past the selection marker);
// the selected/normal/dimmed row styling mirrors
// list.DefaultDelegate.Render.
func (d selectDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	entry, ok := item.(Entry)
	if !ok {
		return
	}
	if m.Width() <= 0 {
		return
	}
	s := &d.styles
	textwidth := m.Width() - s.NormalTitle.GetPaddingLeft() - s.NormalTitle.GetPaddingRight()

	// Optional Nerd Font icon column between the marker and the name.
	// Budgeted with the measured glyph width + 1 space (some glyphs
	// measure 1 cell but terminals may render them wide, so the space
	// keeps the name readable either way). Empty glyph = column absent.
	icon, iconColor := style.IconFor(entry.name, entry.isDir, entry.isSymlink)
	iconCol := ""
	iconPad := 0
	if icon != "" {
		iconCol = icon + " "
		iconPad = ansi.StringWidth(icon) + 1
	}

	// Degrade gracefully at narrow widths: drop mtime, then size, always
	// preserving minNameWidth for the name.
	avail := textwidth - markerWidth - iconPad
	showSize := avail >= minNameWidth+colGap+sizeWidth
	showMtime := showSize && avail >= minNameWidth+2*colGap+sizeWidth+mtimeWidth

	meta := metaColumns(entry, showSize, showMtime)
	nameWidth := textwidth - markerWidth - iconPad - lipgloss.Width(meta)
	name := ansi.Truncate(entry.name, nameWidth, "…")
	namePad := ""
	if gap := nameWidth - lipgloss.Width(name); gap > 0 {
		namePad = strings.Repeat(" ", gap)
	}
	row := d.markerFor(entry.name) + iconCol + name + namePad + meta

	var (
		isSelected  = index == m.Index()
		emptyFilter = m.FilterState() == list.Filtering && m.FilterValue() == ""
		isFiltered  = m.FilterState() == list.Filtering || m.FilterState() == list.FilterApplied
	)

	// Filter matches are rune indexes into the name; shift them past the
	// marker column (and the icon column when present) and drop any beyond
	// the truncated name.
	var matchedRunes []int
	if isFiltered && index < len(m.VisibleItems()) {
		nameRunes := len([]rune(name))
		shift := markerWidth + len([]rune(iconCol))
		for _, r := range m.MatchesForItem(index) {
			if r < nameRunes {
				matchedRunes = append(matchedRunes, r+shift)
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
		} else if icon != "" && iconColor != nil {
			base := s.SelectedTitle.Inline(true)
			row = lipgloss.StyleRunes(row, iconRuneIndexes(icon), base.Foreground(iconColor), base)
		}
		row = s.SelectedTitle.Render(row)
	default:
		if isFiltered {
			unmatched := s.NormalTitle.Inline(true)
			matched := unmatched.Inherit(s.FilterMatch)
			row = lipgloss.StyleRunes(row, matchedRunes, matched, unmatched)
		} else if icon != "" && iconColor != nil {
			base := s.NormalTitle.Inline(true)
			row = lipgloss.StyleRunes(row, iconRuneIndexes(icon), base.Foreground(iconColor), base)
		}
		row = s.NormalTitle.Render(row)
	}

	fmt.Fprintf(w, "%s", row) //nolint:errcheck
}
